package aichat

import "testing"

func f64(v float64) *float64 { return &v }

func sampledBody(p Provider, modelID string, conv Conversation, reasoningOn bool) (map[string]any, ModelSpec) {
	spec := InferModelSpec(p, modelID)
	conv.ModelID = modelID
	body := map[string]any{}
	applySampling(body, conv, spec, reasoningOn)
	return body, spec
}

// temperature = 0 是合法取值(完全确定性输出),不能被当成"没设置"吞掉
func TestSamplingZeroTemperatureIsSent(t *testing.T) {
	p := Provider{Type: TypeOpenAICompat, BaseURL: "https://api.deepseek.com"}
	body, _ := sampledBody(p, "deepseek-chat", Conversation{Temperature: f64(0)}, false)
	v, ok := body["temperature"]
	if !ok {
		t.Fatalf("temperature=0 应该被发出去: %#v", body)
	}
	if v != 0.0 {
		t.Errorf("temperature = %v, want 0", v)
	}
}

func TestSamplingUnsetSendsNothing(t *testing.T) {
	p := Provider{Type: TypeOpenAICompat, BaseURL: "https://api.deepseek.com"}
	body, _ := sampledBody(p, "deepseek-chat", Conversation{}, false)
	if len(body) != 0 {
		t.Errorf("什么都没设时请求体不该多字段: %#v", body)
	}
}

// o 系列 / gpt-5 拒收采样参数,发过去会报错
func TestSamplingSkippedForOpenAIReasoningModels(t *testing.T) {
	p := Provider{Type: TypeOpenAI, BaseURL: "https://api.openai.com/v1"}
	conv := Conversation{Temperature: f64(0.7), TopP: f64(0.9), MaxTokens: 4096}
	body, spec := sampledBody(p, "gpt-5", conv, false)
	if spec.Sampling.Temperature || spec.Sampling.TopP {
		t.Fatal("gpt-5 不该声明支持采样参数")
	}
	if _, ok := body["temperature"]; ok {
		t.Errorf("不该发 temperature: %#v", body)
	}
	if _, ok := body["top_p"]; ok {
		t.Errorf("不该发 top_p: %#v", body)
	}
	// 输出上限仍然要发,只是 Responses 端点用的是 max_output_tokens
	if body["max_output_tokens"] != 4096 {
		t.Errorf("max_output_tokens = %v, want 4096", body["max_output_tokens"])
	}
}

// chat-completions 上,o 系列 / gpt-5 只认 max_completion_tokens,发 max_tokens 会被拒
func TestSamplingMaxTokensFieldName(t *testing.T) {
	cases := []struct {
		name     string
		provider Provider
		model    string
		wantKey  string
	}{
		{
			"OpenAI 思考模型走 chat-completions",
			Provider{Type: TypeOpenAICompat, BaseURL: "https://api.openai.com/v1"},
			"gpt-5",
			"max_completion_tokens",
		},
		{
			"普通 chat-completions 模型",
			Provider{Type: TypeOpenAICompat, BaseURL: "https://api.deepseek.com"},
			"deepseek-chat",
			"max_tokens",
		},
		{
			"Responses 端点",
			Provider{Type: TypeOpenAI, BaseURL: "https://api.openai.com/v1"},
			"gpt-4o",
			"max_output_tokens",
		},
		{
			"Gemini 在 generationConfig 下",
			Provider{Type: TypeGemini},
			"gemini-2.5-flash",
			"generationConfig.maxOutputTokens",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := sampledBody(tc.provider, tc.model, Conversation{MaxTokens: 2048}, false)
			var got any
			if tc.wantKey == "generationConfig.maxOutputTokens" {
				gc, _ := body["generationConfig"].(map[string]any)
				got = gc["maxOutputTokens"]
			} else {
				got = body[tc.wantKey]
			}
			if got != 2048 {
				t.Errorf("%s = %v, want 2048 (body=%#v)", tc.wantKey, got, body)
			}
		})
	}
}

