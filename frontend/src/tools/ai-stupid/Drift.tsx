import { useState } from 'react'
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ChevronUp,
  Loader2,
  Minus,
  RefreshCw,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import {
  AXIS_ORDER,
  axisPct,
  fmtCI,
  fmtLastChanged,
  fmtTimeLocal,
  overallScore,
  scoreBarColor,
  severityOf,
  severityTheme,
  summarizeVendors,
  vendorOf,
  type Axes,
  type AxisKey,
  type Model,
} from './logic'
import { t, type Lang } from './i18n'
import type { aistupid } from '../../../wailsjs/go/models'

interface Props {
  lang: Lang
  data: aistupid.DriftBatchResponse | null
  loading: boolean
  errMsg: string
  onRetry: () => void
}

export function Drift({ lang, data, loading, errMsg, onRetry }: Props) {
  const [expanded, setExpanded] = useState<Record<number, boolean>>({})
  const models = data?.data ?? []

  if (loading && !data) return <LoadingState lang={lang} />
  if (!data && errMsg) return <ErrorState lang={lang} message={errMsg} onRetry={onRetry} />
  if (!data) return null

  return (
    <div className="mx-auto max-w-6xl space-y-5">
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

      <Overview lang={lang} models={models} />

      {models.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border bg-card px-6 py-16 text-center text-sm text-muted-foreground">
          {t(lang, 'empty')}
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {models.map((m) => (
            <Card
              key={m.modelId ?? m.modelName}
              lang={lang}
              model={m}
              open={!!expanded[m.modelId]}
              onToggle={() =>
                setExpanded((s) => ({ ...s, [m.modelId]: !s[m.modelId] }))
              }
            />
          ))}
        </div>
      )}
    </div>
  )
}

