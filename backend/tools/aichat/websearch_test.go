package aichat

import (
	"encoding/json"
	"testing"
)

func toolTypes(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, ok := body["tools"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if s, ok := m["type"].(string); ok {
			out = append(out, s)
			continue
		}
		// Gemini 的工具没有 type 字段,用唯一的 key 作标识
		for k := range m {
			out = append(out, k)
		}
	}
	return out
}

func TestWebSearchPatchPerEndpoint(t *testing.T) {
	cases := []struct {
		name        string
		provider    Provider
		model       string
		wantTools   []string
		wantBodyKey string
	}{
		{
			name:      "anthropic",
			provider:  Provider{Type: TypeAnthropic, BaseURL: "https://api.anthropic.com"},
			model:     "claude-sonnet-4-5",
			wantTools: []string{"web_search_20250305"},
		},
		{
			name:      "gemini",
			provider:  Provider{Type: TypeGemini},
			model:     "gemini-2.5-flash",
			wantTools: []string{"google_search"},
		},
		{
			name:      "openai responses",
			provider:  Provider{Type: TypeOpenAI, BaseURL: "https://api.openai.com/v1"},
			model:     "gpt-5",
			wantTools: []string{"web_search"},
		},
		{
			// xAI 把联网拆成两个工具,公开网页和 X 上的实时讨论各一个
			name:      "xai responses",
			provider:  Provider{Type: TypeXAI, BaseURL: "https://api.x.ai/v1"},
			model:     "grok-4",
			wantTools: []string{"web_search", "x_search"},
		},
		{
			name:      "zhipu chat completions",
			provider:  Provider{Type: TypeOpenAICompat, BaseURL: "https://open.bigmodel.cn/api/paas/v4"},
			model:     "glm-4.6",
			wantTools: []string{"web_search"},
		},
		{
			// 阿里百炼是顶层字段,不进 tools 数组
			name:        "dashscope chat completions",
			provider:    Provider{Type: TypeOpenAICompat, BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1"},
			model:       "qwen-plus",
			wantBodyKey: "enable_search",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := InferModelSpec(tc.provider, tc.model)
			if !spec.Has(CapWebSearch) {
				t.Fatalf("%s 应该支持内置联网, caps=%v", tc.model, spec.Capabilities)
			}
			body := map[string]any{"model": tc.model}
			applyWebSearchPatch(body, buildWebSearchPatch(spec))

			if tc.wantBodyKey != "" {
				if _, ok := body[tc.wantBodyKey]; !ok {
					t.Fatalf("请求体缺少 %s: %#v", tc.wantBodyKey, body)
				}
				return
			}
			got := toolTypes(t, body)
			if len(got) != len(tc.wantTools) {
				t.Fatalf("tools = %v, want %v", got, tc.wantTools)
			}
			for i, w := range tc.wantTools {
				if got[i] != w {
					t.Errorf("tools[%d] = %q, want %q", i, got[i], w)
				}
			}
		})
	}
}

// 认不出来的中转不该给联网开关 —— 打开后发出去必然被拒
func TestUnknownRelayHasNoWebSearch(t *testing.T) {
	spec := InferModelSpec(
		Provider{Type: TypeOpenAICompat, BaseURL: "https://relay.example.com/v1"},
		"some-random-model",
	)
	if spec.Has(CapWebSearch) {
		t.Errorf("未知中转不该声明联网能力: %v", spec.Capabilities)
	}
	body := map[string]any{}
	applyWebSearchPatch(body, buildWebSearchPatch(spec))
	if len(body) != 0 {
		t.Errorf("不支持时不该往请求体里塞东西: %#v", body)
	}
}

// tools 数组已经有内容时应该追加而不是覆盖
func TestApplyWebSearchPatchAppends(t *testing.T) {
	body := map[string]any{"tools": []any{map[string]any{"type": "existing"}}}
	spec := InferModelSpec(Provider{Type: TypeGemini}, "gemini-2.5-flash")
	applyWebSearchPatch(body, buildWebSearchPatch(spec))
	got := toolTypes(t, body)
	if len(got) != 2 || got[0] != "existing" {
		t.Errorf("tools = %v, 原有工具应该保留在前", got)
	}
}

