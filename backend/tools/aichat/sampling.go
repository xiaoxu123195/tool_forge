package aichat

// 采样参数(temperature / top_p / 输出上限)。
//
// 看着简单,但有三处必须按模型和端点区分,写死一套一定踩坑:
//
//	字段名不同   Responses 是 max_output_tokens,Gemini 在 generationConfig 下,
//	             OpenAI 的思考模型只认 max_completion_tokens —— 发 max_tokens 会直接报错
//	支持度不同   o 系列 / gpt-5 完全不接受 temperature 和 top_p;Kimi K2.5 起也锁死了
//	取值范围不同 OpenAI 上限是 2,Claude / GLM / Kimi 上限是 1
//
// 还有一条运行期规则:Claude 开着 extended thinking 时 temperature 只能是 1,
// 与其猜用户想要什么,不如整个不发。

// SamplingSpec 模型接受哪些采样参数
type SamplingSpec struct {
	Temperature bool `json:"temperature"`
	TopP        bool `json:"topP"`
	// MaxTemp temperature 的上限;前端滑块用它定刻度
	MaxTemp float64 `json:"maxTemp"`
}

// defaultSampling 大多数模型的情况:两个都支持,上限 2
func defaultSampling() SamplingSpec {
	return SamplingSpec{Temperature: true, TopP: true, MaxTemp: 2}
}

// cappedSampling 上限为 1 的那批(Claude / GLM / Kimi / 部分国产模型)
func cappedSampling() SamplingSpec {
	return SamplingSpec{Temperature: true, TopP: true, MaxTemp: 1}
}

// fixedSampling 采样被锁死、发了会报错的模型(o 系列 / gpt-5 / Kimi K2.5+)
func fixedSampling() SamplingSpec {
	return SamplingSpec{}
}

// effectiveMaxTokens 本次请求的输出上限:用户在会话里设了就用他的,否则用模型推断值
func effectiveMaxTokens(conv Conversation, spec ModelSpec) int {
	if conv.MaxTokens > 0 {
		return conv.MaxTokens
	}
	if spec.MaxOutput > 0 {
		return spec.MaxOutput
	}
	return defaultMaxOutput
}

// applySampling 把采样参数写进请求体。
//
//	reasoningOn 本次请求是否真的开了思考(Claude 开着思考就不能带 temperature)
//
// 输出上限只在用户显式设过时才发 —— spec.MaxOutput 是我们猜的模型能力上限,
// 拿它当硬性截断会把正常的长回答切掉。Anthropic 是唯一例外(max_tokens 必填),
// 由 streamAnthropic 自己写,这里不重复。
func applySampling(body map[string]any, conv Conversation, spec ModelSpec, reasoningOn bool) {
	s := spec.Sampling

	// Claude 的 extended thinking 要求 temperature 恒为 1;与其猜用户想要什么,不如不发
	allowSampling := !(reasoningOn && spec.dialect == dialectAnthropicBudget)

	if allowSampling && s.Temperature && conv.Temperature != nil {
		setJSONPath(body, samplingPath(spec.Endpoint, "temperature"), clampTemp(*conv.Temperature, s.MaxTemp))
	}
	if allowSampling && s.TopP && conv.TopP != nil {
		setJSONPath(body, samplingPath(spec.Endpoint, "topP"), clamp01(*conv.TopP))
	}
	if conv.MaxTokens > 0 && spec.Endpoint != EndpointAnthropic {
		setJSONPath(body, maxTokensPath(spec), conv.MaxTokens)
	}
}

// samplingPath 采样字段在各端点请求体里的路径
func samplingPath(ep EndpointType, field string) []string {
	if ep == EndpointGemini {
		switch field {
		case "temperature":
			return []string{"generationConfig", "temperature"}
		case "topP":
			return []string{"generationConfig", "topP"}
		}
	}
	if field == "topP" {
		return []string{"top_p"}
	}
	return []string{"temperature"}
}

// maxTokensPath 输出上限字段的路径。OpenAI 这边三种叫法,选错直接 400。
func maxTokensPath(spec ModelSpec) []string {
	switch spec.Endpoint {
	case EndpointGemini:
		return []string{"generationConfig", "maxOutputTokens"}
	case EndpointOpenAIResponses:
		return []string{"max_output_tokens"}
	case EndpointOpenAIChat:
		if spec.useMaxCompletionTokens {
			// o 系列 / gpt-5 只认这个,发 max_tokens 会被拒
			return []string{"max_completion_tokens"}
		}
		return []string{"max_tokens"}
	}
	return []string{"max_tokens"}
}

func clampTemp(v, max float64) float64 {
	if max <= 0 {
		max = 2
	}
	if v < 0 {
		return 0
	}
	if v > max {
		return max
	}
	return v
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
