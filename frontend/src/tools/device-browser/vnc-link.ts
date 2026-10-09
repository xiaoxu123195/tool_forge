import type RFB from '@novnc/novnc'
import { StopIOSMirror } from '../../../wailsjs/go/main/App'

export interface VncCallbacks {
  /** 画面尺寸变了(连上、手机转屏) */
  onSize: (w: number, h: number) => void
  /** 连上了,开始出画面 */
  onLive: () => void
  /** 手机上复制了东西:TrollVNC 会把手机剪贴板的变化推过来 */
  onClipboard: (text: string) => void
  /** 断开了,不会再有画面 */
  onEnded: (reason: string) => void
}

/** VNC 协议里的鼠标按键:TrollVNC 把右键当 Home、中键当电源键 */
export const BUTTON = { HOME: 4, POWER: 2 } as const

/**
 * 一路 iOS 投屏在界面这一侧的全部:noVNC 的 VNC 客户端,外加一块显示用的画布。
 *
 * noVNC 的画布是手机原尺寸(1242×2208 这样),交给浏览器按 CSS 缩进面板,走的是最粗的那种缩放,
 * 细字会发虚。所以它那块画布留在上面接鼠标、但是透明,画面按屏幕实际像素高质量缩小,另画到下面这块上。
 * 只在有数据进来时才重画:手机画面不动,一帧都不画
 */
export class VncLink {
  private rfb: RFB | null = null
  private ws: WebSocket | null = null
  /** noVNC 自己的画布,原尺寸 */
  private src: HTMLCanvasElement | null = null
  private ctx: CanvasRenderingContext2D | null = null
  private raf = 0
  /** 收到数据后再画一小会儿:图片是异步解出来的,落到画布上要晚几十毫秒 */
  private paintUntil = 0
  private w = 0
  private h = 0
  private failure = ''
  private ended = false

  private constructor(
    private readonly sessionId: string,
    private readonly view: HTMLCanvasElement,
    private readonly cb: VncCallbacks,
  ) {}

  /** 连上手机。noVNC 用到时才加载,不拖慢别的页面 */
  static async open(
    sessionId: string,
    url: string,
    password: string,
    host: HTMLElement,
    view: HTMLCanvasElement,
    quality: number,
    cb: VncCallbacks,
  ): Promise<VncLink> {
    const { default: Rfb } = await import('@novnc/novnc')
    const link = new VncLink(sessionId, view, cb)
    link.connect(Rfb, url, password, host, quality)
    return link
  }

  private connect(Rfb: typeof RFB, url: string, password: string, host: HTMLElement, quality: number) {
    // WebSocket 自己建:要知道什么时候有数据进来,好重画
    const ws = new WebSocket(url)
    ws.binaryType = 'arraybuffer'
    ws.addEventListener('message', () => this.touch())
    // 上一路的画面元素 noVNC 断开时会自己拿走,但断开是异步的:先清掉,免得新旧两块叠在一起
    host.replaceChildren()
    const rfb = new Rfb(host, ws, { shared: true, credentials: { password } })
    rfb.scaleViewport = true
    // 键盘归面板自己的输入框(要接输入法);noVNC 只管鼠标和滚轮
    rfb.focusOnClick = false
    rfb.background = 'transparent'
    rfb.qualityLevel = quality
    rfb.addEventListener('connect', () => {
      this.touch()
      this.cb.onLive()
    })
    rfb.addEventListener('credentialsrequired', () => rfb.sendCredentials({ password }))
    rfb.addEventListener('securityfailure', () => {
      this.failure = '手机上的 TrollVNC 没认这次的密码，点重新连接再试一次'
    })
    rfb.addEventListener('clipboard', (e) => this.cb.onClipboard((e as CustomEvent<{ text: string }>).detail.text ?? ''))
    rfb.addEventListener('disconnect', () =>
      this.end(this.failure || '投屏断开了：手机拔掉了，或者手机上的 TrollVNC 停了'),
    )
    this.ws = ws
    this.rfb = rfb
    this.src = host.querySelector('canvas')
  }

  /** 设成 0–9:越大越清楚、越占 USB。不用重连 */
  setQuality(level: number) {
    if (this.rfb) this.rfb.qualityLevel = level
  }

  /** 依次按下这几个键,再倒着抬起来 */
  press(keys: number[]) {
    const r = this.rfb
    if (!r || this.ended) return
    for (const k of keys) r.sendKey(k, null, true)
    for (const k of [...keys].reverse()) r.sendKey(k, null, false)
  }

  /** 往手机剪贴板里放字:TrollVNC 收到就写进 iPhone 的剪贴板 */
  setClipboard(text: string) {
    if (!this.ended) this.rfb?.clipboardPasteFrom(text)
  }

  /**
   * 按一下 Home 或电源键。TrollVNC 把鼠标右键、中键映射成这两个,
   * 这里直接发一条 VNC 的鼠标消息(类型 5:按键、横坐标、纵坐标),和在画面上点是同一回事
   */
  click(button: number) {
    const ws = this.ws
    if (!ws || ws.readyState !== WebSocket.OPEN || this.ended || !this.w) return
    const msg = (mask: number) => {
      const x = this.w >> 1
      const y = this.h >> 1
      return new Uint8Array([5, mask, x >> 8, x & 0xff, y >> 8, y & 0xff])
    }
    ws.send(msg(button))
    ws.send(msg(0))
  }

