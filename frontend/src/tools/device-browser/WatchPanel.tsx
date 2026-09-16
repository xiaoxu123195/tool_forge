import { Eye, Pause, Play, RefreshCw, Square, Trash2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { fmtSize } from './PreviewPane'

/**
 * 监视模式:定时给目录子树拍快照、和上一张比,把变化按时间列出来。
 *
 * 取证里最常用的手法就是"在手机上做一个动作,看它落到了哪些文件上" ——
 * 想知道某个功能的数据存在哪儿,这是最直接的路。设备上没有 inotify 这类东西,
 * 只能轮询对比;所以它叫监视而不是监听,变化不会自己冒出来,是每隔几秒去看一眼。
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

export interface WatchState {
  dir: string
  /** 秒 */
  interval: number
  paused: boolean
  busy: boolean
  /** 已经检查了几次(含拍基线那次) */
  ticks: number
  lastAt: number
  total: number
  truncated: boolean
  error: string
  /** 最新的在前 */
  events: WatchEvent[]
}

export const WATCH_INTERVALS = [2, 5, 10, 30]
/** 记录最多留多少条;取证现场一次操作也就几十处变化,留几百条足够翻 */
export const WATCH_MAX_EVENTS = 500

export function newWatch(dir: string, interval = 5): WatchState {
  return {
    dir,
    interval,
    paused: false,
    busy: false,
    ticks: 0,
    lastAt: 0,
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
  onClear,
  onStop,
  onInterval,
  onGo,
}: {
  watch: WatchState
  onPause: () => void
  onCheckNow: () => void
  onClear: () => void
  onStop: () => void
  onInterval: (sec: number) => void
  onGo: (ev: WatchEvent) => void
}) {
  return (
    <div className="rounded-lg border border-info/40 bg-card">
      <div className="flex flex-wrap items-center gap-2 px-3 py-1.5 text-xs">
        <Eye className={cn('h-3.5 w-3.5 text-info', watch.busy && 'animate-pulse')} />
        <span className="font-medium">{watch.paused ? '监视已暂停' : '监视中'}</span>
        <span className="min-w-0 max-w-[40%] truncate font-mono text-[11px]" title={watch.dir}>
          {watch.dir}
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
          秒 · 已检查 {watch.ticks} 次 · {watch.events.length} 处变化
          {watch.total > 0 && ` · 子树 ${watch.total} 条`}
        </span>
        {watch.truncated && (
          <span className="text-[11px] text-amber-700 dark:text-amber-400">
            子树超过上限，只看了前面一部分，对比不完整
          </span>
        )}
        <div className="ml-auto flex items-center gap-0.5">
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
              : '基线拍好了。去手机上操作，落到这棵目录树里的改动会按时间列在这儿。'}
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
