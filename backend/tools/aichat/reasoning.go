package aichat

import (
	"math"
	"strings"
)

// 思考档位 → 请求体字段的翻译层。
//
// 核心想法:不要在每个协议的 stream 函数里写 if/else 拼字段,而是把"该往哪个路径写什么值"
// 描述成数据(wireProfile),再由一个解释器(resolveReasoning)统一算出一组 emission,
// 最后 applyEmissions 按点分路径写进请求体。
//
// 这样新增一家供应商 = 加一份 profile,流式循环一行不改。

// reasoningDialect 思考参数的方言;由 catalog.go 按模型 + 端点推断
type reasoningDialect string

const (
	dialectNone            reasoningDialect = ""                 // 不发任何思考参数
	dialectOpenAIEffort    reasoningDialect = "openai-effort"    // reasoning.effort / reasoning_effort
	dialectAnthropicBudget reasoningDialect = "anthropic-budget" // thinking.type + budget_tokens
	dialectGeminiBudget    reasoningDialect = "gemini-budget"    // thinkingConfig.thinkingBudget(2.x)
	dialectGeminiLevel     reasoningDialect = "gemini-level"     // thinkingConfig.thinkingLevel(3.x)
	dialectThinkingToggle  reasoningDialect = "thinking-toggle"  // thinking.type 开关(DeepSeek / GLM)
	dialectQwenToggle      reasoningDialect = "qwen-toggle"      // enable_thinking + thinking_budget
)

// wireSource 一个字段的值从哪来
type wireSource int

const (
	srcLiteral wireSource = iota // 写死的常量
	srcEffort                    // 解析出来的档位词
	srcBudget                    // 算出来的 token 预算
)

// wireOp 一次写入操作:把某个值写到请求体的某条点分路径上
type wireOp struct {
	Target string
	Source wireSource
	Value  any // 仅 srcLiteral 用
}

// budgetPolicy 预算制协议怎么把档位换算成 token 数
type budgetPolicy struct {
	// Min 协议本身要求的下限(0 = 无要求);算出来的预算不会低于它
	Min int
	// Fallback 模型没声明预算区间时的兜底值;0 = 没兜底,此时整个 mode 放弃
	Fallback int
	// ClampToMaxTokens 预算必须严格小于本次请求的 max_tokens(Anthropic 的硬约束)
	ClampToMaxTokens bool
}

// wireMode 某一种选择(关 / 开)对应的一组写入操作
type wireMode struct {
	Ops []wireOp
	// EffortMap 把我们的统一档位翻译成厂商自己的词(如 minimal → low)
	EffortMap map[Effort]Effort
	// Budget 非空表示这个 mode 需要先算出 token 预算
	Budget *budgetPolicy
}

// wireProfile 一种方言的完整描述。Off 为空表示这个协议不支持显式关闭思考。
type wireProfile struct {
	Off    *wireMode
	Effort *wireMode
}

// budgetFloor 各家预算制协议的共同下限;低于 1024 的预算基本等于没思考,
// 而 Anthropic 更是直接要求 >= 1024
const budgetFloor = 1024

func literalOp(target string, v any) wireOp {
	return wireOp{Target: target, Source: srcLiteral, Value: v}
}

func effortOp(target string) wireOp {
	return wireOp{Target: target, Source: srcEffort}
}

func budgetOp(target string) wireOp {
	return wireOp{Target: target, Source: srcBudget}
}

