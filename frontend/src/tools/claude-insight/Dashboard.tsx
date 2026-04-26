import { useEffect, useMemo, useState } from 'react'
import {
  AlertCircle,
  Calendar,
  Clock,
  Coins,
  Folder,
  Loader2,
  MessageSquare,
  ShieldCheck,
  Sparkles,
  Zap,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { BuildClaudeDashboard } from '../../../wailsjs/go/main/App'
import type { claudeinsight } from '../../../wailsjs/go/models'
import {
  formatDate,
  formatDuration,
  formatLocalDate,
  formatRelative,
  formatTokens,
  shortenProject,
  weekdayLabel,
} from './lib/format'

type Report = claudeinsight.DashboardReport

interface DashboardProps {
  reloadToken: number
}

export function Dashboard({ reloadToken }: DashboardProps) {
  const [report, setReport] = useState<Report | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    BuildClaudeDashboard('')
      .then((r) => {
        if (!cancelled) setReport(r)
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

  if (loading && !report) return <Loading />
  if (error) return <ErrorBox message={error} />
  if (!report) return null
  if (report.total_sessions === 0) return <EmptyClaudeDir dir={report.claude_dir} />

  return (
    <div className="mx-auto max-w-6xl space-y-5">
      <PrivacyBanner dir={report.claude_dir} />
      <OverviewCards report={report} />
      <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
        <Last7DaysChart buckets={report.last_7_days} />
        <HourDistributionChart hours={report.hour_distribution} />
      </div>
      <CalendarHeatmap buckets={report.calendar} />
      <TokensByModelTable rows={report.tokens_by_model} />
      <LongestSessionBanner s={report.longest_session} />
      <RecentSessionsList list={report.recent_sessions} />
    </div>
  )
}

function Loading() {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
      <Loader2 className="h-6 w-6 animate-spin" />
      正在扫描 ~/.claude/projects ...
    </div>
  )
}

function ErrorBox({ message }: { message: string }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
      <AlertCircle className="h-8 w-8 text-red-500" />
      <div className="max-w-md text-sm text-muted-foreground">{message}</div>
    </div>
  )
}

function EmptyClaudeDir({ dir }: { dir: string }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
      <Sparkles className="h-10 w-10 text-info" />
      <div className="space-y-1">
        <h2 className="text-base font-medium">未找到任何 Claude Code 会话</h2>
        <p className="max-w-md text-sm text-muted-foreground">
          在 <code className="rounded bg-secondary px-1 py-0.5 font-mono text-xs">{dir}</code>{' '}
          下没有读到 JSONL 会话文件。
          如果你还没有用过 Claude Code 就会是这个状态。
        </p>
      </div>
    </div>
  )
}

function PrivacyBanner({ dir }: { dir: string }) {
  return (
    <div className="flex items-center gap-2 rounded-md border border-border bg-card px-3 py-2 text-xs text-muted-foreground">
      <ShieldCheck className="h-3.5 w-3.5 shrink-0 text-emerald-500" />
      <span>所有数据仅在本地读取,不上传任何服务器。</span>
      <span className="ml-auto truncate font-mono text-[11px]" title={dir}>
        {dir}
      </span>
    </div>
  )
}

function OverviewCards({ report }: { report: Report }) {
  const totalTokens = useMemo(
    () =>
      report.tokens_by_model.reduce(
        (sum, m) =>
          sum +
          m.input_tokens +
          m.output_tokens +
          m.cache_creation_tokens +
          m.cache_read_tokens,
        0
      ),
    [report.tokens_by_model]
  )

  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
      <StatCard icon={<MessageSquare className="h-4 w-4" />} label="总会话" value={report.total_sessions.toLocaleString()} />
      <StatCard icon={<Zap className="h-4 w-4" />} label="总消息" value={report.total_messages.toLocaleString()} />
      <StatCard icon={<Calendar className="h-4 w-4" />} label="活跃天数" value={report.active_days.toLocaleString()} />
      <StatCard icon={<Coins className="h-4 w-4" />} label="总 Token" value={formatTokens(totalTokens)} />
      <StatCard
        icon={<Clock className="h-4 w-4" />}
        label="首次使用"
        value={formatDate(report.first_used_at)}
        wide
      />
      <StatCard
        icon={<Clock className="h-4 w-4" />}
        label="最近使用"
        value={formatRelative(report.last_used_at)}
        wide
      />
    </div>
  )
}

