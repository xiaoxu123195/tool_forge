import { useEffect, useMemo, useState } from 'react'
import {
  Activity,
  BarChart3,
  Calendar,
  Clock,
  Flame,
  RefreshCw,
  Timer,
  TrendingDown,
  TrendingUp,
} from 'lucide-react'
import { ListAIUsage } from '../../../wailsjs/go/main/App'
import type { Provider, UsageRecord } from './types'
import { ProviderAvatar } from './ProviderAvatar'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

type RangeKey = '7d' | '30d' | 'mtd' | 'all'

const RANGE_LABEL: Record<RangeKey, string> = {
  '7d': '近 7 天',
  '30d': '近 30 天',
  mtd: '本月',
  all: '全部',
}

// 同色系:input=蓝、output=绿、reasoning=紫、cached=灰
const COLOR = {
  input: 'hsl(var(--info))',
  output: 'hsl(var(--success))',
  reasoning: '#a855f7',
  cached: 'hsl(var(--muted-foreground))',
}

function formatNumber(n: number): string {
  if (n < 1000) return String(n)
  if (n < 1_000_000) return (n / 1000).toFixed(1) + 'k'
  return (n / 1_000_000).toFixed(2) + 'M'
}

function formatMs(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function startOfDay(ts: number): number {
  const d = new Date(ts)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

function startOfToday(): number {
  return startOfDay(Date.now())
}

function startOfMonth(offset = 0): number {
  const d = new Date()
  d.setMonth(d.getMonth() + offset)
  d.setDate(1)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

function startOfRange(range: RangeKey): number {
  switch (range) {
    case '7d':
      return Date.now() - 7 * 24 * 3600_000
    case '30d':
      return Date.now() - 30 * 24 * 3600_000
    case 'mtd':
      return startOfMonth()
    case 'all':
      return 0
  }
}

interface DayBucket {
  date: number
  input: number
  output: number
  reasoning: number
  cached: number
  calls: number
}

function bucketDaily(records: UsageRecord[], days: number): DayBucket[] {
  const today = startOfToday()
  const map = new Map<number, DayBucket>()
  for (let i = days - 1; i >= 0; i--) {
    const date = today - i * 86400_000
    map.set(date, { date, input: 0, output: 0, reasoning: 0, cached: 0, calls: 0 })
  }
  const minDate = today - (days - 1) * 86400_000
  const maxDate = today + 86400_000
  for (const r of records) {
    if (r.ts < minDate || r.ts >= maxDate) continue
    const b = map.get(startOfDay(r.ts))
    if (b) {
      b.input += r.inputTokens
      b.output += r.outputTokens
      b.reasoning += r.reasoningTokens ?? 0
      b.cached += r.cachedTokens ?? 0
      b.calls++
    }
  }
  return Array.from(map.values()).sort((a, b) => a.date - b.date)
}

interface AggRow {
  key: string
  label: string
  sub?: string
  providerId?: string
  providerName?: string
  providerLogo?: string
  calls: number
  input: number
  output: number
  reasoning: number
  cached: number
  totalDuration: number
}

export function UsagePane({ providers }: { providers: Provider[] }) {
  const [records, setRecords] = useState<UsageRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [range, setRange] = useState<RangeKey>('7d')
  const [filterProvider, setFilterProvider] = useState('')
  const [filterModel, setFilterModel] = useState('')

  const providerById = useMemo(() => {
    const m = new Map<string, Provider>()
    for (const p of providers) m.set(p.id, p)
    return m
  }, [providers])

  const reload = async () => {
    setLoading(true)
    try {
      const r = ((await ListAIUsage()) ?? []) as unknown as UsageRecord[]
      setRecords(r)
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => {
    void reload()
  }, [])

  // 时间范围 + 供应商 + 模型筛选
  const filtered = useMemo(() => {
    const since = startOfRange(range)
    return records.filter((r) => {
      if (r.ts < since) return false
      if (filterProvider && r.providerId !== filterProvider) return false
      if (filterModel && r.model !== filterModel) return false
      return true
    })
  }, [records, range, filterProvider, filterModel])

  // 候选 model 列表(基于已有记录)
  const allModels = useMemo(() => {
    const s = new Set<string>()
    for (const r of records) s.add(r.model)
    return Array.from(s).sort()
  }, [records])

  // —— Hero 数据(本月 vs 上月)
  const monthStart = startOfMonth()
  const lastMonthStart = startOfMonth(-1)
  const monthAgg = useMemo(() => sumOver(records, monthStart), [records, monthStart])
  const lastMonthAgg = useMemo(
    () => sumOverRange(records, lastMonthStart, monthStart),
    [records, lastMonthStart, monthStart],
  )
  const heroBuckets = useMemo(() => bucketDaily(records, 14), [records])

  // —— KPI: 今日(7天 sparkline) + 总累计 + 平均时长
  const todayStart = startOfToday()
  const todayAgg = useMemo(() => sumOver(records, todayStart), [records, todayStart])
  const last7Buckets = useMemo(() => bucketDaily(records, 7), [records])
  const totalCalls = records.length
  const totalTokens = useMemo(
    () => records.reduce((s, r) => s + r.inputTokens + r.outputTokens, 0),
    [records],
  )
  const avgDuration = useMemo(() => {
    if (records.length === 0) return 0
    const sum = records.reduce((s, r) => s + r.durationMs, 0)
    return sum / records.length
  }, [records])

  // —— 14 天堆叠柱状图(基于全量,不受筛选影响)
  const chartBuckets = heroBuckets

  // —— 聚合表(受筛选影响)
  const byModel = useMemo(() => aggregate(filtered, (r) => r.model, providerById, false), [filtered, providerById])
  const byProvider = useMemo(
    () => aggregate(filtered, (r) => r.providerId, providerById, true),
    [filtered, providerById],
  )

  return (
    <div className="flex h-full min-w-0 flex-1 flex-col overflow-auto">
      <header className="sticky top-0 z-10 flex h-12 shrink-0 items-center gap-3 border-b border-border bg-card/95 px-4 backdrop-blur">
        <Activity className="h-4 w-4 text-info" />
        <span className="text-sm font-semibold">AI 用量</span>
        <div className="ml-auto flex items-center gap-2">
          <FilterSelect
            value={range}
            onChange={(v) => setRange(v as RangeKey)}
            options={(Object.keys(RANGE_LABEL) as RangeKey[]).map((k) => ({
              value: k,
              label: RANGE_LABEL[k],
            }))}
            icon={<Calendar className="h-3 w-3" />}
          />
          <FilterSelect
            value={filterProvider}
            onChange={setFilterProvider}
            options={[
              { value: '', label: '全部供应商' },
              ...providers.map((p) => ({ value: p.id, label: p.name })),
            ]}
          />
          <FilterSelect
            value={filterModel}
            onChange={setFilterModel}
            options={[
              { value: '', label: '全部模型' },
              ...allModels.map((m) => ({ value: m, label: m })),
            ]}
          />
          <Button size="sm" variant="ghost" onClick={() => void reload()} disabled={loading}>
            <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />
          </Button>
        </div>
      </header>

      <div className="flex-1 space-y-6 p-4">
        {/* —— Hero —— */}
        <Hero
          monthTokens={monthAgg.tokens}
          monthCalls={monthAgg.calls}
          lastMonthTokens={lastMonthAgg.tokens}
          buckets={heroBuckets}
        />

        {/* —— KPI —— */}
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <KpiCard
            label="今日调用"
            value={String(todayAgg.calls)}
            spark={last7Buckets.map((b) => b.calls)}
            color={COLOR.input}
            icon={<Clock className="h-4 w-4" />}
          />
          <KpiCard
            label="今日 tokens"
            value={formatNumber(todayAgg.tokens)}
            spark={last7Buckets.map((b) => b.input + b.output)}
            color={COLOR.output}
            icon={<BarChart3 className="h-4 w-4" />}
          />
          <KpiCard
            label="累计调用"
            value={String(totalCalls)}
            sub={`累计 ${formatNumber(totalTokens)} tokens`}
            color={COLOR.reasoning}
            icon={<Flame className="h-4 w-4" />}
          />
          <KpiCard
            label="平均响应时长"
            value={avgDuration > 0 ? formatMs(avgDuration) : '—'}
            sub={records.length > 0 ? `${records.length} 次样本` : '尚无数据'}
            color={COLOR.cached}
            icon={<Timer className="h-4 w-4" />}
          />
        </div>

        {/* —— 14 天堆叠柱状图 —— */}
        <Section title="近 14 天 token 用量" hint="所有供应商累计,堆叠展示">
          <DailyBarChart buckets={chartBuckets} />
        </Section>

        {records.length === 0 && !loading ? null : (
          <>
            {/* —— 按模型聚合表 —— */}
            <Section title="按模型" hint={`${RANGE_LABEL[range]} · 共 ${filtered.length} 次调用`}>
              <ModelTable rows={byModel} />
            </Section>

            {/* —— 按供应商聚合表 —— */}
            <Section title="按供应商" hint={`共 ${byProvider.length} 个供应商`}>
              <ProviderTable rows={byProvider} />
            </Section>
          </>
        )}
      </div>
    </div>
  )
}

// ============ helpers ============

function sumOver(records: UsageRecord[], start: number) {
  let calls = 0
  let tokens = 0
  for (const r of records) {
    if (r.ts < start) continue
    calls++
    tokens += r.inputTokens + r.outputTokens
  }
  return { calls, tokens }
}

function sumOverRange(records: UsageRecord[], start: number, end: number) {
  let calls = 0
  let tokens = 0
  for (const r of records) {
    if (r.ts < start || r.ts >= end) continue
    calls++
    tokens += r.inputTokens + r.outputTokens
  }
  return { calls, tokens }
}

function aggregate(
  records: UsageRecord[],
  keyFn: (r: UsageRecord) => string,
  providerById: Map<string, Provider>,
  byProvider: boolean,
): AggRow[] {
  const map = new Map<string, AggRow>()
  for (const r of records) {
    const k = keyFn(r)
    if (!k) continue
    let row = map.get(k)
    if (!row) {
      const prov = providerById.get(r.providerId)
      row = {
        key: k,
        label: byProvider ? r.providerName || prov?.name || '未知' : r.model,
        sub: byProvider ? '' : r.providerName || prov?.name || '',
        providerId: r.providerId,
        providerName: r.providerName,
        providerLogo: prov?.logo,
        calls: 0,
        input: 0,
        output: 0,
        reasoning: 0,
        cached: 0,
        totalDuration: 0,
      }
      map.set(k, row)
    }
    row.calls++
    row.input += r.inputTokens
    row.output += r.outputTokens
    row.reasoning += r.reasoningTokens ?? 0
    row.cached += r.cachedTokens ?? 0
    row.totalDuration += r.durationMs
  }
  return Array.from(map.values()).sort((a, b) => b.input + b.output - (a.input + a.output))
}

// ============ Hero ============

function Hero({
  monthTokens,
  monthCalls,
  lastMonthTokens,
  buckets,
}: {
  monthTokens: number
  monthCalls: number
  lastMonthTokens: number
  buckets: DayBucket[]
}) {
  const delta =
    lastMonthTokens > 0
      ? ((monthTokens - lastMonthTokens) / lastMonthTokens) * 100
      : monthTokens > 0
        ? 100
        : 0
  const up = delta >= 0
  const series = buckets.map((b) => b.input + b.output)
  return (
    <div className="relative overflow-hidden rounded-xl border border-info/30 bg-gradient-to-br from-info/15 via-info/5 to-transparent p-5">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <div className="text-xs font-medium uppercase tracking-wider text-info">本月用量</div>
          <div className="mt-1.5 flex items-baseline gap-2">
            <div className="text-4xl font-semibold tabular-nums tracking-tight">
              {formatNumber(monthTokens)}
            </div>
            <div className="text-base text-muted-foreground">tokens</div>
          </div>
          <div className="mt-1 flex items-center gap-3 text-xs text-muted-foreground">
            <span>{monthCalls} 次调用</span>
            {lastMonthTokens > 0 && (
              <span
                className={cn(
                  'flex items-center gap-1',
                  up ? 'text-success' : 'text-destructive',
                )}
              >
                {up ? (
                  <TrendingUp className="h-3 w-3" />
                ) : (
                  <TrendingDown className="h-3 w-3" />
                )}
                {up ? '↑' : '↓'} {Math.abs(delta).toFixed(0)}% vs 上月
              </span>
            )}
          </div>
        </div>
        <div className="min-w-[180px] flex-1">
          <Sparkline data={series} color={COLOR.input} height={40} fill />
          <div className="mt-1 flex justify-between text-[10px] text-muted-foreground">
            <span>{shortDate(buckets[0]?.date ?? Date.now())}</span>
            <span>今日</span>
          </div>
        </div>
      </div>
    </div>
  )
}

function shortDate(ts: number): string {
  const d = new Date(ts)
  return `${d.getMonth() + 1}/${d.getDate()}`
}

// ============ Sparkline ============

function Sparkline({
  data,
  color,
  height = 28,
  fill = false,
  className,
}: {
  data: number[]
  color: string
  height?: number
  fill?: boolean
  className?: string
}) {
  if (data.length < 2) {
    return <div style={{ height }} className={className} />
  }
  const max = Math.max(...data, 1)
  const W = 100 // viewBox 单位,跟随容器宽度自适应
  const stepX = W / (data.length - 1)
  const points = data.map((v, i) => {
    const x = i * stepX
    const y = height - (v / max) * (height - 2) - 1
    return `${x},${y}`
  })
  const linePath = `M ${points.join(' L ')}`
  const areaPath = `M 0,${height} L ${points.join(' L ')} L ${W},${height} Z`
  return (
    <svg
      viewBox={`0 0 ${W} ${height}`}
      preserveAspectRatio="none"
      className={cn('h-full w-full', className)}
      style={{ height }}
    >
      {fill && <path d={areaPath} fill={color} fillOpacity={0.18} />}
      <path
        d={linePath}
        fill="none"
        stroke={color}
        strokeWidth={1.5}
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}

// ============ KPI ============

function KpiCard({
  label,
  value,
  sub,
  spark,
  color,
  icon,
}: {
  label: string
  value: string
  sub?: string
  spark?: number[]
  color: string
  icon: React.ReactNode
}) {
  return (
    <div className="relative overflow-hidden rounded-lg border border-border bg-card p-3">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span
          className="flex h-6 w-6 items-center justify-center rounded-md"
          style={{ background: `${color}26`, color }}
        >
          {icon}
        </span>
        {label}
      </div>
      <div className="mt-1.5 text-2xl font-semibold tabular-nums tracking-tight">{value}</div>
      {spark && spark.length > 0 ? (
        <div className="-mx-1 mt-1">
          <Sparkline data={spark} color={color} height={28} fill />
        </div>
      ) : (
        sub && <div className="mt-1 text-[11px] text-muted-foreground">{sub}</div>
      )}
    </div>
  )
}

// ============ Daily bar chart ============

function DailyBarChart({ buckets }: { buckets: DayBucket[] }) {
  const max = Math.max(
    ...buckets.map((b) => b.input + b.output + b.reasoning),
    1,
  )
  const hasAny = buckets.some((b) => b.calls > 0)
  return (
    <div className="rounded-lg border border-border bg-card p-4">
      {hasAny ? (
        <>
          <div className="flex h-32 items-end gap-1.5">
            {buckets.map((b) => {
              const total = b.input + b.output + b.reasoning
              const heightPct = total > 0 ? (total / max) * 100 : 0
              return (
                <div
                  key={b.date}
                  className="group relative flex flex-1 flex-col justify-end"
                  style={{ minWidth: 0 }}
                >
                  <div
                    className="flex w-full flex-col overflow-hidden rounded-t-sm transition-all"
                    style={{ height: `${heightPct}%`, minHeight: total > 0 ? 2 : 0 }}
                  >
                    {b.reasoning > 0 && (
                      <div
                        style={{
                          flexBasis: `${(b.reasoning / total) * 100}%`,
                          background: COLOR.reasoning,
                        }}
                      />
                    )}
                    {b.output > 0 && (
                      <div
                        style={{
                          flexBasis: `${(b.output / total) * 100}%`,
                          background: COLOR.output,
                        }}
                      />
                    )}
                    {b.input > 0 && (
                      <div
                        style={{
                          flexBasis: `${(b.input / total) * 100}%`,
                          background: COLOR.input,
                        }}
                      />
                    )}
                  </div>
                  {/* hover tooltip */}
                  <div className="pointer-events-none absolute -top-12 left-1/2 z-10 hidden w-32 -translate-x-1/2 rounded-md border border-border bg-popover px-2 py-1.5 text-[11px] shadow-lg group-hover:block">
                    <div className="font-medium">{shortDate(b.date)}</div>
                    <div className="text-muted-foreground">
                      {b.calls} 次 · {formatNumber(total)} tokens
                    </div>
                  </div>
                </div>
              )
            })}
          </div>
          <div className="mt-2 flex justify-between text-[10px] text-muted-foreground">
            <span>{shortDate(buckets[0]?.date)}</span>
            <span>{shortDate(buckets[Math.floor(buckets.length / 2)]?.date)}</span>
            <span>今日</span>
          </div>
          <div className="mt-3 flex items-center gap-4 text-[11px] text-muted-foreground">
            <Legend color={COLOR.input} label="Input" />
            <Legend color={COLOR.output} label="Output" />
            <Legend color={COLOR.reasoning} label="Reasoning" />
          </div>
        </>
      ) : (
        <div className="flex h-32 items-center justify-center text-xs text-muted-foreground">
          近 14 天没有调用记录
        </div>
      )}
    </div>
  )
}

function Legend({ color, label }: { color: string; label: string }) {
  return (
    <span className="flex items-center gap-1.5">
      <span className="h-2 w-2 rounded-sm" style={{ background: color }} />
      {label}
    </span>
  )
}

// ============ Section + tables ============

function Section({
  title,
  hint,
  children,
}: {
  title: string
  hint?: string
  children: React.ReactNode
}) {
  return (
    <section className="space-y-2">
      <div className="flex items-baseline justify-between gap-2">
        <h3 className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
          {title}
        </h3>
        {hint && <span className="text-[11px] text-muted-foreground">{hint}</span>}
      </div>
      {children}
    </section>
  )
}

function ModelTable({ rows }: { rows: AggRow[] }) {
  if (rows.length === 0) {
    return (
      <div className="rounded-lg border border-border bg-card p-6 text-center text-xs text-muted-foreground">
        当前筛选条件下没有数据
      </div>
    )
  }
  const max = Math.max(...rows.map((r) => r.input + r.output), 1)
  return (
    <div className="overflow-hidden rounded-lg border border-border bg-card">
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="bg-secondary/30 text-[11px] uppercase tracking-wider text-muted-foreground">
            <tr>
              <th className="px-3 py-2 text-left">模型</th>
              <th className="px-3 py-2 text-right">调用</th>
              <th className="w-[42%] px-3 py-2 text-left">Input · Output 占比</th>
              <th className="px-3 py-2 text-right">Reasoning</th>
              <th className="px-3 py-2 text-right">均时长</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {rows.map((row) => {
              const total = row.input + row.output
              const widthPct = (total / max) * 100
              const inputPct = total > 0 ? (row.input / total) * 100 : 0
              return (
                <tr key={row.key} className="hover:bg-secondary/20">
                  <td className="px-3 py-2.5">
                    <div className="min-w-0">
                      <div className="truncate font-medium">{row.label}</div>
                      {row.sub && (
                        <div className="truncate text-[11px] text-muted-foreground">
                          {row.sub}
                        </div>
                      )}
                    </div>
                  </td>
                  <td className="px-3 py-2.5 text-right tabular-nums">{row.calls}</td>
                  <td className="px-3 py-2.5">
                    <div className="space-y-1">
                      <div
                        className="flex h-3 overflow-hidden rounded-sm bg-secondary/40"
                        style={{ width: `${Math.max(widthPct, 4)}%`, minWidth: 24 }}
                        title={`Input ${formatNumber(row.input)} · Output ${formatNumber(row.output)}`}
                      >
                        <div style={{ width: `${inputPct}%`, background: COLOR.input }} />
                        <div style={{ width: `${100 - inputPct}%`, background: COLOR.output }} />
                      </div>
                      <div className="flex justify-between text-[10px] tabular-nums text-muted-foreground">
                        <span>{formatNumber(row.input)} in</span>
                        <span>{formatNumber(row.output)} out</span>
                      </div>
                    </div>
                  </td>
                  <td className="px-3 py-2.5 text-right tabular-nums text-muted-foreground">
                    {row.reasoning > 0 ? formatNumber(row.reasoning) : '—'}
                  </td>
                  <td className="px-3 py-2.5 text-right tabular-nums text-muted-foreground">
                    {formatMs(row.totalDuration / row.calls)}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function ProviderTable({ rows }: { rows: AggRow[] }) {
  if (rows.length === 0) {
    return (
      <div className="rounded-lg border border-border bg-card p-6 text-center text-xs text-muted-foreground">
        当前筛选条件下没有数据
      </div>
    )
  }
  const max = Math.max(...rows.map((r) => r.input + r.output), 1)
  return (
    <div className="overflow-hidden rounded-lg border border-border bg-card">
      <ul className="divide-y divide-border">
        {rows.map((row) => {
          const total = row.input + row.output
          const pct = (total / max) * 100
          return (
            <li key={row.key} className="flex items-center gap-3 px-4 py-3">
              <ProviderAvatar logo={row.providerLogo ?? ''} name={row.label} size={24} />
              <div className="min-w-0 flex-1">
                <div className="flex items-baseline justify-between gap-2">
                  <span className="truncate text-sm font-medium">{row.label}</span>
                  <span className="shrink-0 text-sm font-semibold tabular-nums">
                    {formatNumber(total)}
                  </span>
                </div>
                <div className="mt-1.5 flex items-center gap-2">
                  <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-secondary/40">
                    <div
                      className="h-full rounded-full"
                      style={{ width: `${Math.max(pct, 4)}%`, background: COLOR.input }}
                    />
                  </div>
                  <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">
                    {row.calls} 次
                  </span>
                </div>
              </div>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

// ============ FilterSelect ============

function FilterSelect({
  value,
  onChange,
  options,
  icon,
}: {
  value: string
  onChange: (v: string) => void
  options: { value: string; label: string }[]
  icon?: React.ReactNode
}) {
  return (
    <div className="relative">
      {icon && (
        <span className="pointer-events-none absolute left-2 top-1/2 -translate-y-1/2 text-muted-foreground">
          {icon}
        </span>
      )}
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={cn(
          'h-7 max-w-[160px] rounded-md border border-input bg-background pr-2 text-xs outline-none focus:ring-1 focus:ring-ring',
          icon ? 'pl-6' : 'pl-2',
        )}
      >
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </div>
  )
}
