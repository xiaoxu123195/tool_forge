import { IOS_PATH_PRESETS } from '../mobile-forensic/ios-presets'

/**
 * 常用位置。
 *
 * 存在的理由:这两个平台的路径都又长又容易打错,`/private/var/mobile/...`
 * 少一层就什么都拿不到,`/data/data` 敲成 `/data/user/0` 又是另一份视图。
 * 让人每次手敲是这类功能"没人会用"的主要原因。
 */
export interface Preset {
  label: string
  path: string
  note?: string
}

/** iOS 的复用取证工具那份,两边说的是同一批路径 */
const IOS: Preset[] = IOS_PATH_PRESETS.flatMap((g) => g.items).slice(0, 8)

/**
 * Android。前四条要 root —— 没 root 的机器上点了只会得到"权限不足",
 * 所以 needRoot 标出来,界面按当前会话有没有 root 决定灰不灰。
 */
const ANDROID: (Preset & { needRoot?: boolean })[] = [
  {
    label: '应用数据',
    path: '/data/data',
    note: '各家 App 的私有目录,取证的主战场',
    needRoot: true,
  },
  {
    label: '应用数据(多用户)',
    path: '/data/user/0',
    note: '和 /data/data 是同一份数据的另一个入口;分身/工作资料在 /data/user/999 之类',
    needRoot: true,
  },
  {
    label: '账号与系统配置',
    path: '/data/system',
    note: '系统级配置,账号数据库在这下面',
    needRoot: true,
  },
  {
    label: '已装应用',
    path: '/data/app',
    note: '安装包本体',
    needRoot: true,
  },
  { label: '内部存储', path: '/sdcard', note: '不需要 root' },
  {
    label: '应用外部数据',
    path: '/sdcard/Android/data',
    note: 'App 放在外部存储的那部分,不需要 root',
  },
  { label: '相册', path: '/sdcard/DCIM', note: '不需要 root' },
  { label: '下载', path: '/sdcard/Download', note: '不需要 root' },
]

export function presetsFor(platform: string, rooted: boolean): Preset[] {
  if (platform !== 'android') return IOS
  // 没 root 就别把点不开的入口摆出来 —— 摆着只会让人反复点然后收到权限错误
  return ANDROID.filter((p) => rooted || !p.needRoot)
}
