import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { AlertCircle, Check, Copy, Download, Loader2, Package, type LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'
import { CopyAppIcon, RevealInExplorer, SaveAppIcon } from '../../../wailsjs/go/main/App'
import type { appsearch } from '../../../wailsjs/go/models'

type Action = 'save' | 'copy'

export type IconOutcome =
  | { kind: 'saved'; info: appsearch.IconInfo }
  | { kind: 'copied'; info: appsearch.IconInfo }
  | { kind: 'failed'; action: Action; text: string }

/** 图标的保存和复制。列表里是缩略图,这两样拿的都是原图(后端按图床换地址) */
export function useIconActions(icon: string, name: string, id: string) {
  const [busy, setBusy] = useState<Action | null>(null)
  const [outcome, setOutcome] = useState<IconOutcome | null>(null)

  const run = async (action: Action) => {
    if (busy || !icon) return
    setBusy(action)
    setOutcome(null)
    try {
      if (action === 'save') {
        const info = await SaveAppIcon({ icon, name, id })
        // 空:在保存框里点了取消,什么都不用说
        if (info) setOutcome({ kind: 'saved', info })
      } else {
        setOutcome({ kind: 'copied', info: await CopyAppIcon(icon) })
      }
    } catch (e) {
      setOutcome({ kind: 'failed', action, text: e instanceof Error ? e.message : String(e) })
    } finally {
      setBusy(null)
    }
  }

  return { busy, outcome, save: () => void run('save'), copy: () => void run('copy') }
}

interface AppIconProps {
  src?: string
  busy: Action | null
  onSave: () => void
  onCopy: () => void
}

/**
 * 列表里的图标。鼠标移上去浮出「保存原图」「复制图标」,右键弹同样两项 ——
 * 正式版里 WebView 自带的右键菜单是关着的,原来右键图标什么反应都没有,
 * 而右键恰恰是想存图时的第一反应
 */
export function AppIcon({ src, busy, onSave, onCopy }: AppIconProps) {
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null)
  const closeMenu = useCallback(() => setMenu(null), [])

  if (!src) {
    return (
      <div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-md border border-border bg-muted">
        <Package className="h-5 w-5 text-muted-foreground" />
      </div>
    )
  }

  return (
    <div
      className="group relative h-14 w-14 shrink-0"
      onContextMenu={(e) => {
        e.preventDefault()
        setMenu({ x: e.clientX, y: e.clientY })
      }}
    >
      <img
        src={src}
        alt=""
        className="h-14 w-14 rounded-md border border-border object-cover"
        loading="lazy"
      />
      <div
        className={cn(
          'absolute inset-0 flex items-center justify-center gap-1 rounded-md bg-black/40 transition-opacity',
          busy ? 'opacity-100' : 'opacity-0 focus-within:opacity-100 group-hover:opacity-100'
        )}
      >
        <OverlayButton title="保存原图" spinning={busy === 'save'} disabled={busy !== null} onClick={onSave}>
          <Download className="h-3.5 w-3.5" />
        </OverlayButton>
        <OverlayButton title="复制图标" spinning={busy === 'copy'} disabled={busy !== null} onClick={onCopy}>
          <Copy className="h-3.5 w-3.5" />
        </OverlayButton>
      </div>
      {menu && (
        <IconMenu
          x={menu.x}
          y={menu.y}
          disabled={busy !== null}
          onSave={onSave}
          onCopy={onCopy}
          onClose={closeMenu}
        />
      )}
    </div>
  )
}

function OverlayButton({
  title,
  spinning,
  disabled,
  onClick,
  children,
}: {
  title: string
  spinning: boolean
  disabled: boolean
  onClick: () => void
  children: ReactNode
}) {
  return (
    <button
      type="button"
      title={title}
      aria-label={title}
      disabled={disabled}
      onClick={onClick}
      className="flex h-6 w-6 items-center justify-center rounded bg-background/90 text-foreground shadow-sm transition-colors hover:bg-background disabled:cursor-wait"
    >
      {spinning ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : children}
    </button>
  )
}

function IconMenu({
  x,
  y,
  disabled,
  onSave,
  onCopy,
  onClose,
}: {
  x: number
  y: number
  disabled: boolean
  onSave: () => void
  onCopy: () => void
  onClose: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ x, y })

  // 贴着窗口边时往回翻。菜单多大要等画出来才量得到
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const w = el.offsetWidth
    const h = el.offsetHeight
    setPos({
      x: x + w + 8 > window.innerWidth ? Math.max(4, window.innerWidth - w - 4) : x,
      y: y + h + 8 > window.innerHeight ? Math.max(4, window.innerHeight - h - 4) : y,
    })
  }, [x, y])

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) onClose()
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    // mousedown 晚一拍再挂:弹出菜单的这次右键还没结束,同步挂上会被它自己关掉
    const timer = window.setTimeout(() => window.addEventListener('mousedown', onDown), 0)
    window.addEventListener('keydown', onKey)
    // 列表一滚,菜单就和图标对不上了
    window.addEventListener('scroll', onClose, true)
    window.addEventListener('blur', onClose)
    return () => {
      window.clearTimeout(timer)
      window.removeEventListener('mousedown', onDown)
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('scroll', onClose, true)
      window.removeEventListener('blur', onClose)
    }
  }, [onClose])

  const item = (label: string, Icon: LucideIcon, onClick: () => void) => (
    <button
      type="button"
      role="menuitem"
      disabled={disabled}
      onClick={() => {
        onClose()
        onClick()
      }}
      className={cn(
        'flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm transition-colors',
        disabled ? 'cursor-not-allowed text-muted-foreground' : 'hover:bg-accent hover:text-foreground'
      )}
    >
      <Icon className="h-3.5 w-3.5" />
      <span>{label}</span>
    </button>
  )

  return createPortal(
    <div
      ref={ref}
      role="menu"
      className="fixed z-[200] min-w-[160px] rounded-md border border-border bg-card py-1 shadow-lg animate-in fade-in zoom-in-95"
      style={{ left: pos.x, top: pos.y }}
      onContextMenu={(e) => e.preventDefault()}
    >
      {item('保存原图…', Download, onSave)}
      {item('复制图标', Copy, onCopy)}
    </div>,
    document.body
  )
}

/** 保存或复制之后的那一句,放在这一行文字的最下面 */
export function IconOutcomeLine({ outcome }: { outcome: IconOutcome | null }) {
  if (!outcome) return null
  if (outcome.kind === 'failed') {
    return (
      <div className="flex items-start gap-1.5 text-[11px] text-destructive">
        <AlertCircle className="mt-0.5 h-3 w-3 shrink-0" />
        <span>
          {outcome.action === 'save' ? '没保存成' : '没复制成'}：{outcome.text}
        </span>
      </div>
    )
  }
  const { info } = outcome
  // 列表里看到的是缩略图,得说清楚实际拿到的有多大
  const size = `${info.width}×${info.height} ${info.format.toUpperCase()}`
  return (
    <div className="flex flex-wrap items-center gap-x-1.5 text-[11px] text-emerald-700 dark:text-emerald-400">
      <Check className="h-3 w-3 shrink-0" />
      {outcome.kind === 'saved' ? (
        <>
          <span>已保存 · {size}</span>
          <span className="text-muted-foreground">·</span>
          <button
            type="button"
            onClick={() => void RevealInExplorer(info.path ?? '')}
            className="underline underline-offset-2 transition-colors hover:text-foreground"
          >
            在文件夹中显示
          </button>
        </>
      ) : (
        <span>已复制 · {size}，可以直接粘贴</span>
      )}
    </div>
  )
}
