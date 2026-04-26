import { useEffect, useMemo, useState } from 'react'
import { AlertCircle, Clock, Copy, Star } from 'lucide-react'
import { ToolShell } from '@/components/tool/ToolShell'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { meta } from './meta'
import {
  PRESETS,
  describe,
  nextFireTimes,
  parseCron,
} from './lib'

const FIELD_LABELS_5 = ['分', '时', '日', '月', '周']
const FIELD_LABELS_6 = ['秒', '分', '时', '日', '月', '周']

export default function CronTool() {
  const [expr, setExpr] = useState('*/5 9-18 * * 1-5')
  const [count, setCount] = useState(10)
  const [now, setNow] = useState(() => new Date())

  // 每秒刷新参考时间，仅用于相对展示
  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), 1000)
    return () => clearInterval(t)
  }, [])

  const { parsed, error, normalized } = useMemo(() => parseCron(expr), [expr])

  const fires = useMemo(() => {
    if (!parsed) return []
    // 用当前分钟/秒为起点，避免每次 now 改变都重算大列表
    const from = new Date()
    from.setMilliseconds(0)
    if (parsed.mode === 5) from.setSeconds(0)
    return nextFireTimes(parsed, from, Math.min(count, 50))
  }, [parsed, count])

  const humanReadable = useMemo(() => {
    if (!parsed) return ''
    try {
      return describe(parsed)
    } catch {
      return ''
    }
  }, [parsed])

  const fieldParts = normalized.trim().split(/\s+/)
  const labels = fieldParts.length === 6 ? FIELD_LABELS_6 : FIELD_LABELS_5

  const clear = () => setExpr('')
  const loadExample = () => setExpr('*/5 9-18 * * 1-5')

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      onClear={clear}
      onLoadExample={loadExample}
    >
      <div className="mx-auto flex h-full max-w-5xl flex-col gap-3">
        {/* 输入 */}
        <div className="flex flex-wrap items-start gap-2">
          <input
            value={expr}
            onChange={(e) => setExpr(e.target.value)}
            placeholder="输入 cron 表达式，如 0 9 * * 1-5 或 @daily"
            spellCheck={false}
            className="flex-1 rounded-md border border-border bg-background px-3 py-2 font-mono text-sm outline-none focus:border-primary/50"
          />
          <Button
            variant="ghost"
            size="sm"
            onClick={() => void navigator.clipboard.writeText(expr)}
            disabled={!expr}
          >
            <Copy className="h-3.5 w-3.5" />
            复制
          </Button>
        </div>

        {/* 字段分解 */}
        {!error && parsed && fieldParts.length === labels.length && (
          <div className="flex flex-wrap items-center gap-1.5">
            {fieldParts.map((f, i) => (
              <div
                key={i}
                className="flex items-center gap-1 rounded-md border border-border bg-secondary/40 px-2 py-1 text-xs"
              >
                <span className="text-[10px] text-muted-foreground">
                  {labels[i]}
                </span>
                <code className="font-mono font-medium">{f}</code>
              </div>
            ))}
          </div>
        )}

        {error ? (
          <div className="flex items-center gap-2 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">
            <AlertCircle className="h-3.5 w-3.5" />
            <span className="font-mono">{error}</span>
          </div>
        ) : (
          humanReadable && (
            <div className="rounded-md border border-border bg-info/5 px-3 py-2 text-sm">
              <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
                含义
              </span>
              <div className="mt-0.5 text-info">
                {humanReadable}
              </div>
            </div>
          )
        )}

        {/* 主区：左预设，中时间列表，右可视化 */}
        <div className="grid flex-1 min-h-0 grid-cols-1 gap-3 lg:grid-cols-[200px_minmax(0,1fr)_200px]">
          <PresetsPanel onPick={setExpr} current={expr} />
          <FireTimesPanel
            fires={fires}
            count={count}
            onCount={setCount}
            now={now}
            hasParsed={!!parsed}
          />
          <TimelinePanel fires={fires} now={now} />
        </div>
      </div>
    </ToolShell>
  )
}

function PresetsPanel({
  onPick,
  current,
}: {
  onPick: (v: string) => void
  current: string
}) {
  return (
    <div className="flex min-h-0 flex-col overflow-hidden rounded-md border border-border bg-card">
      <div className="flex items-center gap-1.5 border-b border-border px-2 py-1.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
        <Star className="h-3 w-3" />
        预设
      </div>
      <ul className="flex-1 overflow-auto">
        {PRESETS.map((p) => (
          <li key={p.expr}>
            <button
              onClick={() => onPick(p.expr)}
              className={cn(
                'flex w-full flex-col gap-0.5 border-b border-border/60 px-2 py-1.5 text-left text-xs hover:bg-accent',
                current === p.expr && 'bg-info/10',
              )}
            >
              <span className="font-medium">{p.name}</span>
              <code className="font-mono text-[10px] text-muted-foreground">
                {p.expr}
              </code>
            </button>
          </li>
        ))}
      </ul>
    </div>
  )
}

