package aichat

import (
	"encoding/json"
	"testing"
)

// emissionMap 把解析结果摊平成 target → value,方便断言
func emissionMap(r resolvedReasoning) map[string]any {
	out := map[string]any{}
	for _, e := range r.Emissions {
		out[e.Target] = e.Value
	}
	return out
}

func specFor(t *testing.T, providerType ProviderType, baseURL, modelID string) ModelSpec {
	t.Helper()
	return InferModelSpec(Provider{Type: providerType, BaseURL: baseURL}, modelID)
}

func TestResolveReasoningAnthropicBudget(t *testing.T) {
	spec := specFor(t, TypeAnthropic, "https://api.anthropic.com", "claude-sonnet-4-5-20250929")
	if !spec.CanTuneReasoning() {
		t.Fatalf("claude-sonnet-4-5 应该支持调思考档位, spec=%+v", spec)
	}
	if spec.MaxOutput != 64000 {
		t.Errorf("MaxOutput = %d, want 64000", spec.MaxOutput)
	}

	got := emissionMap(resolveReasoning(EffortHigh, spec, spec.MaxOutput))
	if got["thinking.type"] != "enabled" {
		t.Errorf("thinking.type = %v, want enabled", got["thinking.type"])
	}
	budget, ok := got["thinking.budget_tokens"].(int)
	if !ok {
		t.Fatalf("budget_tokens 缺失或类型不对: %#v", got["thinking.budget_tokens"])
	}
	// Anthropic 硬约束:1024 <= budget < max_tokens
	if budget < 1024 || budget >= spec.MaxOutput {
		t.Errorf("budget_tokens = %d, 必须落在 [1024, %d)", budget, spec.MaxOutput)
	}

	// 档位越高预算越大
	low := emissionMap(resolveReasoning(EffortLow, spec, spec.MaxOutput))["thinking.budget_tokens"].(int)
	if low >= budget {
		t.Errorf("low(%d) 的预算应该小于 high(%d)", low, budget)
	}
}

func TestResolveReasoningAnthropicOff(t *testing.T) {
	spec := specFor(t, TypeAnthropic, "https://api.anthropic.com", "claude-sonnet-4-5")
	got := emissionMap(resolveReasoning(EffortNone, spec, spec.MaxOutput))
	if got["thinking.type"] != "disabled" {
		t.Errorf("thinking.type = %v, want disabled", got["thinking.type"])
	}
	if _, ok := got["thinking.budget_tokens"]; ok {
		t.Error("关闭思考时不应该带 budget_tokens")
	}
}

// 输出上限小到装不下最小思考预算时,宁可整个不开思考,也不要发一个必然被拒的请求
func TestResolveReasoningAnthropicTinyMaxTokens(t *testing.T) {
	spec := specFor(t, TypeAnthropic, "https://api.anthropic.com", "claude-sonnet-4-5")
	r := resolveReasoning(EffortHigh, spec, 512)
	if r.Enabled() {
		t.Errorf("max_tokens=512 装不下 1024 的最小预算,应该放弃思考,却发了 %+v", r.Emissions)
	}
}

func TestResolveReasoningClaude35HasNoThinking(t *testing.T) {
	spec := specFor(t, TypeAnthropic, "https://api.anthropic.com", "claude-3-5-sonnet-20241022")
	if spec.CanTuneReasoning() {
		t.Error("claude-3.5 没有 extended thinking,不该给调档旋钮")
	}
	if r := resolveReasoning(EffortHigh, spec, spec.MaxOutput); r.Enabled() {
		t.Errorf("不该发思考参数: %+v", r.Emissions)
	}
}

func TestResolveReasoningOpenAIResponses(t *testing.T) {
	spec := specFor(t, TypeOpenAI, "https://api.openai.com/v1", "gpt-5")
	got := emissionMap(resolveReasoning(EffortHigh, spec, spec.MaxOutput))
	if got["reasoning.effort"] != EffortHigh {
		t.Errorf("reasoning.effort = %v, want high", got["reasoning.effort"])
	}
	// 官方端点必须带 summary,否则一个思考字都拿不到
	if got["reasoning.summary"] != "auto" {
		t.Errorf("reasoning.summary = %v, want auto", got["reasoning.summary"])
	}
}

