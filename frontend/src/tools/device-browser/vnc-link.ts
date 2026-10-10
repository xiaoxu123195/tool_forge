import type RFB from '@novnc/novnc'

export interface VncCallbacks {
  /** 画面尺寸变了(连上、手机转屏) */
  onSize: (w: number, h: number) => void
  /** 画了一帧:录屏从这里拿画面。给的是 noVNC 那块原尺寸的画布 */
  onFrame?: (src: HTMLCanvasElement) => void
  /** 连上了,开始出画面 */
  onLive: () => void
  /** 手机上复制了东西:TrollVNC 会把手机剪贴板的变化推过来 */
  onClipboard: (text: string) => void
  /** 断开了,不会再有画面 */
  onEnded: (reason: string) => void
}

/** VNC 协议里的鼠标按键:TrollVNC 把右键当 Home、中键当电源键 */
export const BUTTON = { HOME: 4, POWER: 2 } as const

/** VNC 扩展剪贴板(传得了 UTF-8 的那种)里的两个标志:文字格式、「直接给你」 */
const CLIP_TEXT = 1
const CLIP_PROVIDE = 1 << 28
/** 手机那头一条剪贴板最多收 1 MB,超了会直接断开连接。留一点给压缩后多出来的包头 */
const CLIP_MAX = (1 << 20) - 1024

