// Package aichat 提供 AI 问答工具的后端能力:
//   - Provider(OpenAI 兼容供应商)CRUD + 列模型 + 检测
//   - Conversation(对话)CRUD + 流式聊天(走 wails 事件)
//   - Config(默认助手模型)
//
// 持久化路径: ~/.toolforge/ai-chat/
//
//	providers.json          所有供应商
//	config.json             默认模型等全局配置
//	conversations/{id}.json 单条对话(消息列表)
package aichat

import (
	"bytes"
	"encoding/json"
)

// ProviderType 供应商协议类型;决定走哪套 API
//
//	"openai"             OpenAI 新版 Responses API(POST /v1/responses)
//	"openai-compatible"  OpenAI 兼容旧 API(POST /v1/chat/completions),如 SiliconFlow / DeepSeek / 中转
//	"gemini"             Google Gemini(generativelanguage.googleapis.com)
//	"anthropic"          Anthropic Claude(api.anthropic.com,/v1/messages)
//	"xai"                xAI Grok(api.x.ai,走 Responses 端点 + web_search/x_search 内置工具)
type ProviderType = string

const (
	TypeOpenAI       ProviderType = "openai"
	TypeOpenAICompat ProviderType = "openai-compatible"
	TypeGemini       ProviderType = "gemini"
	TypeAnthropic    ProviderType = "anthropic"
	TypeXAI          ProviderType = "xai"
)

// ModelOverride 用户对某个模型能力推断结果的手动修正。
//
// 能力目录是按模型 ID 前缀猜的。中转把 claude-sonnet-4-5 挂成 my-claude-pro 之后就猜不出来,
// 思考档位和联网开关会凭空消失,而界面上不会说明为什么 —— 这里是那种情况的逃生舱。
type ModelOverride struct {
	// AliasOf 这个模型实际对应的标准模型 ID。填它最省事:按标准 ID 推断能一次拿到
	// 正确的思考方言、预算区间、输出上限,不用一项项手勾。
	AliasOf string `json:"aliasOf,omitempty"`
	// CapabilitiesSet 为 true 时 Capabilities 是权威值(允许显式设成空集,
	// 用来关掉推断错了的能力);false 时忽略 Capabilities,沿用推断结果。
	CapabilitiesSet bool         `json:"capabilitiesSet,omitempty"`
	Capabilities    []Capability `json:"capabilities,omitempty"`
	// MaxOutput > 0 时覆盖推断出的单次回复 token 上限
	MaxOutput int `json:"maxOutput,omitempty"`
}

// Provider 用户配置的一个 AI 供应商
type Provider struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`    // 用户给的名字,如 "OpenAI"、"我的中转"
	Type    ProviderType `json:"type"`    // 供应商协议类型;空值按 openai 处理(向前兼容)
	Logo    string       `json:"logo"`    // 头像;builtin id (如 "openai") 或 data: URL;空 → 名字首字母
	BaseURL string       `json:"baseUrl"` // 例如 https://api.openai.com/v1
	// APIKey 单密钥字段。新逻辑一律走 APIKeys 池,这里保留两个作用:
	// 老配置的迁移来源,以及协议层实际使用的"这次请求选中的那把"(见 keys.go 的 withKey)
	APIKey string `json:"apiKey"`
	// APIKeys 密钥池;多把轮着用,某把失效时自动换下一把(见 keys.go)
	APIKeys  []APIKeyEntry `json:"apiKeys,omitempty"`
	Enabled  bool          `json:"enabled"`  // 总开关;关闭后不在模型选择器里出现
	Models   []string      `json:"models"`   // 用户从 /v1/models 选进来的 model id
	IsSystem bool          `json:"isSystem"` // 系统内置预设(可改但删除会重新注入)
	// SortOrder 用户拖动排出来的顺序,从 1 开始;0 表示"没排过",
	// 这类回落到按 UpdatedAt 倒序 —— 也就是没人拖动过时的老行为
	SortOrder int `json:"sortOrder,omitempty"`
	// ModelOverrides 按模型 ID 索引的能力修正;能力推断不准时由用户手动纠正
	ModelOverrides map[string]ModelOverride `json:"modelOverrides,omitempty"`
	// CustomBody 原样并进请求体的自定义字段。
	//
	// 中转五花八门,常有一两个非标参数不给就不干活(要个 stream_options、
	// 要个自家的 provider 路由字段、要把某个开关显式关掉)。没有这个口子,
	// 每遇到一家就得改一次协议层代码再重编 —— 那不是用户能做的事。
	//
	// 合并发生在最后一步,所以它能覆盖我们自己算出来的任何字段:
	// 这是有意的逃生舱,推断错了至少还有救。
	CustomBody map[string]any `json:"customBody,omitempty"`
	CreatedAt      int64                    `json:"createdAt"`
	UpdatedAt      int64                    `json:"updatedAt"`
}

// ModelInfo 从 /v1/models 拉到的一条
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object,omitempty"`
	OwnedBy string `json:"ownedBy,omitempty"`
}

