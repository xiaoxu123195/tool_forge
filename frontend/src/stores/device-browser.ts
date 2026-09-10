import { create } from 'zustand'
import { persist } from 'zustand/middleware'

/**
 * 真机浏览的状态。
 *
 * 连接本身活在 Go 那边(一个 USB 转发进程 + 一条 SSH),前端只拿着 sessionId。
 * 所以这里存的是"指向那个会话的把手"和界面位置,不是连接本身 ——
 * 切到别的工具再切回来,连接还在。
 */
interface DeviceBrowserState {
  sessionId: string
  /** 当前所在目录 */
  cwd: string
  /** 最近打开过的目录,回头找路用 */
  recent: string[]

  /** 这两个记住是为了少敲一次;密码不在这里,它进系统凭据库 */
  user: string
  deviceId: string

  setSession: (id: string, startPath: string) => void
  clearSession: () => void
  setCwd: (p: string) => void
  setUser: (u: string) => void
  setDeviceId: (d: string) => void
}

const MAX_RECENT = 12

export const useDeviceBrowserStore = create<DeviceBrowserState>()(
  persist(
    (set) => ({
      sessionId: '',
      cwd: '',
      recent: [],
      user: 'root',
      deviceId: '',

      setSession: (id, startPath) => set({ sessionId: id, cwd: startPath }),
      // 断开时不清 recent:下次连上还想回到上次翻的地方
      clearSession: () => set({ sessionId: '', cwd: '' }),
      setCwd: (p) =>
        set((s) => ({
          cwd: p,
          recent: [p, ...s.recent.filter((x) => x !== p)].slice(0, MAX_RECENT),
        })),
      setUser: (u) => set({ user: u }),
      setDeviceId: (d) => set({ deviceId: d }),
    }),
    {
      name: 'tool-forge:device-browser',
      // sessionId 和 cwd 不能持久化:app 一关,Go 那边的会话和转发进程就没了,
      // 下次启动拿着一个死 id 会一直报"会话不存在"
      partialize: (s) => ({ recent: s.recent, user: s.user, deviceId: s.deviceId }),
    }
  )
)