/**
 * 一路 iOS 投屏在界面这一侧的画面:noVNC 的 VNC 客户端,外加一块显示用的画布。
 * 只管画面和输入;手机上的服务什么时候开、什么时候停,归面板管
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
    private readonly view: HTMLCanvasElement,
    private readonly cb: VncCallbacks,
  ) {}

  /** 连上手机。noVNC 用到时才加载,不拖慢别的页面 */
  static async open(
    url: string,
    password: string,
    host: HTMLElement,
    view: HTMLCanvasElement,
    quality: number,
    cb: VncCallbacks,
  ): Promise<VncLink> {
    const { default: Rfb } = await import('@novnc/novnc')
    const link = new VncLink(view, cb)
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

  /**
   * 往手机剪贴板里放字:TrollVNC 收到就写进 iPhone 的剪贴板。
   *
   * 中文要走扩展剪贴板(UTF-8)。noVNC 自己发的时候只会先「通知」、等对方来要,
   * 而 TrollVNC 用的 libvncserver 不理这个通知,noVNC 就退回老格式 Latin-1,汉字全成了「?」。
   * 所以绕开它,自己发一条「直接给你」的消息 —— libvncserver 收这个
   */
  async setClipboard(text: string): Promise<void> {
    const r = this.rfb
    const ws = this.ws
    if (!r || !ws || this.ended || !text) return
    if (!this.takesUtf8()) {
      // 对方没开扩展剪贴板,只能走老格式:西文字母还行,中文发不过去
      if ([...text].some((c) => c.codePointAt(0)! > 0xff)) {
        throw new Error('手机上的 TrollVNC 没开 UTF-8 剪贴板，中文发不过去：点重新连接再试')
      }
      r.clipboardPasteFrom(text)
      return
    }
    const msg = await provideMessage(text)
    if (!this.ended && ws.readyState === WebSocket.OPEN) ws.send(msg)
  }

  /**
   * 对方收不收扩展剪贴板里的文字:连上时它报过自己能做什么,noVNC 记在这两个内部字段里
   * (noVNC 钉死在 1.7.0)。读不到就当不收:对方没开扩展剪贴板时收到这种消息,会把连接断掉
   */
  private takesUtf8(): boolean {
    const caps = this.rfb as unknown as {
      _clipboardServerCapabilitiesFormats?: Record<number, boolean>
      _clipboardServerCapabilitiesActions?: Record<number, boolean>
    }
    return !!(
      caps._clipboardServerCapabilitiesFormats?.[CLIP_TEXT] && caps._clipboardServerCapabilitiesActions?.[CLIP_PROVIDE]
    )
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

  /** noVNC 那块原尺寸的画布:录屏从它上面取画面 */
  source(): HTMLCanvasElement | null {
    return this.src
  }

  /** 画布换了尺寸:马上重画一次 */
  redraw() {
    this.paint()
  }

  /** 断开画面(关面板、窗口藏起来、组件卸载)。不回调 */
  close() {
    if (this.ended) return
    this.ended = true
    cancelAnimationFrame(this.raf)
    this.rfb?.disconnect()
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
    this.cb.onFrame?.(src)
  }

  private end(reason: string) {
    if (this.ended) return
    this.ended = true
    cancelAnimationFrame(this.raf)
    this.cb.onEnded(reason)
  }
}

// ---- 录屏 ----

/** 浏览器能不能录屏:要能从画布取视频流,还要有 MediaRecorder */
export function supportsRecording(): boolean {
  return (
    typeof MediaRecorder !== 'undefined' &&
    typeof HTMLCanvasElement !== 'undefined' &&
    typeof HTMLCanvasElement.prototype.captureStream === 'function'
  )
}

/** 录屏格式:能录 MP4 就录 MP4(Windows 自带的播放器就能放),不行再退到 WebM */
export function recordingType(): { mime: string; ext: '.mp4' | '.webm' } {
  for (const mime of ['video/mp4;codecs=avc1', 'video/mp4']) {
    if (MediaRecorder.isTypeSupported(mime)) return { mime, ext: '.mp4' }
  }
  for (const mime of ['video/webm;codecs=vp9', 'video/webm']) {
    if (MediaRecorder.isTypeSupported(mime)) return { mime, ext: '.webm' }
  }
  return { mime: '', ext: '.webm' }
}

/** 画面不动时多久补一帧:不补的话,不动的那一段在录像里没有帧,时长就短了 */
const HEARTBEAT_MS = 1000

/** 录屏要往后端交的东西 */
export interface RecordingSink {
  /** 接一段数据(base64),按顺序 */
  append: (b64: string) => Promise<void>
  /** 手机转了屏:后端把这个文件收尾,接着往下一个文件里录 */
  nextPart: () => Promise<void>
}

/**
 * 录 iOS 投屏。
 *
 * 不直接录 noVNC 那块画布:手机一转屏它就换尺寸,而一个 MP4 只能有一种画面尺寸,接着录下去文件就坏了。
 * 所以每一帧拷到录像自己的画布上再录,那块的尺寸不变;尺寸一变(转屏)就收掉这一段、另起一个文件 ——
 * 和安卓录屏一样分成几个文件
 */
export class SplitRecorder {
  private part: PartRecorder | null = null
  private last: HTMLCanvasElement | null = null
  /** 换段排着队做:前一段收完尾、后端开好下一个文件,下一段才开始录 */
  private queue: Promise<void> = Promise.resolve()
  private switching = false
  private stopped = false
  private failed: unknown = null
  private readonly timer: number

  constructor(
    private readonly mime: string,
    private readonly sink: RecordingSink,
  ) {
    this.timer = window.setInterval(() => {
      if (this.last) this.frame(this.last)
    }, HEARTBEAT_MS)
  }

  /** noVNC 画了一帧(或者画面不动、到点补一帧) */
  frame(src: HTMLCanvasElement) {
    if (this.stopped || this.failed) return
    this.last = src
    const { width: w, height: h } = src
    if (!w || !h) return
    const cur = this.part
    if (cur && cur.width === w && cur.height === h) {
      cur.draw(src)
      return
    }
    if (this.switching) return
    this.switching = true
    this.queue = this.queue
      .then(async () => {
        if (cur) {
          await cur.stop()
          await this.sink.nextPart()
        }
        if (this.stopped) return
        const next = new PartRecorder(w, h, this.mime)
        next.start(this.sink.append)
        // 新的一段先录上眼下这一帧:转完屏画面可能就不动了
        next.draw(src)
        this.part = next
      })
      .catch((e) => {
        this.failed ??= e
      })
      .finally(() => {
        this.switching = false
      })
  }

  /** 停下来,等最后一段交完。中途哪一段没交成功就抛出来 */
  async stop(): Promise<void> {
    this.stopped = true
    window.clearInterval(this.timer)
    await this.queue
    const part = this.part
    this.part = null
    if (part) {
      try {
        await part.stop()
      } catch (e) {
        this.failed ??= e
      }
    }
    if (this.failed) throw this.failed
  }
}

/** 录一段:自己的一块画布,尺寸固定,每秒交出一段数据(按顺序,base64) */
class PartRecorder {
  private readonly canvas: HTMLCanvasElement
  private readonly ctx: CanvasRenderingContext2D | null
  private rec: MediaRecorder | null = null
  private queue: Promise<void> = Promise.resolve()
  private failed: unknown = null

  constructor(
    readonly width: number,
    readonly height: number,
    private readonly mime: string,
  ) {
    const c = document.createElement('canvas')
    c.width = width
    c.height = height
    // 挂进页面、挪到看不见的地方:脱离页面的画布能不能出帧,各家浏览器不一样
    c.style.cssText = 'position:fixed;left:-100000px;top:0;pointer-events:none'
    c.setAttribute('aria-hidden', 'true')
    document.body.appendChild(c)
    this.canvas = c
    this.ctx = c.getContext('2d')
  }

  draw(src: CanvasImageSource) {
    this.ctx?.drawImage(src, 0, 0)
  }

  start(onChunk: (b64: string) => Promise<void>) {
    const rec = new MediaRecorder(this.canvas.captureStream(30), {
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

  /** 停下来,等最后一段也交出去 */
  async stop(): Promise<void> {
    const rec = this.rec
    if (rec && rec.state !== 'inactive') {
      await new Promise<void>((resolve) => {
        rec.addEventListener('stop', () => resolve(), { once: true })
        rec.stop()
      })
    }
    await this.queue
    this.canvas.remove()
    if (this.failed) throw this.failed
  }
}

/**
 * 扩展剪贴板里「直接给你」的那条消息:类型 6、空三个字节、长度(取负数,表示扩展格式)、标志,
 * 后面跟 zlib 压过的「4 字节长度 + UTF-8 文字」。有两处和协议写的不一样,都是照手机那头来的:
 * - 文字末尾不补 0:TrollVNC 按长度原样收,补的 0 会跟着进 iPhone 剪贴板,粘出来多一个看不见的字符
 * - 换行统一成 \n:协议上写的是 \r\n,可手机那头原样放进剪贴板,而 iOS 用的是 \n
 *
 * text 不能是空的:不补 0 的话,空文字 libvncserver 解不开,会把连接断掉
 */
async function provideMessage(text: string): Promise<Uint8Array> {
  let utf8 = new TextEncoder().encode(text.replace(/\r\n?/g, '\n'))
  // 太长就截掉后面的(安卓也是这样),截在一个字的边上
  if (utf8.length > CLIP_MAX) {
    let n = CLIP_MAX
    while (n > 0 && (utf8[n] & 0xc0) === 0x80) n--
    utf8 = utf8.subarray(0, n)
  }
  const plain = new Uint8Array(4 + utf8.length)
  new DataView(plain.buffer).setUint32(0, utf8.length)
  plain.set(utf8, 4)
  const packed = await zlib(plain)
  const msg = new Uint8Array(12 + packed.length)
  const dv = new DataView(msg.buffer)
  dv.setUint8(0, 6)
  dv.setInt32(4, -(4 + packed.length))
  dv.setUint32(8, CLIP_PROVIDE | CLIP_TEXT)
  msg.set(packed, 12)
  return msg
}

async function zlib(data: Uint8Array<ArrayBuffer>): Promise<Uint8Array> {
  const packed = new Blob([data]).stream().pipeThrough(new CompressionStream('deflate'))
  return new Uint8Array(await new Response(packed).arrayBuffer())
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
