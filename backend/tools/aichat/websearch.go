package aichat

import "strings"

// 供应商内置联网搜索。
//
// "内置"指的是搜索这一步由模型服务商在自己那边做完,我们只是在请求里挂一个工具声明,
// 不需要自己去爬网页。四套端点的挂法各不相同:
//
//	openai-responses   tools: [{type:"web_search"}]
//	anthropic-messages tools: [{type:"web_search_20250305", name:"web_search", max_uses:N}]
//	google-generate    tools: [{google_search:{}}]
//	openai-chat        各家自定义:官方是 web_search_options,智谱塞进 tools,阿里是顶层 enable_search
//
// 引用(citations)则由各协议的 parse 函数从流里抠出来,统一成 Citation 往上抛。

// webSearchMaxUses 单次对话允许模型做多少次搜索。给 Anthropic 的 max_uses 与
// OpenAI 的 search_context_size 用;5 次足够覆盖绝大多数提问,再多只是烧 token。
const webSearchMaxUses = 5

// webSearchPatch 联网参数补丁。有的家把开关放在 tools 数组里,有的是请求体顶层字段,
// 所以两条路都留着,由调用方各自合并进自己的请求体。
type webSearchPatch struct {
	// Tools 追加到请求体的 tools 数组
	Tools []any
	// Body 直接并进请求体顶层的字段
	Body map[string]any
}

// Empty 补丁是否为空(没有任何联网参数可发)
func (p webSearchPatch) Empty() bool { return len(p.Tools) == 0 && len(p.Body) == 0 }

// endpointSupportsWebSearch 端点 + 家族 + 模型能不能挂内置联网。
// 由 InferModelSpec 调用:模型自称支持还不够,端点也得有对应的工具形状,
// 否则宁可不给用户这个开关,也别让他打开后发一个必然被拒的请求。
func endpointSupportsWebSearch(ep EndpointType, family providerFamily, normalizedID string) bool {
	switch ep {
	case EndpointAnthropic, EndpointGemini, EndpointOpenAIResponses:
		return true
	case EndpointOpenAIChat:
		switch family {
		case familyZhipu, familyDashScope:
			return true
		}
		// OpenAI 官方的 chat-completions 只有 *-search-preview 系列带内置搜索
		return strings.Contains(normalizedID, "search-preview")
	}
	return false
}

// buildWebSearchPatch 按端点 + 家族构造联网参数。模型不支持时返回空补丁。
func buildWebSearchPatch(spec ModelSpec) webSearchPatch {
	if !spec.Has(CapWebSearch) {
		return webSearchPatch{}
	}
	switch spec.Endpoint {
	case EndpointAnthropic:
		return webSearchPatch{Tools: []any{map[string]any{
			"type":     "web_search_20250305",
			"name":     "web_search",
			"max_uses": webSearchMaxUses,
		}}}

	case EndpointGemini:
		// Gemini 2.x / 3.x 的联网工具就是一个空对象;1.5 时代的
		// google_search_retrieval 已经不再对新模型开放,不做兼容
		return webSearchPatch{Tools: []any{map[string]any{"google_search": map[string]any{}}}}

	case EndpointOpenAIResponses:
		if spec.family == familyXAI {
			// xAI 把联网拆成两个工具:web_search 抓公开网页,x_search 抓 X 上的实时讨论。
			// 两个都挂上,模型自己决定用哪个。
			return webSearchPatch{Tools: []any{
				map[string]any{"type": "web_search"},
				map[string]any{"type": "x_search"},
			}}
		}
		tool := map[string]any{"type": "web_search"}
		if spec.family == familyOpenAI {
			// search_context_size 只有 OpenAI 原厂认;第三方 Responses 中转带上会 400
			tool["search_context_size"] = "medium"
		}
		return webSearchPatch{Tools: []any{tool}}

	case EndpointOpenAIChat:
		switch spec.family {
		case familyZhipu:
			// 智谱的联网开关必须走 tools 数组,providerOptions 那种顶层字段它不认
			return webSearchPatch{Tools: []any{map[string]any{
				"type": "web_search",
				"web_search": map[string]any{
					"enable":        true,
					"search_engine": "search_pro",
					"search_result": true,
				},
			}}}
		case familyDashScope:
			// 阿里百炼是顶层 enable_search + search_options,不进 tools
			return webSearchPatch{Body: map[string]any{
				"enable_search":  true,
				"search_options": map[string]any{"forced_search": true},
			}}
		default:
			// OpenAI 官方 chat-completions 的 *-search-preview 系列
			return webSearchPatch{Body: map[string]any{"web_search_options": map[string]any{}}}
		}
	}
	return webSearchPatch{}
}

// applyWebSearchPatch 把补丁合并进请求体。tools 数组已存在时追加,不覆盖。
func applyWebSearchPatch(body map[string]any, patch webSearchPatch) {
	if patch.Empty() {
		return
	}
	for k, v := range patch.Body {
		body[k] = v
	}
	if len(patch.Tools) == 0 {
		return
	}
	existing, _ := body["tools"].([]any)
	body["tools"] = append(existing, patch.Tools...)
}

// dedupeCitations 按 URL 去重并保序。各协议的流里同一条来源经常重复出现
// (每引用一次就推一条),直接累加会让引用列表里全是重复项。
func dedupeCitations(list []Citation) []Citation {
	if len(list) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(list))
	out := make([]Citation, 0, len(list))
	for _, c := range list {
		if c.URL == "" {
			continue
		}
		if _, ok := seen[c.URL]; ok {
			continue
		}
		seen[c.URL] = struct{}{}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
