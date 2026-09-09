import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { MessageSquare, Search, X } from 'lucide-react'
import { SearchAIConversations } from '../../../wailsjs/go/main/App'
import type { SearchResult } from './types'
import { cn } from '@/lib/utils'

/** 输入停顿多久才发一次搜索。每敲一下就读一遍所有会话文件太浪费 */
const DEBOUNCE_MS = 200

/**
 * 跨会话搜索。
 *
 * 会话内搜索(Ctrl+F)回答的是"这条对话里那句话在哪";这个回答的是
 * "我上次问过的那个东西,在十几条会话的哪一条里" —— 后者只能靠一条条翻,
 * 所以单独给一个入口(Ctrl+Shift+F)。
 *
 * 搜索在后端做:会话是一条一个 JSON 文件,全捞到前端再过滤等于把几 MB 数据
 * 搬过来只为了扔掉 99%。
 */
export function GlobalSearchDialog({
  onPick,
  onClose,
}: {
  /** 选中某处命中:切到那条会话并定位到那条消息 */
  onPick: (convId: string, messageId: string) => void
  onClose: () => void
}) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [searching, setSearching] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  useEffect(() => {
    const q = query.trim()
    if (!q) {
      setResults([])
      setSearching(false)
      return
    }
    setSearching(true)
    let alive = true
    const timer = setTimeout(() => {
      void (async () => {
        const r = ((await SearchAIConversations(q).catch(() => [])) ??
          []) as unknown as SearchResult[]
        if (!alive) return
        setResults(r)
        setSearching(false)
      })()
    }, DEBOUNCE_MS)
    return () => {
      alive = false
      clearTimeout(timer)
    }
  }, [query])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  // ?? [] 不是多余的谨慎:后端任何一个 nil 切片过 JSON 都会变成 null,
  // 而这一行在 reduce 里,炸了就是整页白 —— 类型上写着数组不代表运行时拿到的是数组
  const totalHits = results.reduce((n, r) => n + (r.hits ?? []).length + (r.more ?? 0), 0)
  const empty = query.trim() !== '' && !searching && results.length === 0

  return createPortal(
    <div
      className="fixed inset-0 z-[75] flex items-start justify-center bg-black/50 p-6 pt-[12vh]"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div className="flex max-h-[70vh] w-full max-w-2xl flex-col overflow-hidden rounded-lg border border-border bg-card shadow-2xl">
        <div className="flex shrink-0 items-center gap-2 border-b border-border px-4 py-3">
          <Search className="h-4 w-4 shrink-0 text-muted-foreground" />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="在所有会话里查找…"
            className="h-6 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
          <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
            {searching
              ? '搜索中…'
              : query.trim() === ''
                ? ''
                : `${results.length} 条会话 · ${totalHits} 处`}
          </span>
          <button
            type="button"
            onClick={onClose}
            title="关闭 (Esc)"
            className="flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-auto p-2">
          {query.trim() === '' ? (
            <p className="px-3 py-8 text-center text-xs text-muted-foreground">
              搜标题、正文和思考内容 · Esc 关闭
            </p>
          ) : empty ? (
            <p className="px-3 py-8 text-center text-xs text-muted-foreground">
              没找到「{query.trim()}」
            </p>
          ) : (
            results.map((r) => (
              <div key={r.convId} className="mb-2">
                <div className="flex items-center gap-1.5 px-2 py-1 text-xs font-medium">
                  <MessageSquare className="h-3 w-3 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 truncate">{r.title}</span>
                  {r.titleMatch && (
                    <span className="shrink-0 rounded bg-info/15 px-1 text-[10px] text-info">
                      标题匹配
                    </span>
                  )}
                </div>
                {/* 只有标题命中、正文一处都没有时,给一个能点的入口 ——
                    否则这条会话在结果里就是个点不动的标题 */}
                {(r.hits ?? []).length === 0 ? (
                  <button
                    type="button"
                    onClick={() => onPick(r.convId, '')}
                    className="w-full rounded px-3 py-1 text-left text-xs text-muted-foreground transition-colors hover:bg-secondary"
                  >
                    打开这条会话
                  </button>
                ) : (
                  (r.hits ?? []).map((h) => (
                    <button
                      key={h.messageId}
                      type="button"
                      onClick={() => onPick(r.convId, h.messageId)}
                      className="flex w-full items-baseline gap-2 rounded px-3 py-1 text-left text-xs transition-colors hover:bg-secondary"
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
                  ))
                )}
                {(r.more ?? 0) > 0 && (
                  <div className="px-3 py-0.5 text-[10px] text-muted-foreground">
                    这条会话里另有 {r.more} 处
                  </div>
                )}
              </div>
            ))
          )}
        </div>
      </div>
    </div>,
    document.body,
  )
}

/** 是不是"全局搜索"那个组合键(Ctrl/Cmd+Shift+F)。Ctrl+F 归会话内搜索 */
export function isGlobalSearchHotkey(e: KeyboardEvent): boolean {
  return (e.ctrlKey || e.metaKey) && e.shiftKey && e.key.toLowerCase() === 'f'
}
