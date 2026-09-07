package aichat

import "testing"

// 家族判断只认主机名:用户选了哪个类型不代表他连的就是原厂
func TestInferFamilyIsHostBased(t *testing.T) {
	cases := []struct {
		name     string
		provider Provider
		want     providerFamily
	}{
		{"官方 OpenAI", Provider{Type: TypeOpenAI, BaseURL: "https://api.openai.com/v1"}, familyOpenAI},
		{"类型选 OpenAI 但连中转", Provider{Type: TypeOpenAI, BaseURL: "https://relay.example.com/v1"}, familyGeneric},
		{"类型选 Anthropic 但连中转", Provider{Type: TypeAnthropic, BaseURL: "https://relay.example.com"}, familyGeneric},
		{"官方 Anthropic", Provider{Type: TypeAnthropic, BaseURL: "https://api.anthropic.com"}, familyAnthropic},
		{"官方 xAI", Provider{Type: TypeXAI, BaseURL: "https://api.x.ai/v1"}, familyXAI},
		{"子域名也算原厂", Provider{Type: TypeOpenAI, BaseURL: "https://eu.api.openai.com/v1"}, familyOpenAI},
		{"没填地址按类型默认端点", Provider{Type: TypeGemini}, familyGoogle},
		{"没填地址且类型未知", Provider{Type: TypeOpenAICompat}, familyGeneric},
		{"按主机名认出 DeepSeek", Provider{Type: TypeOpenAICompat, BaseURL: "https://api.deepseek.com"}, familyDeepSeek},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inferFamily(tc.provider); got != tc.want {
				t.Errorf("inferFamily = %q, want %q", got, tc.want)
			}
		})
	}
}

// 中转给模型改了名 → 用别名指回标准 ID,一次拿回全部推断结果
func TestModelOverrideAlias(t *testing.T) {
	p := Provider{Type: TypeAnthropic, BaseURL: "https://api.anthropic.com"}
	bare := InferModelSpec(p, "my-claude-pro")
	if bare.CanTuneReasoning() {
		t.Fatal("改过名的模型本来就该推不出思考能力,否则这个测试没意义")
	}

	p.ModelOverrides = map[string]ModelOverride{
		"my-claude-pro": {AliasOf: "claude-sonnet-4-5"},
	}
	spec := InferModelSpec(p, "my-claude-pro")
	if spec.ID != "my-claude-pro" {
		t.Errorf("对外仍应是真实模型 ID, got %q", spec.ID)
	}
	if !spec.CanTuneReasoning() {
		t.Errorf("别名指向后应该拿到思考档位: %+v", spec.Reasoning)
	}
	if spec.MaxOutput != 64000 {
		t.Errorf("MaxOutput = %d, want 64000(跟着别名一起来)", spec.MaxOutput)
	}
	if !spec.Has(CapWebSearch) {
		t.Error("别名指向后应该拿到联网能力")
	}
	// 思考参数也要能真的发出去
	if r := resolveReasoning(EffortHigh, spec, spec.MaxOutput); !r.Enabled() {
		t.Error("别名指向后思考参数应该能发出去")
	}
}

// 推断多给了能力时,用户要能显式关掉(空集也是合法答案)
func TestModelOverrideCanClearCapabilities(t *testing.T) {
	p := Provider{
		Type:    TypeOpenAI,
		BaseURL: "https://api.openai.com/v1",
		ModelOverrides: map[string]ModelOverride{
			"gpt-5": {CapabilitiesSet: true, Capabilities: []Capability{CapVision}},
		},
	}
	spec := InferModelSpec(p, "gpt-5")
	if spec.Has(CapWebSearch) {
		t.Error("用户显式去掉了联网,不该再有")
	}
	if !spec.Has(CapVision) {
		t.Error("用户保留的 vision 应该还在")
	}
	// 思考也被关掉了 → 旋钮一并收掉,不留按了没反应的开关
	if spec.CanTuneReasoning() {
		t.Errorf("思考能力被关掉后不该还有档位: %+v", spec.Reasoning)
	}
	if r := resolveReasoning(EffortHigh, spec, spec.MaxOutput); r.Enabled() {
		t.Errorf("不该再发思考参数: %+v", r.Emissions)
	}
}

// 用户说这个中转支持联网 —— 即便我们的保守兜底摘掉了它,也以用户为准
func TestModelOverrideCanForceWebSearch(t *testing.T) {
	p := Provider{
		Type:    TypeOpenAICompat,
		BaseURL: "https://relay.example.com/v1",
		ModelOverrides: map[string]ModelOverride{
			"mystery-model": {CapabilitiesSet: true, Capabilities: []Capability{CapWebSearch}},
		},
	}
	spec := InferModelSpec(p, "mystery-model")
	if !spec.Has(CapWebSearch) {
		t.Fatal("用户强开的联网应该保留")
	}
	body := map[string]any{}
	applyWebSearchPatch(body, buildWebSearchPatch(spec))
	if _, ok := body["web_search_options"]; !ok {
		t.Errorf("强开后应该真的往请求体里写东西: %#v", body)
	}
}

func TestModelOverrideMaxOutputClampsBudget(t *testing.T) {
	p := Provider{
		Type:    TypeAnthropic,
		BaseURL: "https://api.anthropic.com",
		ModelOverrides: map[string]ModelOverride{
			"claude-sonnet-4-5": {MaxOutput: 8192},
		},
	}
	spec := InferModelSpec(p, "claude-sonnet-4-5")
	if spec.MaxOutput != 8192 {
		t.Fatalf("MaxOutput = %d, want 8192", spec.MaxOutput)
	}
	if spec.Reasoning.BudgetMax > 8192 {
		t.Errorf("思考预算上限 %d 应该跟着压到 8192 以内", spec.Reasoning.BudgetMax)
	}
	budget := emissionMap(resolveReasoning(EffortHigh, spec, spec.MaxOutput))["thinking.budget_tokens"].(int)
	if budget >= 8192 {
		t.Errorf("预算 %d 必须严格小于 max_tokens 8192", budget)
	}
}

func TestNoOverrideLeavesInferenceAlone(t *testing.T) {
	p := Provider{Type: TypeAnthropic, BaseURL: "https://api.anthropic.com"}
	want := InferModelSpec(p, "claude-sonnet-4-5")
	p.ModelOverrides = map[string]ModelOverride{"别的模型": {MaxOutput: 1}}
	got := InferModelSpec(p, "claude-sonnet-4-5")
	if got.MaxOutput != want.MaxOutput || len(got.Capabilities) != len(want.Capabilities) {
		t.Errorf("其他模型的覆盖不该影响这个模型: got %+v want %+v", got, want)
	}
}
