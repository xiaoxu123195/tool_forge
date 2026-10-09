import { StopMirror } from '../../../wailsjs/go/main/App'
import { codecFromConfig, concatBytes, parsePacket, type ControlEvent } from './mirror-video'

export interface MirrorCallbacks {
  /** 画面尺寸变了(开始投屏、手机转屏) */
  onSize: (w: number, h: number) => void
  /** 第一帧画出来了 */
  onLive: () => void
  /** 手机端的提醒,会话照常 */
  onNotice: (text: string) => void
  /**
   * 手机剪贴板的内容到了。code 是 replaced 时,这是第一次粘贴前剪贴板里原来的东西
   * (原来是空的话 text 也是空的)
   */
  onClipboard: (text: string, code: string) => void
  /** 录屏停了(手机拔了、写盘出错):存成了哪几个文件,录了多久 */
  onRecorded: (files: string[], ms: number, error: string) => void
  /** 拖进来的文件处理到哪了;空字符串 = 处理完了 */
  onTransfer: (text: string) => void
  /** 会话结束了,不会再有画面 */
  onEnded: (reason: string) => void
}

/** 浏览器有没有 WebCodecs。没有就解不了视频,投屏无从谈起 */
export function supportsDecoding(): boolean {
  return typeof VideoDecoder !== 'undefined' && typeof EncodedVideoChunk !== 'undefined'
}

/**
 * 一路投屏在界面这一侧的全部:一条 WebSocket、一个解码器、一块画布。
 *
 * 后端把手机发来的包原样转过来(一个包一条二进制消息),这里解析、解码、画出来;
 * 鼠标操作编成 JSON 从同一条连接发回去
 */
export class MirrorLink {
  private ws: WebSocket
  private decoder: VideoDecoder | null = null
  private ctx: CanvasRenderingContext2D | null = null
  /**
   * 最近一帧。留着它,画布换尺寸(拖窗口、进出全屏)时能马上重画,
   * 不用对着一块空白等下一帧。只留一帧,解码器的帧池不会被占满
   */
  private last: VideoFrame | null = null
  /** 最近一个配置包:每个关键帧前面都垫上它,解码器才不挑 */
  private config: Uint8Array | null = null
  /** 新一段编码开始了,下一个配置包到了要重建解码器 */
  private fresh = true
  /** 解码器要从关键帧开始,在那之前的帧都得丢掉 */
  private waitKey = true
  private live = false
  private ended = false
  private failures = 0

  constructor(
    private readonly sessionId: string,
    url: string,
    private readonly canvas: HTMLCanvasElement,
    private readonly cb: MirrorCallbacks,
  ) {
    this.ws = new WebSocket(url)
    this.ws.binaryType = 'arraybuffer'
    this.ws.onmessage = (ev) => this.onMessage(ev)
    this.ws.onclose = () => this.end('投屏断开了')
  }

  send(e: ControlEvent) {
    if (this.ws.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(e))
  }

  /** 主动收掉(关面板、换画质、组件卸载):不回调,顺带让后端停掉手机端程序 */
  close() {
    if (!this.ended) {
      this.ended = true
      this.ws.onclose = null
      this.ws.close()
      this.closeDecoder()
    }
    this.last?.close()
    this.last = null
    void StopMirror(this.sessionId).catch(() => {})
  }

  /** 画布换了尺寸:用最近一帧马上重画一次 */
  redraw() {
    this.paint()
  }

  private onMessage(ev: MessageEvent) {
    if (typeof ev.data === 'string') {
      let n: { type?: string; code?: string; text?: string; files?: string[]; ms?: number }
      try {
        n = JSON.parse(ev.data)
      } catch {
        return
      }
      switch (n.type) {
        case 'ended':
          this.end(n.text || '投屏断开了')
          break
        case 'notice':
          if (n.text) this.cb.onNotice(n.text)
          break
        case 'clipboard':
          this.cb.onClipboard(n.text ?? '', n.code ?? '')
          break
        case 'recorded':
          this.cb.onRecorded(n.files ?? [], n.ms ?? 0, n.text ?? '')
          break
        case 'transfer':
          this.cb.onTransfer(n.text ?? '')
      }
      return
    }
    const p = parsePacket(new Uint8Array(ev.data as ArrayBuffer))
    if (!p) return
    switch (p.kind) {
      case 'session':
        // 画布多大由面板按显示尺寸定,这里只报视频尺寸(坐标换算和长宽比要用)
        this.fresh = true
        this.cb.onSize(p.width, p.height)
        return
      case 'config': {
        this.config = p.data.slice()
        const codec = codecFromConfig(p.data)
        if (!codec) return
        if (this.fresh || !this.decoder || this.decoder.state === 'closed') this.configure(codec)
        this.fresh = false
        this.waitKey = true
        return
      }
      case 'frame':
        this.decode(p.key, p.pts, p.data)
    }
  }

  private configure(codec: string) {
    this.closeDecoder()
    const decoder = new VideoDecoder({
      output: (frame) => this.draw(frame),
      error: (e) => this.onDecodeError(e),
    })
    try {
      decoder.configure({ codec, optimizeForLatency: true })
    } catch (e) {
      this.end(`这台电脑解不了手机发来的视频（${codec}）：${e instanceof Error ? e.message : String(e)}`)
      return
    }
    this.decoder = decoder
  }

  private decode(key: boolean, pts: number, data: Uint8Array) {
    const d = this.decoder
    if (!d || d.state !== 'configured') return
    if (this.waitKey && !key) return
    // 解不过来了(电脑太忙):积压的全丢掉,要一个新的关键帧从头来,
    // 不然延迟会越积越大,点下去半天才有反应
    if (d.decodeQueueSize > 30) {
      this.waitKey = true
      this.send({ t: 'reset' })
      return
    }
    const chunk = key && this.config ? concatBytes(this.config, data) : data
    d.decode(new EncodedVideoChunk({ type: key ? 'key' : 'delta', timestamp: pts, data: chunk }))
    if (key) this.waitKey = false
  }

  private draw(frame: VideoFrame) {
    this.last?.close()
    this.last = frame
    this.paint()
    if (!this.live) {
      this.live = true
      this.failures = 0
      this.cb.onLive()
    }
  }

  /**
   * 画布是按屏幕实际像素建的(见 MirrorPanel),这里把视频缩进去。
   * 缩小一定要用高质量插值:浏览器默认的缩放只是双线性,
   * 缩到一半以下时细字的笔画会被跳过,看上去就是发虚
   */
  private paint() {
    const f = this.last
    if (!f) return
    this.ctx ??= this.canvas.getContext('2d')
    const ctx = this.ctx
    if (!ctx) return
    // 画布一改尺寸,这两项就会被重置,所以每次都设
    ctx.imageSmoothingEnabled = true
    ctx.imageSmoothingQuality = 'high'
    ctx.drawImage(f, 0, 0, this.canvas.width, this.canvas.height)
  }

  // 解码器出错后就关掉了:换一个新的,让手机端从关键帧重来。连着错几次就别硬撑了
  private onDecodeError(e: DOMException) {
    if (this.ended) return
    this.failures++
    if (this.failures > 3) {
      this.end('画面解码一直出错：' + e.message)
      return
    }
    this.decoder = null
    this.fresh = true
    this.send({ t: 'reset' })
  }

  private end(reason: string) {
    if (this.ended) return
    this.ended = true
    this.ws.onclose = null
    this.ws.close()
    this.closeDecoder()
    this.cb.onEnded(reason)
  }

  private closeDecoder() {
    if (this.decoder && this.decoder.state !== 'closed') this.decoder.close()
    this.decoder = null
  }
}
