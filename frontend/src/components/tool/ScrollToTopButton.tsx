import { useEffect, useRef, useState } from 'react'
import { ArrowUp } from 'lucide-react'
import { cn } from '@/lib/utils'

/**
 * 吸附在 ToolShell 滚动容器右下角的"一键到顶"按钮。
 * 用法:把它放在内容顶层,它自己会冒泡找到最近的 [data-tool-scroll]。
 */
export function ScrollToTopButton({ threshold = 240 }: { threshold?: number }) {
  const anchorRef = useRef<HTMLSpanElement>(null)
  const [visible, setVisible] = useState(false)
  const [scroller, setScroller] = useState<HTMLElement | null>(null)

  useEffect(() => {
    const el = anchorRef.current?.closest('[data-tool-scroll]') as HTMLElement | null
    if (!el) return
    setScroller(el)
    const onScroll = () => setVisible(el.scrollTop > threshold)
    onScroll()
    el.addEventListener('scroll', onScroll, { passive: true })
    return () => el.removeEventListener('scroll', onScroll)
  }, [threshold])

  const toTop = () => {
    scroller?.scrollTo({ top: 0, behavior: 'smooth' })
  }

  return (
    <>
      <span ref={anchorRef} aria-hidden className="hidden" />
      <button
        onClick={toTop}
        aria-label="回到顶部"
        title="回到顶部"
        className={cn(
          'fixed bottom-6 right-6 z-30 inline-flex h-10 w-10 items-center justify-center rounded-full border border-border bg-card text-foreground shadow-md transition-all',
          'hover:bg-indigo-500/10 hover:text-indigo-600 dark:hover:text-indigo-300',
          visible ? 'opacity-100' : 'pointer-events-none opacity-0 translate-y-2'
        )}
      >
        <ArrowUp className="h-4 w-4" />
      </button>
    </>
  )
}

/** 读取包裹 `[data-tool-scroll]` 的祖先滚动容器(若存在)。 */
export function findToolScroller(from: HTMLElement | null): HTMLElement | null {
  if (!from) return null
  return from.closest('[data-tool-scroll]') as HTMLElement | null
}
