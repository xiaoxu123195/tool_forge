import { useEffect, useState } from 'react'
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
import { ToolShell } from '@/components/tool/ToolShell'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { meta } from './meta'
import {
  AXIS_LABEL,
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
import { FetchAIStupidDrift } from '../../../wailsjs/go/main/App'
import type { aistupid } from '../../../wailsjs/go/models'

type Phase = 'loading' | 'ready' | 'error'

export default function AIStupidTool() {
  const [phase, setPhase] = useState<Phase>('loading')
  const [data, setData] = useState<aistupid.DriftBatchResponse | null>(null)
  const [errMsg, setErrMsg] = useState('')
  const [expanded, setExpanded] = useState<Record<number, boolean>>({})

  const load = async () => {
    setPhase((p) => (data ? p : 'loading'))
    setErrMsg('')
    try {
      const r = await FetchAIStupidDrift()
      setData(r)
      setPhase('ready')
    } catch (e) {
      setErrMsg(String(e instanceof Error ? e.message : e))
      setPhase(data ? 'ready' : 'error')
    }
  }

  useEffect(() => {
    load()
    // 只在挂载时请求一次；刷新靠手动按钮
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const models = data?.data ?? []
  const isLoading = phase === 'loading'

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      actions={
        <>
          {data?.meta?.timestamp && (
            <span className="mr-1 text-xs text-muted-foreground">
              最后更新 {fmtTimeLocal(data.meta.timestamp)}
            </span>
          )}
          <Button variant="default" size="sm" onClick={load} disabled={isLoading}>
            {isLoading ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <RefreshCw className="h-3.5 w-3.5" />
            )}
            刷新
          </Button>
        </>
      }
    >
      {phase === 'loading' && !data && <LoadingState />}

      {phase === 'error' && !data && (
        <ErrorState message={errMsg} onRetry={load} />
      )}

      {data && (
        <div className="mx-auto max-w-6xl space-y-5">
          {errMsg && (
            <div className="flex items-start gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-300">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
              <div>
                上次刷新失败：{errMsg}（展示的是上一次数据）
              </div>
            </div>
          )}

          <Overview models={models} />

          {models.length === 0 ? (
            <div className="rounded-lg border border-dashed border-border bg-card px-6 py-16 text-center text-sm text-muted-foreground">
              暂无数据
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
              {models.map((m) => (
                <Card
                  key={m.modelId ?? m.modelName}
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
      )}
    </ToolShell>
  )
}

function LoadingState() {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
      <Loader2 className="h-6 w-6 animate-spin" />
      正在拉取漂移数据...
    </div>
  )
}

function ErrorState({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-4 text-center">
      <div className="flex h-16 w-16 items-center justify-center rounded-full bg-red-500/10 text-red-500">
        <AlertTriangle className="h-8 w-8" />
      </div>
      <div className="space-y-1">
        <h2 className="text-base font-medium">拉取失败</h2>
        <p className="max-w-md break-all text-sm text-muted-foreground">{message}</p>
      </div>
      <Button onClick={onRetry}>
        <RefreshCw className="h-4 w-4" />
        重试
      </Button>
    </div>
  )
}

function Overview({ models }: { models: Model[] }) {
  const score = overallScore(models)
  const vendors = summarizeVendors(models)
  const stableCount = models.filter((m) => severityOf(m.driftStatus) === 'stable').length
  return (
    <section className="flex flex-wrap items-center gap-4 rounded-lg border border-border bg-card px-4 py-3">
      <div className="flex items-baseline gap-2">
        <span className="text-xs uppercase tracking-wide text-muted-foreground">
          Stupid Meter
        </span>
        <span className={cn('text-2xl font-semibold tabular-nums', scoreTextColor(score))}>
          {score}
        </span>
        <span className="text-xs text-muted-foreground">
          / 100 · {stableCount}/{models.length} OK
        </span>
      </div>
      <div className="h-5 w-px bg-border" />
      <div className="flex flex-wrap items-center gap-2">
        {vendors.map((v) => {
          const theme = severityTheme[v.worst]
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
              {v.worst === 'stable' ? 'OK' : theme.label}
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
  model,
  open,
  onToggle,
}: {
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
            {theme.label}
          </span>
        </header>

        <div className="mb-3 flex items-baseline gap-1.5">
          <span
            className={cn('text-3xl font-semibold tabular-nums', scoreTextColor(model.currentScore))}
          >
            {Math.round(model.currentScore)}
          </span>
          {ci && (
            <span className="text-xs text-muted-foreground tabular-nums">{ci}</span>
          )}
        </div>

        <AxisMini axes={model.axes} keys={['correctness', 'spec', 'codeQuality']} />

        <div className="mt-3 text-[11px] italic text-muted-foreground">
          Last changed: {last}
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
            <ChevronUp className="h-3 w-3" /> 收起
          </>
        ) : (
          <>
            <ChevronDown className="h-3 w-3" /> 展开详情
          </>
        )}
      </button>

      {open && <Expanded model={model} />}
    </article>
  )
}

function AxisMini({
  axes,
  keys,
}: {
  axes: Axes
  keys: AxisKey[]
}) {
  return (
    <div className="grid grid-cols-3 gap-2">
      {keys.map((k) => {
        const a = axes[k]
        const pct = axisPct(a)
        const label = AXIS_LABEL[k]
        return (
          <div key={k} className="min-w-0">
            <div className="mb-1 flex items-center justify-between text-[10px] uppercase tracking-wide text-muted-foreground">
              <span className="truncate">{label}</span>
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

function Expanded({ model }: { model: Model }) {
  return (
    <div className="space-y-4 border-t border-border p-4">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Stat label="Baseline" value={Math.round(model.baselineScore).toString()} />
        <Stat label="Current" value={Math.round(model.currentScore).toString()} />
        <Stat
          label="Variance (24h)"
          value={`±${(model.variance24h ?? 0).toFixed(1)}`}
        />
        <Stat label="Status" value={model.regime || model.driftStatus || '—'} />
      </div>

      <div className="space-y-2">
        <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
          Performance Dimensions
        </div>
        <div className="space-y-1.5">
          {AXIS_ORDER.map((k) => {
            const a = model.axes[k]
            if (!a) return null
            const pct = axisPct(a)
            return (
              <div key={k} className="flex items-center gap-3 text-xs">
                <span className="w-24 shrink-0 text-muted-foreground">
                  {AXIS_LABEL[k]}
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
              <span className="opacity-70">建议：</span>
              {model.recommendation}
            </div>
          )}
          {model.lastSignificantChange && (
            <div className="mt-1 pl-5 text-[11px] opacity-80">
              Detected: {fmtTimeLocal(model.lastSignificantChange)}
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

function TrendIcon({
  trend,
  className,
}: {
  trend?: string
  className?: string
}) {
  const t = (trend || '').toLowerCase()
  if (t === 'up' || t === 'rising' || t === 'improving') {
    return <ArrowUp className={cn('text-emerald-500', className)} />
  }
  if (t === 'down' || t === 'falling' || t === 'degrading') {
    return <ArrowDown className={cn('text-red-500', className)} />
  }
  return <Minus className={cn('text-muted-foreground', className)} />
}
