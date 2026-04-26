import type { ToolCategory } from '@/stores/tools'

/**
 * 分类 → 图标底色(渐变彩色)。所有"有氛围"的主题(Nebula / Ocean / Forest)
 * 都使用同一套分类色,Minimal 主题回退到 bg-secondary 中性灰。
 */
export const THEMED_CATEGORY_COLORS: Record<ToolCategory, string> = {
  forensic: 'bg-gradient-to-br from-red-500/20 to-red-600/10 text-red-600 dark:from-red-500/25 dark:to-red-600/10 dark:text-red-300',
  data: 'bg-gradient-to-br from-blue-500/20 to-blue-600/10 text-blue-600 dark:from-blue-500/25 dark:to-blue-600/10 dark:text-blue-300',
  ai: 'bg-gradient-to-br from-cyan-500/20 to-cyan-600/10 text-cyan-600 dark:from-cyan-500/25 dark:to-cyan-600/10 dark:text-cyan-300',
  codec: 'bg-gradient-to-br from-sky-500/20 to-sky-600/10 text-sky-600 dark:from-sky-500/25 dark:to-sky-600/10 dark:text-sky-300',
  crypto: 'bg-gradient-to-br from-rose-500/20 to-rose-600/10 text-rose-600 dark:from-rose-500/25 dark:to-rose-600/10 dark:text-rose-300',
  time: 'bg-gradient-to-br from-amber-500/20 to-amber-600/10 text-amber-600 dark:from-amber-500/25 dark:to-amber-600/10 dark:text-amber-300',
  text: 'bg-gradient-to-br from-pink-500/20 to-pink-600/10 text-pink-600 dark:from-pink-500/25 dark:to-pink-600/10 dark:text-pink-300',
  network: 'bg-gradient-to-br from-teal-500/20 to-teal-600/10 text-teal-600 dark:from-teal-500/25 dark:to-teal-600/10 dark:text-teal-300',
  gen: 'bg-gradient-to-br from-emerald-500/20 to-emerald-600/10 text-emerald-600 dark:from-emerald-500/25 dark:to-emerald-600/10 dark:text-emerald-300',
  dev: 'bg-gradient-to-br from-orange-500/20 to-orange-600/10 text-orange-600 dark:from-orange-500/25 dark:to-orange-600/10 dark:text-orange-300',
  system: 'bg-gradient-to-br from-slate-500/20 to-slate-600/10 text-slate-600 dark:from-slate-500/25 dark:to-slate-600/10 dark:text-slate-300',
}

export const MINIMAL_ICON_CLASS = 'bg-secondary text-foreground'

export function iconClassForCategory(
  category: ToolCategory,
  styleId: string
): string {
  if (styleId === 'minimal') return MINIMAL_ICON_CLASS
  return THEMED_CATEGORY_COLORS[category]
}
