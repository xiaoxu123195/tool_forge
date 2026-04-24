import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ClipboardList, Maximize2, Minus, Square, X } from 'lucide-react'
import {
  EventsOn,
  Quit,
  WindowIsMaximised,
  WindowMinimise,
  WindowToggleMaximise,
} from '../../wailsjs/runtime/runtime'
import logoUrl from '@/assets/logo.png'
import { cn } from '@/lib/utils'

const dragStyle = { '--wails-draggable': 'drag' } as React.CSSProperties
const noDragStyle = { '--wails-draggable': 'no-drag' } as React.CSSProperties

export function TitleBar() {
  const [maximised, setMaximised] = useState(false)
  const [pulsing, setPulsing] = useState(false)
  const navigate = useNavigate()
  const pulseTimerRef = useRef<number | null>(null)

  useEffect(() => {
    let cancelled = false
    const sync = () => {
      WindowIsMaximised().then((v) => {
        if (!cancelled) setMaximised(v)
      })
    }
    sync()
    const id = window.setInterval(sync, 500)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [])

  // 监听新剪贴板条目 → 触发图标脉冲;监听全局热键事件 → 跳转剪贴板页
  // 注意:使用 EventsOn 返回的 cancel 函数,不用 EventsOff(会误伤其他组件注册的同名监听)
  useEffect(() => {
    const offNew = EventsOn('clipboard:new', () => {
      setPulsing(true)
      if (pulseTimerRef.current) window.clearTimeout(pulseTimerRef.current)
      pulseTimerRef.current = window.setTimeout(() => setPulsing(false), 700)
    })
    const offNav = EventsOn('nav:goto-clipboard', () => {
      navigate('/tools/clipboard')
    })
    return () => {
      offNew()
      offNav()
      if (pulseTimerRef.current) window.clearTimeout(pulseTimerRef.current)
    }
  }, [navigate])

  const handleToggleMax = () => {
    WindowToggleMaximise()
    setTimeout(() => WindowIsMaximised().then(setMaximised), 50)
  }

  return (
    <div
      style={dragStyle}
      onDoubleClick={handleToggleMax}
      className="flex h-8 shrink-0 select-none items-center border-b border-border bg-sidebar"
    >
      <div className="flex items-center gap-2 px-3">
        <img src={logoUrl} alt="" className="h-4 w-4 rounded-sm" />
        <span className="text-xs font-medium text-foreground/80">Tool Forge</span>
      </div>

      <div className="flex-1" />

      <div style={noDragStyle} className="flex h-full items-center">
        <button
          onClick={() => navigate('/tools/clipboard')}
          title="剪贴板 (Ctrl+Shift+V)"
          aria-label="剪贴板"
          className={cn(
            'mr-1 flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-all hover:bg-accent hover:text-foreground',
            pulsing && 'tf-clipboard-pulse',
          )}
        >
          <ClipboardList className="h-3.5 w-3.5" />
        </button>
        <TitleBarButton onClick={() => WindowMinimise()} label="最小化">
          <Minus className="h-3.5 w-3.5" />
        </TitleBarButton>
        <TitleBarButton onClick={handleToggleMax} label={maximised ? '还原' : '最大化'}>
          {maximised ? (
            <Square className="h-3 w-3" />
          ) : (
            <Maximize2 className="h-3 w-3" />
          )}
        </TitleBarButton>
        <TitleBarButton onClick={() => Quit()} label="关闭" variant="danger">
          <X className="h-3.5 w-3.5" />
        </TitleBarButton>
      </div>
    </div>
  )
}

function TitleBarButton({
  onClick,
  label,
  variant,
  children,
}: {
  onClick: () => void
  label: string
  variant?: 'danger'
  children: React.ReactNode
}) {
  return (
    <button
      onClick={onClick}
      title={label}
      aria-label={label}
      className={
        'flex h-full w-11 items-center justify-center text-muted-foreground transition-colors ' +
        (variant === 'danger'
          ? 'hover:bg-red-600 hover:text-white'
          : 'hover:bg-accent hover:text-foreground')
      }
    >
      {children}
    </button>
  )
}
