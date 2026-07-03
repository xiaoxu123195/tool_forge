import { useCallback, useEffect, useMemo, useState } from 'react'
import { Activity, AlertTriangle, Clock, Coins, Gauge, Loader2, RefreshCw, Timer, Zap } from 'lucide-react'
import { cn } from '@/lib/utils'
import { CostCard } from '@/components/tool/CostCard'
import { usePricingStore, priceForModel, formatUSD } from '@/lib/pricing'
import { GetLLMProxyStats } from '../../../wailsjs/go/main/App'
import type { llmproxy } from '../../../wailsjs/go/models'
import { fmtMs, fmtTokens } from './lib'

type Range = 'today' | '7d' | '30d' | 'all'

const RANGES: { value: Range; label: string }[] = [
  { value: 'today', label: '今天' },
  { value: '7d', label: '近 7 天' },
  { value: '30d', label: '近 30 天' },
  { value: 'all', label: '全部' },
]

function sinceFor(r: Range): number {
  if (r === 'all') return 0
  const d = new Date()
  if (r === 'today') {
    d.setHours(0, 0, 0, 0)
    return d.getTime()
  }
  const days = r === '7d' ? 7 : 30
  return Date.now() - days * 86400_000
}

export function Dashboard({ reloadToken }: { reloadToken: number }) {
  const [range, setRange] = useState<Range>('7d')
  const [stats, setStats] = useState<llmproxy.Stats | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(async (r: Range) => {
    setLoading(true)
    setError('')
    try {
      const s = await GetLLMProxyStats({ since: sinceFor(r), until: 0 })
      setStats(s)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load(range)
  }, [range, reloadToken, load])

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <div className="inline-flex items-center rounded-md border border-border bg-background p-0.5">
          {RANGES.map((r) => (
            <button
              key={r.value}
              onClick={() => setRange(r.value)}
              className={cn(
                'rounded-sm px-2.5 py-1 text-[11px] font-medium transition-colors',
                range === r.value ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {r.label}
            </button>
          ))}
        </div>
        <button
          onClick={() => load(range)}
          className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-secondary hover:text-foreground"
          title="刷新"
        >
          <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />
        </button>
        {loading && !stats && <span className="text-xs text-muted-foreground">加载中…</span>}
      </div>

      {error && (
        <div className="rounded-lg border border-red-500/40 bg-red-500/5 p-2.5 text-xs text-red-600 dark:text-red-400">{error}</div>
      )}

      {stats && (stats.requests === 0 ? <EmptyDash /> : <StatsBody stats={stats} />)}
      {!stats && loading && (
        <div className="flex h-40 items-center justify-center text-muted-foreground">
          <Loader2 className="h-5 w-5 animate-spin" />
        </div>
      )}
    </div>
  )
}

function EmptyDash() {
  return (
    <div className="flex h-40 flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-border text-sm text-muted-foreground">
      <Activity className="h-6 w-6" />
      这个时间范围内还没有请求记录
    </div>
  )
}

function StatsBody({ stats }: { stats: llmproxy.Stats }) {
  const prices = usePricingStore((s) => s.prices)

  const totalCost = useMemo(() => {
    let sum = 0
    for (const m of stats.models) {
      const p = priceForModel(m.model, prices)
      if (p) sum += (m.promptTokens * p.input + m.completionTokens * p.output) / 1_000_000
    }
    return sum
  }, [stats.models, prices])

  const costModels = useMemo(
    () =>
      stats.models.map((m) => ({
        model: m.model,
        input_tokens: m.promptTokens,
        output_tokens: m.completionTokens,
        cached_tokens: 0,
        reasoning_tokens: 0,
      })),
    [stats.models],
  )

  const successRate = stats.requests > 0 ? ((stats.requests - stats.errors) / stats.requests) * 100 : 0

  return (
    <div className="space-y-4">
      {/* 指标卡 */}
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-6">
        <StatCard icon={<Activity className="h-4 w-4" />} label="请求数" value={stats.requests.toLocaleString()} sub={`${stats.streamed} 流式`} />
        <StatCard
          icon={<AlertTriangle className="h-4 w-4" />}
          label="成功率"
          value={`${successRate.toFixed(successRate >= 99.95 ? 0 : 1)}%`}
          sub={stats.errors > 0 ? `${stats.errors} 个错误` : '无错误'}
          tone={stats.errors > 0 ? 'warn' : 'ok'}
        />
        <StatCard icon={<Coins className="h-4 w-4" />} label="总 Token" value={fmtTokens(stats.totalTokens)} sub={`${fmtTokens(stats.promptTokens)}↑ ${fmtTokens(stats.completionTokens)}↓`} />
        <StatCard icon={<Zap className="h-4 w-4" />} label="估算花费" value={formatUSD(totalCost)} sub="按价目表估算" tone="cost" />
        <StatCard icon={<Clock className="h-4 w-4" />} label="平均延迟" value={fmtMs(stats.avgDurationMs)} sub={`p95 ${fmtMs(stats.p95DurationMs)}`} />
        <StatCard icon={<Timer className="h-4 w-4" />} label="平均首字节" value={fmtMs(stats.avgTtftMs)} sub={stats.p95TtftMs > 0 ? `p95 ${fmtMs(stats.p95TtftMs)}` : 'TTFT'} />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Timeline buckets={stats.timeline} />
        <UpstreamRanking rows={stats.upstreams} />
      </div>

      <ModelTable rows={stats.models} prices={prices} />
      <CostCard kind="codex" models={costModels} />
    </div>
  )
}

function StatCard({
  icon,
  label,
  value,
  sub,
  tone,
}: {
  icon: React.ReactNode
  label: string
  value: string
  sub?: string
  tone?: 'ok' | 'warn' | 'cost'
}) {
  return (
    <div className="rounded-lg border border-border bg-card p-3">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span
          className={cn(
            'flex h-6 w-6 items-center justify-center rounded-md',
            tone === 'warn'
              ? 'bg-amber-500/15 text-amber-600 dark:text-amber-400'
              : tone === 'cost'
                ? 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400'
                : 'bg-primary/10 text-primary',
          )}
        >
          {icon}
        </span>
        {label}
      </div>
      <div className="mt-1.5 text-xl font-semibold tracking-tight tabular-nums">{value}</div>
      {sub && <div className="mt-0.5 text-[10px] text-muted-foreground">{sub}</div>}
    </div>
  )
}

function niceAxisMax(dataMax: number): number {
  if (dataMax <= 0) return 1
  const mag = Math.pow(10, Math.floor(Math.log10(dataMax)))
  const step = Math.max(1, mag / 2)
  return (Math.floor(dataMax / step) + 1) * step
}

function Timeline({ buckets }: { buckets: llmproxy.DayBucket[] }) {
  const [metric, setMetric] = useState<'requests' | 'tokens'>('requests')
  const shown = buckets.slice(-30)
  const val = (b: llmproxy.DayBucket) => (metric === 'requests' ? b.requests : b.totalTokens)
  const axisMax = niceAxisMax(Math.max(0, ...shown.map(val)))

  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <div className="mb-3 flex items-baseline justify-between gap-2">
        <h3 className="flex items-center gap-1.5 text-sm font-medium">
          <Gauge className="h-4 w-4 text-primary" /> 趋势
        </h3>
        <div className="flex overflow-hidden rounded-md border border-border text-[11px]">
          {(['requests', 'tokens'] as const).map((m) => (
            <button
              key={m}
              onClick={() => setMetric(m)}
              className={cn('px-2 py-0.5 transition-colors', metric === m ? 'bg-primary/15 text-primary' : 'text-muted-foreground hover:bg-secondary')}
            >
              {m === 'requests' ? '请求' : 'Token'}
            </button>
          ))}
        </div>
      </div>
      {shown.length === 0 ? (
        <div className="py-8 text-center text-xs text-muted-foreground">暂无数据</div>
      ) : (
        <div className="flex h-40 items-stretch gap-1.5">
          {shown.map((b) => {
            const v = val(b)
            const h = (v / axisMax) * 100
            return (
              <div key={b.date} className="group flex flex-1 flex-col items-center" title={`${b.date} · ${metric === 'requests' ? `${b.requests} 请求` : `${fmtTokens(b.totalTokens)} tokens`}${b.errors ? ` · ${b.errors} 错误` : ''}`}>
                <div className="flex w-full flex-1 items-end">
                  <div
                    className={cn('w-full rounded-t transition-colors', v === 0 ? 'bg-secondary' : 'bg-primary/40 group-hover:bg-primary/70')}
                    style={{ height: `${h}%`, minHeight: v > 0 ? 3 : 2 }}
                  />
                </div>
                <div className="mt-1 w-full truncate text-center text-[9px] text-muted-foreground">{b.date.slice(5)}</div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

const BAR_PALETTE = ['bg-primary/70', 'bg-emerald-500/60', 'bg-amber-500/60', 'bg-violet-500/60', 'bg-rose-500/60', 'bg-cyan-500/60']

function UpstreamRanking({ rows }: { rows: llmproxy.UpstreamStat[] }) {
  const max = Math.max(1, ...rows.map((r) => r.requests))
  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <h3 className="mb-3 text-sm font-medium">按上游</h3>
      {rows.length === 0 ? (
        <div className="py-8 text-center text-xs text-muted-foreground">暂无数据</div>
      ) : (
        <div className="space-y-2">
          {rows.slice(0, 8).map((r, i) => (
            <div key={r.upstream} className="group">
              <div className="mb-0.5 flex items-baseline justify-between gap-2 text-xs">
                <span className="min-w-0 flex-1 truncate font-mono text-foreground/80">{r.upstream || '(空)'}</span>
                <span className="shrink-0 font-mono tabular-nums">{r.requests}</span>
                {r.errors > 0 && <span className="w-12 shrink-0 text-right text-[10px] text-amber-600 dark:text-amber-400">{r.errors} 错</span>}
              </div>
              <div className="h-2 w-full overflow-hidden rounded-full bg-secondary">
                <div className={cn('h-full rounded-full', BAR_PALETTE[i % BAR_PALETTE.length])} style={{ width: `${(r.requests / max) * 100}%` }} />
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function ModelTable({ rows, prices }: { rows: llmproxy.ModelStat[]; prices: ReturnType<typeof usePricingStore.getState>['prices'] }) {
  if (rows.length === 0) return null
  return (
    <div className="overflow-hidden rounded-lg border border-border bg-card">
      <div className="border-b border-border px-4 py-2 text-sm font-medium">按模型</div>
      <div className="overflow-x-auto">
        <table className="w-full text-xs">
          <thead className="bg-secondary/50 text-muted-foreground">
            <tr>
              <th className="px-3 py-2 text-left font-medium">模型</th>
              <th className="px-3 py-2 text-right font-medium">请求</th>
              <th className="px-3 py-2 text-right font-medium">输入</th>
              <th className="px-3 py-2 text-right font-medium">输出</th>
              <th className="px-3 py-2 text-right font-medium">花费</th>
              <th className="px-3 py-2 text-right font-medium">平均延迟</th>
              <th className="px-3 py-2 text-right font-medium">首字节</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => {
              const p = priceForModel(r.model, prices)
              const cost = p ? (r.promptTokens * p.input + r.completionTokens * p.output) / 1_000_000 : null
              return (
                <tr key={r.model} className="border-t border-border">
                  <td className="px-3 py-2 font-mono">{r.model}{r.errors > 0 && <span className="ml-1 text-[10px] text-amber-600 dark:text-amber-400">({r.errors}错)</span>}</td>
                  <td className="px-3 py-2 text-right font-mono tabular-nums">{r.requests.toLocaleString()}</td>
                  <td className="px-3 py-2 text-right font-mono tabular-nums">{fmtTokens(r.promptTokens)}</td>
                  <td className="px-3 py-2 text-right font-mono tabular-nums">{fmtTokens(r.completionTokens)}</td>
                  <td className="px-3 py-2 text-right font-mono tabular-nums">{cost == null ? <span className="text-muted-foreground">—</span> : formatUSD(cost)}</td>
                  <td className="px-3 py-2 text-right font-mono tabular-nums text-muted-foreground">{fmtMs(r.avgDurationMs)}</td>
                  <td className="px-3 py-2 text-right font-mono tabular-nums text-muted-foreground">{fmtMs(r.avgTtftMs)}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
