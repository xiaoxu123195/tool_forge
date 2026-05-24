// Package apiserver 暴露 Tool Forge 内部工具为本地 HTTP API,
// 让外部脚本 / AI Agent / 其他客户端能调用。
//
// 设计:
//   - 默认只 listen 127.0.0.1,不绑 0.0.0.0(MVP 阶段不开放局域网)
//   - 路由前缀 /api/v1/tools/<name>,统一鉴权 + 错误格式
//   - 每个工具实现 ToolHandler 接口,自带 enable/disable 标志,用户可单独开关
//   - 配置文件存在 ~/.toolforge/api-server.json
package apiserver

import (
	"context"
	"encoding/json"
	"net/http"
)

// ToolHandler 单个工具的 HTTP 处理器(同步)
type ToolHandler interface {
	// Name 唯一 ID,作 URL 路径,如 "app-search"
	Name() string
	// Title 中文显示名,UI 列表用,如 "包名搜索"
	Title() string
	// Description 一句话描述,UI tooltip / GET /api/v1/tools 列表元信息用
	Description() string
	// Methods 允许的 HTTP 方法(为空默认 POST)
	Methods() []string
	// Handle 处理请求体并返回 JSON 字节
	//   ctx     来自 http.Request,客户端断开时会 cancel
	//   body    请求体原始字节(JSON)
	//   返回    响应体 JSON 字节;返回 error 时 server 包装成统一错误格式
	Handle(ctx context.Context, body []byte) ([]byte, error)
}

// StreamHandler 流式工具处理器(SSE)。
// handler 同时实现 ToolHandler 时,server 优先走流式分支。
//
// emit 函数:每次调用会把 ev 序列化成一个 SSE 事件推给客户端。
// handler 内部应在结束时(无论成功 / 取消 / 错误)返回 nil 或 error;
// server 在 emit 失败 / ctx 取消时也会主动停止 handler。
type StreamHandler interface {
	ToolHandler
	HandleStream(ctx context.Context, body []byte, emit func(ev StreamEvent) error) error
}

// StreamEvent 一个 SSE 事件载荷
type StreamEvent struct {
	// Type 事件类型,如 "log" / "progress" / "done" / "error",由 handler 自定义
	Type string `json:"type"`
	// Data 任意 JSON 载荷;handler 自行决定结构
	Data any `json:"data,omitempty"`
}

// Config 持久化到 ~/.toolforge/api-server.json 的配置
type Config struct {
	// Enabled 启用 server(关闭时不监听端口)
	Enabled bool `json:"enabled"`
	// Port 监听端口
	Port int `json:"port"`
	// AuthEnabled 是否要求 Authorization: Bearer <Token>
	AuthEnabled bool `json:"auth_enabled"`
	// Token 鉴权 token(明文存,工具用)
	Token string `json:"token"`
	// EnabledTools 已暴露的工具名集合;key 是 ToolHandler.Name()
	EnabledTools map[string]bool `json:"enabled_tools"`
}

// DefaultConfig 全新安装时的默认配置:不启用,端口 11435,
// 鉴权关闭,所有工具默认关。
func DefaultConfig() Config {
	return Config{
		Enabled:      false,
		Port:         11435,
		AuthEnabled:  false,
		Token:        "",
		EnabledTools: map[string]bool{},
	}
}

// ToolInfo 给前端列工具卡片用
type ToolInfo struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Path        string `json:"path"`    // "/api/v1/tools/<name>"
	Enabled     bool   `json:"enabled"` // 配置里是否勾选
}

// Status server 实时状态
type Status struct {
	Running bool   `json:"running"`
	Addr    string `json:"addr"`            // "127.0.0.1:11435"
	Error   string `json:"error,omitempty"` // 启动失败原因(端口占用等)
}

// apiError 统一错误响应格式
type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Error: code, Message: msg})
}
