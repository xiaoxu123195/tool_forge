/**
 * iOS 系统自带应用的数据位置。
 *
 * 这些数据不属于任何第三方 App,没有包名可搜 —— 只能按绝对路径取。而路径本身
 * 既长又容易打错(`/private/var/mobile/...` 少一层就什么都拿不到),让人每次手敲
 * 是这个功能"没人会用"的主要原因,所以做成一点就加的预设。
 *
 * 路径以 `/private/var/mobile/` 开头而不是 `/var/mobile/`:后者是前者的软链,
 * 多数情况下都能走通,但写全的那个不依赖链接是否存在。
 */
export interface PathPreset {
  label: string
  path: string
  /** 简短说明,鼠标悬停时显示 */
  note?: string
}

export interface PresetGroup {
  title: string
  items: PathPreset[]
}

export const IOS_PATH_PRESETS: PresetGroup[] = [
  {
    title: '邮件与账号',
    items: [
      {
        label: '邮件',
        path: '/private/var/mobile/Library/Mail/',
        note: '系统自带邮箱的邮件正文、附件与索引库',
      },
      {
        label: '账号',
        path: '/private/var/mobile/Library/Accounts/',
        note: '系统账号库(邮箱、社交等账号的登录信息)',
      },
      {
        label: '通讯录捐赠',
        path: '/private/var/mobile/Library/Contacts/Donations/',
        note: '系统记录的联系人交互(谁在什么时候被联系过)',
      },
    ],
  },
  {
    title: '通讯',
    items: [
      { label: '短信 / iMessage', path: '/private/var/mobile/Library/SMS/' },
      { label: '通话记录', path: '/private/var/mobile/Library/CallHistoryDB/' },
      { label: '通讯录', path: '/private/var/mobile/Library/AddressBook/' },
    ],
  },
  {
    title: '其他系统数据',
    items: [
      { label: 'Safari', path: '/private/var/mobile/Library/Safari/' },
      { label: '备忘录', path: '/private/var/mobile/Library/Notes/' },
      { label: '日历', path: '/private/var/mobile/Library/Calendar/' },
      { label: '照片库', path: '/private/var/mobile/Media/PhotoData/' },
      {
        label: '偏好设置',
        path: '/private/var/mobile/Library/Preferences/',
        note: '各 App 的 plist 配置,常含账号名、最后登录时间等',
      },
    ],
  },
]
