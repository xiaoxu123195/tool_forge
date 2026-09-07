package aichat

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"tool_forge/backend/tools/mcp"
)

// 工具调用(function calling)。
//
// 四家的线上格式差别不小,但抽象是一样的:我们在请求里声明一组工具 → 模型回一个
// "我要调用 X,参数是 Y" → 我们在本地执行 → 把结果塞回对话再发一轮,直到模型不再要调工具。
//
// 这里只放"工具是什么、怎么执行";各协议怎么声明、怎么从流里把调用抠出来、
// 结果该以什么形状回传,分别在 openai.go / anthropic.go / gemini.go 里。

// maxToolRounds 一次提问最多允许模型连续调几轮工具。
// 防的是模型陷在"调用 → 看结果 → 再调同一个"的循环里把 token 烧光。
const maxToolRounds = 5

// Tool 一个可供模型调用的本地工具
type Tool struct {
	Name string
	// Description 写给模型看的,决定它会不会在该用的时候想起这个工具
	Description string
	// Parameters JSON Schema(必须是 type=object);四家协议都吃这个形状
	Parameters map[string]any
	// Handler 执行体。args 是模型给的 JSON 字符串(可能是空串)。
	// 返回的字符串原样回传给模型,所以要写成模型读得懂的样子。
	Handler func(ctx context.Context, args string) (string, error)
}

// mcpService 外部注入的 MCP 客户端。为 nil 时只有内置工具可用 ——
// 用包级变量而不是往 Tool 里传,是因为工具解析发生在协议层深处,
// 一路把 service 传下去要改四个协议的十几个函数签名。
var (
	mcpMu      sync.RWMutex
	mcpService *mcp.Service
)

// SetMCPService 注入 MCP 客户端(由 app.go 在启动时调用)
func SetMCPService(s *mcp.Service) {
	mcpMu.Lock()
	defer mcpMu.Unlock()
	mcpService = s
}

func currentMCP() *mcp.Service {
	mcpMu.RLock()
	defer mcpMu.RUnlock()
	return mcpService
}

// builtinTools 内置工具表。MCP 服务器提供的工具在 listTools 里与它合并。
var builtinTools = map[string]Tool{
	"get_current_time": {
		Name: "get_current_time",
		Description: "获取当前的日期和时间。模型自身没有时间概念,凡是涉及" +
			"「今天」「现在」「最近」的问题都应该先调用这个工具。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"timezone": map[string]any{
					"type": "string",
					"description": "IANA 时区名,如 Asia/Shanghai、America/New_York。" +
						"不填则用本机时区。",
				},
			},
		},
		Handler: getCurrentTime,
	},
}

// listTools 内置工具 + 所有已启用 MCP 服务器提供的工具。
//
// 按名字排序保证请求体稳定 —— 否则每次请求的 tools 顺序都不同,会白白打断
// 供应商侧的提示词缓存。MCP 服务器连不上不影响这里:它自己会把错误记进 status,
// 返回的列表里就是少了它那几个工具而已。
func listTools() []Tool {
	out := make([]Tool, 0, len(builtinTools)+8)
	for _, t := range builtinTools {
		out = append(out, t)
	}
	// 只取已连好的:这里在聊天请求的关键路径上,不能为了连一个冷服务器把消息卡住。
	// 预热由 Service.Warm 在启动和配置变更时做。
	if svc := currentMCP(); svc != nil {
		for _, info := range svc.CachedTools() {
			out = append(out, mcpTool(info))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// findTool 按名字取工具。内置优先 —— 内置名字是我们自己定的,不会和 MCP 撞
// (MCP 的名字都带服务器前缀)。
func findTool(name string) (Tool, bool) {
	if t, ok := builtinTools[name]; ok {
		return t, true
	}
	svc := currentMCP()
	if svc == nil {
		return Tool{}, false
	}
	for _, info := range svc.CachedTools() {
		if info.QualifiedName == name {
			return mcpTool(info), true
		}
	}
	return Tool{}, false
}

// mcpTool 把一个 MCP 工具包成本地 Tool。
// 描述前面缀上服务器名,模型在有多个来源时能分清工具属于谁。
func mcpTool(info mcp.ToolInfo) Tool {
	return Tool{
		Name:        info.QualifiedName,
		Description: "[" + info.ServerName + "] " + info.Description,
		Parameters:  info.InputSchema,
		Handler: func(ctx context.Context, args string) (string, error) {
			svc := currentMCP()
			if svc == nil {
				return "", fmt.Errorf("MCP 客户端未初始化")
			}
			return svc.CallTool(ctx, info.QualifiedName, decodeToolArgs(args))
		},
	}
}

// executeToolCalls 依次执行模型请求的这一批调用,把结果 / 错误写回每一条。
//
// 出错不中断整批,也不往上抛:错误信息本身就是给模型的输入 —— 让它知道"这个工具失败了、
// 为什么失败",它往往能自己换个参数重试或者改口。直接中断反而把这个机会掐掉了。
func executeToolCalls(ctx context.Context, calls []ToolCall) []ToolCall {
	out := make([]ToolCall, len(calls))
	for i, c := range calls {
		out[i] = c
		tool, ok := findTool(c.Name)
		if !ok {
			out[i].Error = fmt.Sprintf("没有名为 %q 的工具", c.Name)
			continue
		}
		result, err := tool.Handler(ctx, c.Arguments)
		if err != nil {
			out[i].Error = err.Error()
			continue
		}
		out[i].Result = result
	}
	return out
}

// toolOutput 回传给模型的内容:成功给结果,失败给错误说明
func toolOutput(c ToolCall) string {
	if c.Error != "" {
		return "工具执行失败:" + c.Error
	}
	if c.Result == "" {
		return "(工具没有返回内容)"
	}
	return c.Result
}

// ---- 内置工具实现 ----

func getCurrentTime(_ context.Context, args string) (string, error) {
	var in struct {
		Timezone string `json:"timezone"`
	}
	// 参数是可选的,空串 / 解析失败都按"用本机时区"处理 —— 模型偶尔会传个空对象或者干脆不传
	if s := strings.TrimSpace(args); s != "" && s != "{}" {
		_ = json.Unmarshal([]byte(s), &in)
	}
	loc := time.Local
	if in.Timezone != "" {
		l, err := time.LoadLocation(in.Timezone)
		if err != nil {
			return "", fmt.Errorf("无法识别的时区 %q", in.Timezone)
		}
		loc = l
	}
	now := time.Now().In(loc)
	return fmt.Sprintf("%s(%s,%s)",
		now.Format("2006-01-02 15:04:05"),
		loc.String(),
		weekdayCN(now.Weekday()),
	), nil
}

func weekdayCN(d time.Weekday) string {
	names := [...]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
	return names[int(d)%7]
}