  /** 截一张原尺寸的 PNG,给出 base64 */
  snapshot(): Promise<string> {
    return new Promise((resolve, reject) => {
      const r = this.rfb
      if (!r || !this.w) {
        reject(new Error('还没有画面'))
        return
      }
      r.toBlob((blob) => {
        if (!blob) {
          reject(new Error('截图失败：画面取不出来'))
          return
        }
        blobBase64(blob).then(resolve, reject)
      }, 'image/png')
    })
  }

  /** noVNC 那块原尺寸的画布:录屏从它上面录 */
  source(): HTMLCanvasElement | null {
    return this.src
  }

  /** 画布换了尺寸:马上重画一次 */
  redraw() {
    this.paint()
  }

  /** 主动收掉(关面板、换设置、组件卸载):不回调,顺带让后端停掉手机上的服务 */
  close() {
    if (!this.ended) {
      this.ended = true
      cancelAnimationFrame(this.raf)
      this.rfb?.disconnect()
    }
    void StopIOSMirror(this.sessionId).catch(() => {})
  }

  private touch() {
    this.paintUntil = performance.now() + 300
    if (!this.raf && !this.ended) this.raf = requestAnimationFrame(this.frame)
  }

  private frame = () => {
    this.raf = 0
    if (this.ended) return
    this.paint()
    if (performance.now() < this.paintUntil) this.raf = requestAnimationFrame(this.frame)
  }

  private paint() {
    const src = this.src
    if (!src || !src.width || !src.height) return
    if (src.width !== this.w || src.height !== this.h) {
      this.w = src.width
      this.h = src.height
      this.cb.onSize(this.w, this.h)
    }
    this.ctx ??= this.view.getContext('2d')
    const ctx = this.ctx
    if (!ctx) return
    // 画布一改尺寸,这两项就会被重置,所以每次都设
    ctx.imageSmoothingEnabled = true
    ctx.imageSmoothingQuality = 'high'
    ctx.drawImage(src, 0, 0, this.view.width, this.view.height)
  }

  private end(reason: string) {
    if (this.ended) return
    this.ended = true
    cancelAnimationFrame(this.raf)
    this.cb.onEnded(reason)
  }
}

/** 浏览器能不能录屏:要能从画布取视频流,还要有 MediaRecorder */
export function supportsRecording(): boolean {
  return (
    typeof MediaRecorder !== 'undefined' &&
    typeof HTMLCanvasElement !== 'undefined' &&
    typeof HTMLCanvasElement.prototype.captureStream === 'function'
  )
}

/** 录屏格式:能录 MP4 就录 MP4(Windows 自带的播放器就能放),不行再退到 WebM */
function recordingType(): { mime: string; ext: '.mp4' | '.webm' } {
  for (const mime of ['video/mp4;codecs=avc1', 'video/mp4']) {
    if (MediaRecorder.isTypeSupported(mime)) return { mime, ext: '.mp4' }
  }
  for (const mime of ['video/webm;codecs=vp9', 'video/webm']) {
    if (MediaRecorder.isTypeSupported(mime)) return { mime, ext: '.webm' }
  }
  return { mime: '', ext: '.webm' }
}

/**
 * 把一块画布录成视频,每秒交出一段(按顺序,base64)。
 * 先建好、问清楚是什么格式,后端开好文件再 start
 */
export class CanvasRecorder {
  readonly ext: '.mp4' | '.webm'
  private readonly mime: string
  private rec: MediaRecorder | null = null
  private queue: Promise<void> = Promise.resolve()
  private failed: unknown = null

  constructor(private readonly canvas: HTMLCanvasElement) {
    const t = recordingType()
    this.ext = t.ext
    this.mime = t.mime
  }

  start(onChunk: (b64: string) => Promise<void>) {
    const stream = this.canvas.captureStream(30)
    const rec = new MediaRecorder(stream, {
      ...(this.mime ? { mimeType: this.mime } : {}),
      videoBitsPerSecond: 8_000_000,
    })
    rec.ondataavailable = (e) => {
      if (!e.data.size) return
      // 一段一段排着队交:前一段没写完,后一段不能先到
      this.queue = this.queue
        .then(() => blobBase64(e.data))
        .then(onChunk)
        .catch((err) => {
          this.failed ??= err
        })
    }
    rec.start(1000)
    this.rec = rec
  }

  /** 停下来,等最后一段也交出去。中途哪一段没交成功就抛出来 */
  async stop(): Promise<void> {
    const rec = this.rec
    if (rec && rec.state !== 'inactive') {
      await new Promise<void>((resolve) => {
        rec.addEventListener('stop', () => resolve(), { once: true })
        rec.stop()
      })
    }
    await this.queue
    if (this.failed) throw this.failed
  }
}

function blobBase64(b: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader()
    r.onload = () => {
      const s = String(r.result)
      resolve(s.slice(s.indexOf(',') + 1))
    }
    r.onerror = () => reject(r.error ?? new Error('读不出数据'))
    r.readAsDataURL(b)
  })
}
