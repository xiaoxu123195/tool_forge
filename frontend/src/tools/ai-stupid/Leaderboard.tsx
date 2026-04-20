import {
  AlertTriangle,
  ArrowDown,
  ArrowRight,
  ArrowUp,
  Loader2,
  RefreshCw,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { severityOf, severityTheme, vendorOf, type Severity } from './logic'
import {
  fmtScore,
  fmtUpdatedAgo,
  pickSparkline,
  trendDir,
  type LeaderModel,
} from './leaderboard-logic'
import { fmtPrice, priceOf } from './pricing'
import { t, type Lang } from './i18n'
import type { aistupid } from '../../../wailsjs/go/models'

interface Props {
  lang: Lang
  data: aistupid.LeaderboardResponse | null
  loading: boolean
  errMsg: string
  onRetry: () => void
}

export function Leaderboard({ lang, data, loading, errMsg, onRetry }: Props) {
  if (loading && !data) return <LoadingState lang={lang} />
  if (!data && errMsg) return <ErrorState lang={lang} message={errMsg} onRetry={onRetry} />
  if (!data) return null

  const models = data.data?.modelScores ?? []
  const historyMap = data.data?.historyMap ?? {}

  // 按分数从高到低
  const sorted = [...models].sort(
    (a, b) => (b.currentScore ?? b.score ?? 0) - (a.currentScore ?? a.score ?? 0)
  )

  return (
    <div className="mx-auto max-w-6xl space-y-4">
      {errMsg && (
        <div className="flex items-start gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-300">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
          <div>
            {t(lang, 'prev_data_hint')}
            {errMsg}
            {t(lang, 'prev_data_suffix')}
          </div>
        </div>
      )}

      <Overview lang={lang} models={sorted} />

      {sorted.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border bg-card px-6 py-16 text-center text-sm text-muted-foreground">
          {t(lang, 'empty')}
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border border-border bg-card">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border bg-muted/30 text-[11px] uppercase tracking-wide text-muted-foreground">
                  <Th className="w-10 text-center">{t(lang, 'col_rank')}</Th>
                  <Th>{t(lang, 'col_model')}</Th>
                  <Th className="w-16 text-right">{t(lang, 'col_score')}</Th>
                  <Th className="w-14 text-center">{t(lang, 'col_trend')}</Th>
                  <Th className="w-20 text-center">{t(lang, 'col_regime')}</Th>
                  <Th className="w-16 text-right">{t(lang, 'col_updated')}</Th>
                  <Th className="w-32 text-right">{t(lang, 'col_price')}</Th>
                  <Th className="w-36 text-center">{t(lang, 'col_history')}</Th>
                </tr>
              </thead>
              <tbody>
                {sorted.map((m, i) => (
                  <Row
                    key={m.id || m.name}
                    lang={lang}
                    rank={i + 1}
                    model={m}
                    history={historyMap[m.id] ?? []}
                  />
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}

function LoadingState({ lang }: { lang: Lang }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
      <Loader2 className="h-6 w-6 animate-spin" />
      {t(lang, 'loading_leaderboard')}
    </div>
  )
}

function ErrorState({
  lang,
  message,
  onRetry,
}: {
  lang: Lang
  message: string
  onRetry: () => void
}) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-4 text-center">
      <div className="flex h-16 w-16 items-center justify-center rounded-full bg-red-500/10 text-red-500">
        <AlertTriangle className="h-8 w-8" />
      </div>
      <div className="space-y-1">
        <h2 className="text-base font-medium">{t(lang, 'fetch_failed')}</h2>
        <p className="max-w-md break-all text-sm text-muted-foreground">{message}</p>
      </div>
      <Button onClick={onRetry}>
        <RefreshCw className="h-4 w-4" />
        {t(lang, 'retry')}
      </Button>
    </div>
  )
}

function Overview({ lang, models }: { lang: Lang; models: LeaderModel[] }) {
  const avg =
    models.length > 0
      ? Math.round(
          models.reduce((acc, m) => acc + (m.currentScore ?? m.score ?? 0), 0) / models.length
        )
      : 0
  const stableCount = models.filter((m) => severityOf(m.status) === 'stable').length

  // 按厂商聚合 worst-severity
  type VendorRow = { key: string; label: string; worst: Severity; total: number; stable: number }
  const bySeverityRank: Record<Severity, number> = {
    stable: 0, warning: 1, degraded: 2, critical: 3,
  }
  const vmap = new Map<string, VendorRow>()
  for (const m of models) {
    const v = vendorOf(m.name)
    const sev = severityOf(m.status)
    let row = vmap.get(v.key)
    if (!row) {
      row = { key: v.key, label: v.label, worst: 'stable', total: 0, stable: 0 }
      vmap.set(v.key, row)
    }
    row.total++
    if (sev === 'stable') row.stable++
    if (bySeverityRank[sev] > bySeverityRank[row.worst]) row.worst = sev
  }
  const vendors = Array.from(vmap.values()).sort((a, b) => a.label.localeCompare(b.label))

  return (
    <section className="flex flex-wrap items-center gap-4 rounded-lg border border-border bg-card px-4 py-3">
      <div className="flex items-baseline gap-2">
        <span className="text-xs uppercase tracking-wide text-muted-foreground">
          {t(lang, 'stupid_meter')}
        </span>
        <span className={cn('text-2xl font-semibold tabular-nums', scoreTextColor(avg))}>
          {avg}
        </span>
        <span className="text-xs text-muted-foreground">
          {t(lang, 'of_100')} · {stableCount}/{models.length} {t(lang, 'all_ok')}
        </span>
      </div>
      <div className="h-5 w-px bg-border" />
      <div className="flex flex-wrap items-center gap-2">
        {vendors.map((v) => {
          const theme = severityTheme[v.worst]
          const label = v.worst === 'stable' ? t(lang, 'all_ok') : t(lang, `sev_${v.worst}`)
          return (
            <span
              key={v.key}
              className={cn(
                'inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium',
                theme.bg,
                theme.text
              )}
              title={`${v.stable}/${v.total}`}
            >
              {v.label}
              <span className="opacity-70">·</span>
              {label}
            </span>
          )
        })}
      </div>
    </section>
  )
}

function scoreTextColor(v: number): string {
  if (v >= 70) return 'text-emerald-500'
  if (v >= 50) return 'text-amber-500'
  if (v >= 30) return 'text-orange-500'
  return 'text-red-500'
}

function Th({
  children,
  className,
}: {
  children: React.ReactNode
  className?: string
}) {
  return <th className={cn('px-3 py-2 text-left font-medium', className)}>{children}</th>
}

function Row({
  lang,
  rank,
  model,
  history,
}: {
  lang: Lang
  rank: number
  model: LeaderModel
  history: aistupid.HistoryPoint[]
}) {
  const score = model.currentScore ?? model.score ?? 0
  const sev = severityOf(model.status)
  const theme = severityTheme[sev]
  const vendor = vendorOf(model.name)
  const tr = trendDir(model.trend)
  const updated = fmtUpdatedAgo(model.lastUpdated)
  const price = fmtPrice(priceOf(model.name))
  const sparkValues = pickSparkline(history, 25)

  return (
    <tr className="border-b border-border/60 last:border-b-0 hover:bg-muted/20">
      <td className="px-3 py-2 text-center text-xs font-medium text-muted-foreground tabular-nums">
        #{rank}
      </td>
      <td className="px-3 py-2">
        <div className="truncate text-sm font-medium">{model.name}</div>
        <div className="truncate text-[11px] text-muted-foreground">{vendor.label}</div>
      </td>
      <td
        className={cn('px-3 py-2 text-right text-base font-semibold tabular-nums', scoreTextColor(score))}
      >
        {fmtScore(score)}
      </td>
      <td className="px-3 py-2 text-center">
        <TrendArrow dir={tr} />
      </td>
      <td className="px-3 py-2 text-center">
        <span
          className={cn(
            'inline-block rounded-md px-2 py-0.5 text-[10px] font-medium tracking-wide',
            theme.bg,
            theme.text
          )}
        >
          {t(lang, `sev_${sev}`)}
        </span>
      </td>
      <td className="px-3 py-2 text-right text-xs text-muted-foreground tabular-nums">
        {updated}
      </td>
      <td className="px-3 py-2 text-right text-xs text-muted-foreground tabular-nums">
        {price}
      </td>
      <td className="px-3 py-2">
        <Sparkline values={sparkValues} />
      </td>
    </tr>
  )
}

function TrendArrow({ dir }: { dir: 'up' | 'down' | 'stable' }) {
  if (dir === 'up') return <ArrowUp className="inline h-4 w-4 text-emerald-500" />
  if (dir === 'down') return <ArrowDown className="inline h-4 w-4 text-red-500" />
  return <ArrowRight className="inline h-4 w-4 text-muted-foreground" />
}

function Sparkline({ values }: { values: number[] }) {
  if (values.length === 0) {
    return <span className="text-xs text-muted-foreground">—</span>
  }
  const w = 120
  const h = 28
  const max = 100
  const min = 0
  const span = max - min || 1
  const step = values.length > 1 ? w / (values.length - 1) : 0
  // 柱状 sparkline：每个值一根柱子
  const barWidth = Math.max(2, Math.floor(w / Math.max(values.length, 1)) - 1)
  return (
    <svg
      width={w}
      height={h}
      viewBox={`0 0 ${w} ${h}`}
      className="block"
      aria-hidden
    >
      {values.map((v, i) => {
        const x = values.length > 1 ? i * step : 0
        const barH = Math.max(1, ((v - min) / span) * (h - 2))
        const y = h - barH
        const color =
          v >= 70 ? 'fill-emerald-500' : v >= 50 ? 'fill-amber-500' : v >= 30 ? 'fill-orange-500' : 'fill-red-500'
        return (
          <rect
            key={i}
            x={x}
            y={y}
            width={barWidth}
            height={barH}
            className={color}
          />
        )
      })}
    </svg>
  )
}
