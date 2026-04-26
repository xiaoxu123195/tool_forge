import { create } from 'zustand'
import { persist } from 'zustand/middleware'

type Theme = 'light' | 'dark' | 'system'
export type StyleId = 'minimal' | 'nebula' | 'ocean' | 'forest' | 'glass'

interface LayoutState {
  sidebarCollapsed: boolean
  theme: Theme
  styleId: StyleId
  /** Glass 主题:浅色模式壁纸(URL 或 data: URL),空 → 走默认 */
  glassWallpaperLight: string
  /** Glass 主题:深色模式壁纸 */
  glassWallpaperDark: string
  toggleSidebar: () => void
  setTheme: (theme: Theme) => void
  setStyle: (styleId: StyleId) => void
  setGlassWallpaper: (mode: 'light' | 'dark', url: string) => void
}

export const useLayoutStore = create<LayoutState>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      theme: 'system',
      styleId: 'minimal',
      glassWallpaperLight: '',
      glassWallpaperDark: '',
      toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
      setTheme: (theme) => set({ theme }),
      setStyle: (styleId) => set({ styleId }),
      setGlassWallpaper: (mode, url) =>
        set(mode === 'light' ? { glassWallpaperLight: url } : { glassWallpaperDark: url }),
    }),
    { name: 'tool-forge:layout' }
  )
)

export function applyAppearance(
  theme: Theme,
  styleId: StyleId,
  glassWallpaperLight = '',
  glassWallpaperDark = ''
) {
  const root = document.documentElement
  const resolved =
    theme === 'system'
      ? window.matchMedia('(prefers-color-scheme: dark)').matches
        ? 'dark'
        : 'light'
      : theme
  root.classList.toggle('dark', resolved === 'dark')
  if (styleId === 'minimal') {
    root.removeAttribute('data-style')
  } else {
    root.setAttribute('data-style', styleId)
  }
  // Glass:把用户自定义壁纸注入到 CSS 变量;空字符串 → 让 CSS 走 fallback (cherry 默认图)
  if (glassWallpaperLight) {
    root.style.setProperty('--glass-wallpaper-light', `url("${glassWallpaperLight}")`)
  } else {
    root.style.removeProperty('--glass-wallpaper-light')
  }
  if (glassWallpaperDark) {
    root.style.setProperty('--glass-wallpaper-dark', `url("${glassWallpaperDark}")`)
  } else {
    root.style.removeProperty('--glass-wallpaper-dark')
  }
}
