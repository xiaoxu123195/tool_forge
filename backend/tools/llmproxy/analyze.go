package llmproxy

import (
	"encoding/json"
	"strings"
)

// extractModel 从请求体 JSON 取 model,取不到再从响应体取。
func extractModel(reqBody, respBody string) string {
	if m := jsonString(reqBody, "model"); m != "" {
		return m
	}
	return jsonString(respBody, "model")
}

// extractUsage 从响应体(非流)或原始 SSE(流)里尽力解析 token 用量。
// 兼容 OpenAI(prompt_tokens/completion_tokens/total_tokens)与
// Anthropic(input_tokens/output_tokens)。
func extractUsage(respBody string, stream bool) (prompt, completion, total int) {
	if !stream {
		return usageFromJSON(respBody)
	}
	// 流式:逐行找带 usage 的 data 行,取最后一次出现的
	for _, line := range strings.Split(respBody, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[5:])
		if payload == "" || payload == "[DONE]" {
			continue
		}
		if p, c, t := usageFromJSON(payload); p+c+t > 0 {
			prompt, completion, total = p, c, t
		}
	}
	return
}

func usageFromJSON(s string) (prompt, completion, total int) {
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &m); err != nil {
		return 0, 0, 0
	}
	usage, ok := m["usage"].(map[string]any)
	if !ok {
		// Anthropic message_start 把 usage 套在 message 里
		if msg, ok := m["message"].(map[string]any); ok {
			usage, _ = msg["usage"].(map[string]any)
		}
	}
	if usage == nil {
		// OpenAI Responses API 的 response.completed 把 usage 套在 response 里
		if resp, ok := m["response"].(map[string]any); ok {
			usage, _ = resp["usage"].(map[string]any)
		}
	}
	if usage == nil {
		return 0, 0, 0
	}
	prompt = numField(usage, "prompt_tokens", "input_tokens")
	completion = numField(usage, "completion_tokens", "output_tokens")
	total = numField(usage, "total_tokens")
	if total == 0 {
		total = prompt + completion
	}
	return
}

// mergeSSE 把原始 SSE 文本里的增量内容拼成可读文本。兼容:
//   - OpenAI Responses API:type=response.output_text.delta,delta 为字符串
//   - OpenAI chat:choices[].delta.content / completions:choices[].text
//   - Anthropic:content_block_delta 的 delta.text
//
// 解析不出就返回空(UI 退回看原始)。
func mergeSSE(raw string) string {
	var sb strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[5:])
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			continue
		}
		// OpenAI Responses API:delta 是字符串。只取正文增量(output_text.delta),
		// 避开推理摘要(reasoning_summary_text.delta)与工具入参(function_call_arguments.delta)。
		if s, ok := m["delta"].(string); ok {
			t, _ := m["type"].(string)
			if t == "" || strings.HasSuffix(t, "output_text.delta") {
				sb.WriteString(s)
			}
			continue
		}
		// Anthropic:delta 是对象
		if delta, ok := m["delta"].(map[string]any); ok {
			if t, ok := delta["text"].(string); ok {
				sb.WriteString(t)
				continue
			}
		}
		// OpenAI chat / completions
		if choices, ok := m["choices"].([]any); ok && len(choices) > 0 {
			if ch, ok := choices[0].(map[string]any); ok {
				if d, ok := ch["delta"].(map[string]any); ok {
					if t, ok := d["content"].(string); ok {
						sb.WriteString(t)
						continue
					}
				}
				if t, ok := ch["text"].(string); ok {
					sb.WriteString(t)
				}
			}
		}
	}
	return sb.String()
}

func jsonString(s, key string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &m); err != nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func numField(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if f, ok := v.(float64); ok {
				return int(f)
			}
		}
	}
	return 0
}
