package aichat

import (
	"net/url"
	"strings"
)

// 能力目录:把"这个模型能干什么、它的思考/联网参数该往哪写"从流式代码里抽出来。
//
// 分三层:
//
//	EndpointType    线上协议形状(请求体长什么样)—— 决定 build/parse 走哪套
//	providerFamily  厂商家族 —— 决定同一端点下的方言差异(联网参数、思考字段名)
//	ModelSpec       单个模型的能力与思考规格 —— 决定 UI 给不给开关、发不发参数
//
// 新增一家供应商 = 在这里补一条推断规则 + 在 reasoning.go 补一份 wire profile,
// 不需要动 openai.go / anthropic.go / gemini.go 的流式循环。

// EndpointType 请求走哪套线上协议。与 ProviderType 是多对一:同一家可能有多个端点
// (DeepSeek 同时提供 chat-completions 与 responses),拆开是为了让思考 / 联网参数
// 按端点而不是按厂商决定。
type EndpointType = string

const (
	EndpointOpenAIResponses EndpointType = "openai-responses"
	EndpointOpenAIChat      EndpointType = "openai-chat"
	EndpointAnthropic       EndpointType = "anthropic-messages"
	EndpointGemini          EndpointType = "google-generate-content"
)

// endpointFor Provider.Type → 端点协议
func endpointFor(t ProviderType) EndpointType {
	switch t {
	case TypeOpenAICompat:
		return EndpointOpenAIChat
	case TypeAnthropic:
		return EndpointAnthropic
	case TypeGemini:
		return EndpointGemini
	case TypeXAI:
		return EndpointOpenAIResponses
	default:
		// 空值(旧数据)与 "openai" 都按新版 Responses API 处理
		return EndpointOpenAIResponses
	}
}

// providerFamily 厂商家族;同一个端点下各家的方言差异靠它区分。
// 对用户自建的 openai-compatible 中转,靠 baseURL 主机名猜;猜不出就是 generic。
type providerFamily string

const (
	familyGeneric   providerFamily = ""
	familyOpenAI    providerFamily = "openai"
	familyAnthropic providerFamily = "anthropic"
	familyGoogle    providerFamily = "google"
	familyXAI       providerFamily = "xai"
	familyDeepSeek  providerFamily = "deepseek"
	familyZhipu     providerFamily = "zhipu"
	familyDashScope providerFamily = "dashscope"
	familyMoonshot  providerFamily = "moonshot"
)

// hostFamilies baseURL 主机名 → 家族。只列"内置联网参数和标准 OpenAI 不一样"的几家,
// 其余中转一律 generic(按标准 OpenAI 兼容协议处理)。
var hostFamilies = []struct {
	host   string
	family providerFamily
}{
	{"api.deepseek.com", familyDeepSeek},
	{"open.bigmodel.cn", familyZhipu},
	{"bigmodel.cn", familyZhipu},
	{"dashscope.aliyuncs.com", familyDashScope},
	{"api.moonshot.cn", familyMoonshot},
	{"api.moonshot.ai", familyMoonshot},
	{"api.x.ai", familyXAI},
	{"api.openai.com", familyOpenAI},
	{"api.anthropic.com", familyAnthropic},
	{"generativelanguage.googleapis.com", familyGoogle},
}

// inferFamily 判断这个供应商是不是"原厂"。
//
// 只认 baseURL 主机名,不认用户在下拉框里选的类型 —— "类型选 OpenAI、地址填中转"是最常见的
// 配置,那种情况必须按中转处理:原厂专有字段(reasoning.summary、Anthropic 的 cache_control)
// 发给中转经常直接 400。baseURL 为空时才按类型的默认端点算,那是真的原厂。
func inferFamily(p Provider) providerFamily {
	host := ""
	if u, err := url.Parse(strings.TrimSpace(p.BaseURL)); err == nil {
		host = strings.ToLower(u.Hostname())
	}
	if host == "" {
		return defaultFamilyFor(p.Type)
	}
	for _, hf := range hostFamilies {
		if host == hf.host || strings.HasSuffix(host, "."+hf.host) {
			return hf.family
		}
	}
	return familyGeneric
}

// defaultFamilyFor 没填 baseURL 时,各协议类型走的是自家默认端点
func defaultFamilyFor(t ProviderType) providerFamily {
	switch t {
	case TypeAnthropic:
		return familyAnthropic
	case TypeGemini:
		return familyGoogle
	case TypeXAI:
		return familyXAI
	case TypeOpenAI:
		return familyOpenAI
	}
	return familyGeneric
}

