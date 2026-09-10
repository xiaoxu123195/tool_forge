import { create } from 'zustand'
import type { mmkv } from '../../wailsjs/go/models'

/** 当前打开的文件。解析结果整个由后端给,前端不再自己解 */
export interface LoadedFile {
  path: string
  /** 解密参数。留着是为了能回头再读一次(比如取某个值的完整十六进制)——
   *  只在内存里,store 本来就不做持久化 */
  crcPath: string
  keyHex: string
  result: mmkv.FileResult
}

/** 每个 key 的每个历史值当前被看成什么类型 */
export type TypesMap = Record<string, string[]>

interface MmkvState {
  file: LoadedFile | null
  typesByKey: TypesMap
  search: string
  /** Key 列宽度（像素），持久于会话内 */
  keyColWidth: number

  setFile: (f: LoadedFile | null) => void
  setTypesByKey: (t: TypesMap) => void
  cycleType: (key: string, index: number, next: string) => void
  setSearch: (s: string) => void
  setKeyColWidth: (px: number) => void
  reset: () => void
}

/**
 * MMKV 工具状态。刻意不走 persist —— 解析结果可能有几千条,不适合 localStorage,
 * 只要保证切换路由不丢（组件卸载后 store 仍在内存中）即可。
 */
export const useMmkvStore = create<MmkvState>((set) => ({
  file: null,
  typesByKey: {},
  search: '',
  keyColWidth: 260,

  setFile: (f) => set({ file: f }),
  setTypesByKey: (t) => set({ typesByKey: t }),
  cycleType: (key, index, next) =>
    set((s) => {
      const arr = s.typesByKey[key] ?? []
      const nextArr = [...arr]
      nextArr[index] = next
      return { typesByKey: { ...s.typesByKey, [key]: nextArr } }
    }),
  setSearch: (s) => set({ search: s }),
  setKeyColWidth: (px) => set({ keyColWidth: px }),
  reset: () => set({ file: null, typesByKey: {}, search: '' }),
}))
