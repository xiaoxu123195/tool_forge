import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Check,
  ChevronDown,
  ChevronUp,
  Copy,
  Image as ImageIcon,
  Link as LinkIcon,
  Pin,
  PinOff,
  Power,
  RefreshCw,
  Search,
  Trash2,
  Type,
  X,
} from 'lucide-react'
import { ToolShell } from '@/components/tool/ToolShell'
import { ScrollToTopButton } from '@/components/tool/ScrollToTopButton'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { cn } from '@/lib/utils'
import {
  ClearClipboardHistory,
  CopyClipboardItem,
  DeleteClipboardItem,
  GetClipboardImage,
  ListClipboard,
  SetClipboardEnabled,
  ToggleClipboardPin,
} from '../../../wailsjs/go/main/App'
import { EventsOn } from '../../../wailsjs/runtime/runtime'
import type { clipboard as cb } from '../../../wailsjs/go/models'
import { meta } from './meta'

type KindFilter = 'all' | 'text' | 'image' | 'link'

const URL_RE = /^https?:\/\/[^\s]+$/i

function isLink(text: string | undefined): boolean {
  if (!text) return false
  const trimmed = text.trim()
  return URL_RE.test(trimmed) && !/\s/.test(trimmed)
}

function safeHost(url: string): string {
  try {
    return new URL(url.trim()).host
  } catch {
    return ''
  }
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(2)} MB`
}

function relativeTime(ms: number): string {
  const diff = Date.now() - ms
  if (diff < 60_000) return '刚刚'
  if (diff < 3600_000) return `${Math.floor(diff / 60_000)} 分钟前`
  if (diff < 86_400_000) return `${Math.floor(diff / 3600_000)} 小时前`
  const d = new Date(ms)
  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function bucketOf(ms: number): 'today' | 'yesterday' | 'earlier' {
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  if (ms >= today) return 'today'
  if (ms >= today - 86_400_000) return 'yesterday'
  return 'earlier'
}

const BUCKET_LABEL: Record<'today' | 'yesterday' | 'earlier', string> = {
  today: '今天',
  yesterday: '昨天',
  earlier: '更早',
}

export default function ClipboardTool() {
  const confirm = useConfirm()
  const [items, setItems] = useState<cb.Item[]>([])
  const [enabled, setEnabled] = useState(true)
  const [query, setQuery] = useState('')
  const [kind, setKind] = useState<KindFilter>('all')
  const [copiedId, setCopiedId] = useState<string | null>(null)
  const [lightbox, setLightbox] = useState<{ id: string; src: string } | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const copyTimerRef = useRef<number | null>(null)

  const refresh = useCallback(async () => {
    const res = await ListClipboard()
    setItems(res.items ?? [])
    setEnabled(res.enabled)
  }, [])

  const manualRefresh = useCallback(async () => {
    setRefreshing(true)
    try {
      await refresh()
    } finally {
      // 最小旋转时长,避免一闪即过
      setTimeout(() => setRefreshing(false), 350)
    }
  }, [refresh])

  useEffect(() => {
    refresh()
    // 使用 EventsOn 返回的 cancel 函数,避免 EventsOff 误伤其他组件的同名监听
    const off = EventsOn('clipboard:new', () => {
      refresh()
    })
    return () => {
      off()
      if (copyTimerRef.current) window.clearTimeout(copyTimerRef.current)
    }
  }, [refresh])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return items.filter((it) => {
      if (kind === 'text' && it.kind !== 'text') return false
      if (kind === 'image' && it.kind !== 'image') return false
      if (kind === 'link' && !(it.kind === 'text' && isLink(it.text))) return false
      if (!q) return true
      if (it.kind === 'text') return (it.text ?? '').toLowerCase().includes(q)
      return false
    })
  }, [items, query, kind])

  const groups = useMemo(() => {
    const pinned: cb.Item[] = []
    const today: cb.Item[] = []
    const yesterday: cb.Item[] = []
    const earlier: cb.Item[] = []
    for (const it of filtered) {
      if (it.pinned) {
        pinned.push(it)
        continue
      }
      const b = bucketOf(it.createdAt)
      if (b === 'today') today.push(it)
      else if (b === 'yesterday') yesterday.push(it)
      else earlier.push(it)
    }
    return { pinned, today, yesterday, earlier }
  }, [filtered])

  const handleCopy = useCallback(async (id: string) => {
    await CopyClipboardItem(id)
    setCopiedId(id)
    if (copyTimerRef.current) window.clearTimeout(copyTimerRef.current)
    copyTimerRef.current = window.setTimeout(() => setCopiedId(null), 1200)
  }, [])

  const handleDelete = useCallback(
    async (id: string) => {
      await DeleteClipboardItem(id)
      refresh()
    },
    [refresh],
  )

  const handleTogglePin = useCallback(
    async (id: string) => {
      await ToggleClipboardPin(id)
      refresh()
    },
    [refresh],
  )

  const handleClear = useCallback(async () => {
    const ok = await confirm({
      title: '清空剪贴板历史',
      message: '将删除所有非置顶的剪贴板条目及对应图片文件,此操作不可撤销。置顶项会保留。',
      confirmLabel: '清空',
      danger: true,
    })
    if (!ok) return
    await ClearClipboardHistory()
    refresh()
  }, [confirm, refresh])

  const handleToggleEnabled = useCallback(async () => {
    await SetClipboardEnabled(!enabled)
    setEnabled(!enabled)
  }, [enabled])

  const handleOpenImage = useCallback(async (id: string) => {
    const src = await GetClipboardImage(id)
    setLightbox({ id, src })
  }, [])

  const totalCount = items.length

  return (
    <ToolShell
      title={meta.title}
      description={`${meta.description}  ·  共 ${totalCount} 条`}
      actions={
        <>
          <Button
            variant={enabled ? 'ghost' : 'secondary'}
            size="sm"
            onClick={handleToggleEnabled}
            title={enabled ? '暂停监听' : '恢复监听'}
            className={cn(!enabled && 'text-amber-600 dark:text-amber-400')}
          >
            <Power className="h-3.5 w-3.5" />
            {enabled ? '监听中' : '已暂停'}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={manualRefresh}
            disabled={refreshing}
            title="刷新"
          >
            <RefreshCw className={cn('h-3.5 w-3.5', refreshing && 'animate-spin')} />
            刷新
          </Button>
          <Button variant="ghost" size="sm" onClick={handleClear}>
            <Trash2 className="h-3.5 w-3.5" />
            清空
          </Button>
        </>
      }
    >
      <div className="flex h-full flex-col gap-3">
        <div className="flex shrink-0 items-center gap-2">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索文本内容..."
              className="h-8 w-full rounded-md border border-border bg-background pl-8 pr-3 text-sm outline-none transition-colors placeholder:text-muted-foreground/70 focus:border-indigo-500/60 focus:ring-1 focus:ring-indigo-500/30"
            />
          </div>
          <KindFilterTabs value={kind} onChange={setKind} />
        </div>

        <div data-tool-scroll className="-mx-1 flex-1 overflow-y-auto px-1 pb-2">
          {filtered.length === 0 ? (
            <EmptyState enabled={enabled} hasItems={items.length > 0} query={query} />
          ) : (
            <div className="space-y-5">
              {groups.pinned.length > 0 && (
                <Section
                  label="已置顶"
                  count={groups.pinned.length}
                  accent="indigo"
                  items={groups.pinned}
                  copiedId={copiedId}
                  onCopy={handleCopy}
                  onDelete={handleDelete}
                  onTogglePin={handleTogglePin}
                  onOpenImage={handleOpenImage}
                />
              )}
              {groups.today.length > 0 && (
                <Section
                  label={BUCKET_LABEL.today}
                  count={groups.today.length}
                  items={groups.today}
                  copiedId={copiedId}
                  onCopy={handleCopy}
                  onDelete={handleDelete}
                  onTogglePin={handleTogglePin}
                  onOpenImage={handleOpenImage}
                />
              )}
              {groups.yesterday.length > 0 && (
                <Section
                  label={BUCKET_LABEL.yesterday}
                  count={groups.yesterday.length}
                  items={groups.yesterday}
                  copiedId={copiedId}
                  onCopy={handleCopy}
                  onDelete={handleDelete}
                  onTogglePin={handleTogglePin}
                  onOpenImage={handleOpenImage}
                />
              )}
              {groups.earlier.length > 0 && (
                <Section
                  label={BUCKET_LABEL.earlier}
                  count={groups.earlier.length}
                  items={groups.earlier}
                  copiedId={copiedId}
                  onCopy={handleCopy}
                  onDelete={handleDelete}
                  onTogglePin={handleTogglePin}
                  onOpenImage={handleOpenImage}
                />
              )}
              <ScrollToTopButton threshold={120} />
            </div>
          )}
        </div>
      </div>

      {lightbox && (
        <Lightbox
          src={lightbox.src}
          onClose={() => setLightbox(null)}
          onCopy={() => {
            handleCopy(lightbox.id)
          }}
          copied={copiedId === lightbox.id}
        />
      )}
    </ToolShell>
  )
}

function KindFilterTabs({ value, onChange }: { value: KindFilter; onChange: (v: KindFilter) => void }) {
  const tabs: { id: KindFilter; label: string; icon: React.ReactNode }[] = [
    { id: 'all', label: '全部', icon: null },
    { id: 'text', label: '文本', icon: <Type className="h-3 w-3" /> },
    { id: 'image', label: '图片', icon: <ImageIcon className="h-3 w-3" /> },
    { id: 'link', label: '链接', icon: <LinkIcon className="h-3 w-3" /> },
  ]
  return (
    <div className="inline-flex h-8 shrink-0 items-center gap-0.5 rounded-md border border-border bg-secondary/40 p-0.5">
      {tabs.map((t) => (
        <button
          key={t.id}
          onClick={() => onChange(t.id)}
          className={cn(
            'inline-flex h-7 items-center gap-1 rounded px-2 text-xs font-medium transition-colors',
            value === t.id
              ? 'bg-background text-foreground shadow-sm'
              : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {t.icon}
          {t.label}
        </button>
      ))}
    </div>
  )
}

function Section({
  label,
  count,
  accent,
  items,
  copiedId,
  onCopy,
  onDelete,
  onTogglePin,
  onOpenImage,
}: {
  label: string
  count: number
  accent?: 'indigo'
  items: cb.Item[]
  copiedId: string | null
  onCopy: (id: string) => void
  onDelete: (id: string) => void
  onTogglePin: (id: string) => void
  onOpenImage: (id: string) => void
}) {
  return (
    <section>
      <div className="mb-1.5 flex items-center gap-2 px-1">
        <h3
          className={cn(
            'text-[11px] font-semibold uppercase tracking-wider',
            accent === 'indigo' ? 'text-indigo-600 dark:text-indigo-300' : 'text-muted-foreground',
          )}
        >
          {label}
        </h3>
        <span className="text-[11px] text-muted-foreground/70">{count}</span>
        <div className="flex-1 border-t border-border/60" />
      </div>
      <ul className="space-y-2">
        {items.map((it) => (
          <ItemCard
            key={it.id}
            item={it}
            copied={copiedId === it.id}
            onCopy={onCopy}
            onDelete={onDelete}
            onTogglePin={onTogglePin}
            onOpenImage={onOpenImage}
          />
        ))}
      </ul>
    </section>
  )
}

function ItemCard({
  item,
  copied,
  onCopy,
  onDelete,
  onTogglePin,
  onOpenImage,
}: {
  item: cb.Item
  copied: boolean
  onCopy: (id: string) => void
  onDelete: (id: string) => void
  onTogglePin: (id: string) => void
  onOpenImage: (id: string) => void
}) {
  const isImage = item.kind === 'image'
  const link = !isImage && isLink(item.text) ? safeHost(item.text ?? '') : ''
  const [expanded, setExpanded] = useState(false)
  // 截断条件:Preview 比 Text 短 且 Text 不为空
  const fullText = item.text ?? ''
  const previewText = item.preview ?? fullText
  const isTruncated = !isImage && fullText.length > previewText.length

  return (
    <li
      className={cn(
        'group/card relative overflow-hidden rounded-lg border bg-card transition-all',
        copied
          ? 'border-indigo-500/70 ring-2 ring-indigo-500/30'
          : 'border-border hover:border-border hover:bg-card/80 hover:shadow-sm',
      )}
    >
      <div className="flex items-start gap-3 p-3">
        {isImage ? (
          <button
            onClick={() => onOpenImage(item.id)}
            className="group/img relative h-20 w-20 shrink-0 overflow-hidden rounded-md border border-border bg-secondary/40"
          >
            {item.thumbnail ? (
              <img src={item.thumbnail} alt="" className="h-full w-full object-cover" />
            ) : (
              <div className="flex h-full w-full items-center justify-center text-muted-foreground">
                <ImageIcon className="h-6 w-6" />
              </div>
            )}
            <div className="pointer-events-none absolute inset-0 flex items-center justify-center bg-black/40 opacity-0 transition-opacity group-hover/img:opacity-100">
              <span className="text-[10px] font-medium text-white">查看大图</span>
            </div>
          </button>
        ) : (
          <KindBadge kind={item.kind as 'text'} link={link} />
        )}

        <div className="min-w-0 flex-1">
          {isImage ? (
            <div className="flex flex-col gap-1">
              <div className="flex items-center gap-2 text-xs">
                <span className="font-medium text-foreground">图片</span>
                {item.imageWidth && item.imageHeight && (
                  <span className="text-muted-foreground">
                    {item.imageWidth} × {item.imageHeight}
                  </span>
                )}
                <span className="text-muted-foreground/70">·</span>
                <span className="text-muted-foreground">{formatBytes(item.sizeBytes)}</span>
              </div>
              <div className="text-[11px] text-muted-foreground">{relativeTime(item.createdAt)}</div>
            </div>
          ) : (
            <div className="flex flex-col gap-1">
              {link && (
                <a
                  href={item.text}
                  className="inline-flex w-fit items-center gap-1 text-[11px] font-medium text-indigo-600 hover:underline dark:text-indigo-300"
                  target="_blank"
                  rel="noreferrer"
                >
                  <LinkIcon className="h-3 w-3" />
                  {link}
                </a>
              )}
              <p
                className={cn(
                  'whitespace-pre-wrap break-all text-sm text-foreground/90',
                  expanded ? 'max-h-[40vh] overflow-y-auto' : 'line-clamp-3',
                )}
              >
                {expanded ? fullText : previewText}
              </p>
              <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
                <span>{relativeTime(item.createdAt)}</span>
                <span className="text-muted-foreground/50">·</span>
                <span>{fullText.length} 字</span>
                {isTruncated && (
                  <button
                    onClick={(e) => {
                      e.stopPropagation()
                      setExpanded((v) => !v)
                    }}
                    className="ml-auto inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[11px] font-medium text-indigo-600 transition-colors hover:bg-indigo-500/10 dark:text-indigo-300"
                  >
                    {expanded ? (
                      <>
                        <ChevronUp className="h-3 w-3" />
                        收起
                      </>
                    ) : (
                      <>
                        <ChevronDown className="h-3 w-3" />
                        展开全文
                      </>
                    )}
                  </button>
                )}
              </div>
            </div>
          )}
        </div>

        <div className="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover/card:opacity-100">
          <IconBtn
            label={copied ? '已复制' : '复制'}
            onClick={() => onCopy(item.id)}
            highlight={copied}
          >
            {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
          </IconBtn>
          <IconBtn
            label={item.pinned ? '取消置顶' : '置顶'}
            onClick={() => onTogglePin(item.id)}
            highlight={item.pinned}
          >
            {item.pinned ? <PinOff className="h-3.5 w-3.5" /> : <Pin className="h-3.5 w-3.5" />}
          </IconBtn>
          <IconBtn label="删除" onClick={() => onDelete(item.id)} variant="danger">
            <Trash2 className="h-3.5 w-3.5" />
          </IconBtn>
        </div>
      </div>

      {item.pinned && (
        <span className="pointer-events-none absolute left-0 top-0 h-full w-0.5 bg-indigo-500/80" />
      )}
    </li>
  )
}

function KindBadge({ kind, link }: { kind: 'text'; link: string }) {
  if (link) {
    return (
      <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-indigo-500/10 text-indigo-600 dark:text-indigo-300">
        <LinkIcon className="h-4 w-4" />
      </div>
    )
  }
  void kind
  return (
    <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-secondary text-muted-foreground">
      <Type className="h-4 w-4" />
    </div>
  )
}

function IconBtn({
  label,
  onClick,
  children,
  variant,
  highlight,
}: {
  label: string
  onClick: () => void
  children: React.ReactNode
  variant?: 'danger'
  highlight?: boolean
}) {
  return (
    <button
      onClick={onClick}
      title={label}
      className={cn(
        'flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors',
        variant === 'danger'
          ? 'hover:bg-red-500/10 hover:text-red-600 dark:hover:text-red-400'
          : 'hover:bg-accent hover:text-foreground',
        highlight && 'text-indigo-600 dark:text-indigo-300',
      )}
    >
      {children}
    </button>
  )
}

function EmptyState({
  enabled,
  hasItems,
  query,
}: {
  enabled: boolean
  hasItems: boolean
  query: string
}) {
  let title = '尚无剪贴板记录'
  let hint = '复制任意文本或图片后会自动出现在这里。Ctrl+Shift+V 可随时唤起此页。'
  if (!enabled) {
    title = '监听已暂停'
    hint = '点击右上角 "已暂停" 按钮恢复监听。'
  } else if (hasItems && query) {
    title = `没有匹配 "${query}" 的记录`
    hint = '换个关键词试试,或切换上方的类型筛选。'
  } else if (hasItems) {
    title = '该筛选下没有记录'
    hint = '切换到"全部"试试。'
  }
  return (
    <div className="flex h-full min-h-[280px] flex-col items-center justify-center gap-3 px-6 text-center">
      <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-secondary/60 text-muted-foreground">
        <Search className="h-6 w-6" />
      </div>
      <div className="space-y-1">
        <p className="text-sm font-medium text-foreground/90">{title}</p>
        <p className="max-w-md text-xs leading-relaxed text-muted-foreground">{hint}</p>
      </div>
    </div>
  )
}

function Lightbox({
  src,
  onClose,
  onCopy,
  copied,
}: {
  src: string
  onClose: () => void
  onCopy: () => void
  copied: boolean
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div
      className="fixed inset-0 z-[110] flex items-center justify-center bg-black/80 backdrop-blur-sm"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div className="relative flex max-h-[88vh] max-w-[90vw] flex-col items-center gap-3">
        <img
          src={src}
          alt=""
          className="max-h-[80vh] max-w-[90vw] rounded-lg shadow-2xl"
        />
        <div className="flex items-center gap-2">
          <Button variant="secondary" size="sm" onClick={onCopy}>
            {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
            {copied ? '已复制' : '复制图片'}
          </Button>
          <Button variant="ghost" size="sm" onClick={onClose} className="text-white hover:bg-white/10">
            <X className="h-3.5 w-3.5" />
            关闭
          </Button>
        </div>
      </div>
    </div>
  )
}
