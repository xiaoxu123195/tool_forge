// 跟后端 backend/tools/aichat/types.go 对齐
export type ProviderType =
  | 'openai'
  | 'openai-compatible'
  | 'gemini'
  | 'anthropic'
  | 'xai'

/** 用户对某个模型能力推断结果的手动修正(推断不准时的逃生舱) */
export interface ModelOverride {
  /** 这个模型实际对应的标准模型 ID;中转改过名时填它最省事 */
  aliasOf?: string
  /** true 时 capabilities 为权威值(允许空集,用来关掉推断错的能力) */
  capabilitiesSet?: boolean
  capabilities?: Capability[]
  /** > 0 时覆盖推断的单次回复 token 上限 */
  maxOutput?: number
}

/** 密钥池里的一条。多把密钥轮着用,某把失效时后端自动换下一把 */
export interface APIKeyEntry {
  id: string
  key: string
  /** 用户给的备注,如 "个人号" / "公司额度" */
  label?: string
  /** 默认零值即启用 */
  disabled?: boolean
}

/** 一把密钥的检测结果 */
export interface KeyCheckResult {
  keyId: string
  ok: boolean
  statusCode?: number
  durationMs: number
  message?: string
}

export interface Provider {
  id: string
  name: string
  type: ProviderType
  /** builtin id (如 "openai" / "gemini") 或 data: URL;空 → 用名字首字母 */
  logo: string
  baseUrl: string
  /** 单密钥字段;新逻辑一律看 apiKeys,这里只是老配置的迁移来源 */
  apiKey: string
  /** 密钥池 */
  apiKeys?: APIKeyEntry[]
  enabled: boolean
  models: string[]
  /** 系统内置预设 */
  isSystem: boolean
  /** 拖动排出来的顺序,从 1 开始;0 = 没排过,回落到按 updatedAt 倒序 */
  sortOrder?: number
  /** 按模型 ID 索引的能力修正 */
  modelOverrides?: Record<string, ModelOverride>
  createdAt: number
  updatedAt: number
}

export interface ModelInfo {
  id: string
  object?: string
  ownedBy?: string
}

export interface FetchModelsResult {
  ok: boolean
  models?: ModelInfo[]
  message?: string
}

export interface TestResult {
  ok: boolean
  statusCode?: number
  durationMs: number
  message?: string
}

export interface AIConfig {
  defaultProviderId: string
  defaultModelId: string
}

/** 一张图(用户上传给 vision 模型 / 模型生成给用户) */
export interface ImageBlock {
  /** image/png · image/jpeg · ... */
  mimeType?: string
  /** base64,无 data: 前缀 */
  data?: string
  /** 远程 URL(替代 data) */
  url?: string
}

/** 一个非图附件(PDF / docx / xlsx / pptx / 文本 / 代码) */
export interface FileBlock {
  name: string
  mimeType?: string
  /** 已解析的文本(docx/xlsx/pptx/txt/code) */
  text?: string
  /** base64,无 data: 前缀(主要是 PDF) */
  data?: string
  url?: string
  sizeBytes?: number
}

/** 模型的一段「思考」。Anthropic 的 signature 必须原样回传,所以不是一个大字符串 */
export interface ThinkingBlock {
  text?: string
  signature?: string
  redacted?: string
}

/** 模型请求的一次工具调用,以及执行结果 */
export interface ToolCall {
  id: string
  name: string
  /** 模型给的参数,JSON 字符串 */
  arguments?: string
  /** 执行成功时的返回内容 */
  result?: string
  /** 执行失败时的说明(同样会回传给模型) */
  error?: string
  /** 'running' = 模型已请求、本地还在执行;落盘的都执行完了,所以读回来是空的 */
  status?: string
}

/** 供应商内置联网搜索实际发出的一次检索(搜了什么词,不是引了哪些网页) */
export interface SearchQuery {
  query: string
  /** 'running' = 检索已发出还没回来;空 / 'done' = 已完成 */
  status?: string
  /** 命中条数;有些协议给不出,留 0 */
  results?: number
}

/** 联网搜索引用的一条来源 */
export interface Citation {
  url: string
  title?: string
  snippet?: string
}

export interface Message {
  id: string
  /** 'clear' 是前端"清除上下文"分隔标记,只用于渲染,不发给模型 */
  role: 'user' | 'assistant' | 'system' | 'clear'
  content: string
  images?: ImageBlock[]
  files?: FileBlock[]
  /** 模型的「思考」内容(deepseek-r1 / o1 / claude extended) */
  thinking?: ThinkingBlock[]
  /** 联网搜索引用到的来源 */
  citations?: Citation[]
  /** 供应商内置联网搜索发出过的检索词 */
  searches?: SearchQuery[]
  /** assistant 上是模型请求的工具调用(含执行结果) */
  toolCalls?: ToolCall[]
  /** 这条 assistant 消息使用的模型 ID */
  model?: string
  /** 这条回复的 token 用量 */
  usage?: MessageUsage
  /** 从发出请求到流结束的耗时(毫秒) */
  durationMs?: number
  /** 这条回复没写完(点了停止,或流中途断了)—— 可以「继续写」 */
  truncated?: boolean
  createdAt: number
}

