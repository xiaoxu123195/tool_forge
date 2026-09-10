package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeTool 一个最小的工具,回声输入
type fakeTool struct {
	name   string
	fail   bool
	schema map[string]any
}

func (f *fakeTool) Name() string        { return f.name }
func (f *fakeTool) Title() string       { return "假工具" }
func (f *fakeTool) Description() string { return "测试用" }
func (f *fakeTool) Methods() []string   { return []string{http.MethodPost} }
func (f *fakeTool) Handle(_ context.Context, body []byte) ([]byte, error) {
	if f.fail {
		return nil, errors.New("设备没连上")
	}
	return []byte(`{"echo":` + string(body) + `}`), nil
}
func (f *fakeTool) InputSchema() map[string]any { return f.schema }

// fakeStream 一个流式工具,吐两条事件
type fakeStream struct{ fakeTool }

func (f *fakeStream) HandleStream(_ context.Context, _ []byte, emit func(StreamEvent) error) error {
	_ = emit(StreamEvent{Type: "log", Data: "第一步"})
	_ = emit(StreamEvent{Type: "log", Data: "第二步"})
	if f.fail {
		return errors.New("跑到一半挂了")
	}
	return nil
}

// rpc 发一条 JSON-RPC 消息,返回解好的响应和 HTTP 状态码
func rpc(t *testing.T, s *Server, body string) (jsonRPCResponse, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleMCP(rec, req)
	var resp jsonRPCResponse
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("响应不是合法 JSON: %v\n%s", err, rec.Body.String())
		}
	}
	return resp, rec.Code
}

func newTestServer(tools ...ToolHandler) *Server {
	s := New()
	s.cfg.EnabledTools = map[string]bool{}
	for _, h := range tools {
		s.Register(h)
		s.cfg.EnabledTools[h.Name()] = true
	}
	return s
}

func TestMCPInitialize(t *testing.T) {
	s := newTestServer()
	resp, _ := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if resp.Error != nil {
		t.Fatalf("initialize 不该出错: %+v", resp.Error)
	}
	m, _ := resp.Result.(map[string]any)
	if m["protocolVersion"] == "" || m["protocolVersion"] == nil {
		t.Error("没有返回 protocolVersion")
	}
	caps, _ := m["capabilities"].(map[string]any)
	if _, ok := caps["tools"]; !ok {
		t.Error("capabilities 里应该声明 tools")
	}
	// 没实现的能力不能声明:声明了客户端就会来问,然后一路 method not found
	for _, unsupported := range []string{"resources", "prompts"} {
		if _, ok := caps[unsupported]; ok {
			t.Errorf("不该声明没实现的 %s 能力", unsupported)
		}
	}
	// id 要原样回去
	if string(resp.ID) != "1" {
		t.Errorf("id 没原样返回: %s", resp.ID)
	}
}

// 通知(没有 id)不能回响应体 —— 那是明确违反规范的,有客户端会因此断开
func TestMCPNotificationHasNoBody(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, mcpPath,
		strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	rec := httptest.NewRecorder()
	s.handleMCP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Errorf("通知应该回 202,得到 %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("通知不该有响应体,得到: %s", rec.Body.String())
	}
}

func TestMCPToolsListOnlyEnabled(t *testing.T) {
	on := &fakeTool{name: "on-tool", schema: map[string]any{"type": "object"}}
	off := &fakeTool{name: "off-tool"}
	s := newTestServer(on)
	s.Register(off) // 注册了但没启用

	resp, _ := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	m, _ := resp.Result.(map[string]any)
	tools, _ := m["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("只该列出启用的那一个,得到 %d 个", len(tools))
	}
	first, _ := tools[0].(map[string]any)
	if first["name"] != "on-tool" {
		t.Errorf("列错了工具: %v", first["name"])
	}
	// 每个工具都必须有 inputSchema:MCP 客户端拿它决定传什么,缺了就没法调
	if first["inputSchema"] == nil {
		t.Error("工具缺少 inputSchema")
	}
}

// 没实现 SchemaProvider 的工具也要有 schema,给一个宽松的兜底
func TestMCPFallbackSchema(t *testing.T) {
	// 匿名结构体只实现 ToolHandler,不实现 SchemaProvider
	s := newTestServer(&plainTool{})
	resp, _ := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	m, _ := resp.Result.(map[string]any)
	tools, _ := m["tools"].([]any)
	first, _ := tools[0].(map[string]any)
	schema, _ := first["inputSchema"].(map[string]any)
	if schema["type"] != "object" {
		t.Errorf("兜底 schema 应该是 object: %v", schema)
	}
	// 兜底必须允许任意字段。说成"无参数"的话 agent 会真的什么都不传
	if schema["additionalProperties"] != true {
		t.Errorf("兜底 schema 应该允许任意字段: %v", schema)
	}
}

type plainTool struct{}

