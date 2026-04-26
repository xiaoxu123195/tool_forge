import { useEffect, useState } from 'react'
import {
  AlertCircle,
  History as HistoryIcon,
  Loader2,
  Search as SearchIcon,
  User,
} from 'lucide-react'
import { ListCodexHistory } from '../../../wailsjs/go/main/App'
import type { codexinsight } from '../../../wailsjs/go/models'

type Item = codexinsight.HistoryItem
type Result = codexinsight.HistoryResult

export function History() {
  const [query, setQuery] = useState('')
  const [debounced, setDebounced] = useState('')
  const [result, setResult] = useState<Result | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    const id = setTimeout(() => setDebounced(query), 300)
    return () => clearTimeout(id)
  }, [query])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    ListCodexHistory(debounced)
      .then((r) => {
        if (!cancelled) setResult(r)
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [debounced])

  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-3">
      <div className="relative">
        <SearchIcon className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <input
          autoFocus
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="在 history.jsonl 里搜索..."
          className="h-10 w-full rounded-md border border-border bg-background pl-9 pr-10 text-sm outline-none focus:border-foreground/30"
        />
        {loading && (
          <Loader2 className="absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 animate-spin text-muted-foreground" />
        )}
      </div>
      {result && (
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>
            {debounced ? (
              <>
                匹配 <b className="text-foreground">{result.filtered}</b> / {result.total} 条
              </>
            ) : (
              <>共 <b className="text-foreground">{result.total}</b> 条提问</>
            )}
          </span>
          <span className="truncate font-mono text-[11px]" title={result.file_path}>
            {result.file_path}
          </span>
        </div>
      )}

      {error && (
        <div className="flex items-center gap-2 rounded-md border border-red-500/40 bg-red-500/5 p-3 text-sm text-red-600 dark:text-red-400">
          <AlertCircle className="h-4 w-4" />
          {error}
        </div>
      )}

      {!result ? null : result.items.length === 0 ? (
        <EmptyState hasQuery={!!debounced} />
      ) : (
        <ul className="space-y-1.5">
          {result.items.map((it, i) => (
            <HistoryRow key={`${it.session_id}-${it.timestamp}-${i}`} item={it} query={debounced} />
          ))}
        </ul>
      )}
    </div>
  )
}

function EmptyState({ hasQuery }: { hasQuery: boolean }) {
  return (
    <div className="flex h-40 flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-border bg-card text-center text-sm text-muted-foreground">
      <HistoryIcon className="h-8 w-8 text-info/60" />
      {hasQuery ? '没有匹配的提问' : 'history.jsonl 为空或不存在'}
    </div>
  )
}

function HistoryRow({ item, query }: { item: Item; query: string }) {
  return (
    <li className="flex items-start gap-2 rounded-md border border-border bg-card px-3 py-2">
      <User className="mt-0.5 h-3.5 w-3.5 shrink-0 text-info" />
      <div className="min-w-0 flex-1 space-y-1">
        <div className="line-clamp-3 text-sm text-foreground/90">
          <HighlightedText text={item.text} query={query} />
        </div>
        <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
          <span>{formatTimestamp(item.timestamp)}</span>
          <span className="font-mono" title={item.session_id}>
            {item.session_id.slice(0, 8)}
          </span>
        </div>
      </div>
    </li>
  )
}

function HighlightedText({ text, query }: { text: string; query: string }) {
  const q = query.trim()
  if (!q) return <>{text}</>
  const pattern = new RegExp(q.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gi')
  const parts: React.ReactNode[] = []
  let lastIndex = 0
  let m: RegExpExecArray | null
  let i = 0
  while ((m = pattern.exec(text)) !== null) {
    if (m.index > lastIndex) parts.push(text.slice(lastIndex, m.index))
    parts.push(
      <mark key={i++} className="rounded bg-info/30 px-0.5 text-foreground">
        {m[0]}
      </mark>
    )
    lastIndex = m.index + m[0].length
    if (m[0].length === 0) pattern.lastIndex++
  }
  if (lastIndex < text.length) parts.push(text.slice(lastIndex))
  return <>{parts}</>
}

function formatTimestamp(ts: number): string {
  if (!ts) return ''
  const d = new Date(ts * 1000)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString()
}