function StatCard({
  icon,
  label,
  value,
  wide,
}: {
  icon: React.ReactNode
  label: string
  value: string
  wide?: boolean
}) {
  return (
    <div
      className={cn(
        'group rounded-lg border border-border bg-card p-3 transition-all duration-200 hover:-translate-y-0.5 hover:shadow-md hover:shadow-foreground/5',
        wide && 'md:col-span-2'
      )}
    >
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span className="flex h-6 w-6 items-center justify-center rounded-md bg-gradient-to-br from-info/20 to-info/10 text-info transition-transform duration-200 group-hover:scale-110">
          {icon}
        </span>
        {label}
      </div>
      <div className="mt-1.5 text-xl font-semibold tracking-tight">{value}</div>
    </div>
  )
}

function Last7DaysChart({ buckets }: { buckets: claudeinsight.DailyBucket[] }) {
  const max = Math.max(1, ...buckets.map((b) => b.messages))
  const total = buckets.reduce((s, b) => s + b.messages, 0)
  const peak = buckets.reduce((p, b) => (b.messages > p.messages ? b : p), buckets[0])
  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <div className="mb-3 flex items-baseline justify-between gap-2">
        <h3 className="text-sm font-medium">近 7 天消息数</h3>
        <span className="text-[11px] text-muted-foreground">
          共 {total.toLocaleString()} 条 · 日均{' '}
          {Math.round(total / 7).toLocaleString()}
          {peak && peak.messages > 0 && (
            <> · 峰值 {peak.messages} 条（{weekdayLabel(peak.date)}）</>
          )}
        </span>
      </div>
      <div className="flex h-36 items-end gap-3">
        {buckets.map((b) => {
          const h = (b.messages / max) * 100
          return (
            <div
              key={b.date}
              className="group flex flex-1 flex-col items-center gap-1"
              title={`${b.date} · ${b.messages} 条`}
            >
              <div className="text-[11px] font-mono tabular-nums text-foreground/80">
                {b.messages > 0 ? b.messages : ''}
              </div>
              <div className="flex w-full flex-1 items-end">
                <div
                  className={cn(
                    'w-full rounded-t transition-colors',
                    b.messages === 0
                      ? 'bg-secondary'
                      : 'bg-info/40 group-hover:bg-info/70'
                  )}
                  style={{ height: `${h}%`, minHeight: b.messages > 0 ? 4 : 2 }}
                />
              </div>
              <div className="text-[10px] text-muted-foreground">
                {weekdayLabel(b.date)}
              </div>
              <div className="text-[10px] text-muted-foreground/70">
                {b.date.slice(5)}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function HourDistributionChart({ hours }: { hours: number[] }) {
  const max = Math.max(1, ...hours)
  const total = hours.reduce((s, n) => s + n, 0)
  const peakHour = hours.reduce((pi, n, i) => (n > hours[pi] ? i : pi), 0)
  const peakValue = hours[peakHour]
  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <div className="mb-3 flex items-baseline justify-between gap-2">
        <h3 className="text-sm font-medium">24 小时活跃分布</h3>
        <span className="text-[11px] text-muted-foreground">
          {peakValue > 0
            ? `最活跃 ${peakHour.toString().padStart(2, '0')}:00 · ${peakValue} 条`
            : '暂无数据'}
        </span>
      </div>
      <div className="flex h-36 items-end gap-[3px]">
        {hours.map((n, i) => {
          const h = (n / max) * 100
          const isPeak = n > 0 && i === peakHour
          return (
            <div
              key={i}
              className="group relative flex flex-1 flex-col items-center"
              title={`${i.toString().padStart(2, '0')}:00 · ${n} 条`}
            >
              <div className="flex h-full w-full items-end">
                <div
                  className={cn(
                    'w-full rounded-t transition-colors',
                    n === 0
                      ? 'bg-secondary'
                      : isPeak
                      ? 'bg-info/80 group-hover:bg-info'
                      : 'bg-info/40 group-hover:bg-info/70'
                  )}
                  style={{ height: `${h}%`, minHeight: n > 0 ? 3 : 2 }}
                />
              </div>
            </div>
          )
        })}
      </div>
      <div className="mt-2 flex justify-between text-[10px] text-muted-foreground">
        <span>00</span>
        <span>06</span>
        <span>12</span>
        <span>18</span>
        <span>23</span>
      </div>
      {total > 0 && (
        <div className="mt-1 text-[10px] text-muted-foreground/70">
          合计 {total.toLocaleString()} 条消息
        </div>
      )}
    </div>
  )
}

function CalendarHeatmap({ buckets }: { buckets: claudeinsight.DailyBucket[] }) {
  const weeks = 26
  const days = weeks * 7
  const today = new Date()
  today.setHours(0, 0, 0, 0)

  const map = new Map<string, number>()
  for (const b of buckets) map.set(b.date, b.messages)

  const cells: { date: string; messages: number }[] = []
  for (let i = days - 1; i >= 0; i--) {
    const d = new Date(today)
    d.setDate(d.getDate() - i)
    const key = formatLocalDate(d)
    cells.push({ date: key, messages: map.get(key) ?? 0 })
  }

  const max = Math.max(1, ...cells.map((c) => c.messages))

  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <h3 className="mb-3 text-sm font-medium">活跃日历（最近 26 周）</h3>
      <div className="flex gap-[3px] overflow-x-auto">
        {Array.from({ length: weeks }).map((_, w) => (
          <div key={w} className="flex flex-col gap-[3px]">
            {Array.from({ length: 7 }).map((_, d) => {
              const idx = w * 7 + d
              const c = cells[idx]
              if (!c) return <div key={d} className="h-3 w-3" />
              const level = c.messages === 0 ? 0 : Math.ceil((c.messages / max) * 4)
              return (
                <div
                  key={d}
                  title={`${c.date} · ${c.messages} 条`}
                  className={cn('h-3 w-3 rounded-sm', heatLevelClass(level))}
                />
              )
            })}
          </div>
        ))}
      </div>
    </div>
  )
}

function heatLevelClass(level: number): string {
  switch (level) {
    case 0:
      return 'bg-secondary'
    case 1:
      return 'bg-info/20'
    case 2:
      return 'bg-info/40'
    case 3:
      return 'bg-info/60'
    default:
      return 'bg-info/90'
  }
}

function TokensByModelTable({ rows }: { rows: claudeinsight.ModelTokens[] }) {
  if (rows.length === 0) return null
  return (
    <div className="overflow-hidden rounded-lg border border-border bg-card">
      <div className="border-b border-border px-4 py-2 text-sm font-medium">
        按模型统计 Token
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="bg-secondary/50 text-xs text-muted-foreground">
            <tr>
              <th className="px-3 py-2 text-left font-medium">模型</th>
              <th className="px-3 py-2 text-right font-medium">消息</th>
              <th className="px-3 py-2 text-right font-medium">Input</th>
              <th className="px-3 py-2 text-right font-medium">Output</th>
              <th className="px-3 py-2 text-right font-medium">Cache 写</th>
              <th className="px-3 py-2 text-right font-medium">Cache 读</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.model} className="border-t border-border">
                <td className="px-3 py-2 font-mono text-xs">{r.model}</td>
                <td className="px-3 py-2 text-right font-mono text-xs">{r.messages.toLocaleString()}</td>
                <td className="px-3 py-2 text-right font-mono text-xs">{formatTokens(r.input_tokens)}</td>
                <td className="px-3 py-2 text-right font-mono text-xs">{formatTokens(r.output_tokens)}</td>
                <td className="px-3 py-2 text-right font-mono text-xs">{formatTokens(r.cache_creation_tokens)}</td>
                <td className="px-3 py-2 text-right font-mono text-xs">{formatTokens(r.cache_read_tokens)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function LongestSessionBanner({ s }: { s?: claudeinsight.SessionSummary }) {
  if (!s) return null
  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-2 rounded-lg border border-border bg-gradient-to-r from-info/10 to-transparent px-4 py-3">
      <div className="flex items-center gap-2 text-sm font-medium">
        <Sparkles className="h-4 w-4 text-info" />
        最长会话
      </div>
      <div className="flex min-w-0 flex-1 items-center gap-2 text-xs text-muted-foreground">
        <Folder className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate font-mono" title={s.project}>
          {s.project || '—'}
        </span>
      </div>
      <BannerMetric label="消息" value={s.messages.toLocaleString()} />
      <BannerMetric label="时长" value={formatDuration(s.duration_sec)} />
      <BannerMetric
        label="时间"
        value={`${formatDate(s.started_at)} → ${formatDate(s.ended_at)}`}
      />
    </div>
  )
}

function BannerMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline gap-1.5">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="font-mono text-sm">{value}</span>
    </div>
  )
}

function RecentSessionsList({ list }: { list: claudeinsight.SessionSummary[] }) {
  if (list.length === 0) return null
  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <h3 className="mb-3 text-sm font-medium">最近会话</h3>
      <ul className="space-y-2">
        {list.map((s) => (
          <li key={s.id} className="flex items-center gap-2 text-sm">
            <MessageSquare className="h-3.5 w-3.5 shrink-0 text-info" />
            <span className="min-w-0 flex-1 truncate font-mono text-xs" title={s.project}>
              {shortenProject(s.project)}
            </span>
            <span className="shrink-0 rounded bg-secondary px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">
              {s.messages} 条
            </span>
            <span className="shrink-0 text-[11px] text-muted-foreground">
              {formatRelative(s.ended_at)}
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}