func (plainTool) Name() string        { return "plain" }
func (plainTool) Title() string       { return "朴素工具" }
func (plainTool) Description() string { return "没有 schema" }
func (plainTool) Methods() []string   { return []string{http.MethodPost} }
func (plainTool) Handle(context.Context, []byte) ([]byte, error) {
	return []byte(`{}`), nil
}

func TestMCPCallTool(t *testing.T) {
	s := newTestServer(&fakeTool{name: "echo"})
	resp, _ := rpc(t, s,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"echo","arguments":{"a":1}}}`)
	if resp.Error != nil {
		t.Fatalf("不该是 JSON-RPC 错误: %+v", resp.Error)
	}
	text := mcpText(t, resp)
	if !strings.Contains(text, `"a":1`) {
		t.Errorf("工具没收到参数: %s", text)
	}
}

// 工具自身失败要走 isError,不能走 JSON-RPC error ——
// 前者 agent 读得到原文能改参数重试,后者多数客户端直接把整轮中断
func TestMCPToolFailureIsIsError(t *testing.T) {
	s := newTestServer(&fakeTool{name: "boom", fail: true})
	resp, _ := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"boom"}}`)
	if resp.Error != nil {
		t.Fatalf("工具执行失败不该变成 JSON-RPC error: %+v", resp.Error)
	}
	m, _ := resp.Result.(map[string]any)
	if m["isError"] != true {
		t.Error("应该标成 isError")
	}
	if !strings.Contains(mcpText(t, resp), "设备没连上") {
		t.Error("错误原文要带给 agent")
	}
}

func TestMCPStreamToolCollectsEvents(t *testing.T) {
	s := newTestServer(&fakeStream{fakeTool{name: "stream"}})
	resp, _ := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"stream"}}`)
	text := mcpText(t, resp)
	if !strings.Contains(text, "第一步") || !strings.Contains(text, "第二步") {
		t.Errorf("流式事件应该被收齐返回: %s", text)
	}
}

// 跑到一半失败:已经产生的输出也要给出去,不然排查时什么线索都没有
func TestMCPStreamToolPartialOutputOnError(t *testing.T) {
	s := newTestServer(&fakeStream{fakeTool{name: "stream", fail: true}})
	resp, _ := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"stream"}}`)
	m, _ := resp.Result.(map[string]any)
	if m["isError"] != true {
		t.Error("应该标成 isError")
	}
	text := mcpText(t, resp)
	if !strings.Contains(text, "跑到一半挂了") || !strings.Contains(text, "第一步") {
		t.Errorf("错误和已产生的输出都要带上: %s", text)
	}
}

// 没启用的工具报"不存在"而不是"没启用" —— 后者等于告诉外面这台机器上装了什么
func TestMCPCallDisabledToolLooksMissing(t *testing.T) {
	s := New()
	s.Register(&fakeTool{name: "secret"})
	resp, _ := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"secret"}}`)
	if resp.Error == nil {
		t.Fatal("调没启用的工具应该报错")
	}
	if strings.Contains(resp.Error.Message, "启用") {
		t.Errorf("不该透露它只是没启用: %s", resp.Error.Message)
	}
	if !strings.Contains(resp.Error.Message, "不存在") {
		t.Errorf("应该报不存在: %s", resp.Error.Message)
	}
}

func TestMCPProtocolErrors(t *testing.T) {
	s := newTestServer()
	cases := []struct {
		name, body string
		wantCode   int
	}{
		{"坏 JSON", `{not json`, rpcParseError},
		{"jsonrpc 版本不对", `{"jsonrpc":"1.0","id":1,"method":"ping"}`, rpcInvalidRequest},
		{"未知方法", `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`, rpcMethodNotFound},
		{"tools/call 缺 name", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{}}`, rpcInvalidParams},
	}
	for _, c := range cases {
		resp, _ := rpc(t, s, c.body)
		if resp.Error == nil {
			t.Errorf("%s: 应该报错", c.name)
			continue
		}
		if resp.Error.Code != c.wantCode {
			t.Errorf("%s: 错误码 %d,期望 %d", c.name, resp.Error.Code, c.wantCode)
		}
	}
}

// GET 不支持要明确拒绝,不能 200 一个空的 —— 客户端会以为连上了然后一直等
func TestMCPRejectsGET(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	s.handleMCP(rec, httptest.NewRequest(http.MethodGet, mcpPath, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET 应该回 405,得到 %d", rec.Code)
	}
}

func mcpText(t *testing.T, resp jsonRPCResponse) string {
	t.Helper()
	m, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result 不是对象: %#v", resp.Result)
	}
	content, ok := m["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("没有 content: %#v", m)
	}
	first, _ := content[0].(map[string]any)
	if first["type"] != "text" {
		t.Fatalf("content 第一项应该是 text: %#v", first)
	}
	s, _ := first["text"].(string)
	return s
}