function LoadingState({ lang }: { lang: Lang }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
      <Loader2 className="h-6 w-6 animate-spin" />
      {t(lang, 'loading_drift')}
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

function Overview({ lang, models }: { lang: Lang; models: Model[] }) {
  const score = overallScore(models)
  const vendors = summarizeVendors(models)
  const stableCount = models.filter((m) => severityOf(m.driftStatus) === 'stable').length
  return (
    <section className="flex flex-wrap items-center gap-4 rounded-lg border border-border bg-card px-4 py-3">
      <div className="flex items-baseline gap-2">
        <span className="text-xs uppercase tracking-wide text-muted-foreground">
          {t(lang, 'stupid_meter')}
        </span>
        <span className={cn('text-2xl font-semibold tabular-nums', scoreTextColor(score))}>
          {score}
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
              title={`${v.stable}/${v.total} stable`}
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

function Card({
  lang,
  model,
  open,
  onToggle,
}: {
  lang: Lang
  model: Model
  open: boolean
  onToggle: () => void
}) {
  const sev = severityOf(model.driftStatus)
  const theme = severityTheme[sev]
  const vendor = vendorOf(model.modelName)
  const ci = fmtCI(model.confidenceInterval)
  const last = fmtLastChanged(model.hoursSinceChange)

  return (
    <article
      className={cn(
        'group rounded-lg border border-border bg-card transition-colors',
        'hover:border-foreground/20'
      )}
    >
      <button
        type="button"
        onClick={onToggle}
        className="block w-full cursor-pointer p-4 text-left"
      >
        <header className="mb-3 flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="truncate text-sm font-semibold">{model.modelName}</div>
            <div className="truncate text-xs text-muted-foreground">{vendor.label}</div>
          </div>
          <span
            className={cn(
              'shrink-0 rounded-md px-2 py-0.5 text-[11px] font-medium tracking-wide',
              theme.bg,
              theme.text
            )}
          >
            {t(lang, `sev_${sev}`)}
          </span>
        </header>

        <div className="mb-3 flex items-baseline gap-1.5">
          <span
            className={cn(
              'text-3xl font-semibold tabular-nums',
              scoreTextColor(model.currentScore)
            )}
          >
            {Math.round(model.currentScore)}
          </span>
          {ci && (
            <span className="text-xs text-muted-foreground tabular-nums">{ci}</span>
          )}
        </div>

        <AxisMini lang={lang} axes={model.axes} keys={['correctness', 'spec', 'codeQuality']} />

        <div className="mt-3 text-[11px] italic text-muted-foreground">
          {t(lang, 'last_changed')}: {last}
        </div>

        {model.primaryIssue && (
          <div className="mt-2 flex items-start gap-1.5 rounded-md border border-amber-500/40 bg-amber-500/10 px-2 py-1.5 text-[11px] text-amber-700 dark:text-amber-300">
            <AlertTriangle className="mt-0.5 h-3 w-3 shrink-0" />
            <span className="truncate">{model.primaryIssue}</span>
          </div>
        )}
      </button>

      <button
        type="button"
        onClick={onToggle}
        className="flex w-full cursor-pointer items-center justify-center gap-1 border-t border-border bg-muted/20 py-1.5 text-[11px] text-muted-foreground hover:bg-muted/40"
      >
        {open ? (
          <>
            <ChevronUp className="h-3 w-3" /> {t(lang, 'collapse')}
          </>
        ) : (
          <>
            <ChevronDown className="h-3 w-3" /> {t(lang, 'expand_detail')}
          </>
        )}
      </button>

      {open && <Expanded lang={lang} model={model} />}
    </article>
  )
}

function AxisMini({
  lang,
  axes,
  keys,
}: {
  lang: Lang
  axes: Axes
  keys: AxisKey[]
}) {
  return (
    <div className="grid grid-cols-3 gap-2">
      {keys.map((k) => {
        const a = axes[k]
        const pct = axisPct(a)
        return (
          <div key={k} className="min-w-0">
            <div className="mb-1 flex items-center justify-between text-[10px] uppercase tracking-wide text-muted-foreground">
              <span className="truncate">{axisLabel(lang, k)}</span>
              <TrendIcon trend={a?.trend} className="h-3 w-3" />
            </div>
            <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
              <div
                className={cn('h-full rounded-full transition-all', scoreBarColor(pct))}
                style={{ width: `${pct}%` }}
              />
            </div>
          </div>
        )
      })}
    </div>
  )
}

function Expanded({ lang, model }: { lang: Lang; model: Model }) {
  return (
    <div className="space-y-4 border-t border-border p-4">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Stat label={t(lang, 'baseline')} value={Math.round(model.baselineScore).toString()} />
        <Stat label={t(lang, 'current')} value={Math.round(model.currentScore).toString()} />
        <Stat
          label={t(lang, 'variance_24h')}
          value={`±${(model.variance24h ?? 0).toFixed(1)}`}
        />
        <Stat label={t(lang, 'status')} value={model.regime || model.driftStatus || '—'} />
      </div>

      <div className="space-y-2">
        <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
          {t(lang, 'performance_dimensions')}
        </div>
        <div className="space-y-1.5">
          {AXIS_ORDER.map((k) => {
            const a = model.axes[k]
            if (!a) return null
            const pct = axisPct(a)
            return (
              <div key={k} className="flex items-center gap-3 text-xs">
                <span className="w-24 shrink-0 text-muted-foreground">
                  {axisLabel(lang, k)}
                </span>
                <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                  <div
                    className={cn('h-full rounded-full', scoreBarColor(pct))}
                    style={{ width: `${pct}%` }}
                  />
                </div>
                <span className="w-10 shrink-0 text-right tabular-nums">
                  {Math.round(pct)}
                </span>
                <TrendIcon trend={a.trend} className="h-3.5 w-3.5" />
              </div>
            )
          })}
        </div>
      </div>

      {(model.primaryIssue || model.recommendation) && (
        <div className="rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-xs text-amber-700 dark:text-amber-300">
          {model.primaryIssue && (
            <div className="mb-1 flex items-start gap-1.5 font-medium">
              <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              {model.primaryIssue}
            </div>
          )}
          {model.recommendation && (
            <div className="pl-5 leading-relaxed">
              <span className="opacity-70">{t(lang, 'recommendation')}：</span>
              {model.recommendation}
            </div>
          )}
          {model.lastSignificantChange && (
            <div className="mt-1 pl-5 text-[11px] opacity-80">
              {t(lang, 'detected')}: {fmtTimeLocal(model.lastSignificantChange)}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-border bg-background/60 px-3 py-2">
      <div className="text-[10px] uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      <div className="mt-0.5 text-sm font-medium tabular-nums">{value}</div>
    </div>
  )
}

function axisLabel(lang: Lang, k: AxisKey): string {
  const map: Record<AxisKey, string> = {
    correctness: t(lang, 'dim_correctness'),
    spec: t(lang, 'dim_spec'),
    codeQuality: t(lang, 'dim_code_quality'),
    efficiency: t(lang, 'dim_efficiency'),
    stability: t(lang, 'dim_stability'),
    refusal: t(lang, 'dim_refusal'),
    recovery: t(lang, 'dim_recovery'),
  }
  return map[k]
}

function TrendIcon({
  trend,
  className,
}: {
  trend?: string
  className?: string
}) {
  const v = (trend || '').toLowerCase()
  if (v === 'up' || v === 'rising' || v === 'improving') {
    return <ArrowUp className={cn('text-emerald-500', className)} />
  }
  if (v === 'down' || v === 'falling' || v === 'degrading') {
    return <ArrowDown className={cn('text-red-500', className)} />
  }
  return <Minus className={cn('text-muted-foreground', className)} />
}