function FireTimesPanel({
  fires,
  count,
  onCount,
  now,
  hasParsed,
}: {
  fires: Date[]
  count: number
  onCount: (n: number) => void
  now: Date
  hasParsed: boolean
}) {
  return (
    <div className="flex min-h-0 flex-col overflow-hidden rounded-md border border-border bg-card">
      <div className="flex items-center justify-between gap-2 border-b border-border px-2 py-1.5">
        <span className="flex items-center gap-1.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
          <Clock className="h-3 w-3" />
          下 {fires.length} 次触发
        </span>
        <div className="flex items-center gap-1 text-xs">
          <span className="text-muted-foreground">条数</span>
          <select
            value={count}
            onChange={(e) => onCount(parseInt(e.target.value, 10))}
            className="rounded-md border border-border bg-background px-1.5 py-0.5 text-xs outline-none"
          >
            {[5, 10, 20, 50].map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
        </div>
      </div>
      {!hasParsed ? (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          等待合法表达式
        </div>
      ) : fires.length === 0 ? (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          未来一年内无触发
        </div>
      ) : (
        <ul className="flex-1 divide-y divide-border overflow-auto text-xs">
          {fires.map((d, i) => (
            <li key={i} className="flex items-center gap-2 px-2 py-1.5">
              <span className="shrink-0 rounded bg-info/15 px-1.5 py-0.5 font-mono text-[10px] text-info">
                #{i + 1}
              </span>
              <code className="min-w-0 flex-1 truncate font-mono">
                {formatDateTime(d)}
              </code>
              <span className="shrink-0 text-[10px] text-muted-foreground">
                {formatRelative(d, now)}
              </span>
              <button
                onClick={() => void navigator.clipboard.writeText(formatDateTime(d))}
                className="shrink-0 rounded p-1 text-muted-foreground hover:bg-accent"
                title="复制"
              >
                <Copy className="h-3 w-3" />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

/**
 * 可视化：未来 24 小时时间轴，用刻度显示触发点的时间分布。
 */
function TimelinePanel({ fires, now }: { fires: Date[]; now: Date }) {
  // 窗口：从 now 开始 24h
  const windowMs = 24 * 60 * 60 * 1000
  const start = now.getTime()
  const end = start + windowMs
  const inWindow = fires.filter((d) => d.getTime() >= start && d.getTime() <= end)

  return (
    <div className="flex min-h-0 flex-col overflow-hidden rounded-md border border-border bg-card">
      <div className="border-b border-border px-2 py-1.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
        未来 24 小时（{inWindow.length} 次）
      </div>
      <div className="relative flex-1 overflow-hidden">
        {/* 纵向时间轴 */}
        <div className="absolute inset-y-2 left-1/2 w-px -translate-x-1/2 bg-border" />
        {/* 每小时刻度 */}
        {Array.from({ length: 25 }).map((_, i) => {
          const top = (i / 24) * 100
          return (
            <div
              key={i}
              className="absolute left-0 right-0 flex items-center"
              style={{ top: `calc(${top}% * 0.95 + 0.5rem)` }}
            >
              <span className="w-10 text-right font-mono text-[9px] text-muted-foreground">
                {i === 0 ? '现在' : `+${i}h`}
              </span>
              <div className="ml-1 h-px flex-1 bg-border/40" />
            </div>
          )
        })}
        {/* 触发点 */}
        {inWindow.map((d, i) => {
          const offset = (d.getTime() - start) / windowMs
          return (
            <div
              key={i}
              className="absolute left-1/2 flex -translate-x-1/2 items-center"
              style={{ top: `calc(${offset * 95}% + 0.5rem)` }}
              title={formatDateTime(d)}
            >
              <span className="h-2 w-2 rounded-full bg-info ring-2 ring-info/30" />
              <span className="ml-2 rounded bg-info/15 px-1 py-0.5 font-mono text-[9px] text-info">
                {d.getHours().toString().padStart(2, '0')}:
                {d.getMinutes().toString().padStart(2, '0')}
              </span>
            </div>
          )
        })}
        {inWindow.length === 0 && (
          <div className="absolute inset-0 flex items-center justify-center text-[11px] text-muted-foreground">
            24 小时内无触发
          </div>
        )}
      </div>
    </div>
  )
}

function formatDateTime(d: Date): string {
  const y = d.getFullYear()
  const m = (d.getMonth() + 1).toString().padStart(2, '0')
  const day = d.getDate().toString().padStart(2, '0')
  const h = d.getHours().toString().padStart(2, '0')
  const mi = d.getMinutes().toString().padStart(2, '0')
  const s = d.getSeconds().toString().padStart(2, '0')
  const wd = ['日', '一', '二', '三', '四', '五', '六'][d.getDay()]
  return `${y}-${m}-${day} 周${wd} ${h}:${mi}:${s}`
}

function formatRelative(target: Date, from: Date): string {
  const diff = target.getTime() - from.getTime()
  if (diff < 0) return '已过去'
  const s = Math.floor(diff / 1000)
  if (s < 60) return `${s} 秒后`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} 分钟后`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} 小时 ${m % 60} 分后`
  const d = Math.floor(h / 24)
  return `${d} 天 ${h % 24} 小时后`
}
