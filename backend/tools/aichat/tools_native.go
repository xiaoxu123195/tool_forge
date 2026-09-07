package aichat

import "encoding/json"

// Anthropic 与 Gemini 的工具声明、流式解析与结果回传。
//
// 两家都不走 OpenAI 那套 tool_calls,各有各的形状:
//
//	Anthropic  声明用 input_schema;调用是一个 tool_use content block,
//	           参数走 input_json_delta 分片;结果作为 user 消息里的 tool_result 块回传
//	Gemini     声明包在 functionDeclarations 里;调用是 parts[].functionCall,
//	           参数是整个对象一次到位(不分片);结果作为 user content 里的 functionResponse

// ---- Anthropic ----

// anthropicToolDecls 工具声明
func anthropicToolDecls() []any {
	tools := listTools()
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": t.Parameters,
		})
	}
	return out
}

// buildAnthropicToolBlocks 把工具调用 / 结果翻成 content 块。
//
//	assistant 侧  tool_use{id,name,input}
//	结果侧        tool_result{tool_use_id,content},且必须放在 user 消息里
//
// input 要求是对象而不是字符串,所以这里要把模型给的参数 JSON 解回来;
// 解不动就退回空对象 —— 宁可让工具收到空参数,也别让整个请求因为格式不合法被拒。
func buildAnthropicToolBlocks(m Message) []map[string]any {
	out := make([]map[string]any, 0, len(m.ToolCalls)+1)
	if m.Role == RoleTool {
		for _, c := range m.ToolCalls {
			out = append(out, map[string]any{
				"type":        "tool_result",
				"tool_use_id": c.ID,
				"content":     toolOutput(c),
				"is_error":    c.Error != "",
			})
		}
		return out
	}
	if m.Content != "" {
		out = append(out, map[string]any{"type": "text", "text": m.Content})
	}
	for _, c := range m.ToolCalls {
		out = append(out, map[string]any{
			"type":  "tool_use",
			"id":    c.ID,
			"name":  c.Name,
			"input": decodeToolArgs(c.Arguments),
		})
	}
	return out
}

// ---- Gemini ----

// geminiToolDecls 工具声明。Gemini 把所有函数装在一个 tools 元素的 functionDeclarations 里。
func geminiToolDecls() []any {
	tools := listTools()
	decls := make([]any, 0, len(tools))
	for _, t := range tools {
		decls = append(decls, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"parameters":  t.Parameters,
		})
	}
	if len(decls) == 0 {
		return nil
	}
	return []any{map[string]any{"functionDeclarations": decls}}
}

// buildGeminiToolParts 把工具调用 / 结果翻成 parts。
// 结果的 response 必须是对象,所以统一包成 {"result": "..."}。
func buildGeminiToolParts(m Message) []map[string]any {
	out := make([]map[string]any, 0, len(m.ToolCalls)+1)
	if m.Role == RoleTool {
		for _, c := range m.ToolCalls {
			out = append(out, map[string]any{
				"functionResponse": map[string]any{
					"name":     c.Name,
					"response": map[string]any{"result": toolOutput(c)},
				},
			})
		}
		return out
	}
	if m.Content != "" {
		out = append(out, map[string]any{"text": m.Content})
	}
	for _, c := range m.ToolCalls {
		out = append(out, map[string]any{
			"functionCall": map[string]any{
				"name": c.Name,
				"args": decodeToolArgs(c.Arguments),
			},
		})
	}
	return out
}

// parseGeminiToolCalls 从 chunk 抠 functionCall。
// Gemini 不分片推参数,一个 part 就是一次完整调用;它也不给调用 ID,
// 回传时是靠函数名配对的,所以这里用名字当 ID。
func parseGeminiToolCalls(payload string) []ToolCall {
	var ev struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					FunctionCall *struct {
						Name string          `json:"name"`
						Args json.RawMessage `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return nil
	}
	var out []ToolCall
	for _, c := range ev.Candidates {
		for _, p := range c.Content.Parts {
			if p.FunctionCall == nil || p.FunctionCall.Name == "" {
				continue
			}
			args := string(p.FunctionCall.Args)
			if args == "" || args == "null" {
				args = "{}"
			}
			out = append(out, ToolCall{
				ID:        p.FunctionCall.Name,
				Name:      p.FunctionCall.Name,
				Arguments: args,
			})
		}
	}
	return out
}

// decodeToolArgs 把参数 JSON 字符串解回对象。
// Anthropic 和 Gemini 的入参都要求是对象;解不动就给空对象,让工具自己处理缺参,
// 总比整个请求因为格式不合法被拒强。
func decodeToolArgs(args string) map[string]any {
	out := map[string]any{}
	if args == "" {
		return out
	}
	// 注意 "null" 会被成功解析成 nil map —— 那样发出去是个 JSON null 而不是对象,
	// 各家都会拒。解完必须再判一次空。
	if err := json.Unmarshal([]byte(args), &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}
