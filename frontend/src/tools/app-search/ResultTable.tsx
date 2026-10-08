import { useMemo, useState } from 'react'
import {
  AlertCircle,
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
  ExternalLink,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { SOURCES, type SourceID } from './types'
import { AppIcon, IconOutcomeLine, useIconActions } from './AppIcon'
import type { appsearch } from '../../../wailsjs/go/models'

interface Props {
  items: appsearch.SearchResultItem[]
  statuses: appsearch.SourceStatus[]
}

export function ResultTable({ items, statuses }: Props) {
  const [filter, setFilter] = useState('')

  const sourceLabel = (id: string) =>
    SOURCES.find((s) => s.id === id)?.label ?? id

  // 分组：同一来源的结果聚在一起；即使该源 0 条也保留占位（仅当无过滤时）
  const grouped = useMemo(() => {
    const q = filter.trim().toLowerCase()
    const byId = new Map<string, appsearch.SearchResultItem[]>()
    for (const it of items) {
      if (
        q &&
        !it.name.toLowerCase().includes(q) &&
        !(it.pkgName ?? '').toLowerCase().includes(q) &&
        !(it.developer ?? '').toLowerCase().includes(q) &&
        !(it.extra?.trackId ?? '').toLowerCase().includes(q)
      ) {
        continue
      }
      const key = it.source as string
      if (!byId.has(key)) byId.set(key, [])
      byId.get(key)!.push(it)
    }
    // 保持 statuses 顺序；成功但 0 条的源也给一个空组，让用户看到"该源 0 条"
    return statuses
      .filter((s) => s.ok || byId.has(s.source as string))
      .map((s) => ({
        source: s.source as string,
        items: byId.get(s.source as string) ?? [],
      }))
  }, [items, statuses, filter])

  if (statuses.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-border bg-muted/20 p-8 text-center text-sm text-muted-foreground">
        输入关键词并点击搜索
      </div>
    )
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <SourceBar statuses={statuses} />
        <div className="ml-auto">
          <input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="在结果中过滤（名字/包名/开发者）"
            className="h-7 w-64 rounded-md border border-input bg-background px-2.5 text-xs outline-none focus:ring-1 focus:ring-ring"
          />
        </div>
      </div>

      {items.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border bg-muted/20 p-8 text-center text-sm text-muted-foreground">
          没有返回结果
        </div>
      ) : grouped.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border bg-muted/20 p-8 text-center text-sm text-muted-foreground">
          没有匹配「{filter}」的结果
        </div>
      ) : (
        <div className="space-y-4">
          {grouped.map((g) => (
            <SourceGroup
              key={g.source}
              source={g.source}
              label={sourceLabel(g.source)}
              items={g.items}
              isFiltered={filter.trim().length > 0}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function SourceBar({ statuses }: { statuses: appsearch.SourceStatus[] }) {
  const labelOf = (id: string) => SOURCES.find((s) => s.id === id)?.label ?? id

  const failed = statuses.filter((st) => !st.ok && st.error)
  // 成功但空得可疑的,后端会附一句说明(见 SourceStatus.Note)
  const noted = statuses.filter((st) => st.ok && st.note)

  return (
    <div className="space-y-1.5">
      <div className="flex flex-wrap gap-1.5">
        {statuses.map((st) => (
          <div
            key={st.source}
            className={cn(
              'flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs',
              st.ok
                ? 'border-emerald-500/30 bg-emerald-500/5 text-emerald-700 dark:text-emerald-400'
                : 'border-destructive/30 bg-destructive/5 text-destructive'
            )}
          >
            {st.ok ? <Check className="h-3 w-3" /> : <AlertCircle className="h-3 w-3" />}
            <span className="font-medium">{labelOf(st.source)}</span>
            {st.ok && <span className="text-muted-foreground">· {st.count}</span>}
          </div>
        ))}
      </div>
      {/* 失败原因原来只在 title 里 —— 一个源挂了,界面上就是个小红药丸,
          得把鼠标悬上去才知道为什么。这类原因("登录态过期"之类)恰恰是
          用户当场就能处理的,藏起来没有道理 */}
      {failed.map((st) => (
        <div
          key={st.source}
          className="flex items-start gap-1.5 text-[11px] text-destructive"
        >
          <AlertCircle className="mt-0.5 h-3 w-3 shrink-0" />
          <span>
            <span className="font-medium">{labelOf(st.source)}</span>：{st.error}
          </span>
        </div>
      ))}
      {/* 「接口回了成功、零条」在界面上和「真的没这个 App」一模一样。
          不说一句的话,用户只会反复换关键词 */}
      {noted.map((st) => (
        <div
          key={st.source}
          className="flex items-start gap-1.5 text-[11px] text-amber-700 dark:text-amber-400"
        >
          <AlertCircle className="mt-0.5 h-3 w-3 shrink-0" />
          <span>
            <span className="font-medium">{labelOf(st.source)}</span>：{st.note}
          </span>
        </div>
      ))}
    </div>
  )
}

function SourceGroup({
  source,
  label,
  items,
  isFiltered,
}: {
  source: string
  label: string
  items: appsearch.SearchResultItem[]
  isFiltered: boolean
}) {
  // 有过滤时全部展开；否则默认只展开第一条
  const [expanded, setExpanded] = useState(false)
  const isEmpty = items.length === 0
  const restCount = Math.max(0, items.length - 1)
  const showAll = isFiltered || expanded
  const visible = showAll ? items : items.slice(0, 1)

  return (
    <div className="rounded-lg border border-border bg-card">
      <div className="flex items-center justify-between border-b border-border px-4 py-2 text-xs">
        <div className="flex items-center gap-2">
          <span className="font-semibold">{label}</span>
          <span className="text-muted-foreground">· {items.length} 条</span>
          {!isFiltered && !isEmpty && restCount > 0 && !expanded && (
            <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
              仅显示第 1 条
            </span>
          )}
        </div>
        {!isFiltered && restCount > 0 && (
          <button
            onClick={() => setExpanded((v) => !v)}
            className="flex items-center gap-1 rounded-md px-2 py-0.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            {expanded ? (
              <>
                <ChevronDown className="h-3.5 w-3.5" />
                收起
              </>
            ) : (
              <>
                <ChevronRight className="h-3.5 w-3.5" />
                展开剩余 {restCount} 条
              </>
            )}
          </button>
        )}
      </div>
      {isEmpty ? (
        <div className="px-4 py-6 text-center text-xs text-muted-foreground">
          该源没有匹配「{isFiltered ? '过滤词' : '关键词'}」的结果
        </div>
      ) : (
        <div className="divide-y divide-border">
          {/* key 带上图标地址:行里记着「已保存」这类状态,换一次搜索、
              同一个位置上换成别的 App 时得重新来过,不能把上一个的结果挂过来 */}
          {visible.map((it, idx) => (
            <ResultRow key={`${source}-${idx}-${it.icon || it.name}`} item={it} />
          ))}
        </div>
      )}
    </div>
  )
}

function ResultRow({ item }: { item: appsearch.SearchResultItem }) {
  const pkg = item.pkgName || ''
  const trackId = item.extra?.trackId || ''
  const primaryId = pkg || trackId
  const primaryLabel = pkg
    ? item.platform === 'ios'
      ? 'bundleId'
      : '包名'
    : 'trackId'

  // 元信息标签：版本 / 评分 / 分类 / 大小 / 国家
  const meta: string[] = []
  if (item.version) meta.push(`v${item.version}`)
  if (item.rating) meta.push(`★ ${item.rating.toFixed(2)}`)
  const genre = item.extra?.genre
  if (genre) meta.push(genre)
  const fileSize = item.extra?.fileSize
  if (fileSize) meta.push(fileSize)
  if (item.country) meta.push(item.country.toUpperCase())

  const trackURL = item.extra?.url
  const iconActions = useIconActions(item.icon ?? '', item.name, primaryId)

  return (
    <div className="flex gap-3 p-3 transition-colors hover:bg-accent/30">
      <AppIcon
        src={item.icon}
        busy={iconActions.busy}
        onSave={iconActions.save}
        onCopy={iconActions.copy}
      />

      <div className="min-w-0 flex-1 space-y-1.5">
        <div className="flex flex-wrap items-center gap-2">
          <span className="truncate text-sm font-semibold">{item.name}</span>
          <span
            className={cn(
              'shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium',
              item.platform === 'ios'
                ? 'bg-blue-500/10 text-blue-600 dark:text-blue-400'
                : 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
            )}
          >
            {item.platform.toUpperCase()}
          </span>
          {trackURL && (
            <a
              href={trackURL}
              target="_blank"
              rel="noreferrer"
              title="在应用商店中打开"
              className="shrink-0 text-muted-foreground transition-colors hover:text-foreground"
            >
              <ExternalLink className="h-3.5 w-3.5" />
            </a>
          )}
        </div>

        {item.developer && (
          <div className="truncate text-xs text-muted-foreground">{item.developer}</div>
        )}

        {primaryId && (
          <PkgLine label={primaryLabel} value={primaryId} muted={!pkg} />
        )}

        {meta.length > 0 && (
          <div className="flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground">
            {meta.map((m, i) => (
              <span key={i}>{m}</span>
            ))}
          </div>
        )}

        <IconOutcomeLine outcome={iconActions.outcome} />
      </div>
    </div>
  )
}

function PkgLine({
  label,
  value,
  muted,
}: {
  label: string
  value: string
  muted?: boolean
}) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1200)
    } catch {
      /* ignore */
    }
  }

  return (
    <button
      onClick={copy}
      title="点击复制"
      className={cn(
        'group flex max-w-full items-center gap-1.5 rounded border border-dashed border-border bg-background px-2 py-1 text-left transition-colors hover:border-foreground/40 hover:bg-accent/50'
      )}
    >
      <span className="shrink-0 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      <code
        className={cn(
          'truncate font-mono text-xs',
          muted ? 'text-muted-foreground' : 'text-foreground'
        )}
      >
        {value}
      </code>
      {copied ? (
        <Check className="h-3 w-3 shrink-0 text-emerald-500" />
      ) : (
        <Copy className="h-3 w-3 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
      )}
    </button>
  )
}