/** 单条回复的 token 用量 */
export interface MessageUsage {
  inputTokens: number
  outputTokens: number
  reasoningTokens?: number
  cachedTokens?: number
}

/**
 * 把思考块拼成可展示的一段文本。
 *
 * 老会话文件里 thinking 是一个字符串(那时还没有 signature 的概念)。后端读盘时会把它
 * 升格成数组,正常路径拿到的一定是数组 —— 但这个函数在渲染的最外层,一旦拿到字符串就是
 * 整个窗口白屏。磁盘上确实还有那种格式的会话,所以这里认一下。
 */
export function thinkingText(m: Pick<Message, 'thinking'>): string {
  const t = m.thinking
  if (!t) return ''
  if (typeof t === 'string') return t
  if (!Array.isArray(t)) return ''
  return t.map((b) => b?.text ?? '').join('')
}

export interface Conversation {
  id: string
  title: string
  providerId: string
  modelId: string
  system?: string
  /** 发给模型时保留的最近 user/assistant 消息条数;0/缺省 = 不限 */
  contextCount?: number
  /** 思考档位:'' / 'default' 不干预,'none' 关闭,其余见 ReasoningEffort */
  reasoningEffort?: string
  /** 是否启用供应商内置联网搜索 */
  webSearch?: boolean
  /** 是否允许模型调用本地工具 */
  tools?: boolean
  /** 采样温度;undefined = 不指定,由模型自己决定(0 是合法取值) */
  temperature?: number
  topP?: number
  /** 单次回复 token 上限;0/缺省 = 不指定 */
  maxTokens?: number
  messages: Message[]
  createdAt: number
  updatedAt: number
}

export interface ConversationSummary {
  id: string
  title: string
  providerId: string
  modelId: string
  updatedAt: number
  messageCount: number
}

/** 一条用量记录,后端 append 到 ~/.toolforge/ai-chat/usage.jsonl */
export interface UsageRecord {
  ts: number
  convId: string
  providerId: string
  providerName: string
  model: string
  inputTokens: number
  outputTokens: number
  reasoningTokens?: number
  cachedTokens?: number
  durationMs: number
}

/** 思考档位。后端 reasoning.go 负责把它翻译成各家自己的 wire 字段 */
export type ReasoningEffort = 'default' | 'none' | 'minimal' | 'low' | 'medium' | 'high'

export const EFFORT_LABELS: Record<ReasoningEffort, string> = {
  default: '默认',
  none: '关闭思考',
  minimal: '极简',
  low: '低',
  medium: '中',
  high: '高',
}

/** 模型能力标签,与后端 catalog.go 的 Capability 对齐 */
export type Capability =
  | 'vision'
  | 'pdf'
  | 'reasoning'
  | 'webSearch'
  | 'imageGen'
  | 'tools'

export interface ReasoningSpec {
  efforts: ReasoningEffort[]
  default?: ReasoningEffort
  budgetMin?: number
  budgetMax?: number
}

/** 模型接受哪些采样参数。会思考的模型经常把这些锁死 */
export interface SamplingSpec {
  temperature: boolean
  topP: boolean
  /** temperature 的上限:OpenAI 是 2,Claude / GLM / Kimi 是 1 */
  maxTemp: number
}

/** 模型能力画像;由后端按模型 ID 推断,前端据此决定给哪些开关 */
export interface ModelSpec {
  id: string
  endpoint: string
  capabilities: Capability[]
  reasoning?: ReasoningSpec
  sampling: SamplingSpec
  maxOutput: number
}

/** 可复用的会话预设:系统提示词 + 一组参数覆盖 */
export interface Assistant {
  id: string
  name: string
  /** 列表里的小图标,纯装饰 */
  emoji?: string
  system: string
  /** 下面这些为空 / 零值表示"不干预,用会话默认值" */
  contextCount?: number
  reasoningEffort?: string
  temperature?: number
  topP?: number
  maxTokens?: number
  webSearch?: boolean
  tools?: boolean
  sortOrder?: number
  createdAt: number
  updatedAt: number
}

/** 「工具」开关背后到底会带什么给模型 */
export interface ChatToolInfo {
  name: string
  description: string
  /** 空 = 内置工具;否则是 MCP 服务器名 */
  source?: string
}

export interface ChatToolsView {
  tools: ChatToolInfo[]
  /** 已启用但当前没连上的 MCP 服务器 —— 它们的工具这一轮不会声明 */
  offline?: string[]
}

/** Wails 事件名常量 */
export const EV_CHUNK_PREFIX = 'ai-chat:chunk:'
export const EV_THINKING_PREFIX = 'ai-chat:thinking:'
export const EV_IMAGE_PREFIX = 'ai-chat:image:'
export const EV_CITATION_PREFIX = 'ai-chat:citation:'
export const EV_SEARCH_PREFIX = 'ai-chat:search:'
export const EV_TOOL_PREFIX = 'ai-chat:tool:'
export const EV_DONE_PREFIX = 'ai-chat:done:'
export const EV_ERROR_PREFIX = 'ai-chat:error:'