// 第三方 Responses 中转不认 reasoning.summary,带上会 400
func TestResolveReasoningThirdPartyResponsesOmitsSummary(t *testing.T) {
	spec := specFor(t, TypeOpenAI, "https://my-relay.example.com/v1", "gpt-5")
	spec.family = familyGeneric // 显式模拟"猜不出家族"的中转
	got := emissionMap(resolveReasoning(EffortHigh, spec, spec.MaxOutput))
	if got["reasoning.effort"] != EffortHigh {
		t.Errorf("reasoning.effort = %v, want high", got["reasoning.effort"])
	}
	if _, ok := got["reasoning.summary"]; ok {
		t.Error("非官方端点不应该带 reasoning.summary")
	}
}

func TestGPT5DotOneUsesNoneNotMinimal(t *testing.T) {
	five := specFor(t, TypeOpenAI, "https://api.openai.com/v1", "gpt-5")
	if !containsEffort(five.Reasoning.Efforts, EffortMinimal) {
		t.Errorf("gpt-5 应该有 minimal 档: %v", five.Reasoning.Efforts)
	}
	fiveOne := specFor(t, TypeOpenAI, "https://api.openai.com/v1", "gpt-5.1")
	if !containsEffort(fiveOne.Reasoning.Efforts, EffortNone) {
		t.Errorf("gpt-5.1 应该有 none 档: %v", fiveOne.Reasoning.Efforts)
	}
	if containsEffort(fiveOne.Reasoning.Efforts, EffortMinimal) {
		t.Errorf("gpt-5.1 不该再有 minimal 档: %v", fiveOne.Reasoning.Efforts)
	}
}

func TestResolveReasoningGeminiBudgetVsLevel(t *testing.T) {
	g25 := specFor(t, TypeGemini, "", "gemini-2.5-flash")
	got := emissionMap(resolveReasoning(EffortMedium, g25, g25.MaxOutput))
	if got["generationConfig.thinkingConfig.includeThoughts"] != true {
		t.Error("2.5 开思考时必须打开 includeThoughts,否则拿不到思考文本")
	}
	if _, ok := got["generationConfig.thinkingConfig.thinkingBudget"].(int); !ok {
		t.Errorf("2.5 应该写 thinkingBudget(token 数): %#v", got)
	}

	g3 := specFor(t, TypeGemini, "", "gemini-3-pro-preview")
	got3 := emissionMap(resolveReasoning(EffortHigh, g3, g3.MaxOutput))
	if got3["generationConfig.thinkingConfig.thinkingLevel"] != EffortHigh {
		t.Errorf("3.x 应该写 thinkingLevel 档位词: %#v", got3)
	}
	if _, ok := got3["generationConfig.thinkingConfig.thinkingBudget"]; ok {
		t.Error("3.x 不认 thinkingBudget,不该发")
	}
}

// gemini-2.5-pro 的思考预算下限是 128,关不掉;不该给它 none 档
func TestGemini25ProCannotDisableThinking(t *testing.T) {
	spec := specFor(t, TypeGemini, "", "gemini-2.5-pro")
	if containsEffort(spec.Reasoning.Efforts, EffortNone) {
		t.Errorf("2.5-pro 关不掉思考,不该放出 none 档: %v", spec.Reasoning.Efforts)
	}
}

func TestResolveReasoningDefaultSendsNothing(t *testing.T) {
	spec := specFor(t, TypeAnthropic, "https://api.anthropic.com", "claude-sonnet-4-5")
	for _, sel := range []Effort{"", EffortDefault} {
		if r := resolveReasoning(sel, spec, spec.MaxOutput); r.Enabled() {
			t.Errorf("sel=%q 应该什么都不发,却发了 %+v", sel, r.Emissions)
		}
	}
}

