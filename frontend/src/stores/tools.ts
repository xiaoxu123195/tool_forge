import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { LucideIcon } from 'lucide-react'
import { toolRegistry } from '@/tools/registry'

export type ToolCategory =
  | 'forensic'
  | 'data'
  | 'ai'
  | 'codec'
  | 'crypto'
  | 'time'
  | 'text'
  | 'network'
  | 'gen'
  | 'dev'
  | 'system'

export interface ToolMeta {
  id: string
  path: string
  title: string
  description: string
  icon: LucideIcon
  category: ToolCategory
  order: number
  defaultVisible?: boolean
}

export const CATEGORY_LABELS: Record<ToolCategory, string> = {
  forensic: '取证',
  data: '数据处理',
  ai: 'AI 工具',
  codec: '编解码',
  crypto: '加密哈希',
  time: '时间',
  text: '文本',
  network: '网络',
  gen: '生成',
  dev: '开发辅助',
  system: '系统',
}

interface ToolsState {
  visibility: Record<string, boolean>
  order: string[]
  toggleVisibility: (id: string) => void
  setVisibility: (id: string, value: boolean) => void
  setOrder: (order: string[]) => void
  resetOrder: () => void
}

export const useToolsStore = create<ToolsState>()(
  persist(
    (set) => ({
      visibility: {},
      order: [],
      toggleVisibility: (id) =>
        set((s) => ({
          visibility: { ...s.visibility, [id]: !(s.visibility[id] ?? true) },
        })),
      setVisibility: (id, value) =>
        set((s) => ({ visibility: { ...s.visibility, [id]: value } })),
      setOrder: (order) => set({ order }),
      resetOrder: () => set({ order: [] }),
    }),
    { name: 'tool-forge:tools' }
  )
)

export function isVisible(
  id: string,
  visibility: Record<string, boolean>,
  defaultVisible: boolean | undefined
): boolean {
  return visibility[id] ?? defaultVisible ?? true
}

/**
 * 返回按用户排序（若存在）或元数据默认 order 排序的工具。
 * 用户 order 中不存在的工具（新加入的）会按默认 order 追加到末尾。
 */
export function getAllTools(userOrder: string[] = []): ToolMeta[] {
  const byId = new Map(toolRegistry.map((t) => [t.id, t]))
  const result: ToolMeta[] = []
  const seen = new Set<string>()
  for (const id of userOrder) {
    const t = byId.get(id)
    if (t) {
      result.push(t)
      seen.add(id)
    }
  }
  const remaining = toolRegistry
    .filter((t) => !seen.has(t.id))
    .sort((a, b) => a.order - b.order)
  return [...result, ...remaining]
}

export function getVisibleToolsByCategory(
  visibility: Record<string, boolean>,
  userOrder: string[] = []
): Record<ToolCategory, ToolMeta[]> {
  const grouped = {} as Record<ToolCategory, ToolMeta[]>
  for (const tool of getAllTools(userOrder)) {
    if (!isVisible(tool.id, visibility, tool.defaultVisible)) continue
    ;(grouped[tool.category] ||= []).push(tool)
  }
  return grouped
}

export function getToolById(id: string): ToolMeta | undefined {
  return toolRegistry.find((t) => t.id === id)
}
