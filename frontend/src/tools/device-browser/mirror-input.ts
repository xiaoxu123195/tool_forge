// 投屏里的键盘和双指缩放:把电脑上的输入换成发给手机的操作。
// 纯函数,和界面不沾边,冒烟测试直接拿来核对。

/** 安卓 KeyEvent.META_*:总开关和左边那个键一起置上,和真键盘按下时一样 */
export const META = { SHIFT: 0x41, CTRL: 0x3000 } as const

/** 不产生文字的键:按键码发。能打出字的键不在这里,它们走输入框的文字 */
const SPECIAL_KEYS: Record<string, number> = {
  Enter: 66,
  Backspace: 67,
  Delete: 112,
  Tab: 61,
  // 安卓里 Esc 没人接时会当成返回键
  Escape: 111,
  ArrowUp: 19,
  ArrowDown: 20,
  ArrowLeft: 21,
  ArrowRight: 22,
  Home: 122,
  End: 123,
  PageUp: 92,
  PageDown: 93,
}

/** 安卓的 A 键:Ctrl+A 全选 */
const KEYCODE_A = 29

export type KeyAction =
  | { kind: 'key'; code: number; meta: number }
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
export function keyAction(e: KeyLike): KeyAction | null {
  const shift = e.shiftKey ? META.SHIFT : 0
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
        return { kind: 'key', code: KEYCODE_A, meta: META.CTRL }
    }
    // Ctrl+退格删一个词、Ctrl+方向键按词跳,手机上的输入框也认
    const code = SPECIAL_KEYS[e.key]
    return code ? { kind: 'key', code, meta: META.CTRL | shift } : null
  }
  const code = SPECIAL_KEYS[e.key]
  // Shift+方向键是选字
  return code ? { kind: 'key', code, meta: shift } : null
}

/**
 * 这段文字能不能直接按键打出来。手机端是一个字一个字模拟按键的,
 * 只认键盘上有的字符;中文、表情这些只能经剪贴板粘贴
 */
export function typeable(s: string): boolean {
  return /^[\x20-\x7e]+$/.test(s)
}

interface Pt {
  x: number
  y: number
}

/**
 * 双指缩放时两根手指的位置:以 center 为中心横着摆开,间距 2×half。
 * 靠边时钳在画面里 —— 一根手指顶住边,另一根照样能张合
 */
export function pinchFingers(center: Pt, half: number, size: { w: number; h: number }): [Pt, Pt] {
  const clamp = (v: number, n: number) => Math.max(0, Math.min(n - 1, Math.round(v)))
  const y = clamp(center.y, size.h)
  return [
    { x: clamp(center.x - half, size.w), y },
    { x: clamp(center.x + half, size.w), y },
  ]
}

/**
 * 滚轮转一下,两指间距变成原来的几倍。往前滚(deltaY 为负)是放大。
 * 鼠标一格 deltaY 是 100 左右,大约张开两成;触控板捏合给的 deltaY 很小,按比例来,手感是连续的
 */
export function pinchFactor(deltaY: number): number {
  return Math.exp(-deltaY * 0.002)
}
