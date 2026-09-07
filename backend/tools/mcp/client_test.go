package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 工具名要规整成模型能接受的字符集:OpenAI 要求 ^[a-zA-Z0-9_-]+$,
// 而 MCP 那边点、斜杠、中文什么都可能出现
func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"filesystem":      "filesystem",
		"read_file":       "read_file",
		"GitHub":          "github",
		"my.server/tools": "my_server_tools",
		"文件系统":            "____",
		"":                "server",
	}
	for in, want := range cases {
		if got := sanitizeName(in); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// 无参工具的服务器经常干脆不给 inputSchema,但各家大模型都要求这里有个合法 object
func TestNormalizeSchema(t *testing.T) {
	got := normalizeSchema(nil)
	if got["type"] != "object" {
		t.Errorf("空 schema 应该补成 object: %#v", got)
	}
	got = normalizeSchema(map[string]any{"properties": map[string]any{}})
	if got["type"] != "object" {
		t.Errorf("缺 type 要补上: %#v", got)
	}
	got = normalizeSchema(map[string]any{"type": "object", "required": []any{"a"}})
	if got["required"] == nil {
		t.Error("已有内容不该被抹掉")
	}
}

// 非文本块要退化成一句说明,而不是静默丢掉 ——
// 模型知道"这里有东西但拿不到",好过以为工具返回了空
func TestFlattenContent(t *testing.T) {
	type block = struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	got := flattenContent([]block{{Type: "text", Text: "第一段"}, {Type: "text", Text: "第二段"}})
	if got != "第一段\n第二段" {
		t.Errorf("got %q", got)
	}
	got = flattenContent([]block{{Type: "image"}})
	if !strings.Contains(got, "image") || !strings.Contains(got, "不支持") {
		t.Errorf("非文本块要有说明: %q", got)
	}
	if flattenContent(nil) != "" {
		t.Error("空内容应该返回空串")
	}
}

// mcpTestServer 起一个最小的 Streamable HTTP MCP 服务器
func mcpTestServer(t *testing.T, handler func(method string, params json.RawMessage) (any, *rpcError)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ID == nil { // 通知,不用回
			w.WriteHeader(http.StatusAccepted)
			return
		}
		result, rpcErr := handler(req.Method, req.Params)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "test-session")
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			resp["error"] = rpcErr
		} else {
			resp["result"] = result
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestHTTPHandshakeAndToolList(t *testing.T) {
	srv := mcpTestServer(t, func(method string, _ json.RawMessage) (any, *rpcError) {
		switch method {
		case "initialize":
			return map[string]any{
				"protocolVersion": protocolVersion,
				"serverInfo":      map[string]any{"name": "demo", "version": "1.2"},
			}, nil
		case "tools/list":
			return map[string]any{"tools": []any{
				map[string]any{"name": "read.file", "description": "读文件"},
			}}, nil
		}
		return nil, &rpcError{Code: -32601, Message: "unknown method"}
	})
	defer srv.Close()

	cfg := Server{ID: "s1", Name: "My Files", Kind: TransportHTTP, URL: srv.URL}
	c, err := connect(context.Background(), cfg)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer c.close()

	if c.serverInfo != "demo 1.2" {
		t.Errorf("serverInfo = %q", c.serverInfo)
	}
	tools, err := c.listTools(context.Background(), cfg)
	if err != nil {
		t.Fatalf("拉工具失败: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("got %#v", tools)
	}
	// 名字要带服务器前缀,且两段都规整过
	if tools[0].QualifiedName != "my_files_read_file" {
		t.Errorf("QualifiedName = %q", tools[0].QualifiedName)
	}
	if tools[0].Name != "read.file" {
		t.Errorf("调用时要用的原名不能被改: %q", tools[0].Name)
	}
	if tools[0].InputSchema["type"] != "object" {
		t.Errorf("缺失的 schema 要补成 object: %#v", tools[0].InputSchema)
	}
}

// isError 是"工具自己失败了",内容要原样交给模型,不能变成 Go error 把整轮掐掉
func TestCallToolIsErrorReturnsText(t *testing.T) {
	srv := mcpTestServer(t, func(method string, _ json.RawMessage) (any, *rpcError) {
		switch method {
		case "initialize":
			return map[string]any{"serverInfo": map[string]any{"name": "demo"}}, nil
		case "tools/call":
			return map[string]any{
				"isError": true,
				"content": []any{map[string]any{"type": "text", "text": "文件不存在"}},
			}, nil
		}
		return nil, &rpcError{Message: "unknown"}
	})
	defer srv.Close()

	c, err := connect(context.Background(), Server{Name: "x", Kind: TransportHTTP, URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()

	out, err := c.callTool(context.Background(), "read", nil)
	if err != nil {
		t.Fatalf("工具自身失败不该变成 Go error: %v", err)
	}
	if !strings.Contains(out, "文件不存在") {
		t.Errorf("失败原因要回传给模型: %q", out)
	}
}

// 协议级错误(方法不存在之类)才是真的 error
func TestCallToolProtocolErrorIsError(t *testing.T) {
	srv := mcpTestServer(t, func(method string, _ json.RawMessage) (any, *rpcError) {
		if method == "initialize" {
			return map[string]any{"serverInfo": map[string]any{"name": "demo"}}, nil
		}
		return nil, &rpcError{Code: -32601, Message: "Method not found"}
	})
	defer srv.Close()

	c, err := connect(context.Background(), Server{Name: "x", Kind: TransportHTTP, URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()

	if _, err := c.callTool(context.Background(), "nope", nil); err == nil {
		t.Error("协议错误应该往上抛")
	}
}

// 服务器用 SSE 回响应时,要能从流里挑出 id 对得上的那条(中间可能夹着进度通知)
func TestHTTPReadsSSEResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// 先来一条不相干的通知,再来真正的响应
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n"))
		body := map[string]any{"jsonrpc": "2.0", "id": *req.ID,
			"result": map[string]any{"serverInfo": map[string]any{"name": "sse-demo"}}}
		raw, _ := json.Marshal(body)
		_, _ = w.Write([]byte("data: " + string(raw) + "\n\n"))
	}))
	defer srv.Close()

	c, err := connect(context.Background(), Server{Name: "x", Kind: TransportHTTP, URL: srv.URL})
	if err != nil {
		t.Fatalf("SSE 响应应该能正常解析: %v", err)
	}
	defer c.close()
	if c.serverInfo != "sse-demo" {
		t.Errorf("serverInfo = %q", c.serverInfo)
	}
}

func TestHTTPTransportRejectsBadURL(t *testing.T) {
	for _, url := range []string{"", "  ", "ftp://x", "example.com"} {
		if _, err := newHTTPTransport(Server{URL: url}); err == nil {
			t.Errorf("url=%q 应该被拒", url)
		}
	}
}

func TestStdioRejectsEmptyCommand(t *testing.T) {
	if _, err := newStdioTransport(context.Background(), Server{Kind: TransportStdio}); err == nil {
		t.Error("没有命令应该报错")
	}
}

// stderr 只留最后一段:服务器可能一直刷日志,但报错时只需要最近那点
func TestRingBufferKeepsTail(t *testing.T) {
	b := &ringBuffer{limit: 10}
	_, _ = b.Write([]byte("0123456789"))
	_, _ = b.Write([]byte("ABCDE"))
	if !strings.Contains(b.suffix(), "56789ABCDE") {
		t.Errorf("应该只留最后 10 字节: %q", b.suffix())
	}
	empty := &ringBuffer{limit: 10}
	if empty.suffix() != "" {
		t.Error("没有输出时不该拼出多余的段落")
	}
}
