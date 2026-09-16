import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export interface ForensicStatus {
  checked: boolean
  found: boolean
  path: string
  version: string
  error: string
}

interface ForensicState {
  /** 用户自定义 go-forensic 路径。空 = 使用系统 PATH */
  binaryPath: string
  /**
   * 是否启用 go-forensic 这个备选引擎。
   *
   * 默认不启用:两个平台的提取都已内置,绝大多数人没装过它 —— 让每个人先面对一个
   * 「内置 / go-forensic」的选择题没有意义。启用后取证页面才出现引擎选择。
   */
  cliEnabled: boolean
  /** 默认 SSH 地址（iOS 用） */
  defaultSshAddr: string
  /** 默认输出根目录，前端仅用于 UI 默认值 */
  defaultOutputBase: string
  /** 缓存最近一次检测结果，避免每次进工具页都重新 exec */
  checkCache: {
    /** 缓存针对的路径；与当前 binaryPath 不一致时视为失效 */
    forPath: string
    found: boolean
    resolvedPath: string
    version: string
    error: string
    at: number
  } | null
  /** 最近使用的命令（最多 10 条） */
  history: HistoryItem[]
  /**
   * 用户自己存的常用路径,按平台分开记。
   *
   * 内置的那几组是 iOS 系统数据的固定路径;办案的人各有各的常用位置
   * (某个 App 的数据库目录之类),路径又长又容易少打一层,存个标签比每次翻记录省事
   */
  customPresets: Record<PresetPlatform, CustomPreset[]>
  setBinaryPath: (p: string) => void
  setCliEnabled: (on: boolean) => void
  setDefaultSshAddr: (a: string) => void
  setDefaultOutputBase: (p: string) => void
  setCheckCache: (c: ForensicState['checkCache']) => void
  invalidateCheck: () => void
  pushHistory: (item: HistoryItem) => void
  clearHistory: () => void
  /** 存几条常用路径;同一条路径再存就是改标签,不会存成两条 */
  saveCustomPresets: (platform: PresetPlatform, items: CustomPreset[]) => void
  removeCustomPreset: (platform: PresetPlatform, path: string) => void
}

export type PresetPlatform = 'android' | 'ios'

export interface CustomPreset {
  label: string
  path: string
}

const EMPTY_PRESETS: Record<PresetPlatform, CustomPreset[]> = { android: [], ios: [] }

export interface HistoryItem {
  at: number
  platform: 'android' | 'ios'
  args: string[]
  exitCode: number
  canceled?: boolean
}

export const useForensicStore = create<ForensicState>()(
  persist(
    (set) => ({
      binaryPath: '',
      cliEnabled: false,
      defaultSshAddr: 'root@127.0.0.1:22',
      defaultOutputBase: '',
      checkCache: null,
      history: [],
      customPresets: EMPTY_PRESETS,
      setBinaryPath: (p) =>
        set((s) => ({
          binaryPath: p,
          // 路径变了，旧缓存失效
          checkCache:
            s.checkCache && s.checkCache.forPath === p ? s.checkCache : null,
        })),
      setCliEnabled: (on) => set({ cliEnabled: on }),
      setDefaultSshAddr: (a) => set({ defaultSshAddr: a }),
      setDefaultOutputBase: (p) => set({ defaultOutputBase: p }),
      setCheckCache: (c) => set({ checkCache: c }),
      invalidateCheck: () => set({ checkCache: null }),
      pushHistory: (item) =>
        set((s) => ({ history: [item, ...s.history].slice(0, 10) })),
      clearHistory: () => set({ history: [] }),
      saveCustomPresets: (platform, items) =>
        set((s) => {
          const cur = [...(s.customPresets[platform] ?? [])]
          for (const it of items) {
            const label = it.label.trim()
            const path = it.path.trim()
            if (!label || !path) continue
            const i = cur.findIndex((c) => c.path === path)
            if (i >= 0) cur[i] = { label, path }
            else cur.push({ label, path })
          }
          return { customPresets: { ...s.customPresets, [platform]: cur } }
        }),
      removeCustomPreset: (platform, path) =>
        set((s) => ({
          customPresets: {
            ...s.customPresets,
            [platform]: (s.customPresets[platform] ?? []).filter((c) => c.path !== path),
          },
        })),
    }),
    {
      name: 'tool-forge:forensic',
      version: 2,
      migrate: (state, from) => {
        const s = { ...((state ?? {}) as Partial<ForensicState>) }
        // v1:cliEnabled 是后加的,老配置里没有,反序列化出来是 false。
        // 但配过路径的人显然一直在用 go-forensic —— 不迁的话他们升级后会发现
        // 引擎选择器凭空消失了,而"去哪儿把它找回来"完全没有线索
        if (from < 1 && typeof s.binaryPath === 'string' && s.binaryPath.trim() !== '') {
          s.cliEnabled = true
        }
        // v2:自定义常用路径。两个平台各一份,缺哪个补哪个
        if (from < 2 || !s.customPresets) {
          s.customPresets = { ...EMPTY_PRESETS, ...(s.customPresets ?? {}) }
        }
        return s as ForensicState
      },
    }
  )
)

/** 凭据库中 SSH 密码的 key 规则 */
export function sshPasswordKey(sshAddr: string): string {
  return `forensic:ssh:${sshAddr}`
}
