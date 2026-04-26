import { useEffect, useState } from 'react'
import {
  AlertCircle,
  Folder,
  Loader2,
  MessageSquare,
  Search as SearchIcon,
  User,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { SearchCodexSessions } from '../../../wailsjs/go/main/App'
import type { codexinsight } from '../../../wailsjs/go/models'
import { formatRelative, shortenProject } from './lib/format'
import { SessionDetail } from './SessionDetail'

type Hit = codexinsight.SearchHit
type Result = codexinsight.SearchResult

export function Search() {
  const [query, setQuery] = useState('')
  const [result, setResult] = useState<Result | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [debounced, setDebounced] = useState('')
  const [opened, setOpened] = useState<Hit | null>(null)

  useEffect(() => {
    const id = setTimeout(() => setDebounced(query), 300)
    return () => clearTimeout(id)
  }, [query])

  useEffect(() => {
    if (!debounced.trim()) {
      setResult(null)
      setError('')
      return
    }
    let cancelled = false
    setLoading(true)
    setError('')
    SearchCodexSessions(debounced)
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

  if (opened) {
    return (
      <SessionDetail
        filePath={opened.file_path}
        project={opened.project}
        focusUUID={opened.message_uuid}
        onBack={() => setOpened(null)}
      />
    )
  }

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-3">
      <SearchBar value={query} onChange={setQuery} loading={loading} />
      {error && (
        <div className="flex items-center gap-2 rounded-md border border-red-500/40 bg-red-500/5 p-3 text-sm text-red-600 dark:text-red-400">
          <AlertCircle className="h-4 w-4" />
          {error}
        </div>
      )}
      {!debounced.trim() ? (
        <EmptyHint />
      ) : loading && !result ? (
        <Loading />
      ) : result && result.hits.length === 0 ? (
        <NoMatch query={result.query} />
      ) : result ? (
        <ResultsList result={result} query={debounced} onOpen={setOpened} />
      ) : null}
    </div>
  )
}

function SearchBar({
  value,
  onChange,
  loading,
}: {
  value: string
  onChange: (v: string) => void
  loading: boolean
}) {
  return (
    <div className="relative">
      <SearchIcon className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
      <input
        autoFocus
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="在所有 Codex 会话里搜索..."
        className="h-10 w-full rounded-md border border-border bg-background pl-9 pr-10 text-sm outline-none focus:border-foreground/30"
      />
      {loading && (
        <Loader2 className="absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 animate-spin text-muted-foreground" />
      )}
    </div>
  )
}

function EmptyHint() {
  return (
    <div className="flex h-64 flex-col items-center justify-center gap-2 text-center">
      <SearchIcon className="h-10 w-10 text-info/60" />
      <p className="max-w-md text-sm text-muted-foreground">
        输入关键词搜索所有 Codex 会话中的消息内容。大小写无关,按时间倒序展示命中片段。
      </p>
    </div>
  )
}

function Loading() {
  return (
    <div className="flex h-32 items-center justify-center gap-3 text-sm text-muted-foreground">
      <Loader2 className="h-5 w-5 animate-spin" />
      正在扫描...
    </div>
  )
}

function NoMatch({ query }: { query: string }) {
  return (
    <div className="flex h-32 flex-col items-center justify-center gap-1 text-center text-sm text-muted-foreground">
      <div>没有匹配 "{query}" 的消息</div>
    </div>
  )
}

function ResultsList({
  result,
  query,
  onOpen,
}: {
  result: Result
  query: string
  onOpen: (hit: Hit) => void
}) {
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between text-xs text-muted-foreground">
        <span>
          共 <b className="text-foreground">{result.total_hits}</b> 个命中
          {result.truncated && ` · 仅展示前 ${result.hits.length} 条`}
        </span>
      </div>
      <ul className="space-y-1.5">
        {result.hits.map((h, i) => (
          <HitRow key={`${h.file_path}-${h.message_uuid}-${i}`} hit={h} query={query} onOpen={() => onOpen(h)} />
        ))}
      </ul>
    </div>
  )
}

function HitRow({
  hit,
  query,
  onOpen,
}: {
  hit: Hit
  query: string
  onOpen: () => void
}) {
  return (
    <li>
      <button
        onClick={onOpen}
        className="flex w-full flex-col gap-1.5 rounded-lg border border-border bg-card p-3 text-left transition-colors hover:border-info/40 hover:bg-info/5"
      >
        <div className="flex items-center gap-2 text-xs">
          {hit.role === 'user' ? (
            <User className="h-3.5 w-3.5 shrink-0 text-info" />
          ) : (
            <MessageSquare className="h-3.5 w-3.5 shrink-0 text-emerald-500" />
          )}
          <span
            className={cn(
              'shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium',
              hit.role === 'user'
                ? 'bg-info/15 text-info'
                : 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-300'
            )}
          >
            {hit.role === 'user' ? '你' : 'Codex'}
          </span>
          <Folder className="h-3 w-3 shrink-0 text-muted-foreground" />
          <span className="min-w-0 flex-1 truncate font-mono text-muted-foreground" title={hit.project}>
            {shortenProject(hit.project)}
          </span>
          <span className="shrink-0 text-[10px] text-muted-foreground">
            {formatRelative(hit.timestamp)}
          </span>
        </div>
        <div className="text-sm leading-relaxed">
          <HighlightedText text={hit.snippet} query={query} />
        </div>
      </button>
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
