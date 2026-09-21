import { Camera, Eye, Pause, Play, RefreshCw, Square, Trash2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { fmtSize } from './PreviewPane'

/**
 * 监视模式:给目录子树拍快照、和基线比,把变化列出来。
 *
 * 取证里最常用的手法就是"在手机上做一个动作,看它落到了哪些文件上" ——
 * 想知道某个功能的数据存在哪儿,这是最直接的路。设备上没有 inotify 这类东西,
 * 只能轮询对比;所以它叫监视而不是监听,变化不会自己冒出来,是主动去看一眼。
 *
 * 两种模式对应两种问法,差别在基线动不动:
 *
 *   基线对比  钉一张基线,之后每次都和它比。回答"这一趟操作总共动了哪些文件"。
 *             同一个文件改三次也只有一行,大小增量是相对基线算的
 *   持续监视  每次和上一次比,基线跟着往前走。回答"刚刚又发生了什么"
 *
 * 默认是基线对比 —— 现场的问法几乎都是前者,而且滚动模式下走开几分钟回来,
 * 看到的是一堆被切碎的中间态
 */

export type WatchKind = 'added' | 'removed' | 'modified'

export interface WatchEvent {
  /** 本机看到这处变化的时间(ms) */
  at: number
  kind: WatchKind
  path: string
  name: string
  isDir: boolean
  size: number
  sizeDelta: number
}

export type WatchMode = 'baseline' | 'rolling'

export interface WatchState {
  dir: string
  mode: WatchMode
  /** 秒 */
  interval: number
  paused: boolean
  busy: boolean
  /** 已经检查了几次(含拍基线那次) */
  ticks: number
  lastAt: number
  /** 基线是什么时候拍的(ms)。基线对比模式下这个值要一直不变 */
  baselineAt: number
  total: number
  truncated: boolean
  error: string
  /**
   * 基线对比模式:这是"从基线到现在"的全量净变化,每次检查整体换掉
   * 持续监视模式:按时间累积,最新的在前
   */
  events: WatchEvent[]
}

export const WATCH_INTERVALS = [2, 5, 10, 30]
/** 记录最多留多少条;取证现场一次操作也就几十处变化,留几百条足够翻 */
export const WATCH_MAX_EVENTS = 500

export function newWatch(dir: string, mode: WatchMode = 'baseline', interval = 5): WatchState {
  return {
    dir,
    mode,
    interval,
    paused: false,
    busy: false,
    ticks: 0,
    lastAt: 0,
    baselineAt: 0,
    total: 0,
    truncated: false,
    error: '',
    events: [],
  }
}

/**
 * 给列表行打标用:哪些路径自己变了,哪些目录底下有变化。
 * 目录本身的修改时间只在直接子项增删时变,所以"底下有变化"要靠祖先关系算出来
 */
export function changeMarks(events: WatchEvent[]): {
  exact: Map<string, WatchKind>
  inside: Set<string>
} {
  const exact = new Map<string, WatchKind>()
  const inside = new Set<string>()
  for (const ev of events) {
    // events 最新在前,先到先得就是"最近一次是什么变化"
    if (!exact.has(ev.path)) exact.set(ev.path, ev.kind)
    let p = ev.path
    for (;;) {
      const i = p.lastIndexOf('/')
      if (i <= 0) break
      p = p.slice(0, i)
      inside.add(p)
    }
  }
  return { exact, inside }
}

export function WatchPanel({
  watch,
  onPause,
  onCheckNow,
  onRebase,
  onMode,
  onClear,
  onStop,
  onInterval,
  onGo,
}: {
  watch: WatchState
  onPause: () => void
  onCheckNow: () => void
  onRebase: () => void
  onMode: (m: WatchMode) => void
  onClear: () => void
  onStop: () => void
  onInterval: (sec: number) => void
  onGo: (ev: WatchEvent) => void
}) {
  const baseline = watch.mode === 'baseline'
  return (
    <div className="rounded-lg border border-info/40 bg-card">
      <div className="flex flex-wrap items-center gap-2 px-3 py-1.5 text-xs">
        <Eye className={cn('h-3.5 w-3.5 text-info', watch.busy && 'animate-pulse')} />
        <span className="font-medium">{watch.paused ? '监视已暂停' : '监视中'}</span>
        <span className="min-w-0 max-w-[30%] truncate font-mono text-[11px]" title={watch.dir}>
          {watch.dir}
        </span>

        {/* 模式切换:两种模式回答的是两个不同的问题,不是同一件事的快慢档 */}
        <span className="flex shrink-0 items-center gap-0.5 rounded-md border border-border p-0.5">
          {(
            [
              ['baseline', '基线对比', '钉住一张基线，之后每次都和它比 —— 回答「这一趟操作总共动了哪些文件」'],
              ['rolling', '持续监视', '每次和上一次比 —— 回答「刚刚又发生了什么」'],
            ] as const
          ).map(([m, label, hint]) => (
            <button
              key={m}
              type="button"
              title={hint}
              onClick={() => onMode(m)}
              className={cn(
                'rounded px-1.5 py-0.5 text-[11px] transition-colors',
                watch.mode === m
                  ? 'bg-secondary font-medium text-foreground'
                  : 'text-muted-foreground hover:bg-secondary/60',
              )}
            >
              {label}
            </button>
          ))}
        </span>

        <span className="flex items-center gap-1 text-[11px] text-muted-foreground">
          每
          <select
            value={watch.interval}
            onChange={(e) => onInterval(Number(e.target.value))}
            title="检查间隔"
            className="h-5 rounded border border-input bg-background px-1 text-[11px]"
          >
            {WATCH_INTERVALS.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
          秒 · {baseline ? `${watch.events.length} 处净变化` : `${watch.events.length} 处变化`}
          {watch.baselineAt > 0 && baseline && ` · 基线 ${hhmmss(watch.baselineAt)}`}
          {watch.total > 0 && ` · 子树 ${watch.total} 条`}
        </span>
        {watch.truncated && (
          <span className="text-[11px] text-amber-700 dark:text-amber-400">
            子树超过上限，只看了前面一部分，对比不完整
          </span>
        )}
        <div className="ml-auto flex items-center gap-0.5">
          <PanelBtn
            title="以此刻为准重新拍基线 —— 去手机上做操作之前按一下"
            onClick={onRebase}
            disabled={watch.busy}
          >
            <Camera className="h-3.5 w-3.5" />
          </PanelBtn>
          <PanelBtn title="立即检查" onClick={onCheckNow} disabled={watch.busy}>
            <RefreshCw className={cn('h-3.5 w-3.5', watch.busy && 'animate-spin')} />
          </PanelBtn>
          <PanelBtn title={watch.paused ? '继续监视' : '暂停监视'} onClick={onPause}>
            {watch.paused ? <Play className="h-3.5 w-3.5" /> : <Pause className="h-3.5 w-3.5" />}
          </PanelBtn>
          <PanelBtn title="清空变化记录" onClick={onClear} disabled={watch.events.length === 0}>
            <Trash2 className="h-3.5 w-3.5" />
          </PanelBtn>
          <PanelBtn title="停止监视" onClick={onStop} danger>
            <Square className="h-3.5 w-3.5" />
          </PanelBtn>
        </div>
      </div>
      {watch.error && (
        <div className="whitespace-pre-wrap break-words border-t border-border/60 px-3 py-1.5 text-[11px] text-destructive">
          {watch.error}
        </div>
      )}
      <div className="max-h-44 overflow-auto border-t border-border/60">
        {watch.events.length === 0 ? (
          <p className="px-3 py-2 text-[11px] text-muted-foreground">
            {watch.ticks === 0
              ? '正在拍基线…'
              : baseline
                ? '基线拍好了。去手机上操作，回来看这一趟总共动了哪些文件。'
                : '基线拍好了。落到这棵目录树里的改动会按时间列在这儿。'}
          </p>
        ) : (
          <ul className="text-[11px]">
            {watch.events.map((ev, i) => (
              <li key={`${ev.at}-${ev.path}-${i}`} className="border-b border-border/40 last:border-0">
                <button
                  type="button"
                  onClick={() => onGo(ev)}
                  title={`${ev.path}\n点击跳到它所在的目录`}
                  className="flex w-full items-center gap-2 px-3 py-1 text-left transition-colors hover:bg-accent/60"
                >
                  <span className="w-[62px] shrink-0 tabular-nums text-muted-foreground">
                    {hhmmss(ev.at)}
                  </span>
                  <KindBadge kind={ev.kind} />
                  <span className="min-w-0 flex-1 truncate font-mono">
                    {relTo(ev.path, watch.dir)}
                    {ev.isDir && '/'}
                  </span>
                  <span className="shrink-0 tabular-nums text-muted-foreground">{sizeText(ev)}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

/** 列表行上的角标:这一行自己变了,或者它底下有变化 */
export function ChangeMark({ mark }: { mark: WatchKind | 'inside' }) {
  if (mark === 'inside') {
    return (
      <span className="shrink-0 rounded-sm bg-info/15 px-1 text-[9px] text-info" title="监视到它底下有变化">
        内有变化
      </span>
    )
  }
  return <KindBadge kind={mark} compact />
}

function KindBadge({ kind, compact }: { kind: WatchKind; compact?: boolean }) {
  const map: Record<WatchKind, { text: string; cls: string }> = {
    added: {
      text: compact ? '新' : '新增',
      cls: 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400',
    },
    modified: {
      text: compact ? '改' : '修改',
      cls: 'bg-amber-500/15 text-amber-700 dark:text-amber-400',
    },
    removed: {
      text: compact ? '删' : '删除',
      cls: 'bg-destructive/15 text-destructive',
    },
  }
  const m = map[kind]
  return (
    <span
      className={cn(
        'shrink-0 rounded-sm px-1 font-medium',
        compact ? 'text-[9px]' : 'w-8 text-center text-[10px]',
        m.cls,
      )}
    >
      {m.text}
    </span>
  )
}

function PanelBtn({
  title,
  onClick,
  disabled,
  danger,
  children,
}: {
  title: string
  onClick: () => void
  disabled?: boolean
  danger?: boolean
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'rounded p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:opacity-40',
        danger && 'hover:bg-destructive/10 hover:text-destructive',
      )}
    >
      {children}
    </button>
  )
}

function relTo(p: string, dir: string): string {
  const base = dir.endsWith('/') ? dir : dir + '/'
  return p.startsWith(base) ? p.slice(base.length) : p
}

function hhmmss(ms: number): string {
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function sizeText(ev: WatchEvent): string {
  if (ev.isDir) return ''
  if (ev.kind === 'added') return fmtSize(ev.size)
  if (ev.kind === 'removed') return ''
  if (ev.sizeDelta === 0) return '仅时间变了'
  return (ev.sizeDelta > 0 ? '+' : '−') + fmtSize(Math.abs(ev.sizeDelta))
}
