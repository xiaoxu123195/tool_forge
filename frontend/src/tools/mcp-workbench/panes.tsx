import { useCallback, useEffect, useRef, useState } from 'react'
import { cn } from '@/lib/utils'

/**
 * 分栏用的两件小东西:量容器宽度,和一根能拖的分隔条。
 *
 * 为什么要自己量而不是用 CSS 断点:断点看的是整个窗口,而这一页真正能用的宽度
 * 还要减去侧边栏,侧边栏是能收起来的。按窗口判断会在侧边栏收起时判错一档。
 */

/** 量一个元素的宽高,跟着它变 */
export function useMeasure<T extends HTMLElement>() {
  const ref = useRef<T | null>(null)
  const [size, setSize] = useState({ width: 0, height: 0 })

  useEffect(() => {
    const el = ref.current
    if (!el) return
    // jsdom 里没有 ResizeObserver。冒烟测试不做真实排版,量不到就保持 0,
    // 让布局退回"没测到宽度"的那一档,而不是整页崩掉
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver((entries) => {
      const r = entries[0]?.contentRect
      if (r) setSize({ width: r.width, height: r.height })
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  return { ref, ...size }
}

/**
 * 分隔条。按住拖动改尺寸,双击恢复默认。
 *
 * 拖动期间在 window 上监听而不是在自己身上:鼠标很容易划出这几像素宽的条,
 * 只监听自己的话一划出去就断了,表现是"拖一下就卡住"。
 */
export function Divider({
  direction,
  onDrag,
  onReset,
  title,
}: {
  direction: 'x' | 'y'
  /** 相对上一次事件的位移(px) */
  onDrag: (delta: number) => void
  onReset: () => void
  title: string
}) {
  const [dragging, setDragging] = useState(false)
  const start = useRef(0)
  const handler = useRef(onDrag)
  handler.current = onDrag

  const onDown = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault()
      start.current = direction === 'x' ? e.clientX : e.clientY
      setDragging(true)
    },
    [direction],
  )

  useEffect(() => {
    if (!dragging) return
    const move = (e: MouseEvent) => {
      const now = direction === 'x' ? e.clientX : e.clientY
      handler.current(now - start.current)
      start.current = now
    }
    const up = () => setDragging(false)
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
    // 拖动时整页禁选,否则会把界面上的文字一路刷蓝
    const prev = document.body.style.userSelect
    document.body.style.userSelect = 'none'
    return () => {
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
      document.body.style.userSelect = prev
    }
  }, [dragging, direction])

  return (
    <div
      role="separator"
      title={title}
      onMouseDown={onDown}
      onDoubleClick={onReset}
      className={cn(
        'group/div shrink-0 transition-colors',
        direction === 'x' ? 'w-1.5 cursor-col-resize' : 'h-1.5 cursor-row-resize',
        dragging && 'bg-info/40',
      )}
    >
      {/* 命中区域比看得见的那条线宽,否则这根线很难点中 */}
      <div
        className={cn(
          'bg-border transition-colors group-hover/div:bg-info/60',
          direction === 'x' ? 'mx-auto h-full w-px' : 'my-auto h-px w-full',
          dragging && 'bg-info',
        )}
      />
    </div>
  )
}

export const clamp = (v: number, lo: number, hi: number) => Math.min(Math.max(v, lo), hi)
