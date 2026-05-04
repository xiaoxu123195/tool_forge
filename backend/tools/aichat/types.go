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

// ProviderType 供应商协议类型;决定走哪套 API
//
//	"openai"             OpenAI 新版 Responses API(POST /v1/responses)
//	"openai-compatible"  OpenAI 兼容旧 API(POST /v1/chat/completions),如 SiliconFlow / DeepSeek / 中转
//	"gemini"             Google Gemini(generativelanguage.googleapis.com)
//	"anthropic"          Anthropic Claude(api.anthropic.com,/v1/messages)
type ProviderType = string

const (
	TypeOpenAI       ProviderType = "openai"
	TypeOpenAICompat ProviderType = "openai-compatible"
	TypeGemini       ProviderType = "gemini"
	TypeAnthropic    ProviderType = "anthropic"
)

// Provider 用户配置的一个 AI 供应商
type Provider struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`     // 用户给的名字,如 "OpenAI"、"我的中转"
	Type      ProviderType `json:"type"`     // 供应商协议类型;空值按 openai 处理(向前兼容)
	Logo      string       `json:"logo"`     // 头像;builtin id (如 "openai") 或 data: URL;空 → 名字首字母
	BaseURL   string       `json:"baseUrl"`  // 例如 https://api.openai.com/v1
	APIKey    string       `json:"apiKey"`   // 明文存,Wails 是本地 app
	Enabled   bool         `json:"enabled"`  // 总开关;关闭后不在模型选择器里出现
	Models    []string     `json:"models"`   // 用户从 /v1/models 选进来的 model id
	IsSystem  bool         `json:"isSystem"` // 系统内置预设(可改但删除会重新注入)
	CreatedAt int64        `json:"createdAt"`
	UpdatedAt int64        `json:"updatedAt"`
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
)

// Message 对话里的一条消息
type Message struct {
	ID      string `json:"id"`
	Role    string `json:"role"` // user / assistant / system / clear
	Content string `json:"content"`
	// Thinking 模型的"思考"内容(deepseek-r1 / o1 / o3 / claude extended thinking)
	Thinking string `json:"thinking,omitempty"`
	// Model 这条消息使用的模型 ID(仅 assistant 有意义)
	Model     string `json:"model,omitempty"`
	CreatedAt int64  `json:"createdAt"`
}

// Conversation 一个对话(多轮)
type Conversation struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`      // 自动从首条 user 消息生成,可重命名
	ProviderID string    `json:"providerId"` // 当前对话用的供应商
	ModelID    string    `json:"modelId"`    // 当前对话用的模型
	System     string    `json:"system,omitempty"`
	// ContextCount 发给模型时保留的最近 user/assistant 消息条数;0 = 不限
	ContextCount int       `json:"contextCount,omitempty"`
	Messages     []Message `json:"messages"`
	CreatedAt    int64     `json:"createdAt"`
	UpdatedAt    int64     `json:"updatedAt"`
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

// Config 全局 AI 配置
type Config struct {
	DefaultProviderID string `json:"defaultProviderId"`
	DefaultModelID    string `json:"defaultModelId"`
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
