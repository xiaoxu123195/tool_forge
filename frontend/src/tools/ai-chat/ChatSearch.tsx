import { useEffect, useMemo, useRef, useState } from 'react'
import { ChevronDown, ChevronUp, Search, X } from 'lucide-react'
import { thinkingText, type Message } from './types'
import { cn } from '@/lib/utils'

/** 一条命中:消息 id + 一段带上下文的摘录,用于结果列表里认出是哪一条 */
export interface SearchHit {
  id: string
  role: Message['role']
  /** 摘录里命中词前后的片段,用于自己拼高亮 */
  before: string
  match: string
  after: string
}

/** 摘录里命中词前后各留多少字 */
const CONTEXT_CHARS = 24

/**
 * 在会话里找一段文字。
 *
 * 搜的是正文 + 思考文本:思考里常常藏着"它当时为什么这么答"的关键句,
 * 排查问题时要找的往往正是那一段。工具调用的原始结果不搜 —— 那是几万字的
 * 网页正文,搜出来全是噪音,而且用户想找的从来不是它。
 */
export function findHits(messages: Message[], query: string): SearchHit[] {
  const q = query.trim().toLowerCase()
  if (!q) return []
  const hits: SearchHit[] = []
  for (const m of messages) {
    if (m.role === 'clear' || m.role === 'system') continue
    const hay = m.content + (thinkingText(m) ? '\n' + thinkingText(m) : '')
    const idx = hay.toLowerCase().indexOf(q)
    if (idx < 0) continue
    // 一条消息只记一次命中。同一段回答里出现五次同一个词,列表里排五行
    // 却都跳到同一个位置 —— 计数变得没有意义,翻起来也更累
    hits.push({
      id: m.id,
      role: m.role,
      before: collapse(hay.slice(Math.max(0, idx - CONTEXT_CHARS), idx)),
      match: hay.slice(idx, idx + q.length),
      after: collapse(hay.slice(idx + q.length, idx + q.length + CONTEXT_CHARS)),
    })
  }
  return hits
}

/** 摘录里的换行会把一行撑成好几行,统一压成空格 */
function collapse(s: string): string {
  return s.replace(/\s+/g, ' ')
}

export function ChatSearch({
  messages,
  onJump,
  onClose,
}: {
  messages: Message[]
  /** 定位到某条消息(滚过去 + 高亮);传空串表示取消高亮 */
  onJump: (msgId: string) => void
  onClose: () => void
}) {
  const [query, setQuery] = useState('')
  const [cursor, setCursor] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)

  const hits = useMemo(() => findHits(messages, query), [messages, query])

  useEffect(() => {
    inputRef.current?.focus()
    inputRef.current?.select()
  }, [])

  // 换关键词就回到第一条。不重置的话,上一轮停在第 7 条、
  // 新关键词只有 2 条命中,光标就落在了不存在的位置上
  useEffect(() => {
    setCursor(0)
  }, [query])

  // 光标落到哪条就跳到哪条。放在 effect 里而不是各个按钮的 onClick 里,
  // 是因为"跳过去"有三个触发源(上/下/点列表),写三遍迟早漏一个。
  //
  // 依赖只写目标消息 id,不写 hits 数组:messages 每次渲染都是新数组,
  // hits 跟着重算也是新数组 —— 按数组身份做依赖的话,流式期间每 40ms 一次渲染
  // 就是每 40ms 一次 scrollIntoView,用户想往回翻都翻不动。
  const targetId = hits[cursor]?.id ?? ''
  useEffect(() => {
    onJump(targetId)
  }, [targetId])

  const step = (delta: number) => {
    if (hits.length === 0) return
    setCursor((c) => (c + delta + hits.length) % hits.length)
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      onClose()
      return
    }
    if (e.key === 'Enter') {
      e.preventDefault()
      step(e.shiftKey ? -1 : 1)
    }
  }

  const empty = query.trim() !== '' && hits.length === 0

  return (
    <div className="shrink-0 border-b border-border bg-card/80 backdrop-blur">
      <div className="flex items-center gap-2 px-4 py-2">
        <Search className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <input
          ref={inputRef}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder="在这个会话里查找…"
          className="h-7 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
        />
        <span
          className={cn(
            'shrink-0 font-mono text-xs tabular-nums',
            empty ? 'text-destructive' : 'text-muted-foreground',
          )}
        >
          {query.trim() === '' ? '' : hits.length === 0 ? '无结果' : `${cursor + 1}/${hits.length}`}
        </span>
        <button
          type="button"
          onClick={() => step(-1)}
          disabled={hits.length === 0}
          title="上一条 (Shift+Enter)"
          className="flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:opacity-30"
        >
          <ChevronUp className="h-3.5 w-3.5" />
        </button>
        <button
          type="button"
          onClick={() => step(1)}
          disabled={hits.length === 0}
          title="下一条 (Enter)"
          className="flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:opacity-30"
        >
          <ChevronDown className="h-3.5 w-3.5" />
        </button>
        <button
          type="button"
          onClick={onClose}
          title="关闭 (Esc)"
          className="flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </div>

      {/* 命中列表。上下键一条条翻在长会话里太慢 —— 摘录能让人一眼挑中要的那条 */}
      {hits.length > 1 && (
        <ul className="max-h-40 overflow-auto border-t border-border/60 px-2 py-1">
          {hits.map((h, i) => (
            <li key={h.id}>
              <button
                type="button"
                onClick={() => setCursor(i)}
                className={cn(
                  'flex w-full items-baseline gap-2 rounded px-2 py-1 text-left text-xs transition-colors',
                  i === cursor ? 'bg-info/15 text-foreground' : 'hover:bg-secondary',
                )}
              >
                <span className="shrink-0 text-[10px] text-muted-foreground">
                  {h.role === 'user' ? '我' : 'AI'}
                </span>
                <span className="min-w-0 flex-1 truncate text-muted-foreground">
                  {h.before}
                  <mark className="rounded-sm bg-warning/40 px-0.5 text-foreground">
                    {h.match}
                  </mark>
                  {h.after}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