func TestParseCitations(t *testing.T) {
	t.Run("openai responses annotation", func(t *testing.T) {
		payload := `{"type":"response.output_text.annotation.added","annotation":{"type":"url_citation","url":"https://a.example/1","title":"A"}}`
		got := parseOpenAIResponsesCitations(payload)
		if len(got) != 1 || got[0].URL != "https://a.example/1" || got[0].Title != "A" {
			t.Errorf("got %#v", got)
		}
	})

	t.Run("openai chat annotations", func(t *testing.T) {
		payload := `{"choices":[{"delta":{"annotations":[{"type":"url_citation","url_citation":{"url":"https://b.example","title":"B"}}]}}]}`
		got := parseOpenAIChatCitations(payload)
		if len(got) != 1 || got[0].URL != "https://b.example" {
			t.Errorf("got %#v", got)
		}
	})

	t.Run("zhipu web_search", func(t *testing.T) {
		payload := `{"web_search":[{"title":"智谱","link":"https://c.example","content":"摘要"}]}`
		got := parseOpenAIChatCitations(payload)
		if len(got) != 1 || got[0].URL != "https://c.example" || got[0].Snippet != "摘要" {
			t.Errorf("got %#v", got)
		}
	})

	t.Run("dashscope search_info", func(t *testing.T) {
		payload := `{"search_info":{"search_results":[{"title":"","site_name":"站点","url":"https://d.example"}]}}`
		got := parseOpenAIChatCitations(payload)
		if len(got) != 1 || got[0].Title != "站点" {
			t.Errorf("got %#v", got)
		}
	})

	t.Run("gemini grounding", func(t *testing.T) {
		payload := `{"candidates":[{"groundingMetadata":{"groundingChunks":[{"web":{"uri":"https://e.example","title":"E"}}]}}]}`
		got := parseGeminiCitations(payload)
		if len(got) != 1 || got[0].URL != "https://e.example" {
			t.Errorf("got %#v", got)
		}
	})

	t.Run("非引用帧不产出", func(t *testing.T) {
		for _, payload := range []string{`{"type":"response.output_text.delta","delta":"hi"}`, `不是 JSON`} {
			if got := parseOpenAIResponsesCitations(payload); len(got) != 0 {
				t.Errorf("payload=%s 不该产出引用: %#v", payload, got)
			}
		}
	})
}

func TestDedupeCitations(t *testing.T) {
	got := dedupeCitations([]Citation{
		{URL: "https://a", Title: "1"},
		{URL: "https://a", Title: "2"},
		{URL: ""},
		{URL: "https://b"},
	})
	if len(got) != 2 || got[0].Title != "1" || got[1].URL != "https://b" {
		t.Errorf("got %#v", got)
	}
	if dedupeCitations(nil) != nil {
		t.Error("空输入应返回 nil")
	}
}

// Anthropic 的思考块必须连同 signature 原样回传;没有 signature 的历史数据要跳过而不是发坏请求
func TestBuildAnthropicMessagesThinkingRoundTrip(t *testing.T) {
	conv := Conversation{
		ModelID: "claude-sonnet-4-5",
		Messages: []Message{
			{Role: "user", Content: "问题"},
			{
				Role:     "assistant",
				Content:  "答案",
				Thinking: []ThinkingBlock{{Text: "想了想", Signature: "sig-1"}},
			},
			{Role: "user", Content: "追问"},
		},
	}
	spec := InferModelSpec(Provider{Type: TypeAnthropic}, "claude-sonnet-4-5")
	req := chatRequest{Conv: conv, Spec: spec}

	msgs := buildAnthropicMessages(req, true)
	raw, _ := json.Marshal(msgs)
	var decoded []map[string]any
	_ = json.Unmarshal(raw, &decoded)
	parts, ok := decoded[1]["content"].([]any)
	if !ok {
		t.Fatalf("assistant 消息应该是分块 content: %s", raw)
	}
	first := parts[0].(map[string]any)
	if first["type"] != "thinking" || first["signature"] != "sig-1" {
		t.Errorf("thinking 块必须排在最前且带 signature: %#v", first)
	}

	// 关闭思考时不该回传 thinking 块(会被拒)
	plain := buildAnthropicMessages(req, false)
	if _, isSlice := plain[1]["content"].([]map[string]any); isSlice {
		t.Error("未开启思考时不该发结构化 thinking 块")
	}

	// 只有纯文本、没有 signature 的历史思考块要被跳过
	conv.Messages[1].Thinking = []ThinkingBlock{{Text: "旧数据没有签名"}}
	legacy := buildAnthropicMessages(chatRequest{Conv: conv, Spec: spec}, true)
	if s, ok := legacy[1]["content"].(string); !ok || s != "答案" {
		t.Errorf("无签名的思考块应被跳过,退回纯文本: %#v", legacy[1]["content"])
	}
}