// 换模型后,旧的档位选择投影到新模型支持的最近一档,而不是整个丢掉
func TestNearestEffortProjection(t *testing.T) {
	supported := []Effort{EffortLow, EffortHigh}
	if got := nearestEffort(EffortMedium, supported); got != EffortHigh {
		t.Errorf("medium 投影到 %v, want high(距离相同取高的一档)", got)
	}
	if got := nearestEffort(EffortMinimal, supported); got != EffortLow {
		t.Errorf("minimal 投影到 %v, want low", got)
	}
	if got := nearestEffort(EffortHigh, nil); got != "" {
		t.Errorf("模型没有可选档位时应返回空, got %q", got)
	}
}

// Claude 经 openai-compatible 中转访问时,anthropic 的 thinking 字段发过去没人认
func TestDialectReconciledByEndpoint(t *testing.T) {
	spec := specFor(t, TypeOpenAICompat, "https://relay.example.com/v1", "claude-sonnet-4-5")
	if r := resolveReasoning(EffortHigh, spec, spec.MaxOutput); r.Enabled() {
		t.Errorf("chat-completions 端点不该发 anthropic 的 thinking 字段: %+v", r.Emissions)
	}
}

func TestApplyEmissionsNestedPath(t *testing.T) {
	body := map[string]any{"model": "x"}
	applyEmissions(body, []reasoningEmission{
		{Target: "generationConfig.thinkingConfig.thinkingBudget", Value: 1234},
		{Target: "generationConfig.temperature", Value: 0.5},
	})
	gc, ok := body["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("generationConfig 没被建出来: %#v", body)
	}
	tc, ok := gc["thinkingConfig"].(map[string]any)
	if !ok {
		t.Fatalf("thinkingConfig 没被建出来: %#v", gc)
	}
	if tc["thinkingBudget"] != 1234 {
		t.Errorf("thinkingBudget = %v", tc["thinkingBudget"])
	}
	if gc["temperature"] != 0.5 {
		t.Errorf("同层的 temperature 被覆盖了: %#v", gc)
	}
	if body["model"] != "x" {
		t.Error("原有字段不该被动")
	}
}

func TestReasoningTagName(t *testing.T) {
	cases := map[string]string{
		"gpt-oss-120b":            "reasoning",
		"seed-oss-36b-instruct":   "seed:think",
		"deepseek-ai/DeepSeek-R1": "think",
		"":                        "think",
	}
	for id, want := range cases {
		if got := reasoningTagName(id); got != want {
			t.Errorf("reasoningTagName(%q) = %q, want %q", id, got, want)
		}
	}
}

// 旧会话文件里 thinking 是一个字符串,读进来要能升格成块数组
func TestMessageThinkingBackwardCompat(t *testing.T) {
	var legacy Message
	if err := json.Unmarshal([]byte(`{"id":"1","role":"assistant","content":"hi","thinking":"想了想"}`), &legacy); err != nil {
		t.Fatalf("旧格式解析失败: %v", err)
	}
	if len(legacy.Thinking) != 1 || legacy.Thinking[0].Text != "想了想" {
		t.Fatalf("旧格式没升格成单元素数组: %#v", legacy.Thinking)
	}

	var modern Message
	raw := `{"id":"2","role":"assistant","content":"hi","thinking":[{"text":"a","signature":"sig"},{"text":"b"}]}`
	if err := json.Unmarshal([]byte(raw), &modern); err != nil {
		t.Fatalf("新格式解析失败: %v", err)
	}
	if len(modern.Thinking) != 2 || modern.Thinking[0].Signature != "sig" {
		t.Fatalf("新格式解析结果不对: %#v", modern.Thinking)
	}
	if modern.ThinkingText() != "ab" {
		t.Errorf("ThinkingText() = %q, want ab", modern.ThinkingText())
	}

	// 空 / 缺失都不该报错
	for _, raw := range []string{`{"id":"3","role":"user","content":"x"}`, `{"id":"4","role":"user","content":"x","thinking":null}`} {
		var m Message
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Errorf("解析 %s 失败: %v", raw, err)
		}
		if m.Thinking != nil {
			t.Errorf("%s 不该产生思考块: %#v", raw, m.Thinking)
		}
	}
}

func containsEffort(list []Effort, want Effort) bool {
	for _, e := range list {
		if e == want {
			return true
		}
	}
	return false
}