// wireProfileFor 按 (方言, 端点, 家族) 取出 wire profile;没有对应 profile 返回 nil。
//
// 端点决定字段名(Responses 是 reasoning.effort,Chat Completions 是 reasoning_effort);
// 家族决定要不要带那些只有原厂认的可选字段(如 reasoning.summary)。
func wireProfileFor(d reasoningDialect, ep EndpointType, family providerFamily) *wireProfile {
	switch d {
	case dialectOpenAIEffort:
		if ep == EndpointOpenAIResponses {
			effort := &wireMode{Ops: []wireOp{effortOp("reasoning.effort")}}
			// reasoning.summary 是拿到思考文本的开关 —— 不带它,Responses 一个思考字都不会返回。
			// 但第三方 Responses 中转普遍不认这个字段(会 400),所以只对原厂发。
			if family == familyOpenAI {
				effort.Ops = append(effort.Ops, literalOp("reasoning.summary", "auto"))
			}
			return &wireProfile{
				// "none" 只有 GPT-5.1 起和 xAI 认;catalog 只会给支持的模型放出 none 档,
				// 不支持的模型压根选不到这一档,所以这里可以直接发。
				Off:    &wireMode{Ops: []wireOp{literalOp("reasoning.effort", "none")}},
				Effort: effort,
			}
		}
		return &wireProfile{
			Off:    &wireMode{Ops: []wireOp{literalOp("reasoning_effort", "none")}},
			Effort: &wireMode{Ops: []wireOp{effortOp("reasoning_effort")}},
		}

	case dialectAnthropicBudget:
		return &wireProfile{
			Off: &wireMode{Ops: []wireOp{literalOp("thinking.type", "disabled")}},
			Effort: &wireMode{
				Ops: []wireOp{
					literalOp("thinking.type", "enabled"),
					budgetOp("thinking.budget_tokens"),
				},
				// Anthropic 要求 budget_tokens >= 1024 且严格小于 max_tokens
				Budget: &budgetPolicy{Min: 1024, Fallback: 13312, ClampToMaxTokens: true},
			},
		}

	case dialectGeminiBudget:
		return &wireProfile{
			Off: &wireMode{Ops: []wireOp{
				literalOp("generationConfig.thinkingConfig.includeThoughts", false),
				literalOp("generationConfig.thinkingConfig.thinkingBudget", 0),
			}},
			Effort: &wireMode{
				Ops: []wireOp{
					literalOp("generationConfig.thinkingConfig.includeThoughts", true),
					budgetOp("generationConfig.thinkingConfig.thinkingBudget"),
				},
				// 推不出预算时用 -1 = 让 Gemini 自己动态决定
				Budget: &budgetPolicy{Fallback: -1},
			},
		}

	case dialectGeminiLevel:
		return &wireProfile{
			Off: &wireMode{Ops: []wireOp{
				literalOp("generationConfig.thinkingConfig.includeThoughts", false),
				literalOp("generationConfig.thinkingConfig.thinkingLevel", "minimal"),
			}},
			Effort: &wireMode{
				Ops: []wireOp{
					literalOp("generationConfig.thinkingConfig.includeThoughts", true),
					effortOp("generationConfig.thinkingConfig.thinkingLevel"),
				},
				// Gemini 3 的档位词只有 low / high
				EffortMap: map[Effort]Effort{EffortMinimal: EffortLow, EffortMedium: EffortHigh},
			},
		}

	case dialectThinkingToggle:
		return &wireProfile{
			Off:    &wireMode{Ops: []wireOp{literalOp("thinking.type", "disabled")}},
			Effort: &wireMode{Ops: []wireOp{literalOp("thinking.type", "enabled")}},
		}

	case dialectQwenToggle:
		return &wireProfile{
			Off: &wireMode{Ops: []wireOp{literalOp("enable_thinking", false)}},
			Effort: &wireMode{
				Ops: []wireOp{
					literalOp("enable_thinking", true),
					budgetOp("thinking_budget"),
				},
				Budget: &budgetPolicy{},
			},
		}
	}
	return nil
}

// reasoningEmission 一条最终要写进请求体的键值
type reasoningEmission struct {
	Target string
	Value  any
}

// resolvedReasoning 解析结果
type resolvedReasoning struct {
	Emissions []reasoningEmission
	// Effort 实际生效的档位;空字符串表示这次请求不带任何思考参数
	Effort Effort
	// Budget 实际算出的 token 预算(仅预算制协议有意义)
	Budget int
}

// Enabled 这次请求是否真的带了思考参数
func (r resolvedReasoning) Enabled() bool { return len(r.Emissions) > 0 }

// nearestEffort 把用户选的档位投影到模型实际支持的最近一档。
// 换模型后旧的选择仍然可用,不至于因为新模型没这一档就把思考整个丢掉。
func nearestEffort(sel Effort, supported []Effort) Effort {
	if len(supported) == 0 {
		return ""
	}
	for _, e := range supported {
		if e == sel {
			return sel
		}
	}
	target, ok := effortRank[sel]
	if !ok {
		return supported[0]
	}
	best, bestDist, bestRank := supported[0], math.MaxInt, -1
	for _, e := range supported {
		r, ok := effortRank[e]
		if !ok {
			continue
		}
		d := r - target
		if d < 0 {
			d = -d
		}
		// 距离相同时取更高的一档 —— 宁可多想一点,别少想
		if d < bestDist || (d == bestDist && r > bestRank) {
			best, bestDist, bestRank = e, d, r
		}
	}
	return best
}