// Claude 开着 extended thinking 时 temperature 只能是 1,与其猜不如不发
func TestSamplingOmittedWhenAnthropicThinking(t *testing.T) {
	p := Provider{Type: TypeAnthropic, BaseURL: "https://api.anthropic.com"}
	conv := Conversation{Temperature: f64(0.3), TopP: f64(0.8)}

	off, _ := sampledBody(p, "claude-sonnet-4-5", conv, false)
	if off["temperature"] != 0.3 {
		t.Errorf("没开思考时应该正常发 temperature: %#v", off)
	}

	on, _ := sampledBody(p, "claude-sonnet-4-5", conv, true)
	if _, ok := on["temperature"]; ok {
		t.Errorf("开着思考时不该发 temperature: %#v", on)
	}
	if _, ok := on["top_p"]; ok {
		t.Errorf("开着思考时不该发 top_p: %#v", on)
	}
}

// Anthropic 的 max_tokens 是必填项,由 streamAnthropic 自己写,applySampling 不能重复插手
func TestSamplingLeavesAnthropicMaxTokensAlone(t *testing.T) {
	p := Provider{Type: TypeAnthropic, BaseURL: "https://api.anthropic.com"}
	body, _ := sampledBody(p, "claude-sonnet-4-5", Conversation{MaxTokens: 4096}, false)
	if _, ok := body["max_tokens"]; ok {
		t.Errorf("Anthropic 的 max_tokens 归 streamAnthropic 管,这里不该写: %#v", body)
	}
}

// 各家 temperature 上限不同,超了要夹住而不是原样发过去被拒
func TestSamplingClampsToModelRange(t *testing.T) {
	claude := Provider{Type: TypeAnthropic, BaseURL: "https://api.anthropic.com"}
	body, spec := sampledBody(claude, "claude-sonnet-4-5", Conversation{Temperature: f64(1.8)}, false)
	if spec.Sampling.MaxTemp != 1 {
		t.Fatalf("Claude 的 temperature 上限应该是 1, got %v", spec.Sampling.MaxTemp)
	}
	if body["temperature"] != 1.0 {
		t.Errorf("超上限的 temperature 应被夹到 1, got %v", body["temperature"])
	}

	// top_p 恒定在 [0,1]
	tp, _ := sampledBody(claude, "claude-sonnet-4-5", Conversation{TopP: f64(2)}, false)
	if tp["top_p"] != 1.0 {
		t.Errorf("top_p 应被夹到 1, got %v", tp["top_p"])
	}
}

func TestKimiK25FixedSampling(t *testing.T) {
	p := Provider{Type: TypeOpenAICompat, BaseURL: "https://api.moonshot.cn/v1"}
	if spec := InferModelSpec(p, "kimi-k2.5-turbo"); spec.Sampling.Temperature {
		t.Error("Kimi K2.5 起采样被锁死,不该声明支持")
	}
	if spec := InferModelSpec(p, "moonshot-v1-8k"); !spec.Sampling.Temperature {
		t.Error("老型号 Kimi 仍然支持 temperature")
	}
}

func TestEffectiveMaxTokens(t *testing.T) {
	spec := InferModelSpec(Provider{Type: TypeAnthropic}, "claude-sonnet-4-5")
	if got := effectiveMaxTokens(Conversation{}, spec); got != spec.MaxOutput {
		t.Errorf("没设时应该用模型推断值 %d, got %d", spec.MaxOutput, got)
	}
	if got := effectiveMaxTokens(Conversation{MaxTokens: 1000}, spec); got != 1000 {
		t.Errorf("用户设了就用他的, got %d", got)
	}
	if got := effectiveMaxTokens(Conversation{}, ModelSpec{}); got != defaultMaxOutput {
		t.Errorf("两边都没有时回落到默认值 %d, got %d", defaultMaxOutput, got)
	}
}

// 用户把输出上限调小时,思考预算要跟着一起压下来(Anthropic 要求 budget < max_tokens)
func TestUserMaxTokensClampsThinkingBudget(t *testing.T) {
	p := Provider{Type: TypeAnthropic, BaseURL: "https://api.anthropic.com"}
	spec := InferModelSpec(p, "claude-sonnet-4-5")
	conv := Conversation{ModelID: "claude-sonnet-4-5", ReasoningEffort: EffortHigh, MaxTokens: 4096}

	r := resolveReasoning(conv.ReasoningEffort, spec, effectiveMaxTokens(conv, spec))
	if !r.Enabled() {
		t.Fatal("4096 的上限仍然装得下思考,应该开")
	}
	if r.Budget >= 4096 {
		t.Errorf("思考预算 %d 必须严格小于 max_tokens 4096", r.Budget)
	}
}
