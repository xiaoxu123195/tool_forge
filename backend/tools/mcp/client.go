package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
)

// transport 一条已建立的连接。两种实现:stdio.go 与 http.go。
//
// call 必须并发安全:多个工具调用可能同时在飞。
type transport interface {
	call(ctx context.Context, method string, params any) (json.RawMessage, error)
	notify(ctx context.Context, method string, params any) error
	close() error
}

// client 一个已握手的 MCP 会话
type client struct {
	tr         transport
	serverInfo string

	mu    sync.Mutex
	tools []ToolInfo
}

// connect 建连接 + 握手。失败时保证不留下悬着的进程 / 连接。
func connect(ctx context.Context, s Server) (*client, error) {
	var tr transport
	var err error
	switch s.Kind {
	case TransportHTTP:
		tr, err = newHTTPTransport(s)
	default:
		tr, err = newStdioTransport(ctx, s)
	}
	if err != nil {
		return nil, err
	}

	c := &client{tr: tr}
	if err := c.initialize(ctx); err != nil {
		_ = tr.close()
		return nil, err
	}
	return c, nil
}

// initialize 走一遍握手:initialize 请求 + initialized 通知。
// 少了后面那条通知,一部分服务器会一直不响应后续请求。
func (c *client) initialize(ctx context.Context) error {
	raw, err := c.tr.call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"clientInfo": map[string]any{
			"name":    "ToolForge",
			"version": "1",
		},
	})
	if err != nil {
		return fmt.Errorf("握手失败: %w", err)
	}
	var res initializeResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("握手响应解析失败: %w", err)
	}
	c.serverInfo = res.ServerInfo.Name
	if res.ServerInfo.Version != "" {
		c.serverInfo += " " + res.ServerInfo.Version
	}
	if err := c.tr.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return fmt.Errorf("initialized 通知发送失败: %w", err)
	}
	return nil
}

// listTools 拉全量工具列表(带分页游标的服务器会翻到底)
func (c *client) listTools(ctx context.Context, s Server) ([]ToolInfo, error) {
	prefix := sanitizeName(s.Name)
	var out []ToolInfo
	cursor := ""
	// 防呆:游标实现有 bug 的服务器可能永远返回同一个游标
	for page := 0; page < 20; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.tr.call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var res listToolsResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, fmt.Errorf("工具列表解析失败: %w", err)
		}
		for _, t := range res.Tools {
			if t.Name == "" {
				continue
			}
			out = append(out, ToolInfo{
				ServerID:      s.ID,
				ServerName:    s.Name,
				Name:          t.Name,
				QualifiedName: prefix + "_" + sanitizeName(t.Name),
				Description:   t.Description,
				InputSchema:   normalizeSchema(t.InputSchema),
			})
		}
		if res.NextCursor == "" || res.NextCursor == cursor {
			break
		}
		cursor = res.NextCursor
	}

	c.mu.Lock()
	c.tools = out
	c.mu.Unlock()
	return out, nil
}

// callTool 调用一个工具,把结果压成一段文本。
//
// isError 不返回成 Go error:那是"工具自己失败了",内容要原样交给模型,
// 让它有机会换个参数重试。真正的 error 留给"连接断了/协议错了"这类客户端侧问题。
func (c *client) callTool(ctx context.Context, name string, args map[string]any) (string, error) {
	if args == nil {
		args = map[string]any{}
	}
	raw, err := c.tr.call(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return "", err
	}
	var res callToolResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("工具响应解析失败: %w", err)
	}
	text := flattenContent(res.Content)
	if res.IsError {
		if text == "" {
			text = "(工具报告失败,但没有给出说明)"
		}
		return "工具执行失败:" + text, nil
	}
	if text == "" {
		return "(工具没有返回内容)", nil
	}
	return text, nil
}

func (c *client) close() error { return c.tr.close() }

// flattenContent 把 content 块数组压成文本。
// 非文本块(图片 / 资源引用)退化成一句说明 —— 模型知道"这里有东西但拿不到",
// 好过什么都不说导致它以为工具返回了空。
func flattenContent(blocks []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	var parts []string
	for _, b := range blocks {
		switch {
		case b.Text != "":
			parts = append(parts, b.Text)
		case b.Type != "" && b.Type != "text":
			parts = append(parts, "("+b.Type+" 类型的内容,当前不支持展示)")
		}
	}
	return joinNonEmpty(parts, "\n")
}

func joinNonEmpty(parts []string, sep string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += sep
		}
		out += p
	}
	return out
}

// normalizeSchema 保证入参 schema 至少是个合法的 object。
// 不少服务器对无参工具干脆不给 inputSchema,而各家大模型的工具声明都要求这里有东西。
func normalizeSchema(s map[string]any) map[string]any {
	if len(s) == 0 {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if _, ok := s["type"]; !ok {
		s["type"] = "object"
	}
	return s
}

// sanitizeName 把名字规整成模型能接受的函数名字符集。
// OpenAI 要求 ^[a-zA-Z0-9_-]+$,MCP 那边却什么都可能出现(点、斜杠、中文)。
func sanitizeName(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		default:
			out = append(out, '_')
		}
	}
	// 全是非法字符时给个兜底,免得产生空函数名
	if len(out) == 0 {
		return "server"
	}
	return string(out)
}

// idGen 单调递增的 JSON-RPC 请求 id
type idGen struct{ n atomic.Int64 }

func (g *idGen) next() int64 { return g.n.Add(1) }
