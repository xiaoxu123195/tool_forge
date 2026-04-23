import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  AlertCircle,
  Download,
  Folder,
  Loader2,
  MessageSquare,
  Search,
  Sparkles,
  Trash2,
  Upload,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  ScrollToTopButton,
  findToolScroller,
} from '@/components/tool/ScrollToTopButton'
import { cn } from '@/lib/utils'
import {
  DeleteClaudeSession,
  ExportClaudeSessions,
  ImportClaudeSessions,
  ListClaudeSessions,
  PickClaudeExportPath,
  PickClaudeImportPath,
} from '../../../wailsjs/go/main/App'
import type { claudeinsight } from '../../../wailsjs/go/models'
import { formatDateTime, formatDuration, formatRelative } from './lib/format'
import { SessionDetail } from './SessionDetail'

type Item = claudeinsight.SessionListItem

interface SessionsProps {
  reloadToken: number
}

export function Sessions({ reloadToken }: SessionsProps) {
  const [items, setItems] = useState<Item[] | null>(null)
  const [claudeDir, setClaudeDir] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [query, setQuery] = useState('')
  const [opened, setOpened] = useState<Item | null>(null)
  const [busy, setBusy] = useState(false)
  const [importResult, setImportResult] = useState<string>('')
  const [confirmDelete, setConfirmDelete] = useState<Item | null>(null)

  // 进入 detail 时把列表的滚动位置记下来,返回时还原
  const rootRef = useRef<HTMLDivElement>(null)
  const savedScrollRef = useRef<number>(0)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    ListClaudeSessions('')
      .then((r) => {
        if (cancelled) return
        setItems(r.items ?? [])
        setClaudeDir(r.claude_dir)
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
  }, [reloadToken])

  const openDetail = useCallback((it: Item) => {
    const sc = findToolScroller(rootRef.current)
    savedScrollRef.current = sc?.scrollTop ?? 0
    setOpened(it)
    // 进入详情默认从顶部看
    requestAnimationFrame(() => {
      if (sc) sc.scrollTop = 0
    })
  }, [])

  const closeDetail = useCallback(() => {
    setOpened(null)
    // 列表重新显示后,下一帧把滚动位置复位
    requestAnimationFrame(() => {
      const sc = findToolScroller(rootRef.current)
      if (sc) sc.scrollTop = savedScrollRef.current
    })
  }, [])

  const exportOne = async (it: Item, ev: React.MouseEvent) => {
    ev.stopPropagation()
    try {
      setBusy(true)
      const defaultName = `${(it.id || 'session').slice(0, 8)}-${Date.now()}.zip`
      const dest = await PickClaudeExportPath(defaultName)
      if (!dest) return
      const r = await ExportClaudeSessions([it.file_path], dest)
      setImportResult(`已导出 ${r.sessions} 个会话到 ${r.zip_path}`)
      setTimeout(() => setImportResult(''), 4000)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const doDelete = async (it: Item) => {
    try {
      setBusy(true)
      await DeleteClaudeSession(it.file_path)
      setItems((prev) => (prev ? prev.filter((x) => x.file_path !== it.file_path) : prev))
      setImportResult(`已删除 1 个会话`)
      setTimeout(() => setImportResult(''), 3000)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
      setConfirmDelete(null)
    }
  }

  const importZip = async () => {
    try {
      setBusy(true)
      const src = await PickClaudeImportPath()
      if (!src) return
      const r = await ImportClaudeSessions(src)
      const parts: string[] = []
      if (r.imported > 0) parts.push(`导入 ${r.imported} 个`)
      if (r.skipped > 0) parts.push(`跳过 ${r.skipped} 个(已存在)`)
      setImportResult(parts.join(' · ') || '未导入任何会话')
      setTimeout(() => setImportResult(''), 4000)
      setItems(null)
      const list = await ListClaudeSessions('')
      setItems(list.items ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const filtered = useMemo(() => {
    if (!items) return []
    const q = query.trim().toLowerCase()
    if (!q) return items
    return items.filter((it) => {
      const bag = `${it.project} ${it.preview} ${it.id}`.toLowerCase()
      return bag.includes(q)
    })
  }, [items, query])

  if (loading && !items) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
        <Loader2 className="h-6 w-6 animate-spin" />
        正在加载会话列表...
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
        <AlertCircle className="h-8 w-8 text-red-500" />
        <div className="max-w-md text-sm text-muted-foreground">{error}</div>
      </div>
    )
  }

  if (!items || items.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
        <Sparkles className="h-10 w-10 text-indigo-500" />
        <div className="space-y-1">
          <h2 className="text-base font-medium">暂无会话</h2>
          <p className="max-w-md text-sm text-muted-foreground">
            在 <code className="rounded bg-secondary px-1 py-0.5 font-mono text-xs">{claudeDir}</code>{' '}
            下没有读到任何会话文件。
          </p>
        </div>
      </div>
    )
  }

  return (
    <div ref={rootRef}>
      {/* 列表:detail 打开时保持挂载,仅隐藏,以保留滚动位置 */}
      <div
        className={cn('mx-auto flex max-w-5xl flex-col gap-3', opened && 'hidden')}
      >
        <Toolbar
          items={items}
          filtered={filtered}
          query={query}
          onQuery={setQuery}
          onImport={importZip}
          busy={busy}
        />
        {importResult && (
          <div className="rounded-md border border-emerald-500/40 bg-emerald-500/5 px-3 py-2 text-xs text-emerald-700 dark:text-emerald-300">
            {importResult}
          </div>
        )}
        {filtered.length === 0 ? (
          <EmptyFilter />
        ) : (
          <ul className="space-y-2">
            {filtered.map((it) => (
              <SessionRow
                key={it.id || it.file_path}
                item={it}
                onOpen={() => openDetail(it)}
                onExport={(ev) => exportOne(it, ev)}
                onDelete={(ev) => {
                  ev.stopPropagation()
                  setConfirmDelete(it)
                }}
              />
            ))}
          </ul>
        )}
      </div>

      {opened && (
        <SessionDetail
          filePath={opened.file_path}
          project={opened.project}
          onBack={closeDetail}
        />
      )}

      {confirmDelete && (
        <DeleteConfirm
          item={confirmDelete}
          busy={busy}
          onCancel={() => setConfirmDelete(null)}
          onConfirm={() => doDelete(confirmDelete)}
        />
      )}

      {/* detail 视图内部已经自带按钮,所以这里仅列表态渲染 */}
      {!opened && <ScrollToTopButton />}
    </div>
  )
}

function Toolbar({
  items,
  filtered,
  query,
  onQuery,
  onImport,
  busy,
}: {
  items: Item[]
  filtered: Item[]
  query: string
  onQuery: (v: string) => void
  onImport: () => void
  busy: boolean
}) {
  return (
    <div className="flex items-center gap-3">
      <div className="relative min-w-0 flex-1">
        <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <input
          value={query}
          onChange={(e) => onQuery(e.target.value)}
          placeholder="搜索项目路径或首条消息..."
          className="h-8 w-full rounded-md border border-border bg-background pl-7 pr-2 text-sm outline-none focus:border-foreground/30"
        />
      </div>
      <Button variant="ghost" size="sm" onClick={onImport} disabled={busy}>
        <Upload className="h-3.5 w-3.5" />
        导入 ZIP
      </Button>
      <span className="shrink-0 text-xs text-muted-foreground">
        {filtered.length === items.length ? `${items.length} 个` : `${filtered.length} / ${items.length}`}
      </span>
    </div>
  )
}

function EmptyFilter() {
  return (
    <div className="flex h-40 items-center justify-center text-sm text-muted-foreground">
      没有匹配的会话
    </div>
  )
}

function SessionRow({
  item,
  onOpen,
  onExport,
  onDelete,
}: {
  item: Item
  onOpen: () => void
  onExport: (ev: React.MouseEvent) => void
  onDelete: (ev: React.MouseEvent) => void
}) {
  const started = new Date(item.started_at)
  const ended = new Date(item.ended_at)
  const duration =
    Number.isNaN(started.getTime()) || Number.isNaN(ended.getTime())
      ? 0
      : Math.floor((ended.getTime() - started.getTime()) / 1000)

  return (
    <li className="group relative">
      <button
        onClick={onOpen}
        className="flex w-full flex-col gap-1.5 rounded-lg border border-border bg-card p-3 pr-20 text-left transition-colors hover:border-indigo-500/40 hover:bg-indigo-500/5"
      >
        <div className="flex items-center gap-2 text-xs">
          <Folder className="h-3.5 w-3.5 shrink-0 text-indigo-500" />
          <span className="min-w-0 flex-1 truncate font-mono text-muted-foreground" title={item.project}>
            {item.project || '—'}
          </span>
          <span className="shrink-0 rounded bg-secondary px-1.5 py-0.5 font-mono text-[11px]">
            {item.messages} 条
          </span>
          <span className="shrink-0 text-[11px] text-muted-foreground">
            {formatRelative(item.ended_at)}
          </span>
        </div>
        <div className="line-clamp-2 text-sm text-foreground/90">
          {item.preview || <span className="italic text-muted-foreground">（无文本预览）</span>}
        </div>
        <div className="flex items-center gap-3 text-[11px] text-muted-foreground">
          <span className="inline-flex items-center gap-1">
            <MessageSquare className="h-3 w-3" />
            {formatDateTime(item.started_at)}
          </span>
          {duration > 0 && <span>时长 {formatDuration(duration)}</span>}
        </div>
      </button>
      <div className="absolute right-2 top-2 hidden items-center gap-1 group-hover:inline-flex">
        <button
          onClick={onExport}
          title="导出为 ZIP"
          className="inline-flex h-7 w-7 items-center justify-center rounded text-muted-foreground hover:bg-indigo-500/15 hover:text-indigo-600 dark:hover:text-indigo-300"
        >
          <Download className="h-3.5 w-3.5" />
        </button>
        <button
          onClick={onDelete}
          title="删除此会话"
          className="inline-flex h-7 w-7 items-center justify-center rounded text-muted-foreground hover:bg-red-500/15 hover:text-red-600 dark:hover:text-red-300"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>
    </li>
  )
}

function DeleteConfirm({
  item,
  busy,
  onCancel,
  onConfirm,
}: {
  item: Item
  busy: boolean
  onCancel: () => void
  onConfirm: () => void
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm">
      <div className="w-[420px] max-w-[90vw] rounded-lg border border-border bg-card p-5 shadow-xl">
        <h3 className="mb-1.5 text-base font-semibold">删除会话</h3>
        <p className="mb-3 text-sm text-muted-foreground">
          这个 Claude 会话文件将被从磁盘永久删除,无法恢复。
        </p>
        <div className="mb-4 space-y-1 rounded-md border border-border bg-secondary/40 p-2 text-[11px]">
          <div className="truncate font-mono text-muted-foreground" title={item.project}>
            {item.project || '—'}
          </div>
          <div className="truncate font-mono text-foreground/90" title={item.file_path}>
            {item.file_path}
          </div>
        </div>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onCancel} disabled={busy}>
            取消
          </Button>
          <Button
            size="sm"
            onClick={onConfirm}
            disabled={busy}
            className="bg-red-600 text-white hover:bg-red-700"
          >
            {busy ? '删除中...' : '确认删除'}
          </Button>
        </div>
      </div>
    </div>
  )
}
