// Package mcp 是一个 MCP(Model Context Protocol)客户端。
//
// MCP 是 JSON-RPC 2.0 over 某种传输。我们只做客户端要用的那一小块:
//
//	initialize                 握手,拿到服务器信息
//	notifications/initialized  告诉服务器可以开始了
//	tools/list                 列出它提供的工具
//	tools/call                 调用一个工具
//
// prompts / resources / sampling 这些暂不支持 —— AI 问答目前只用得上工具。
//
// 持久化路径: ~/.toolforge/mcp/servers.json
package mcp

import "encoding/json"

// protocolVersion 我们声称支持的协议版本。
// 服务器返回不同版本时不强行中断:MCP 要求双方尽量兼容,而且实际在跑的服务器
// 版本五花八门,握手就掐断会让一大半服务器用不了。
const protocolVersion = "2025-06-18"

// TransportKind 传输方式
type TransportKind = string

const (
	// TransportStdio 起一个本地进程,JSON-RPC 走它的 stdin/stdout(按行分隔)
	TransportStdio TransportKind = "stdio"
	// TransportHTTP Streamable HTTP:POST 一条 JSON-RPC,响应可能是 JSON 也可能是 SSE。
	// 旧的 HTTP+SSE 双端点传输已废弃,不做兼容。
	TransportHTTP TransportKind = "http"
)

// Server 一个 MCP 服务器的配置
type Server struct {
	ID string `json:"id"`
	// Name 显示名,同时用来给工具名加前缀,避免不同服务器的同名工具打架。
	// 会被 sanitizeName 规整成 [a-z0-9_-]。
	Name    string        `json:"name"`
	Kind    TransportKind `json:"kind"`
	Enabled bool          `json:"enabled"`

	// stdio 用
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`

	// http 用
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`

	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

// ToolInfo 一个服务器提供的工具
type ToolInfo struct {
	ServerID   string `json:"serverId"`
	ServerName string `json:"serverName"`
	// Name MCP 侧的原名,调用时要用它
	Name string `json:"name"`
	// QualifiedName 给模型看的名字(带服务器前缀),全局唯一
	QualifiedName string         `json:"qualifiedName"`
	Description   string         `json:"description"`
	InputSchema   map[string]any `json:"inputSchema"`
}

// Status 一个服务器的连接状态
type Status struct {
	ServerID  string `json:"serverId"`
	Connected bool   `json:"connected"`
	// Error 最近一次连接 / 拉工具失败的原因
	Error string `json:"error,omitempty"`
	// ServerInfo 服务器自报的名字与版本
	ServerInfo string `json:"serverInfo,omitempty"`
	ToolCount  int    `json:"toolCount"`
}

// TestResult 连通性检测结果
type TestResult struct {
	OK         bool     `json:"ok"`
	Message    string   `json:"message,omitempty"`
	ServerInfo string   `json:"serverInfo,omitempty"`
	Tools      []string `json:"tools,omitempty"`
	DurationMs int64    `json:"durationMs"`
}

// ---- JSON-RPC 线上结构 ----

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"` // 通知没有 id
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// initializeResult 握手响应里我们关心的部分
type initializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	ServerInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

// listToolsResult tools/list 的响应
type listToolsResult struct {
	Tools []struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		InputSchema map[string]any `json:"inputSchema"`
	} `json:"tools"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// callToolResult tools/call 的响应。
// content 是一个块数组,类型可能是 text / image / resource;我们只提取文本,
// 其余类型退化成一句说明 —— 模型拿到"这里有一张图但客户端不支持"也好过静默丢内容。
type callToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}
