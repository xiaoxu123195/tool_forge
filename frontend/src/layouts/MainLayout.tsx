import { useEffect, useState } from 'react'
import { Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { TitleBar } from './TitleBar'
import { CommandPalette } from '@/components/CommandPalette'
import { ConfirmProvider } from '@/components/ui/confirm'
import { applyAppearance, useLayoutStore } from '@/stores/layout'

export function MainLayout() {
  const theme = useLayoutStore((s) => s.theme)
  const styleId = useLayoutStore((s) => s.styleId)
  const [paletteOpen, setPaletteOpen] = useState(false)

  useEffect(() => {
    applyAppearance(theme, styleId)
    if (theme !== 'system') return
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => applyAppearance('system', styleId)
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [theme, styleId])

  // 全局快捷键：Ctrl+K / Cmd+K 打开命令面板
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setPaletteOpen((v) => !v)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  return (
    <ConfirmProvider>
      <div className="flex h-full w-full flex-col overflow-hidden bg-background text-foreground">
        <TitleBar />
        <div className="flex flex-1 overflow-hidden">
          <Sidebar onOpenCommandPalette={() => setPaletteOpen(true)} />
          <main className="flex-1 overflow-auto">
            <Outlet />
          </main>
        </div>
        <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)} />
      </div>
    </ConfirmProvider>
  )
}
