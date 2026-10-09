import {
  forwardRef,
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type HTMLAttributes,
  type ReactNode,
  type RefObject,
} from 'react'
import { Copy, FolderOpen, Loader2, RotateCcw, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useDeviceBrowserStore } from '@/stores/device-browser'
import { CopyImageFile, PickDirectory, RevealInExplorer } from '../../../wailsjs/go/main/App'
import { WindowFullscreen, WindowIsMinimised, WindowUnfullscreen } from '../../../wailsjs/runtime/runtime'
import { fitSize } from './mirror-video'

// 两个投屏面板(安卓、iOS)共用的界面零件:右侧按钮栏、提示条、剪贴板卡片、
// 键盘输入框,以及尺寸、全屏、保存文件夹这几样状态

/** 还不知道手机多宽时先按常见的 9:19.5 占位,画面一到就换成真的 */
export const DEFAULT_ASPECT = 9 / 19.5
/** 右侧按钮栏的宽度 */
export const RAIL = 36
/** 停靠时横屏画面最多占窗口宽度的多少。横屏是扁的,宽度给少了就只剩一条缝 */
const LANDSCAPE_SHARE = 0.62

/**
 * 提示条多久后自己消失(毫秒):顺口一提的提醒、成功、失败。失败的留久一点,要看清是哪个文件、为什么。
 * 「手机拒绝了模拟点击」这种不处理就用不了的不在这里,一直留着等人关。
 * 导出是给冒烟测试调短用的,省得真等几秒
 */
export const DISMISS_MS = { hint: 6000, ok: 5000, error: 15000 }

/** 面板底部的提示条 */
export interface Toast {
  text: string
  /** 失败的标红,留得久一点 */
  error?: boolean
  /** 存下来的文件:给「打开所在文件夹」 */
  path?: string
  /** 存的是图片:多给一个「复制图片」 */
  image?: boolean
  /** 保存的文件夹出了问题:给一个「换文件夹」 */
  pickDir?: boolean
}

/**
 * 画面显示多大。停靠时竖屏不超过 720、不超过窗口的 45%,别把左边的文件列表挤没了;
 * 横屏是扁的,按这个上限只剩巴掌大,放宽到窗口的六成。全屏时宽高都看画面区
 */
export function displaySize(aspect: number, fullscreen: boolean, area: { w: number; h: number }) {
  const landscape = aspect > 1
  const maxWidth = fullscreen
    ? area.w
    : landscape
      ? window.innerWidth * LANDSCAPE_SHARE
      : Math.min(720, window.innerWidth * 0.45)
  return { ...fitSize(maxWidth, area.h, aspect), landscape }
}

/** 一个元素现在多大,跟着窗口和布局变 */
export function useElementSize(ref: RefObject<HTMLElement>) {
  const [size, setSize] = useState({ w: 0, h: 0 })
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const measure = () => setSize({ w: el.clientWidth, h: el.clientHeight })
    measure()
    window.addEventListener('resize', measure)
    const ro = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure)
    ro?.observe(el)
    return () => {
      window.removeEventListener('resize', measure)
      ro?.disconnect()
    }
  }, [ref])
  return size
}

/**
 * 全屏:画面盖住整个窗口,窗口也进系统全屏 —— 竖屏手机能用上整块屏幕的高度。
 * Esc 退出;切到别的工具、面板被关掉,都不能把窗口留在全屏里
 */
