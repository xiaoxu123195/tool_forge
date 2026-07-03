// 把 LLM 请求/响应体解析成可读的对话视图。
// 兼容 OpenAI chat(messages)、OpenAI Responses(instructions + input)、Anthropic(system + messages)。

export interface ConvBlock {
  kind: 'text' | 'image' | 'json'
  text?: string
  label?: string
}

export interface ConvMsg {
  role: string
  blocks: ConvBlock[]
}

export interface Conversation {
  params: { key: string; value: string }[]
  messages: ConvMsg[] // system/instructions 作为首条 role=system
}

function partToBlock(part: unknown): ConvBlock {
  if (typeof part === 'string') return { kind: 'text', text: part }
  const p = part as Record<string, any>
  const t: string = p?.type ?? ''
  if (typeof p?.text === 'string' && (t === '' || t.includes('text'))) {
    return { kind: 'text', text: p.text }
  }
  if (t === 'image_url' || t === 'input_image' || t === 'image') {
    const url = p?.image_url?.url ?? p?.image_url ?? p?.url ?? p?.source?.data ?? ''
    return { kind: 'image', label: t, text: typeof url === 'string' ? url : '' }
  }
  return { kind: 'json', label: t || 'part', text: JSON.stringify(part, null, 2) }
}

function contentToBlocks(content: unknown): ConvBlock[] {
  if (content == null) return []
  if (typeof content === 'string') return content ? [{ kind: 'text', text: content }] : []
  if (Array.isArray(content)) return content.map(partToBlock)
  return [{ kind: 'json', text: JSON.stringify(content, null, 2) }]
}

function toMsg(m: any): ConvMsg {
  const role: string = m?.role || m?.type || 'message'
  const blocks = contentToBlocks(m?.content)
  if (Array.isArray(m?.tool_calls)) {
    for (const tc of m.tool_calls) blocks.push({ kind: 'json', label: 'tool_call', text: JSON.stringify(tc, null, 2) })
  }
  // Responses 的 function_call / function_call_output 等没有 content
  if (blocks.length === 0 && m && typeof m === 'object') {
    blocks.push({ kind: 'json', text: JSON.stringify(m, null, 2) })
  }
  return { role, blocks }
}

export function parseConversation(body: string): Conversation | null {
  let obj: any
  try {
    obj = JSON.parse(body)
  } catch {
    return null
  }
  if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return null

  const params: { key: string; value: string }[] = []
  for (const k of ['model', 'stream', 'temperature', 'top_p', 'max_tokens', 'max_output_tokens', 'reasoning_effort']) {
    if (obj[k] !== undefined && typeof obj[k] !== 'object') params.push({ key: k, value: String(obj[k]) })
  }
  if (obj.reasoning?.effort) params.push({ key: 'reasoning', value: String(obj.reasoning.effort) })
  if (Array.isArray(obj.tools) && obj.tools.length) params.push({ key: 'tools', value: String(obj.tools.length) })

  const messages: ConvMsg[] = []
  const sys = obj.instructions ?? obj.system
  if (typeof sys === 'string' && sys) messages.push({ role: 'system', blocks: [{ kind: 'text', text: sys }] })
  else if (Array.isArray(sys)) messages.push({ role: 'system', blocks: contentToBlocks(sys) })

  const rawMsgs = Array.isArray(obj.messages)
    ? obj.messages
    : Array.isArray(obj.input)
      ? obj.input
      : typeof obj.input === 'string'
        ? [{ role: 'user', content: obj.input }]
        : []
  for (const m of rawMsgs) messages.push(toMsg(m))

  if (messages.length === 0) return null
  return { params, messages }
}

// ============ 原始 SSE 解析 ============

export interface SseEvent {
  event: string // event: 行(可能为空)
  data: string // 该事件的 data(多行 data 已用 \n 拼接)
}

// parseSSE 把原始 SSE 文本切成事件数组(空行分隔;支持 \r\n)。
export function parseSSE(raw: string): SseEvent[] {
  if (!raw) return []
  const out: SseEvent[] = []
  let event = ''
  let data: string[] = []
  const flush = () => {
    if (data.length) out.push({ event, data: data.join('\n') })
    event = ''
    data = []
  }
  for (const line of raw.split('\n')) {
    const l = line.replace(/\r$/, '')
    if (l === '') {
      flush()
      continue
    }
    if (l.startsWith('event:')) event = l.slice(6).trim()
    else if (l.startsWith('data:')) data.push(l.slice(5).replace(/^ /, ''))
  }
  flush()
  return out
}

// sseLabel 事件标签:优先 event: 行,其次 data.type,再次 [DONE]/data。
export function sseLabel(ev: SseEvent): string {
  if (ev.event) return ev.event
  if (ev.data === '[DONE]') return '[DONE]'
  try {
    const o = JSON.parse(ev.data)
    if (o?.type) return String(o.type)
  } catch {
    /* ignore */
  }
  return 'data'
}

// ssePreview 折叠状态下的一行预览:优先增量文本,否则截断的原始 data。
export function ssePreview(data: string): string {
  if (data === '[DONE]') return ''
  try {
    const o = JSON.parse(data)
    if (typeof o?.delta === 'string') return o.delta
    if (typeof o?.delta?.text === 'string') return o.delta.text
    const chat = o?.choices?.[0]?.delta?.content
    if (typeof chat === 'string') return chat
  } catch {
    /* ignore */
  }
  return data.length > 120 ? data.slice(0, 120) + '…' : data
}

// extractResponseText 从非流响应 JSON 里抽出助手可读文本;抽不到返回空串。
export function extractResponseText(body: string): string {
  let obj: any
  try {
    obj = JSON.parse(body)
  } catch {
    return ''
  }
  const chat = obj?.choices?.[0]?.message?.content
  if (typeof chat === 'string') return chat
  if (Array.isArray(chat)) return chat.map((p: any) => p?.text ?? '').join('')
  if (typeof obj?.output_text === 'string') return obj.output_text
  if (Array.isArray(obj?.output)) {
    let s = ''
    for (const item of obj.output) {
      if (Array.isArray(item?.content)) for (const p of item.content) if (typeof p?.text === 'string') s += p.text
    }
    if (s) return s
  }
  if (Array.isArray(obj?.content)) return obj.content.map((p: any) => (typeof p?.text === 'string' ? p.text : '')).join('')
  return ''
}