// FetchModelsResult 列模型结果
type FetchModelsResult struct {
	OK      bool        `json:"ok"`
	Models  []ModelInfo `json:"models,omitempty"`
	Message string      `json:"message,omitempty"`
}

// TestResult 检测某个模型的连通性
type TestResult struct {
	OK         bool   `json:"ok"`
	StatusCode int    `json:"statusCode,omitempty"`
	DurationMs int    `json:"durationMs"`
	Message    string `json:"message,omitempty"`
}

// 角色名常量;新加的 "clear" 是前端插入的"清除上下文"分隔标记,
// 不会发给模型 — 构造 prompt 时只取最后一个 clear 之后的消息
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
	RoleClear     = "clear"
	// RoleTool 一条"工具执行结果"消息。它不是用户也不是模型说的话,
	// 各协议对它的形状要求差别很大(见各 build 函数),所以单独一个角色
	RoleTool = "tool"
)

// ImageBlock 一张图(base64 或远程 URL),作为消息的多模态附件。
//
//	user 消息:用户上传给 vision 模型看
//	assistant 消息:模型生成的图(DALL-E / Gemini imagen / grok-imagine 等)
type ImageBlock struct {
	MimeType string `json:"mimeType,omitempty"` // image/png / image/jpeg / ...
	Data     string `json:"data,omitempty"`     // base64,无 data: 前缀
	URL      string `json:"url,omitempty"`      // 远程 URL(替代 Data,不重复存)
}

// FileBlock 一个非图附件(PDF / docx / xlsx / pptx / 纯文本 / 代码)。
//
//	Text 已由前端解析出的文本内容(docx/xlsx/pptx/txt/code → 直接拿到文本)
//	Data 二进制 base64(主要是 PDF,各协议有原生支持)
//	两者通常二选一,允许同时存在(Text 用于 fallback,Data 用于原生)
type FileBlock struct {
	Name      string `json:"name"`              // 显示名(含扩展名)
	MimeType  string `json:"mimeType,omitempty"`
	Text      string `json:"text,omitempty"`    // 提取/原始文本
	Data      string `json:"data,omitempty"`    // base64,无 data: 前缀
	URL       string `json:"url,omitempty"`     // 远程引用(暂未启用)
	SizeBytes int    `json:"sizeBytes,omitempty"`
}

// ThinkingBlock 模型的一段"思考"。
//
// 之所以不是一个大字符串:Anthropic 的 extended thinking 会给每个 thinking block 附一个
// signature,多轮对话里必须把 (thinking, signature) 原样回传,否则请求会被拒。
// 其余协议只有 Text,Signature 留空。
type ThinkingBlock struct {
	Text      string `json:"text,omitempty"`
	Signature string `json:"signature,omitempty"`
	// Redacted 被服务端加密隐藏的思考块(Anthropic redacted_thinking);
	// 内容不可读,但同样必须原样回传
	Redacted string `json:"redacted,omitempty"`
}

// ToolCall 模型请求的一次工具调用,以及执行结果。
//
// 请求和结果放在同一个结构里,是因为回传给模型时这两半必须配对出现
// (每家都要求结果带上对应的调用 ID),拆开存反而容易对不上。
type ToolCall struct {
	// ID 协议侧的调用 ID,回传结果时必须原样带上
	ID   string `json:"id"`
	Name string `json:"name"`
	// Arguments 模型给的参数,JSON 字符串。不解析直接透传给工具,
	// 因为各家给的字段顺序 / 空白不一致,解析再序列化会变形
	Arguments string `json:"arguments,omitempty"`
	// Result 执行成功时的返回内容
	Result string `json:"result,omitempty"`
	// Error 执行失败时的说明;它同样会回传给模型,让它有机会换个参数重试
	Error string `json:"error,omitempty"`
	// Status 仅用于流式展示:ToolStatusRunning 表示模型已经请求、本地还在执行。
	// 执行完就把它留空再推一次(落盘的也是空),所以前端只需判断"是不是 running"
	Status string `json:"status,omitempty"`
}