// Capability 模型能力标签;前端据此决定给不给对应的开关
type Capability = string

const (
	CapVision    Capability = "vision"    // 能看图
	CapPDF       Capability = "pdf"       // 能原生吃 PDF(不用后端先抽文本)
	CapReasoning Capability = "reasoning" // 会思考(会思考 ≠ 能调档,能否调档看 ReasoningSpec)
	CapWebSearch Capability = "webSearch" // 支持供应商内置联网搜索
	CapTools     Capability = "tools"     // 支持工具调用(function calling)
	CapImageGen  Capability = "imageGen"  // 能生图
)

// Effort 思考档位。这是我们自己的统一词汇表,各协议的 wire profile 负责把它翻译成
// 自家的字段(reasoning_effort / budget_tokens / thinkingLevel / ...)。
type Effort = string

const (
	EffortDefault Effort = "default" // 什么都不发,由供应商自己决定
	EffortNone    Effort = "none"    // 显式关闭思考
	EffortMinimal Effort = "minimal"
	EffortLow     Effort = "low"
	EffortMedium  Effort = "medium"
	EffortHigh    Effort = "high"
)

// effortRank 档位强度序;用于把用户选的档位投影到模型实际支持的最近一档
// (换模型后旧选择仍然可用,不至于把 high 直接丢掉)
var effortRank = map[Effort]int{
	EffortNone:    0,
	EffortMinimal: 1,
	EffortLow:     2,
	EffortMedium:  3,
	EffortHigh:    4,
}

// effortRatio 预算制协议(Anthropic / Gemini 2.x / 通义)把档位换算成 token 预算的比例
var effortRatio = map[Effort]float64{
	EffortMinimal: 0.05,
	EffortLow:     0.05,
	EffortMedium:  0.5,
	EffortHigh:    0.8,
}

// ReasoningSpec 一个模型的思考规格
type ReasoningSpec struct {
	// Efforts 该模型可选的档位(不含 default;含 none 表示可以显式关思考)。
	// 空数组 = 会思考但没有调节旋钮(如 deepseek-reasoner / grok-4)
	Efforts []Effort `json:"efforts"`
	// Default 模型自身的默认档位,预算制协议用它把"没明确选"落到具体数值
	Default Effort `json:"default,omitempty"`
	// BudgetMin/BudgetMax 思考 token 预算区间;BudgetMax > 0 表示这是预算制协议
	BudgetMin int `json:"budgetMin,omitempty"`
	BudgetMax int `json:"budgetMax,omitempty"`
}

// ModelSpec 单个模型在某个供应商下的能力画像。
// 由 InferModelSpec 按模型 ID 前缀推断 —— 中转千奇百怪,推断只是尽力而为;
// 推不出来就保守地什么都不声明(UI 不给开关、请求不带参数),不会因为猜错而发坏请求。
type ModelSpec struct {
	ID           string         `json:"id"`
	Endpoint     EndpointType   `json:"endpoint"`
	Capabilities []Capability   `json:"capabilities"`
	Reasoning    *ReasoningSpec `json:"reasoning,omitempty"`
	// Sampling 这个模型接受哪些采样参数(见 sampling.go)
	Sampling SamplingSpec `json:"sampling"`
	// MaxOutput 单次回复的最大 token 数;Anthropic 必须显式带,其余协议用来给思考预算封顶
	MaxOutput int `json:"maxOutput"`

	// 以下为包内路由用,不出前端
	family  providerFamily
	dialect reasoningDialect
	// useMaxCompletionTokens chat-completions 端点上该发 max_completion_tokens 而不是 max_tokens
	useMaxCompletionTokens bool
}

// Has 是否具备某项能力
func (m ModelSpec) Has(c Capability) bool {
	for _, x := range m.Capabilities {
		if x == c {
			return true
		}
	}
	return false
}

// CanTuneReasoning 是否有可调的思考档位(会思考 ≠ 能调档)
func (m ModelSpec) CanTuneReasoning() bool {
	return m.Reasoning != nil && len(m.Reasoning.Efforts) > 0
}

// defaultMaxOutput 推不出模型上限时的保守值
const defaultMaxOutput = 8192

