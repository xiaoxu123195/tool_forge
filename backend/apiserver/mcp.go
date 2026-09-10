package apiserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
)

// MCP(Model Context Protocol)适配层。
//
// 存在的理由:这个应用里真正不可替代的能力,是它碰得到本机 —— 连着的手机、
// 本地的解析器、长期存着的凭据。而 Claude Code / Codex 这类 agent 恰恰碰不到。
// 把已注册的 ToolHandler 换一种协议说出去,agent 就能直接调,
// 不用人再切过来点几下。
//
// 走 Streamable HTTP 而不是 stdio:这个应用本来就是常驻进程,
// stdio 还得再包一层启动器。客户端配一个 URL 就完事。
//
// 复用同一批 handler、同一套启用开关、同一个 Bearer 鉴权 —— 没有第二套权限模型。
// 在 UI 里没勾选暴露的工具,MCP 这边同样看不见。

const (
	mcpPath = "/mcp"
	// mcpProtocolVersion 实现时对齐的协议版本。客户端报别的版本也照常按标准流程走,
	// 因为我们只用最基础的那几个方法(initialize / tools/list / tools/call),
	// 这几个跨版本是稳的
	mcpProtocolVersion = "2024-11-05"
)

// jsonRPCRequest JSON-RPC 2.0 请求。
//
// ID 用 json.RawMessage:规范允许字符串或数字,原样回传最省事,
// 自己解析成某个 Go 类型再序列化回去,反而会把 1 变成 "1" 这种细节搞错。
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// JSON-RPC 标准错误码
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

// handleMCP 是 MCP 的唯一入口。
//
// POST 带一条 JSON-RPC 消息;有 id 的是请求,回一条响应,
// 没有 id 的是通知(如 notifications/initialized),回 202 且不带响应体 ——
// 给通知回响应是明确违反规范的,有的客户端会因此断开。
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// 规范允许服务端不支持 GET(那条通道是给服务端主动推消息用的,
		// 我们没有需要主动推的东西)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"MCP 端点只接受 POST")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeRPC(w, jsonRPCResponse{JSONRPC: "2.0", Error: &jsonRPCError{rpcParseError, "读取请求体失败"}})
		return
	}
	var req jsonRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPC(w, jsonRPCResponse{JSONRPC: "2.0", Error: &jsonRPCError{rpcParseError, "不是合法的 JSON"}})
		return
	}
	if req.JSONRPC != "2.0" {
		writeRPC(w, jsonRPCResponse{JSONRPC: "2.0", ID: req.ID,
			Error: &jsonRPCError{rpcInvalidRequest, "jsonrpc 必须是 \"2.0\""}})
		return
	}

	isNotification := len(req.ID) == 0
	result, rpcErr := s.dispatchMCP(r.Context(), req)
	if isNotification {
		// 通知不回响应体
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeRPC(w, jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr})
}

func (s *Server) dispatchMCP(ctx context.Context, req jsonRPCRequest) (any, *jsonRPCError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": mcpProtocolVersion,
			// 只声明 tools。没有 resources / prompts 就不要声明 ——
			// 声明了客户端就会来问,然后拿到一堆 method not found
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "tool-forge",
				"version": "1",
			},
		}, nil

	case "notifications/initialized", "notifications/cancelled":
		return nil, nil

	case "ping":
		return map[string]any{}, nil

	case "tools/list":
		return map[string]any{"tools": s.mcpToolList()}, nil

	case "tools/call":
		return s.mcpCallTool(ctx, req.Params)
	}
	return nil, &jsonRPCError{rpcMethodNotFound, "不支持的方法: " + req.Method}
}

// mcpToolList 把已启用的工具翻译成 MCP 的工具描述。
//
// 只列启用的,和 REST 那边一个规矩:没勾选就等于"用户不希望被外部知道"。
func (s *Server) mcpToolList() []map[string]any {
	s.mu.RLock()
	handlers := make([]ToolHandler, 0, len(s.handlers))
	for name, h := range s.handlers {
		if s.cfg.EnabledTools[name] {
			handlers = append(handlers, h)
		}
	}
	s.mu.RUnlock()

	// 排个序,列表顺序稳定 —— map 遍历每次都不一样,
	// agent 那边看到的工具顺序跟着乱跳很奇怪
	sort.Slice(handlers, func(i, j int) bool { return handlers[i].Name() < handlers[j].Name() })

	out := make([]map[string]any, 0, len(handlers))
	for _, h := range handlers {
		out = append(out, map[string]any{
			"name":        h.Name(),
			"description": h.Title() + " —— " + h.Description(),
			"inputSchema": inputSchemaOf(h),
		})
	}
	return out
}

// mcpCallTool 执行一次工具调用。
//
// 流式工具(StreamHandler)在这里是"跑到底再一次性返回":MCP 的 tools/call
// 本身是一问一答,没有中途推进度的地方。取证提取那种要跑几分钟的,
// 客户端可能等到超时 —— 这是已知的限制,等真的碍事了再考虑走 SSE 响应。
func (s *Server) mcpCallTool(ctx context.Context, params json.RawMessage) (any, *jsonRPCError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonRPCError{rpcInvalidParams, "params 解析失败: " + err.Error()}
	}
	if p.Name == "" {
		return nil, &jsonRPCError{rpcInvalidParams, "缺少 name"}
	}

	s.mu.RLock()
	h, ok := s.handlers[p.Name]
	enabled := s.cfg.EnabledTools[p.Name]
	s.mu.RUnlock()
	if !ok || !enabled {
		// 没启用的工具报"不存在"而不是"没启用":列表里本来就看不到它,
		// 说"存在但你不能用"反而泄露了这台机器上装了什么
		return nil, &jsonRPCError{rpcInvalidParams, "工具不存在: " + p.Name}
	}

	args := p.Arguments
	if len(args) == 0 {
		args = []byte("{}")
	}

	// 工具自身的失败(参数不对、设备没连上)走 isError 而不是 JSON-RPC error:
	// 前者 agent 能读到原文并自己改参数重试,后者多数客户端只会把整轮对话中断
	if sh, isStream := h.(StreamHandler); isStream {
		var sb strings.Builder
		err := sh.HandleStream(ctx, args, func(ev StreamEvent) error {
			line, _ := json.Marshal(ev)
			sb.Write(line)
			sb.WriteByte('\n')
			return nil
		})
		if err != nil {
			return mcpTextResult(errText(err, sb.String()), true), nil
		}
		return mcpTextResult(sb.String(), false), nil
	}

	out, err := h.Handle(ctx, args)
	if err != nil {
		return mcpTextResult(err.Error(), true), nil
	}
	return mcpTextResult(string(out), false), nil
}

func errText(err error, partial string) string {
	if strings.TrimSpace(partial) == "" {
		return err.Error()
	}
	return err.Error() + "\n\n已经产生的输出:\n" + partial
}

func mcpTextResult(text string, isError bool) map[string]any {
	if text == "" {
		text = "(工具没有返回内容)"
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

// inputSchemaOf 取工具的入参 schema;没实现 SchemaProvider 的给一个宽松的兜底。
//
// 兜底是"任意对象"而不是"无参数":说成无参数的话,agent 会真的什么都不传,
// 而那多半会失败。宽松 schema 至少让它能照着 description 试。
func inputSchemaOf(h ToolHandler) map[string]any {
	if sp, ok := h.(SchemaProvider); ok {
		if s := sp.InputSchema(); s != nil {
			return s
		}
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": true,
	}
}

func writeRPC(w http.ResponseWriter, resp jsonRPCResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
