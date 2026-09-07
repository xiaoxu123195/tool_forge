package aichat

import (
	"encoding/json"
	"strconv"
)

// OpenAI 协议族(Chat Completions + Responses)的工具声明、流式解析与结果回传。
//
// 两个端点的形状不一样,而且都把参数切成碎片流式推送 —— 这是这一层最主要的复杂度来源。

// openAIToolDecls 工具声明。Chat Completions 要求嵌一层 function,Responses 是平的。
func openAIToolDecls(useResponses bool) []any {
	tools := listTools()
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		if useResponses {
			out = append(out, map[string]any{
				"type":        "function",
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			})
			continue
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			},
		})
	}
	return out
}

// appendTools 往请求体的 tools 数组里追加(内置联网可能已经放了东西进去)
func appendTools(body map[string]any, decls []any) {
	if len(decls) == 0 {
		return
	}
	existing, _ := body["tools"].([]any)
	body["tools"] = append(existing, decls...)
}

// toolCallAccumulator 把分片到达的工具调用拼回完整的一条。
//
// 两个端点都是流式推参数,但定位分片的键不一样:Chat Completions 用 choices 里的 index,
// Responses 用事件里的 item_id。用一个 map 兼下两种,key 各自取各自的。
type toolCallAccumulator struct {
	order []string
	byKey map[string]*ToolCall
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{byKey: map[string]*ToolCall{}}
}

// at 取(或新建)某个分片键对应的调用
func (a *toolCallAccumulator) at(key string) *ToolCall {
	if c, ok := a.byKey[key]; ok {
		return c
	}
	c := &ToolCall{}
	a.byKey[key] = c
	a.order = append(a.order, key)
	return c
}

// done 返回拼装完成的调用,保持首次出现的顺序。
// 没有名字的丢掉 —— 那是只收到参数分片、没收到声明帧的残缺项,发回去也没用。
func (a *toolCallAccumulator) done() []ToolCall {
	out := make([]ToolCall, 0, len(a.order))
	for _, k := range a.order {
		c := a.byKey[k]
		if c.Name == "" {
			continue
		}
		out = append(out, *c)
	}
	return out
}

// collectChatToolCalls 解 Chat Completions 的 delta.tool_calls[]。
// 典型形态:第一帧带 index+id+name,后续若干帧只带 index + 一小段 arguments。
func collectChatToolCalls(payload string, acc *toolCallAccumulator) {
	var ev struct {
		Choices []struct {
			Delta struct {
				ToolCalls []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return
	}
	for _, ch := range ev.Choices {
		for _, tc := range ch.Delta.ToolCalls {
			c := acc.at("idx-" + strconv.Itoa(tc.Index))
			if tc.ID != "" {
				c.ID = tc.ID
			}
			if tc.Function.Name != "" {
				c.Name = tc.Function.Name
			}
			c.Arguments += tc.Function.Arguments
		}
	}
}

// collectResponsesToolCalls 解 /v1/responses 的函数调用事件。
//
//	response.output_item.added        item.type=function_call,带 call_id 和 name
//	response.function_call_arguments.delta   按 item_id 追加参数分片
func collectResponsesToolCalls(payload string, acc *toolCallAccumulator) {
	var ev struct {
		Type   string `json:"type"`
		ItemID string `json:"item_id"`
		Delta  string `json:"delta"`
		Item   struct {
			ID        string `json:"id"`
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return
	}
	switch {
	case ev.Item.Type == "function_call" && ev.Item.Name != "":
		// output_item.added / .done 都会带完整声明;done 那次还会带上全量 arguments
		c := acc.at(ev.Item.ID)
		c.ID = ev.Item.CallID
		c.Name = ev.Item.Name
		if ev.Item.Arguments != "" {
			c.Arguments = ev.Item.Arguments
		}
	case ev.Type == "response.function_call_arguments.delta" && ev.ItemID != "":
		acc.at(ev.ItemID).Arguments += ev.Delta
	}
}

// buildOpenAIChatToolMessages 把一条 assistant(带工具调用)或 RoleTool(带结果)
// 消息翻成 Chat Completions 的形状。
//
//	assistant  content + tool_calls[]
//	结果       每条调用一条独立的 {role:"tool", tool_call_id, content}
func buildOpenAIChatToolMessages(m Message) []map[string]any {
	if m.Role == RoleTool {
		out := make([]map[string]any, 0, len(m.ToolCalls))
		for _, c := range m.ToolCalls {
			out = append(out, map[string]any{
				"role":         "tool",
				"tool_call_id": c.ID,
				"content":      toolOutput(c),
			})
		}
		return out
	}
	calls := make([]map[string]any, 0, len(m.ToolCalls))
	for _, c := range m.ToolCalls {
		calls = append(calls, map[string]any{
			"id":   c.ID,
			"type": "function",
			"function": map[string]any{
				"name":      c.Name,
				"arguments": emptyObjectIfBlank(c.Arguments),
			},
		})
	}
	return []map[string]any{{
		"role":       "assistant",
		"content":    m.Content,
		"tool_calls": calls,
	}}
}

// buildOpenAIResponsesToolItems 把工具调用 / 结果翻成 Responses 的 input 项。
// Responses 不用 assistant 消息包着,调用和结果都是平铺的 input 项。
func buildOpenAIResponsesToolItems(m Message) []map[string]any {
	out := make([]map[string]any, 0, len(m.ToolCalls))
	if m.Role == RoleTool {
		for _, c := range m.ToolCalls {
			out = append(out, map[string]any{
				"type":    "function_call_output",
				"call_id": c.ID,
				"output":  toolOutput(c),
			})
		}
		return out
	}
	if m.Content != "" {
		out = append(out, map[string]any{"role": "assistant", "content": m.Content})
	}
	for _, c := range m.ToolCalls {
		out = append(out, map[string]any{
			"type":      "function_call",
			"call_id":   c.ID,
			"name":      c.Name,
			"arguments": emptyObjectIfBlank(c.Arguments),
		})
	}
	return out
}

// emptyObjectIfBlank 参数为空时补一个空对象。
// 无参工具模型经常什么都不给,而各家的 schema 校验又要求这里是合法 JSON。
func emptyObjectIfBlank(args string) string {
	if args == "" {
		return "{}"
	}
	return args
}