// computeBudget 把档位按比例换算成 token 预算
func computeBudget(effort Effort, spec *ReasoningSpec, policy *budgetPolicy, maxTokens int) (int, bool) {
	budget := 0
	if spec != nil && spec.BudgetMax > 0 {
		ratio, ok := effortRatio[effort]
		if !ok {
			ratio = effortRatio[EffortMedium]
		}
		budget = int(float64(spec.BudgetMax-spec.BudgetMin)*ratio) + spec.BudgetMin
		if budget < budgetFloor {
			budget = budgetFloor
		}
	}
	if budget == 0 {
		if policy.Fallback == 0 {
			return 0, false // 既算不出也没兜底 → 整个 mode 放弃
		}
		return policy.Fallback, true
	}
	if policy.Min > 0 && budget < policy.Min {
		budget = policy.Min
	}
	if policy.ClampToMaxTokens && maxTokens > 0 {
		limit := maxTokens - 1
		floor := policy.Min
		if floor == 0 {
			floor = budgetFloor
		}
		if limit < floor {
			// 输出上限根本容不下最小思考预算 → 这次不开思考,总比发个必然被拒的请求好
			return 0, false
		}
		if budget > limit {
			budget = limit
		}
	}
	return budget, true
}

// resolveReasoning 把"用户选的档位"翻译成一组请求体写入操作。
//
//	sel       用户在会话里选的档位;"" / "default" 表示不干预,由供应商自己决定
//	spec      模型能力画像(决定支持哪些档位、预算区间多大)
//	maxTokens 本次请求的输出上限,用于给思考预算封顶
//
// 任何一步推不出结果都返回空结果 —— 宁可不发参数,也不发一个会被拒的请求。
func resolveReasoning(sel Effort, spec ModelSpec, maxTokens int) resolvedReasoning {
	var empty resolvedReasoning
	if spec.dialect == dialectNone || spec.Reasoning == nil {
		return empty
	}
	sel = strings.TrimSpace(strings.ToLower(sel))
	if sel == "" || sel == EffortDefault {
		return empty
	}
	profile := wireProfileFor(spec.dialect, spec.Endpoint, spec.family)
	if profile == nil {
		return empty
	}
	effort := nearestEffort(sel, spec.Reasoning.Efforts)
	if effort == "" {
		return empty // 模型没有可调档位
	}

	mode := profile.Effort
	if effort == EffortNone {
		mode = profile.Off
	}
	if mode == nil {
		return empty
	}

	// 翻译成厂商自己的档位词
	wireEffort := effort
	if mapped, ok := mode.EffortMap[effort]; ok {
		wireEffort = mapped
	}

	budget := 0
	if mode.Budget != nil {
		b, ok := computeBudget(effort, spec.Reasoning, mode.Budget, maxTokens)
		if !ok {
			return empty
		}
		budget = b
	}

	out := resolvedReasoning{Effort: effort, Budget: budget}
	for _, op := range mode.Ops {
		switch op.Source {
		case srcLiteral:
			out.Emissions = append(out.Emissions, reasoningEmission{Target: op.Target, Value: op.Value})
		case srcEffort:
			out.Emissions = append(out.Emissions, reasoningEmission{Target: op.Target, Value: wireEffort})
		case srcBudget:
			out.Emissions = append(out.Emissions, reasoningEmission{Target: op.Target, Value: budget})
		}
	}
	if len(out.Emissions) == 0 {
		return empty
	}
	return out
}

// applyEmissions 把 emission 按点分路径写进请求体,路径上缺的层级自动补 map
func applyEmissions(body map[string]any, ems []reasoningEmission) {
	for _, e := range ems {
		setJSONPath(body, strings.Split(e.Target, "."), e.Value)
	}
}

func setJSONPath(m map[string]any, path []string, v any) {
	if len(path) == 0 {
		return
	}
	for i := 0; i < len(path)-1; i++ {
		next, ok := m[path[i]].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[path[i]] = next
		}
		m = next
	}
	m[path[len(path)-1]] = v
}

// reasoningTagName 内联思考标签名。一部分模型不走独立的 reasoning 字段,而是把思考
// 包在正文的 <think>...</think> 里,不同家用的标签名还不一样。
func reasoningTagName(modelID string) string {
	id := normalizeModelID(modelID)
	switch {
	case strings.Contains(id, "gpt-oss"):
		return "reasoning"
	case strings.Contains(id, "seed-oss"):
		return "seed:think"
	default:
		return "think"
	}
}
