import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { MergeSettings } from './types'

export const DEFAULT_SETTINGS: MergeSettings = {
  layout: 'vertical',
  autoColumns: true,
  columns: 3,
  gap: 0,
  margin: 0,
  radius: 0,
  bg: '#ffffff',
  align: 'min',
  customSize: 1080,
  gridFit: 'contain',
  format: 'png',
  quality: 0.92,
  maxSide: 16384,
}

interface MergeState extends MergeSettings {
  set: <K extends keyof MergeSettings>(key: K, value: MergeSettings[K]) => void
  reset: () => void
}

export const useMergeStore = create<MergeState>()(
  persist(
    (set) => ({
      ...DEFAULT_SETTINGS,
      set: (key, value) => set({ [key]: value } as Pick<MergeSettings, typeof key>),
      reset: () => set({ ...DEFAULT_SETTINGS }),
    }),
    {
      name: 'tool-forge:image-merge',
      version: 1,
      // 升级合并:缺失字段用默认补齐
      merge: (persisted, current) => ({ ...current, ...(persisted as Partial<MergeState>) }),
    },
  ),
)