// ToolStatusRunning 工具已被模型请求、本地还在执行。
// 没有对应的 "done":执行完时 Status 留空,空值即已完成 —— 这样老会话文件
// (那时还没有这个字段)读回来也是"已完成",不用额外迁移。
const ToolStatusRunning = "running"

// SearchQuery 供应商内置联网搜索实际发出的一次检索。
//
// 和 Citation 是两回事:Citation 是"引用了哪些网页",这个是"用什么词搜的"。
// 模型答得跑偏时,看它搜了什么往往比看它引了什么更能说明问题 ——
// 而且检索发生在回答之前,能在等待期间就给用户一点"它在干什么"的反馈。
type SearchQuery struct {
	Query string `json:"query"`
	// Status "running" = 检索已发出还没回来;空 / "done" = 已完成
	Status string `json:"status,omitempty"`
	// Results 这次检索命中的条数;有些协议给不出,留 0
	Results int `json:"results,omitempty"`
}

// Citation 联网搜索引用的一条来源
type Citation struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// Message 对话里的一条消息
type Message struct {
	ID      string `json:"id"`
	Role    string `json:"role"` // user / assistant / system / clear
	Content string `json:"content"`
	// Images 附件图片(用户上传 or 模型生成)
	Images []ImageBlock `json:"images,omitempty"`
	// Files 非图附件(用户上传:PDF / docx / xlsx / pptx / 文本 / 代码)
	Files []FileBlock `json:"files,omitempty"`
	// Thinking 模型的"思考"内容(deepseek-r1 / o1 / o3 / claude extended thinking)
	Thinking []ThinkingBlock `json:"thinking,omitempty"`
	// Citations 联网搜索引用到的来源(仅 assistant 有意义)
	Citations []Citation `json:"citations,omitempty"`
	// Searches 供应商内置联网搜索发出过的检索词(仅 assistant 有意义)
	Searches []SearchQuery `json:"searches,omitempty"`
	// ToolCalls assistant 消息上是"模型请求调用的工具",
	// RoleTool 消息上是"这批调用的执行结果"
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
	// Model 这条消息使用的模型 ID(仅 assistant 有意义)
	Model string `json:"model,omitempty"`
	// Usage 这条回复的 token 用量。usage.jsonl 里记的是流水总账,
	// 出问题时("这个模型怎么突然这么贵")翻账本定位不到是哪条,所以也挂一份在消息上
	Usage *Usage `json:"usage,omitempty"`
	// DurationMs 从发出请求到流结束的耗时
	DurationMs int `json:"durationMs,omitempty"`
	// Truncated 这条回复没写完(用户点了停止,或流中途断了)。
	// 以前是往正文尾部塞一个 " …" —— 那会污染内容,复制出去带着个莫名其妙的省略号,
	// 再发给模型时它也会把省略号当成正文的一部分
	Truncated bool `json:"truncated,omitempty"`
	CreatedAt  int64 `json:"createdAt"`
}

// UnmarshalJSON 兼容旧会话文件:早期 thinking 是单个字符串,现在是带 signature 的块数组。
// 读到字符串就升格成单元素数组;不做破坏性迁移 —— 会话下次写盘时自然变成新格式。
func (m *Message) UnmarshalJSON(data []byte) error {
	// plain 去掉方法集,避免 Unmarshal 递归调用自己
	type plain Message
	aux := struct {
		Thinking json.RawMessage `json:"thinking"`
		*plain
	}{plain: (*plain)(m)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	m.Thinking = nil
	raw := bytes.TrimSpace(aux.Thinking)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		if s != "" {
			m.Thinking = []ThinkingBlock{{Text: s}}
		}
		return nil
	}
	return json.Unmarshal(raw, &m.Thinking)
}

// ThinkingText 把所有思考块拼成一段可展示的文本
func (m Message) ThinkingText() string {
	if len(m.Thinking) == 0 {
		return ""
	}
	var sb bytes.Buffer
	for _, b := range m.Thinking {
		sb.WriteString(b.Text)
	}
	return sb.String()
}

