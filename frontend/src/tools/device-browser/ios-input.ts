// iOS 投屏的键盘:把电脑上的按键换成 VNC 认的 X11 keysym,手机上的 TrollVNC 再变成 iPhone 外接键盘的按键。
// 纯函数,和界面不沾边,冒烟测试直接拿来核对。
//
// TrollVNC 收到的每个键都原样按下、抬起,不会替字符补 Shift:大写字母、!@# 这些得自己先按住 Shift。
// 修饰键也是如此,所以这里发的都是完整的一串:先按修饰键,再按键,倒着抬起来

/** 用到的 keysym */
export const XK = {
  BackSpace: 0xff08,
  Tab: 0xff09,
  Return: 0xff0d,
  Escape: 0xff1b,
  Delete: 0xffff,
  Home: 0xff50,
  Left: 0xff51,
  Up: 0xff52,
  Right: 0xff53,
  Down: 0xff54,
  PageUp: 0xff55,
  PageDown: 0xff56,
  End: 0xff57,
  Shift: 0xffe1,
  // 手机上的设置是标准映射:Alt 是 Option,Super 是 Command
  Option: 0xffe9,
  Command: 0xffeb,
  // 这三个媒体键 TrollVNC 当成手机的音量键
  VolumeUp: 0x1008ff13,
  VolumeDown: 0x1008ff11,
} as const

/** 不产生文字的键 */
const SPECIAL: Record<string, number> = {
  Enter: XK.Return,
  Backspace: XK.BackSpace,
  Delete: XK.Delete,
  Tab: XK.Tab,
  Escape: XK.Escape,
  ArrowUp: XK.Up,
  ArrowDown: XK.Down,
  ArrowLeft: XK.Left,
  ArrowRight: XK.Right,
  Home: XK.Home,
  End: XK.End,
  PageUp: XK.PageUp,
  PageDown: XK.PageDown,
}

/** 要按着 Shift 才打得出来的符号(美式键盘) */
const SHIFTED = '~!@#$%^&*()_+{}|:"<>?'

export type IosKeyAction =
  /** 依次按下这几个键,再倒着抬起来 */
  | { kind: 'keys'; keys: number[] }
  /** Ctrl+V:把电脑剪贴板里的字粘到手机上 */
  | { kind: 'paste' }
  /** Ctrl+C / Ctrl+X:让手机复制(剪切)选中的字,再拿回电脑 */
  | { kind: 'copy'; cut: boolean }

interface KeyLike {
  key: string
  ctrlKey: boolean
  metaKey: boolean
  altKey: boolean
  shiftKey: boolean
}

/**
 * 一次按键该怎么发。返回 null 表示不归这里管:能打出字的键等输入框给出文字再发;
 * 没认领的 Ctrl 组合键不发给手机,留给工具箱自己(比如 Ctrl+K 命令面板)
 */
export function iosKeyAction(e: KeyLike): IosKeyAction | null {
  const shift = e.shiftKey ? [XK.Shift] : []
  // AltGr 在 Windows 上报成 Ctrl+Alt,那是在打字(比如德语键盘的 @),不算快捷键
  if ((e.ctrlKey || e.metaKey) && !e.altKey) {
    switch (e.key.toLowerCase()) {
      case 'v':
        return { kind: 'paste' }
      case 'c':
        return { kind: 'copy', cut: false }
      case 'x':
        return { kind: 'copy', cut: true }
      case 'a':
        return { kind: 'keys', keys: [XK.Command, 0x61] }
      case 'z':
        // Ctrl+Z 撤销、Ctrl+Shift+Z 重做,iPhone 上是 Command
        return { kind: 'keys', keys: [XK.Command, ...shift, 0x7a] }
    }
    // Ctrl+退格删一个词、Ctrl+方向键按词跳:iPhone 上同样的事是 Option 做的
    const k = SPECIAL[e.key]
    return k ? { kind: 'keys', keys: [XK.Option, ...shift, k] } : null
  }
  if (e.altKey && !e.ctrlKey && !e.metaKey) {
    // Alt 当 Command 用:iPhone 外接键盘上的 Command 快捷键,电脑上 Win 键会被系统拦掉
    const k = SPECIAL[e.key] ?? printable(e.key)
    return k ? { kind: 'keys', keys: [XK.Command, ...shift, k] } : null
  }
  const k = SPECIAL[e.key]
  // Shift+方向键是选字
  return k ? { kind: 'keys', keys: [...shift, k] } : null
}

/** 一个能打出来的字符的 keysym(字母一律按小写,大小写看 Shift);不是就返回 0 */
function printable(key: string): number {
  if (key.length !== 1) return 0
  const c = key.toLowerCase().charCodeAt(0)
  return c >= 0x20 && c <= 0x7e ? c : 0
}

/**
 * 一段英文、数字、符号按键打过去要按哪些键:每个字符一串。
 * 大写字母和上排符号前面补上 Shift —— 手机那头不替我们补
 */
export function textKeys(s: string): number[][] {
  const out: number[][] = []
  for (const ch of s) {
    if (ch === '\n') out.push([XK.Return])
    else if (ch === '\t') out.push([XK.Tab])
    else {
      const c = ch.charCodeAt(0)
      if (c < 0x20 || c > 0x7e) continue
      const shifted = (c >= 0x41 && c <= 0x5a) || SHIFTED.includes(ch)
      out.push(shifted ? [XK.Shift, c] : [c])
    }
  }
  return out
}