export function useFullscreen(active: boolean) {
  const [fullscreen, setFullscreen] = useState(false)
  const enter = useCallback(() => {
    setFullscreen(true)
    void WindowFullscreen()
  }, [])
  const exit = useCallback(() => {
    setFullscreen(false)
    void WindowUnfullscreen()
  }, [])
  useEffect(() => {
    if (!fullscreen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') exit()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [fullscreen, exit])
  useEffect(() => {
    if (fullscreen && !active) exit()
  }, [fullscreen, active, exit])
  const fullscreenRef = useRef(fullscreen)
  fullscreenRef.current = fullscreen
  useEffect(
    () => () => {
      if (fullscreenRef.current) void WindowUnfullscreen()
    },
    [],
  )
  return { fullscreen, enter, exit, fullscreenRef }
}

/**
 * 截图、录屏存到哪个文件夹:第一次问一次,之后不再弹框。
 * 文件夹被删了、U 盘拔了,忘掉它,下次重新问
 */
export function useCaptureDir(setToast: (t: Toast) => void) {
  const captureDir = useDeviceBrowserStore((s) => s.captureDir)
  const setCaptureDir = useDeviceBrowserStore((s) => s.setCaptureDir)
  const pickDir = async () => {
    const dir = await PickDirectory('截图、录屏存到哪个文件夹', captureDir).catch(() => '')
    if (dir) setCaptureDir(dir)
    return dir
  }
  const failed = (e: unknown) => {
    const text = e instanceof Error ? e.message : String(e)
    if (text.includes('保存的文件夹不在了')) setCaptureDir('')
    setToast({ text, error: true, pickDir: text.includes('保存的文件夹') })
  }
  /** 存到哪:没选过就先问,问了不选返回空 */
  const ensureDir = async () => captureDir || (await pickDir())
  return { captureDir, pickDir, failed, ensureDir }
}

/** 录着的时候每秒刷一下,按钮上的时长才会走 */
export function useTicker(on: boolean) {
  const [, setTick] = useState(0)
  useEffect(() => {
    if (!on) return
    const t = window.setInterval(() => setTick((n) => n + 1), 1000)
    return () => window.clearInterval(t)
  }, [on])
}

export function RailButton({
  title,
  onClick,
  disabled,
  on,
  children,
}: {
  title: string
  onClick: () => void
  disabled?: boolean
  /** 开关类按钮:开着的时候高亮 */
  on?: boolean
  children: ReactNode
}) {
  return (
    <button
      title={title}
      aria-label={title}
      aria-pressed={on}
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:opacity-40',
        on && 'bg-secondary text-foreground',
      )}
    >
      {children}
    </button>
  )
}

/**
 * 到点自己消失的提示条。鼠标停在上面时不计时,挪开后重新数 ——
 * 正要点上面的「打开所在文件夹」,它不能先没了
 */
export function AutoDismiss({
  ms,
  onDismiss,
  children,
  ...rest
}: { ms: number; onDismiss: () => void; children: ReactNode } & Omit<HTMLAttributes<HTMLDivElement>, 'children'>) {
  const [hover, setHover] = useState(false)
  const done = useRef(onDismiss)
  done.current = onDismiss
  useEffect(() => {
    if (hover) return
    const t = window.setTimeout(() => done.current(), ms)
    return () => window.clearTimeout(t)
  }, [hover, ms])
  return (
    <div {...rest} onMouseEnter={() => setHover(true)} onMouseLeave={() => setHover(false)}>
      {children}
    </div>
  )
}

function ToastAction({ onClick, children }: { onClick: () => void; children: ReactNode }) {
  return (
    <button onClick={onClick} className="flex items-center gap-1 underline-offset-2 hover:underline">
      {children}
    </button>
  )
}

/** 面板底部的提示条:存下来的文件给「打开所在文件夹」,截图再给「复制图片」 */
export function ToastBar({
  toast,
  onChange,
  onFailed,
  onPickDir,
}: {
  toast: Toast | null
  /** 关掉(null),或者换成另一条 */
  onChange: (t: Toast | null) => void
  onFailed: (e: unknown) => void
  onPickDir: () => void
}) {
  // 每来一条新提示(哪怕字一样,比如连着复制两次)都重新计时:给它一个新的 key
  const seq = useRef(0)
  const last = useRef<Toast | null>(null)
  if (toast !== last.current) {
    last.current = toast
    seq.current++
  }
  if (!toast) return null
  return (
    <AutoDismiss
      key={seq.current}
      ms={toast.error ? DISMISS_MS.error : DISMISS_MS.ok}
      onDismiss={() => onChange(null)}
      data-mirror-toast=""
      className={cn(
        'pointer-events-auto rounded-md px-2 py-1.5 text-[11px] shadow',
        toast.error
          ? 'bg-destructive/90 text-destructive-foreground'
          : 'bg-emerald-100 text-emerald-900 dark:bg-emerald-900/90 dark:text-emerald-100',
      )}
    >
      <div className="flex items-start gap-1.5">
        <span className="min-w-0 flex-1 whitespace-pre-wrap break-words">{toast.text}</span>
        <button onClick={() => onChange(null)} title="知道了" className="shrink-0 opacity-70 hover:opacity-100">
          <X className="h-3 w-3" />
        </button>
      </div>
      {(toast.path || toast.pickDir) && (
        <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1">
          {toast.path && (
            <ToastAction onClick={() => void RevealInExplorer(toast.path!)}>
              <FolderOpen className="h-3 w-3" />
              打开所在文件夹
            </ToastAction>
          )}
          {toast.path && toast.image && (
            <ToastAction
              onClick={() =>
                void CopyImageFile(toast.path!)
                  .then(() => onChange({ text: '截图已复制，可以直接粘到文档、聊天里' }))
                  .catch(onFailed)
              }
            >
              <Copy className="h-3 w-3" />
              复制图片
            </ToastAction>
          )}
          <ToastAction onClick={onPickDir}>换保存的文件夹</ToastAction>
        </div>
      )}
    </AutoDismiss>
  )
}

/** 顺口一提的提醒(蓝色),几秒后自己消失;换一条提醒就换一个计时 */
export function HintBar({ hint, onClose }: { hint: string; onClose: () => void }) {
  if (!hint) return null
  return (
    <AutoDismiss
      key={hint}
      ms={DISMISS_MS.hint}
      onDismiss={onClose}
      data-mirror-hint=""
      className="pointer-events-auto flex w-full items-start gap-1.5 rounded-md bg-sky-100 px-2 py-1.5 text-[11px] text-sky-900 shadow dark:bg-sky-900/90 dark:text-sky-100"
    >
      <span className="flex-1 whitespace-pre-line">{hint}</span>
      <button onClick={onClose} title="知道了" className="shrink-0 opacity-70 hover:opacity-100">
        <X className="h-3 w-3" />
      </button>
    </AutoDismiss>
  )
}

/** 不处理就用不了的提醒(黄色):一直留着,等人关 */
export function WarningBar({ warning, onClose }: { warning: string; onClose: () => void }) {
  if (!warning) return null
  return (
    <div
      data-mirror-warning=""
      className="pointer-events-auto flex w-full items-start gap-1.5 rounded-md bg-amber-100 px-2 py-1.5 text-[11px] text-amber-900 shadow dark:bg-amber-900/90 dark:text-amber-100"
    >
      <span className="flex-1 whitespace-pre-line">{warning}</span>
      <button onClick={onClose} title="知道了" className="shrink-0 opacity-70 hover:opacity-100">
        <X className="h-3 w-3" />
      </button>
    </div>
  )
}

/** 手机剪贴板:最近一次读到的,加上第一次粘贴前原来的那份(读得到的话) */
export function ClipboardCard({
  text,
  loading,
  before,
  emptyText,
  footer,
  onCopy,
  onReload,
  onClose,
}: {
  /** 最近一次读到的;null 或 '' 显示 emptyText */
  text: string | null
  loading?: boolean
  /** 第一次粘贴前手机剪贴板里原来的东西;null = 没有这一栏 */
  before?: string | null
  emptyText: string
  footer: string
  onCopy: (text: string) => void
  /** 再读一次;读不了的平台不给 */
  onReload?: () => void
  onClose: () => void
}) {
  return (
    <div
      data-mirror-clipboard=""
      className="pointer-events-auto w-full rounded-md border border-border bg-card p-2 text-[11px] text-foreground shadow-lg"
    >
      <div className="mb-1 flex items-center gap-1.5">
        <span className="font-medium">手机剪贴板</span>
        {onReload && (
          <button onClick={onReload} title="再读一次" className="ml-auto opacity-70 hover:opacity-100">
            <RotateCcw className="h-3 w-3" />
          </button>
        )}
        <button onClick={onClose} title="收起" className={cn('opacity-70 hover:opacity-100', !onReload && 'ml-auto')}>
          <X className="h-3 w-3" />
        </button>
      </div>
      {loading ? (
        <div className="flex items-center gap-1.5 text-muted-foreground">
          <Loader2 className="h-3 w-3 animate-spin" />
          正在读…
        </div>
      ) : text ? (
        <ClipText text={text} onCopy={onCopy} />
      ) : (
        <div className="text-muted-foreground">{emptyText}</div>
      )}
      {before !== undefined && before !== null && (
        <div className="mt-2 border-t border-border pt-1.5">
          <div className="mb-1 text-muted-foreground">第一次粘贴中文之前，手机剪贴板里原来是：</div>
          {before ? <ClipText text={before} onCopy={onCopy} /> : <div className="text-muted-foreground">（空的）</div>}
        </div>
      )}
      <div className="mt-1.5 text-[10px] text-muted-foreground">{footer}</div>
    </div>
  )
}

function ClipText({ text, onCopy }: { text: string; onCopy: (text: string) => void }) {
  return (
    <div className="flex items-start gap-1.5">
      <div className="max-h-28 min-w-0 flex-1 overflow-auto whitespace-pre-wrap break-words rounded bg-muted/60 px-1.5 py-1 font-mono">
        {text}
      </div>
      <button onClick={() => onCopy(text)} title="复制到电脑" className="shrink-0 opacity-70 hover:opacity-100">
        <Copy className="h-3.5 w-3.5" />
      </button>
    </div>
  )
}

/**
 * 键盘输入:看不见的输入框,点画面时拿到焦点。输入法的拼字、上屏都在它身上发生 ——
 * 拼字时的按键归输入法,上屏了才把文字交出去
 */
export const KeyboardCatcher = forwardRef<
  HTMLTextAreaElement,
  {
    /** 跟着点的位置走:中文输入法的候选框会出现在这儿,而不是窗口角落里 */
    at: { x: number; y: number }
    onKeyDown: (e: React.KeyboardEvent<HTMLTextAreaElement>) => void
    onText: (s: string) => void
    onFocusChange: (focused: boolean) => void
  }
>(function KeyboardCatcher({ at, onKeyDown, onText, onFocusChange }, ref) {
  const composing = useRef(false)
  const flush = (ta: HTMLTextAreaElement) => {
    if (!ta.value) return
    const s = ta.value
    ta.value = ''
    onText(s)
  }
  return (
    <textarea
      ref={ref}
      data-mirror-keyboard=""
      aria-label="键盘输入到手机"
      tabIndex={-1}
      autoComplete="off"
      autoCorrect="off"
      autoCapitalize="off"
      spellCheck={false}
      className="pointer-events-none absolute h-4 w-px resize-none overflow-hidden border-0 p-0 opacity-0"
      style={{ left: at.x, top: at.y }}
      onKeyDown={(e) => {
        // 输入法正在拼字:按键归输入法,等它上屏再发
        if (e.nativeEvent.isComposing || e.keyCode === 229) return
        onKeyDown(e)
      }}
      onInput={(e) => {
        if (!composing.current) flush(e.currentTarget)
      }}
      onCompositionStart={() => {
        composing.current = true
      }}
      onCompositionEnd={(e) => {
        composing.current = false
        flush(e.currentTarget)
      }}
      onFocus={() => onFocusChange(true)}
      onBlur={() => onFocusChange(false)}
    />
  )
})

/**
 * 窗口有没有被最小化。Wails 最小化窗口时网页收不到任何通知(页面照样以为自己在显示),
 * 只能隔一会儿问一次;页面自己变成不可见也算。连着两次问到最小化才算数 ——
 * 顺手点了最小化又马上点回来,不至于断一次再连
 */
export function useWindowShown(enabled: boolean) {
  const [shown, setShown] = useState(true)
  useEffect(() => {
    if (!enabled) {
      setShown(true)
      return
    }
    let alive = true
    let misses = 0
    const check = async () => {
      let minimised = false
      try {
        minimised = !!(await WindowIsMinimised())
      } catch {
        // 不在 Wails 里(纯浏览器预览)
      }
      if (!alive) return
      misses = minimised ? misses + 1 : 0
      setShown(document.visibilityState !== 'hidden' && misses < 2)
    }
    const onVisibility = () => {
      if (document.visibilityState === 'hidden') setShown(false)
      else void check()
    }
    const timer = window.setInterval(() => void check(), 1500)
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      alive = false
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [enabled])
  return shown
}

/** 当前屏幕的缩放比。窗口拖到缩放比例不同的另一块显示器上会变 */
export function useDevicePixelRatio() {
  const [dpr, setDpr] = useState(() => window.devicePixelRatio || 1)
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    // 这条查询只在「离开现在这个缩放比」时触发一次,所以每变一次就换一条新的
    const mq = window.matchMedia(`(resolution: ${dpr}dppx)`)
    const onChange = () => setDpr(window.devicePixelRatio || 1)
    mq.addEventListener?.('change', onChange)
    return () => mq.removeEventListener?.('change', onChange)
  }, [dpr])
  return dpr
}

/** 提示里只放开头一段 */
export function preview(text: string) {
  const one = text.replace(/\s+/g, ' ').trim()
  return one.length > 40 ? one.slice(0, 40) + '…' : one
}

export function fmtDuration(ms: number) {
  const s = Math.max(0, Math.round(ms / 1000))
  return s < 60 ? `${s} 秒` : `${Math.floor(s / 60)} 分 ${s % 60} 秒`
}

export function fmtClock(ms: number) {
  const s = Math.max(0, Math.floor(ms / 1000))
  return `${String(Math.floor(s / 60)).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`
}

/** 录屏按钮:没在录时是摄像机,录着时是红点加时长 */
export function RecordIcon({ since }: { since: number | null }) {
  if (since === null) return null
  return (
    <span className="flex flex-col items-center leading-none text-red-500">
      <span className="h-2 w-2 animate-pulse rounded-full bg-red-500" />
      <span className="mt-0.5 text-[8px] tabular-nums">{fmtClock(Date.now() - since)}</span>
    </span>
  )
}
