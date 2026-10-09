// 投屏用到的纯函数:解析手机端发来的包、坐标换算、滚轮换算。
// 和界面、解码器都不沾边,冒烟测试能直接拿来核对。

/** 手机端发来的一个包。字节布局见后端 backend/tools/mirror/protocol.go */
export type Packet =
  /** 会话包:新一段编码开始,带着画面尺寸。手机一转屏就会来一个 */
  | { kind: 'session'; width: number; height: number }
  /** 配置包:SPS、PPS,解码器要先拿到它才能解后面的帧 */
  | { kind: 'config'; data: Uint8Array }
  | { kind: 'frame'; key: boolean; pts: number; data: Uint8Array }

export function parsePacket(p: Uint8Array): Packet | null {
  if (p.length < 12) return null
  const dv = new DataView(p.buffer, p.byteOffset, p.byteLength)
  if (p[0] & 0x80) return { kind: 'session', width: dv.getUint32(4), height: dv.getUint32(8) }
  const data = p.subarray(12)
  if (p[0] & 0x40) return { kind: 'config', data }
  // 低 61 位是微秒时间戳。高 32 位去掉三个标志位再拼回去,不用 BigInt
  const pts = (dv.getUint32(0) & 0x1fffffff) * 2 ** 32 + dv.getUint32(4)
  return { kind: 'frame', key: (p[0] & 0x20) !== 0, pts, data }
}

/**
 * 从配置包里的 SPS 拼出 WebCodecs 认的编码串 avc1.PPCCLL:
 * 起始码之后 NAL 类型为 7 的就是 SPS,紧跟着的三个字节是档次、约束、级别
 */
export function codecFromConfig(b: Uint8Array): string | null {
  for (let i = 0; i + 6 < b.length; i++) {
    if (b[i] === 0 && b[i + 1] === 0 && b[i + 2] === 1 && (b[i + 3] & 0x1f) === 7) {
      return 'avc1.' + [b[i + 4], b[i + 5], b[i + 6]].map((x) => x.toString(16).padStart(2, '0')).join('')
    }
  }
  return null
}

export function concatBytes(a: Uint8Array, b: Uint8Array): Uint8Array {
  const out = new Uint8Array(a.length + b.length)
  out.set(a, 0)
  out.set(b, a.length)
  return out
}

interface Rect {
  left: number
  top: number
  width: number
  height: number
}

/** 屏幕上的点 → 视频画面里的坐标(会话包报的那个尺寸) */
export function toVideoPoint(rect: Rect, size: { w: number; h: number }, clientX: number, clientY: number) {
  const clamp = (v: number, n: number) => Math.max(0, Math.min(n - 1, Math.round(v)))
  return {
    x: clamp(((clientX - rect.left) / (rect.width || 1)) * size.w, size.w),
    y: clamp(((clientY - rect.top) / (rect.height || 1)) * size.h, size.h),
  }
}

/**
 * 滚轮 → 滚了几格。浏览器里一格是 100 像素(按行算的一格是 3 行);
 * 方向也要翻一下:浏览器里往下滚是正的,安卓里往上滚是正的
 */
export function wheelNotches(e: { deltaX: number; deltaY: number; deltaMode: number }) {
  const unit = e.deltaMode === 1 ? 3 : e.deltaMode === 2 ? 1 : 100
  return { hs: e.deltaX / unit, vs: -e.deltaY / unit }
}

/**
 * 画面在面板里显示多大:高度撑满,宽度按比例;太宽(横屏)时按最大宽度缩,高度跟着变
 */
export function fitSize(areaHeight: number, aspect: number, maxWidth: number) {
  let height = Math.max(0, areaHeight)
  let width = height * aspect
  if (width > maxWidth) {
    width = maxWidth
    height = width / aspect
  }
  return { width: Math.round(width), height: Math.round(height) }
}

/** 用到的安卓键码 */
export const KEY = { HOME: 3, POWER: 26, APP_SWITCH: 187 } as const

/** 界面发给后端的操作,和后端 protocol.go 里的 event 一一对应 */
export type ControlEvent =
  | { t: 'touch'; a: 0 | 1 | 2; x: number; y: number; w: number; h: number }
  | { t: 'scroll'; x: number; y: number; w: number; h: number; hs: number; vs: number }
  | { t: 'key'; a: 0 | 1; k: number }
  | { t: 'back'; a: 0 | 1 }
  | { t: 'reset' }