// normalizeModelID 去掉中转常见的命名空间前缀("openai/gpt-5"、"accounts/fireworks/models/xxx"),
// 统一成小写裸 id 用于前缀匹配
func normalizeModelID(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// hasAnyPrefix 前缀命中任一即真
func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// hasAny 子串命中任一即真
func hasAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

// InferModelSpec 推断一个模型在给定供应商下的能力画像。
// 最后会套用用户在 Provider.ModelOverrides 里的手动修正 —— 用户的判断优先于我们的猜测。
func InferModelSpec(p Provider, modelID string) ModelSpec {
	ov, hasOverride := p.ModelOverrides[modelID]
	// 别名:中转给模型改了名时,按用户指定的标准 ID 推断,
	// 一次拿到正确的思考方言 / 预算区间 / 输出上限,不用一项项手勾
	inferID := modelID
	if hasOverride && ov.AliasOf != "" {
		inferID = ov.AliasOf
	}
	id := normalizeModelID(inferID)
	spec := ModelSpec{
		ID:           modelID,
		Endpoint:     endpointFor(p.Type),
		Capabilities: []Capability{},
		Sampling:     defaultSampling(),
		MaxOutput:    defaultMaxOutput,
		family:       inferFamily(p),
	}

	switch {
	case hasAnyPrefix(id, "claude"):
		applyClaudeSpec(&spec, id)
	case hasAnyPrefix(id, "gemini"):
		applyGeminiSpec(&spec, id)
	case hasAnyPrefix(id, "grok"):
		applyGrokSpec(&spec, id)
	case hasAnyPrefix(id, "gpt-", "o1", "o3", "o4", "chatgpt"):
		applyOpenAISpec(&spec, id)
	case hasAnyPrefix(id, "deepseek"):
		applyDeepSeekSpec(&spec, id)
	case hasAnyPrefix(id, "qwen", "qwq", "qvq"):
		applyQwenSpec(&spec, id)
	case hasAnyPrefix(id, "glm", "chatglm"):
		applyZhipuSpec(&spec, id)
	case hasAnyPrefix(id, "kimi", "moonshot"):
		applyMoonshotSpec(&spec, id)
	default:
		applyGenericSpec(&spec, id)
	}

	// 端点兜底:同一个模型经不同端点访问,思考方言按端点重算。
	// 例如 Claude 经某个 openai-compatible 中转时,anthropic 的 thinking 字段发过去没人认。
	spec.dialect = reconcileDialect(spec.Endpoint, spec.dialect)
	if spec.dialect == dialectNone && spec.Reasoning != nil {
		// 仍然保留"会思考"的标签(前端要显示思考块),但把旋钮收掉
		spec.Reasoning = &ReasoningSpec{}
	}
	// 原生 PDF:只有这三种端点有对应的多模态字段
	if spec.Endpoint != EndpointOpenAIChat {
		spec.Capabilities = appendCap(spec.Capabilities, CapPDF)
	}
	// 联网:模型自称支持还不够,端点也得有对应的 tool 形状
	if spec.Has(CapWebSearch) && !endpointSupportsWebSearch(spec.Endpoint, spec.family, id) {
		spec.Capabilities = removeCap(spec.Capabilities, CapWebSearch)
	}
	// 用户的手动修正放在最后 —— 包括上面那些保守的兜底,他说了算。
	// 用户比我们更清楚自己那个中转到底支持什么。
	if hasOverride {
		applyModelOverride(&spec, ov)
	}
	return spec
}

// applyModelOverride 把用户的手动修正盖到推断结果上
func applyModelOverride(s *ModelSpec, ov ModelOverride) {
	if ov.CapabilitiesSet {
		s.Capabilities = append([]Capability{}, ov.Capabilities...)
		switch {
		case !s.Has(CapReasoning):
			// 手动关掉思考:旋钮一并收掉,免得留一个按了没反应的开关
			s.Reasoning = nil
			s.dialect = dialectNone
		case s.Reasoning == nil:
			// 手动打开思考但我们推不出方言:思考内容仍能靠 <think> 标签
			// 或 reasoning_content 字段收到,但没有可发的档位参数
			s.Reasoning = &ReasoningSpec{}
		}
	}
	if ov.MaxOutput > 0 {
		s.MaxOutput = ov.MaxOutput
		// 思考预算不能超过输出上限,跟着一起压下来
		if s.Reasoning != nil && s.Reasoning.BudgetMax > ov.MaxOutput {
			s.Reasoning.BudgetMax = ov.MaxOutput
		}
	}
}

func appendCap(caps []Capability, c Capability) []Capability {
	for _, x := range caps {
		if x == c {
			return caps
		}
	}
	return append(caps, c)
}

func removeCap(caps []Capability, c Capability) []Capability {
	out := make([]Capability, 0, len(caps))
	for _, x := range caps {
		if x != c {
			out = append(out, x)
		}
	}
	return out
}

// ---- 各家能力表 ----

// applyClaudeSpec Claude:思考走 token 预算制,max_tokens 必须显式带且要大于预算
func applyClaudeSpec(s *ModelSpec, id string) {
	s.Capabilities = appendCap(s.Capabilities, CapVision)
	s.Capabilities = appendCap(s.Capabilities, CapTools)
	s.Sampling = cappedSampling()
	switch {
	case hasAny(id, "opus-4"):
		s.MaxOutput = 32000
	case hasAny(id, "sonnet-4", "haiku-4", "3-7-sonnet"):
		s.MaxOutput = 64000
	case hasAny(id, "3-5-"):
		s.MaxOutput = 8192
	case hasAny(id, "claude-3-"):
		s.MaxOutput = 4096
	default:
		s.MaxOutput = 8192
	}

	// extended thinking 从 3.7 开始有,3.5 及以下没有
	if hasAny(id, "3-7-", "sonnet-4", "opus-4", "haiku-4") {
		s.Capabilities = appendCap(s.Capabilities, CapReasoning)
		s.dialect = dialectAnthropicBudget
		s.Reasoning = &ReasoningSpec{
			Efforts:   []Effort{EffortNone, EffortLow, EffortMedium, EffortHigh},
			Default:   EffortMedium,
			BudgetMin: 1024,
			// 预算上限压在 max_tokens 之下;resolveReasoning 还会按实际 max_tokens 再夹一次
			BudgetMax: s.MaxOutput,
		}
	}

	// 内置联网:3.5 haiku / 3.5 sonnet / 3.7 / 4 系列
	if hasAny(id, "3-5-haiku", "3-5-sonnet", "3-7-sonnet", "sonnet-4", "opus-4", "haiku-4") {
		s.Capabilities = appendCap(s.Capabilities, CapWebSearch)
	}
}

// applyGeminiSpec Gemini:2.x 用 thinkingBudget(token 数),3.x 换成 thinkingLevel(档位词)
func applyGeminiSpec(s *ModelSpec, id string) {
	s.Capabilities = appendCap(s.Capabilities, CapVision)
	s.Capabilities = appendCap(s.Capabilities, CapTools)
	s.MaxOutput = 8192

	switch {
	case hasAnyPrefix(id, "gemini-3"):
		s.Capabilities = appendCap(s.Capabilities, CapReasoning)
		s.dialect = dialectGeminiLevel
		s.Reasoning = &ReasoningSpec{
			Efforts: []Effort{EffortNone, EffortLow, EffortHigh},
			Default: EffortHigh,
		}
		s.MaxOutput = 65536
	case hasAnyPrefix(id, "gemini-2.5", "gemini-2-5"):
		s.Capabilities = appendCap(s.Capabilities, CapReasoning)
		s.dialect = dialectGeminiBudget
		// flash / flash-lite 允许把预算调到 0 彻底关思考;pro 不允许(下限 128)
		budgetMin, budgetMax := 0, 24576
		if hasAny(id, "pro") {
			budgetMin, budgetMax = 128, 32768
		}
		efforts := []Effort{EffortNone, EffortLow, EffortMedium, EffortHigh}
		if budgetMin > 0 {
			efforts = []Effort{EffortLow, EffortMedium, EffortHigh}
		}
		s.Reasoning = &ReasoningSpec{
			Efforts:   efforts,
			Default:   EffortMedium,
			BudgetMin: budgetMin,
			BudgetMax: budgetMax,
		}
		s.MaxOutput = 65536
	}
	if hasAny(id, "image") {
		s.Capabilities = appendCap(s.Capabilities, CapImageGen)
	}
	// 内置 google_search:2.x 起
	if hasAnyPrefix(id, "gemini-2", "gemini-3", "gemini-flash", "gemini-pro") {
		s.Capabilities = appendCap(s.Capabilities, CapWebSearch)
	}
}

// applyGrokSpec Grok:走 Responses 端点;联网是 web_search + x_search 两个内置工具
func applyGrokSpec(s *ModelSpec, id string) {
	s.Capabilities = appendCap(s.Capabilities, CapVision)
	s.Capabilities = appendCap(s.Capabilities, CapTools)
	s.MaxOutput = 32768
	s.family = familyXAI
	switch {
	case hasAnyPrefix(id, "grok-4"):
		// grok-4 会思考,但官方明确不接受 reasoning_effort —— 只声明能力,不给旋钮
		s.Capabilities = appendCap(s.Capabilities, CapReasoning)
		s.dialect = dialectNone
		s.Reasoning = &ReasoningSpec{}
	case hasAny(id, "mini"):
		s.Capabilities = appendCap(s.Capabilities, CapReasoning)
		s.dialect = dialectOpenAIEffort
		s.Reasoning = &ReasoningSpec{
			Efforts: []Effort{EffortLow, EffortHigh},
			Default: EffortLow,
		}
	}
	if hasAny(id, "image") {
		s.Capabilities = appendCap(s.Capabilities, CapImageGen)
	}
	if hasAnyPrefix(id, "grok-3", "grok-4") {
		s.Capabilities = appendCap(s.Capabilities, CapWebSearch)
	}
}

// applyOpenAISpec GPT / o 系列
func applyOpenAISpec(s *ModelSpec, id string) {
	s.MaxOutput = 16384

	// gpt-3.5 那批虽然也有 function calling,但表现很差、经常乱调,不给
	if hasAnyPrefix(id, "gpt-4", "gpt-5", "o1", "o3", "o4", "chatgpt") {
		s.Capabilities = appendCap(s.Capabilities, CapTools)
	}

	if hasAnyPrefix(id, "gpt-4o", "gpt-4.1", "gpt-4-1", "gpt-5", "o3", "o4", "chatgpt") {
		s.Capabilities = appendCap(s.Capabilities, CapVision)
	}
	if hasAnyPrefix(id, "gpt-5", "o1", "o3", "o4") {
		s.Capabilities = appendCap(s.Capabilities, CapReasoning)
		s.dialect = dialectOpenAIEffort
		efforts := []Effort{EffortLow, EffortMedium, EffortHigh}
		switch {
		case hasAnyPrefix(id, "gpt-5.1", "gpt-5-1", "gpt-5.2", "gpt-5-2"):
			// 5.1 起把 minimal 换成了真正的 none(可以完全不思考)
			efforts = append([]Effort{EffortNone}, efforts...)
		case hasAnyPrefix(id, "gpt-5"):
			// gpt-5.0 只有 minimal(近似不思考),发 none 会被拒
			efforts = append([]Effort{EffortMinimal}, efforts...)
		}
		s.Reasoning = &ReasoningSpec{Efforts: efforts, Default: EffortMedium}
		s.MaxOutput = 100000
		// o 系列 / gpt-5 拒收 temperature 和 top_p,也只认 max_completion_tokens
		s.Sampling = fixedSampling()
		s.useMaxCompletionTokens = true
	}
	if hasAny(id, "image") {
		s.Capabilities = appendCap(s.Capabilities, CapImageGen)
	}
	if hasAnyPrefix(id, "gpt-4o", "gpt-4.1", "gpt-4-1", "gpt-5", "o3", "o4") {
		s.Capabilities = appendCap(s.Capabilities, CapWebSearch)
	}
}

// applyDeepSeekSpec DeepSeek:V3.x/V4 用 thinking.type 开关思考,reasoner 是固定思考没有旋钮
func applyDeepSeekSpec(s *ModelSpec, id string) {
	s.MaxOutput = 8192
	s.family = familyDeepSeek
	switch {
	case hasAny(id, "reasoner", "-r1"):
		// 固定思考,发任何开关都会报错
		s.Capabilities = appendCap(s.Capabilities, CapReasoning)
		s.dialect = dialectNone
		s.Reasoning = &ReasoningSpec{}
	case hasAny(id, "v3.2", "v3-2", "v4", "chat"):
		s.Capabilities = appendCap(s.Capabilities, CapReasoning)
		s.Capabilities = appendCap(s.Capabilities, CapTools)
		s.dialect = dialectThinkingToggle
		s.Reasoning = &ReasoningSpec{
			Efforts: []Effort{EffortNone, EffortHigh},
			Default: EffortHigh,
		}
	}
}

// applyQwenSpec 通义千问:enable_thinking 布尔开关 + thinking_budget token 预算
func applyQwenSpec(s *ModelSpec, id string) {
	s.MaxOutput = 8192
	if hasAny(id, "vl", "omni") {
		s.Capabilities = appendCap(s.Capabilities, CapVision)
	}
	// 联网走百炼的顶层 enable_search;经非百炼中转访问时会被 endpointSupportsWebSearch 摘掉
	s.Capabilities = appendCap(s.Capabilities, CapWebSearch)
	if hasAnyPrefix(id, "qwq", "qvq") {
		// QwQ / QVQ 固定思考
		s.Capabilities = appendCap(s.Capabilities, CapReasoning)
		s.dialect = dialectNone
		s.Reasoning = &ReasoningSpec{}
		return
	}
	s.Capabilities = appendCap(s.Capabilities, CapTools)
	if hasAny(id, "qwen3", "qwen-plus", "qwen-max", "qwen-turbo", "qwen-flash") {
		s.Capabilities = appendCap(s.Capabilities, CapReasoning)
		s.dialect = dialectQwenToggle
		s.Reasoning = &ReasoningSpec{
			Efforts:   []Effort{EffortNone, EffortLow, EffortMedium, EffortHigh},
			Default:   EffortMedium,
			BudgetMin: 0,
			BudgetMax: 38912,
		}
	}
}

// applyZhipuSpec 智谱 GLM:thinking.type 开关;联网走 tools 数组里的 web_search
func applyZhipuSpec(s *ModelSpec, id string) {
	s.MaxOutput = 8192
	s.family = familyZhipu
	s.Sampling = cappedSampling()
	if hasAny(id, "4v", "-v-") {
		s.Capabilities = appendCap(s.Capabilities, CapVision)
	}
	if hasAny(id, "4.5", "4-5", "4.6", "4-6", "z1") {
		s.Capabilities = appendCap(s.Capabilities, CapReasoning)
		s.dialect = dialectThinkingToggle
		s.Reasoning = &ReasoningSpec{
			Efforts: []Effort{EffortNone, EffortHigh},
			Default: EffortHigh,
		}
	}
	s.Capabilities = appendCap(s.Capabilities, CapWebSearch)
	s.Capabilities = appendCap(s.Capabilities, CapTools)
}

// applyMoonshotSpec Kimi:K2 thinking 走 reasoning_effort
func applyMoonshotSpec(s *ModelSpec, id string) {
	s.MaxOutput = 8192
	s.Capabilities = appendCap(s.Capabilities, CapTools)
	// K2.5 起采样参数被锁死,发过去会报错;更早的型号上限是 1
	if hasAny(id, "k2.5", "k2-5", "k3") {
		s.Sampling = fixedSampling()
	} else {
		s.Sampling = cappedSampling()
	}
	if hasAny(id, "vision", "vl") {
		s.Capabilities = appendCap(s.Capabilities, CapVision)
	}
	if hasAny(id, "thinking", "k2", "k3") {
		s.Capabilities = appendCap(s.Capabilities, CapReasoning)
		s.dialect = dialectOpenAIEffort
		s.Reasoning = &ReasoningSpec{
			Efforts: []Effort{EffortLow, EffortMedium, EffortHigh},
			Default: EffortMedium,
		}
	}
}

// applyGenericSpec 认不出来的模型:只做最保守的推断,不声明任何需要发参数的能力。
// 思考内容仍然能收到 —— 靠 <think> 标签拆分和 reasoning_content 字段解析,那条路不需要发参数。
func applyGenericSpec(s *ModelSpec, id string) {
	if hasAny(id, "vision", "-vl", "vl-", "multimodal", "omni") {
		s.Capabilities = appendCap(s.Capabilities, CapVision)
	}
	if hasAny(id, "thinking", "reasoner", "-r1", "reasoning") {
		s.Capabilities = appendCap(s.Capabilities, CapReasoning)
		s.dialect = dialectNone
		s.Reasoning = &ReasoningSpec{}
	}
	if hasAny(id, "search") {
		s.Capabilities = appendCap(s.Capabilities, CapWebSearch)
	}
}

// reconcileDialect 端点与方言对表:模型说自己用某种方言,只有端点支持时才算数。
func reconcileDialect(ep EndpointType, d reasoningDialect) reasoningDialect {
	switch ep {
	case EndpointAnthropic:
		if d == dialectAnthropicBudget {
			return d
		}
	case EndpointGemini:
		if d == dialectGeminiBudget || d == dialectGeminiLevel {
			return d
		}
	case EndpointOpenAIResponses:
		if d == dialectOpenAIEffort {
			return d
		}
	case EndpointOpenAIChat:
		switch d {
		case dialectOpenAIEffort, dialectThinkingToggle, dialectQwenToggle:
			return d
		}
	}
	return dialectNone
}