// Conversation 一个对话(多轮)
type Conversation struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`      // 自动从首条 user 消息生成,可重命名
	// TitleAuto 标题还是自动来的,允许被更好的自动标题覆盖。
	//
	// 用户一旦手工命名(重命名 / 建会话时自己填 / 会话设置里改)就置 false,
	// 之后自动起标题永远绕开这条会话 —— 用户起的名字被模型悄悄改掉是最糟的体验。
	TitleAuto  bool      `json:"titleAuto,omitempty"`
	ProviderID string    `json:"providerId"` // 当前对话用的供应商
	ModelID    string    `json:"modelId"`    // 当前对话用的模型
	System     string    `json:"system,omitempty"`
	// ContextCount 发给模型时保留的最近 user/assistant 消息条数;0 = 不限
	ContextCount int `json:"contextCount,omitempty"`
	// ReasoningEffort 思考档位:"" / "default" 不干预由供应商决定,"none" 显式关闭,
	// minimal/low/medium/high 由各协议翻译成自己的 wire 字段(见 reasoning.go)
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	// WebSearch 是否启用供应商内置联网搜索;模型不支持时忽略
	WebSearch bool `json:"webSearch,omitempty"`
	// Tools 是否允许模型调用本地工具(function calling);模型不支持时忽略
	Tools bool `json:"tools,omitempty"`
	// Temperature / TopP 采样参数。用指针是为了区分"没设"和"设成 0" ——
	// 0 是合法取值(完全确定性输出),不能拿零值当未设置
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"topP,omitempty"`
	// MaxTokens 单次回复的 token 上限;0 = 不指定,由模型自己决定
	MaxTokens int       `json:"maxTokens,omitempty"`
	Messages  []Message `json:"messages"`
	CreatedAt int64     `json:"createdAt"`
	UpdatedAt int64     `json:"updatedAt"`
}

// ConversationSummary 列表展示用,不含 messages
type ConversationSummary struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	ProviderID   string `json:"providerId"`
	ModelID      string `json:"modelId"`
	UpdatedAt    int64  `json:"updatedAt"`
	MessageCount int    `json:"messageCount"`
}

// SortedConversations 是 ListConversations 的返回值语义说明:
// 拖动排过序的按用户的顺序在前,其余按 UpdatedAt 倒序跟在后面。

// Config 全局 AI 配置
type Config struct {
	DefaultProviderID string `json:"defaultProviderId"`
	DefaultModelID    string `json:"defaultModelId"`
	// AutoTitleOff 关掉"首轮问答后让模型起标题"。
	//
	// 存的是"关"而不是"开":这个功能默认就该开着,而老配置文件里没有这个字段,
	// 反序列化出来是零值 —— 零值必须落在"开"这一侧,否则所有老用户升级后功能都是哑的。
	AutoTitleOff bool `json:"autoTitleOff,omitempty"`
	// TitleProviderID / TitleModelID 起标题专用的模型。两个都填才生效,
	// 留空就用会话自己的模型。用意是拿一个便宜的小模型干这件小事
	TitleProviderID string `json:"titleProviderId,omitempty"`
	TitleModelID    string `json:"titleModelId,omitempty"`
	// ConversationOrder 用户拖出来的会话顺序(会话 ID 列表)。
	//
	// 放在这里而不是每条会话里加一个 SortOrder 字段:会话文件带着全部消息,
	// 有的几百 KB,为了改一个排序位把它们整个重写一遍太亏。这里一次写几百字节就够。
	ConversationOrder []string `json:"conversationOrder,omitempty"`
}

// Usage 单次请求的 token 用量(由各协议从最后一帧解析)
type Usage struct {
	InputTokens     int `json:"inputTokens"`
	OutputTokens    int `json:"outputTokens"`
	ReasoningTokens int `json:"reasoningTokens,omitempty"`
	CachedTokens    int `json:"cachedTokens,omitempty"`
}

// UsageRecord 一条用量日志(append 到 ~/.toolforge/ai-chat/usage.jsonl)
type UsageRecord struct {
	Ts              int64  `json:"ts"`         // unix milli
	ConvID          string `json:"convId"`
	ProviderID      string `json:"providerId"`
	ProviderName    string `json:"providerName"` // 写入时快照,删 provider 后仍可看
	Model           string `json:"model"`
	InputTokens     int    `json:"inputTokens"`
	OutputTokens    int    `json:"outputTokens"`
	ReasoningTokens int    `json:"reasoningTokens,omitempty"`
	CachedTokens    int    `json:"cachedTokens,omitempty"`
	DurationMs      int64  `json:"durationMs"`
}
