import { create } from 'zustand'
import { persist } from 'zustand/middleware'

/**
 * MCP 工作台的分栏尺寸。
 *
 * 单独记而不是写死:三栏各自要多宽完全看人在干什么 —— 翻一个有八十个工具的服务器时
 * 想把左栏拉宽,读一段几百行的 JSON 响应时想把右栏拉宽。记下来是因为这个偏好
 * 一旦调好就不会天天改,每次进来重摆一遍纯属折腾。
 */
interface WorkbenchLayout {
  /** 左栏宽度(px) */
  leftWidth: number
  /** 中栏宽度(px),仅三栏横排时用 */
  midWidth: number
  /** 窄窗口下上下分栏时,参数区占的比例(0-1) */
  vRatio: number
  setLeftWidth: (w: number) => void
  setMidWidth: (w: number) => void
  setVRatio: (r: number) => void
  reset: () => void
}

export const WB_DEFAULT = { leftWidth: 240, midWidth: 320, vRatio: 0.5 }

/** 各栏的下限。比这更窄就没法读了,拖也拖不过去 */
export const WB_MIN = { left: 170, mid: 210, right: 240, vPane: 120 }

export const useWorkbenchLayout = create<WorkbenchLayout>()(
  persist(
    (set) => ({
      ...WB_DEFAULT,
      setLeftWidth: (w) => set({ leftWidth: w }),
      setMidWidth: (w) => set({ midWidth: w }),
      setVRatio: (r) => set({ vRatio: r }),
      reset: () => set({ ...WB_DEFAULT }),
    }),
    { name: 'tool-forge:mcp-workbench-layout' },
  ),
)
