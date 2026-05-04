// 跟后端 backend/tools/aichat/types.go 对齐
export type ProviderType = 'openai' | 'openai-compatible' | 'gemini' | 'anthropic'

export interface Provider {
  id: string
  name: string
  type: ProviderType
  /** builtin id (如 "openai" / "gemini") 或 data: URL;空 → 用名字首字母 */
  logo: string
  baseUrl: string
  apiKey: string
  enabled: boolean
  models: string[]
  /** 系统内置预设 */
  isSystem: boolean
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

export interface Message {
  id: string
  /** 'clear' 是前端"清除上下文"分隔标记,只用于渲染,不发给模型 */
  role: 'user' | 'assistant' | 'system' | 'clear'
  content: string
  images?: ImageBlock[]
  /** 模型的「思考」内容(deepseek-r1 / o1 / claude extended) */
  thinking?: string
  /** 这条 assistant 消息使用的模型 ID */
  model?: string
  createdAt: number
}

export interface Conversation {
  id: string
  title: string
  providerId: string
  modelId: string
  system?: string
  /** 发给模型时保留的最近 user/assistant 消息条数;0/缺省 = 不限 */
  contextCount?: number
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

/** Wails 事件名常量 */
export const EV_CHUNK_PREFIX = 'ai-chat:chunk:'
export const EV_THINKING_PREFIX = 'ai-chat:thinking:'
export const EV_IMAGE_PREFIX = 'ai-chat:image:'
export const EV_DONE_PREFIX = 'ai-chat:done:'
export const EV_ERROR_PREFIX = 'ai-chat:error:'
