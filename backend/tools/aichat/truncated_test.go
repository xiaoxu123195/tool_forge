package aichat

import (
	"encoding/json"
	"testing"
)

// 四家协议各有各的说法。样本都照着真实流里的那一帧写,
// 别改成"看起来更整齐"的形状 —— 这些字段名就是它们协议本来的样子。
func TestLengthCappedParsers(t *testing.T) {
	t.Run("chat-completions", func(t *testing.T) {
		cases := []struct {
			name    string
			payload string
			want    bool
		}{
			{"撞上限", `{"choices":[{"delta":{},"finish_reason":"length"}]}`, true},
			{"正常说完", `{"choices":[{"delta":{},"finish_reason":"stop"}]}`, false},
			{"要调工具", `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`, false},
			{"中间帧还没有 finish_reason", `{"choices":[{"delta":{"content":"你好"}}]}`, false},
			{"不是这个协议的帧", `{"type":"response.output_text.delta"}`, false},
			{"坏 JSON", `not json`, false},
		}
		for _, c := range cases {
			if got := parseOpenAIChatLengthCapped(c.payload); got != c.want {
				t.Errorf("%s: 得到 %v, 期望 %v", c.name, got, c.want)
			}
		}
	})

	t.Run("responses", func(t *testing.T) {
		cases := []struct {
			name    string
			payload string
			want    bool
		}{
			{
				"官方形状:incomplete + max_output_tokens",
				`{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`,
				true,
			},
			{
				"中转只给 incomplete_details,事件名仍是 completed",
				`{"type":"response.completed","response":{"incomplete_details":{"reason":"max_output_tokens"}}}`,
				true,
			},
			{
				"中转只给 status",
				`{"type":"response.completed","response":{"status":"incomplete"}}`,
				true,
			},
			{
				"正常完成",
				`{"type":"response.completed","response":{"status":"completed"}}`,
				false,
			},
			{"正文增量帧", `{"type":"response.output_text.delta","delta":"你好"}`, false},
			// chat-completions 的帧不该被这个解析器认领,否则同一条流会被判两次
			{"不是 response. 开头的帧", `{"choices":[{"finish_reason":"length"}]}`, false},
			{"坏 JSON", `{`, false},
		}
		for _, c := range cases {
			if got := parseOpenAIResponsesLengthCapped(c.payload); got != c.want {
				t.Errorf("%s: 得到 %v, 期望 %v", c.name, got, c.want)
			}
		}
	})

	t.Run("gemini", func(t *testing.T) {
		cases := []struct {
			name    string
			payload string
			want    bool
		}{
			{"撞上限", `{"candidates":[{"finishReason":"MAX_TOKENS"}]}`, true},
			{"正常结束", `{"candidates":[{"finishReason":"STOP"}]}`, false},
			// 安全拦截是另一条路(parseGeminiBlock 会翻成错误文案),不能混进截断
			{"安全拦截", `{"candidates":[{"finishReason":"SAFETY"}]}`, false},
			{"中间帧", `{"candidates":[{"content":{"parts":[{"text":"你好"}]}}]}`, false},
			{"没有 candidates", `{"usageMetadata":{"totalTokenCount":10}}`, false},
		}
		for _, c := range cases {
			if got := parseGeminiLengthCapped(c.payload); got != c.want {
				t.Errorf("%s: 得到 %v, 期望 %v", c.name, got, c.want)
			}
		}
	})
}

// Anthropic 没有独立的解析函数(stop_reason 直接在事件分发的 switch 里判),
// 这里退一步验结构体标签解得出来 —— 标签写错的话上面那行判断永远是 false,
// 而那种错在编译期和 vet 里都看不见
func TestAnthropicStopReasonDecodes(t *testing.T) {
	decode := func(payload string) anthropicEvent {
		var ev anthropicEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			t.Fatalf("解不动这帧: %v", err)
		}
		return ev
	}

	ev := decode(`{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":4096}}`)
	if ev.Type != "message_delta" {
		t.Fatalf("事件类型解错了: %q", ev.Type)
	}
	if ev.Delta.StopReason != "max_tokens" {
		t.Fatalf("stop_reason 没解出来: %q", ev.Delta.StopReason)
	}
	// end_turn 是正常收尾,不能被当成截断
	if got := decode(`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`).Delta.StopReason; got != "end_turn" {
		t.Fatalf("end_turn 解错了: %q", got)
	}
}
