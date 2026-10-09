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
  /** 连上时的起始目录,「回到起点」用 */
  startPath: string
  /** 最近打开过的目录,回头找路用 */
  recent: string[]

  /** 这几个记住是为了少敲一次;密码不在这里,它进系统凭据库 */
  platform: string
  user: string
  deviceId: string
  /** adb 路径,仅 Android。留空走系统 PATH */
  adbPath: string
  /** 上次连上的设备信息,断开后界面上还要用来说明刚才连的是什么 */
  rooted: boolean
  /**
   * 实际连上的那台的序列号和型号。上面的 deviceId 是用户填的,留空表示「第一台」——
   * 投屏要的是确切的那一台,不能再按「第一台」去猜一次
   */
  serial: string
  model: string

  /** 投屏截图、录屏存到哪个文件夹。第一次截的时候问一次,之后不再弹框 */
  captureDir: string
  /** 投屏期间不让手机自动息屏(手机端定时报「有人在用」,不改手机设置) */
  mirrorKeepAwake: boolean

  setSession: (id: string, startPath: string, rooted: boolean, serial: string, model: string) => void
  clearSession: () => void
  setCwd: (p: string) => void
  setUser: (u: string) => void
  setDeviceId: (d: string) => void
  setPlatform: (p: string) => void
  setAdbPath: (p: string) => void
  setCaptureDir: (d: string) => void
  setMirrorKeepAwake: (v: boolean) => void
}

const MAX_RECENT = 12

export const useDeviceBrowserStore = create<DeviceBrowserState>()(
  persist(
    (set) => ({
      sessionId: '',
      cwd: '',
      startPath: '',
      recent: [],
      platform: 'ios',
      user: 'root',
      deviceId: '',
      adbPath: '',
      rooted: false,
      serial: '',
      model: '',
      captureDir: '',
      mirrorKeepAwake: true,

      setSession: (id, startPath, rooted, serial, model) =>
        // startPath 单独留一份:cwd 会随着翻目录一直变,
        // 而「回到起点」要的是连上时那个位置(安卓 /data/data、iOS mobile 的 Library),
        // 比回到 / 有用得多 —— / 底下全是系统目录,翻不到应用数据
        set({ sessionId: id, cwd: startPath, startPath, rooted, serial, model }),
      // 断开时不清 recent:下次连上还想回到上次翻的地方
      clearSession: () => set({ sessionId: '', cwd: '', startPath: '', serial: '', model: '' }),
      setCwd: (p) =>
        set((s) => ({
          cwd: p,
          recent: [p, ...s.recent.filter((x) => x !== p)].slice(0, MAX_RECENT),
        })),
      setUser: (u) => set({ user: u }),
      setDeviceId: (d) => set({ deviceId: d }),
      // 换平台时把当前目录清掉:两个平台的路径完全不相干,
      // 留着上一个的路径下次连上会直接列到一个不存在的目录
      setPlatform: (p) => set({ platform: p, cwd: '' }),
      setAdbPath: (p) => set({ adbPath: p }),
      setCaptureDir: (d) => set({ captureDir: d }),
      setMirrorKeepAwake: (v) => set({ mirrorKeepAwake: v }),
    }),
    {
      name: 'tool-forge:device-browser',
      // sessionId 和 cwd 不能持久化:app 一关,Go 那边的会话和转发进程就没了,
      // 下次启动拿着一个死 id 会一直报"会话不存在"
      partialize: (s) => ({
        recent: s.recent,
        platform: s.platform,
        user: s.user,
        deviceId: s.deviceId,
        adbPath: s.adbPath,
        captureDir: s.captureDir,
        mirrorKeepAwake: s.mirrorKeepAwake,
      }),
    }
  )
)
