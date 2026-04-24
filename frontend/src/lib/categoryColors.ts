import type { ToolCategory } from '@/stores/tools'

/**
 * 分类 → 图标底色（Nebula 风格下生效）。
 * Minimal 风格下调用方应回退到 bg-secondary text-foreground。
 */
export const NEBULA_CATEGORY_COLORS: Record<ToolCategory, string> = {
  forensic: 'bg-red-500/15 text-red-600 dark:bg-red-500/20 dark:text-red-300',
  data: 'bg-violet-500/15 text-violet-600 dark:bg-violet-500/20 dark:text-violet-300',
  ai: 'bg-indigo-500/15 text-indigo-600 dark:bg-indigo-500/20 dark:text-indigo-300',
  codec: 'bg-sky-500/15 text-sky-600 dark:bg-sky-500/20 dark:text-sky-300',
  crypto: 'bg-rose-500/15 text-rose-600 dark:bg-rose-500/20 dark:text-rose-300',
  time: 'bg-amber-500/15 text-amber-600 dark:bg-amber-500/20 dark:text-amber-300',
  text: 'bg-purple-500/15 text-purple-600 dark:bg-purple-500/20 dark:text-purple-300',
  network: 'bg-teal-500/15 text-teal-600 dark:bg-teal-500/20 dark:text-teal-300',
  gen: 'bg-emerald-500/15 text-emerald-600 dark:bg-emerald-500/20 dark:text-emerald-300',
  dev: 'bg-orange-500/15 text-orange-600 dark:bg-orange-500/20 dark:text-orange-300',
  system: 'bg-slate-500/15 text-slate-600 dark:bg-slate-500/20 dark:text-slate-300',
}

export const MINIMAL_ICON_CLASS = 'bg-secondary text-foreground'

export function iconClassForCategory(
  category: ToolCategory,
  styleId: string
): string {
  if (styleId === 'nebula') return NEBULA_CATEGORY_COLORS[category]
  return MINIMAL_ICON_CLASS
}
