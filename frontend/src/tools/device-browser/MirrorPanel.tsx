import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { ChevronLeft, Circle, Loader2, Power, RotateCcw, Square, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { StartMirror, StopMirror } from '../../../wailsjs/go/main/App'
import type { mirror } from '../../../wailsjs/go/models'
import { MirrorLink, supportsDecoding } from './mirror-link'
import { KEY, fitSize, toVideoPoint, wheelNotches, type ControlEvent } from './mirror-video'

/** 画质档位。面板里的画面也就六七百像素高,「标准」已经够看清字 */
const QUALITIES = [
  { id: 'smooth', label: '流畅', maxSize: 800, bitRate: 2_000_000, maxFps: 30 },
  { id: 'normal', label: '标准', maxSize: 1280, bitRate: 6_000_000, maxFps: 60 },
  { id: 'hd', label: '高清', maxSize: 0, bitRate: 12_000_000, maxFps: 60 },
] as const
type QualityId = (typeof QUALITIES)[number]['id']

type Phase = 'starting' | 'live' | 'ended' | 'error' | 'unsupported'

/** 还不知道手机多宽时先按常见的 9:19.5 占位,画面一到就换成真的 */
const DEFAULT_ASPECT = 9 / 19.5

interface Props {
  /** 真机浏览里实际连上的那台 */
  serial: string
  adbPath: string
  /**
   * 真机浏览这一页是不是正在显示。工具页切走时不卸载、只是藏起来,
   * 不停的话手机会在后台一直编码、电脑一直解码 —— 切走就停,切回来自动重连
   */
  active: boolean
  onClose: () => void
}

/**
 * 投屏面板,停靠在真机浏览的右边:一边在画面上操作手机,一边看左边的文件和监视记录 ——
 * 「拍基线 → 去手机上操作 → 回来看变了什么」这一套不用再在手机和电脑之间来回切。
 *
 * 鼠标:左键点按、拖动 = 手指;右键 = 返回;中键 = 主页;滚轮 = 滑动
 */
export function MirrorPanel({ serial, adbPath, active, onClose }: Props) {
  const [quality, setQuality] = useState<QualityId>('normal')
  // 重连就是让这个数变一下,重新开一路
  const [attempt, setAttempt] = useState(0)
  const [phase, setPhase] = useState<Phase>('starting')
  const [message, setMessage] = useState('')
  const [notice, setNotice] = useState('')
  const [deviceName, setDeviceName] = useState('')
  const [video, setVideo] = useState<{ w: number; h: number } | null>(null)
  const [areaHeight, setAreaHeight] = useState(0)

  const canvasRef = useRef<HTMLCanvasElement>(null)
  const areaRef = useRef<HTMLDivElement>(null)
  const linkRef = useRef<MirrorLink | null>(null)
  const videoRef = useRef(video)
  videoRef.current = video

  useEffect(() => {
    if (!supportsDecoding()) {
      setPhase('unsupported')
      return
    }
    if (!active) return
    let cancelled = false
    let link: MirrorLink | null = null
    setPhase('starting')
    setMessage('')
    setNotice('')
    setVideo(null)
    const q = QUALITIES.find((x) => x.id === quality) ?? QUALITIES[1]
    StartMirror({
      serial,
      adbPath,
      options: { maxSize: q.maxSize, bitRate: q.bitRate, maxFps: q.maxFps },
    } as mirror.StartRequest)
      .then((s) => {
        const canvas = canvasRef.current
        if (cancelled || !canvas) {
          // 等它启动的这一会儿面板已经关了(或者换了画质):开起来的这一路马上收掉
          void StopMirror(s.id).catch(() => {})
          return
        }
        setDeviceName(s.deviceName)
        link = new MirrorLink(s.id, s.url, canvas, {
          onSize: (w, h) => setVideo({ w, h }),
          onLive: () => setPhase('live'),
          onNotice: setNotice,
          onEnded: (reason) => {
            setPhase('ended')
            setMessage(reason)
          },
        })
        linkRef.current = link
      })
      .catch((e) => {
        if (cancelled) return
        setPhase('error')
        setMessage(e instanceof Error ? e.message : String(e))
      })
    return () => {
      cancelled = true
      link?.close()
      linkRef.current = null
    }
  }, [serial, adbPath, quality, attempt, active])

  // 画面区有多高决定了画面多大,面板宽度也就跟着定下来
  useLayoutEffect(() => {
    const el = areaRef.current
    if (!el) return
    const measure = () => setAreaHeight(el.clientHeight)
    measure()
    if (typeof ResizeObserver === 'undefined') {
      window.addEventListener('resize', measure)
      return () => window.removeEventListener('resize', measure)
    }
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const send = useCallback((e: ControlEvent) => linkRef.current?.send(e), [])
  const pressKey = (k: number) => {
    send({ t: 'key', a: 0, k })
    send({ t: 'key', a: 1, k })
  }
  const pressBack = () => {
    send({ t: 'back', a: 0 })
    send({ t: 'back', a: 1 })
  }

  // ---- 鼠标 → 触摸 ----
  // 移动和滚轮一帧只发一次:鼠标一秒能报好几百次位置,全发过去手机端反而跟不上
  const dragging = useRef(false)
  const lastPoint = useRef({ x: 0, y: 0 })
  const pendingMove = useRef(false)
  const wheel = useRef({ hs: 0, vs: 0, x: 0, y: 0 })
  const raf = useRef(0)

  const pointAt = (clientX: number, clientY: number) => {
    const c = canvasRef.current
    const v = videoRef.current
    if (!c || !v) return null
    const p = toVideoPoint(c.getBoundingClientRect(), v, clientX, clientY)
    return { ...p, w: v.w, h: v.h }
  }

  const flush = useCallback(() => {
    raf.current = 0
    const v = videoRef.current
    if (!v) return
    if (pendingMove.current && dragging.current) {
      pendingMove.current = false
      send({ t: 'touch', a: 2, ...lastPoint.current, w: v.w, h: v.h })
    }
    const wh = wheel.current
    if (wh.hs || wh.vs) {
      send({ t: 'scroll', x: wh.x, y: wh.y, w: v.w, h: v.h, hs: wh.hs, vs: wh.vs })
      wheel.current = { hs: 0, vs: 0, x: wh.x, y: wh.y }
    }
  }, [send])

  const schedule = useCallback(() => {
    if (!raf.current) raf.current = requestAnimationFrame(flush)
  }, [flush])

  useEffect(() => () => cancelAnimationFrame(raf.current), [])

  const onPointerDown = (e: React.PointerEvent<HTMLCanvasElement>) => {
    const p = pointAt(e.clientX, e.clientY)
    if (!p) return
    e.preventDefault()
    if (e.button === 0) {
      e.currentTarget.setPointerCapture?.(e.pointerId)
      dragging.current = true
      lastPoint.current = { x: p.x, y: p.y }
      send({ t: 'touch', a: 0, ...p })
    } else if (e.button === 2) {
      send({ t: 'back', a: 0 })
    } else if (e.button === 1) {
      send({ t: 'key', a: 0, k: KEY.HOME })
    }
  }

  const onPointerMove = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (!dragging.current) return
    const p = pointAt(e.clientX, e.clientY)
    if (!p) return
    lastPoint.current = { x: p.x, y: p.y }
    pendingMove.current = true
    schedule()
  }

  const release = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (e.button === 0 || e.type === 'pointercancel') {
      if (!dragging.current) return
      dragging.current = false
      pendingMove.current = false
      // 拖到画面外松手也照样抬起:坐标会被钳在画面边上,手机那头不会留着一根按住的手指
      const p = pointAt(e.clientX, e.clientY)
      if (p) send({ t: 'touch', a: 1, ...p })
    } else if (e.button === 2) {
      send({ t: 'back', a: 1 })
    } else if (e.button === 1) {
      send({ t: 'key', a: 1, k: KEY.HOME })
    }
  }

  // 滚轮得自己挂监听:React 挂的是被动监听,preventDefault 不管用,整个页面会跟着滚
  useEffect(() => {
    const c = canvasRef.current
    if (!c) return
    const onWheel = (e: WheelEvent) => {
      const p = pointAt(e.clientX, e.clientY)
      if (!p) return
      e.preventDefault()
      const n = wheelNotches(e)
      wheel.current = { hs: wheel.current.hs + n.hs, vs: wheel.current.vs + n.vs, x: p.x, y: p.y }
      schedule()
    }
    c.addEventListener('wheel', onWheel, { passive: false })
    return () => c.removeEventListener('wheel', onWheel)
    // pointAt 只读 ref,不用跟着重挂
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [schedule])

  const aspect = video ? video.w / video.h : DEFAULT_ASPECT
  // 横屏时别把左边的文件列表挤没了
  const display = fitSize(areaHeight, aspect, Math.min(720, window.innerWidth * 0.45))
  const panelWidth = Math.max(240, display.width)
  const busy = phase === 'starting'

  return (
    <div
      className="flex min-h-0 flex-col overflow-hidden rounded-lg border border-border bg-card"
      style={{ width: panelWidth }}
    >
      <div className="flex items-center gap-1.5 border-b border-border px-2 py-1.5">
        <span className="min-w-0 flex-1 truncate text-xs font-medium" title={serial}>
          {deviceName || serial || '投屏'}
        </span>
        <select
          value={quality}
          onChange={(e) => setQuality(e.target.value as QualityId)}
          title="画质:越清楚越吃 USB 带宽和电脑的解码"
          className="h-6 rounded-sm border border-input bg-background px-1 text-[11px] outline-none"
        >
          {QUALITIES.map((q) => (
            <option key={q.id} value={q.id}>
              {q.label}
            </option>
          ))}
        </select>
        <button
          onClick={onClose}
          title="关闭投屏"
          className="rounded p-1 text-muted-foreground hover:bg-secondary hover:text-foreground"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </div>

      <div ref={areaRef} className="relative flex min-h-0 flex-1 items-center justify-center bg-black">
        <canvas
          ref={canvasRef}
          data-mirror-canvas=""
          style={{ width: display.width, height: display.height }}
          className={cn('touch-none select-none', phase !== 'live' && 'invisible')}
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={release}
          onPointerCancel={release}
          onContextMenu={(e) => e.preventDefault()}
        />

        {phase !== 'live' && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 p-4 text-center text-xs text-white/80">
            {busy && (
              <>
                <Loader2 className="h-5 w-5 animate-spin" />
                <span>正在启动投屏…</span>
              </>
            )}
            {phase === 'unsupported' && (
              <span>当前环境不支持视频解码（WebCodecs），投屏用不了。更新一下 Edge WebView2 运行时再试</span>
            )}
            {(phase === 'error' || phase === 'ended') && (
              <>
                <span className="whitespace-pre-wrap break-words">{message}</span>
                <Button size="sm" variant="secondary" onClick={() => setAttempt((n) => n + 1)}>
                  <RotateCcw className="h-3.5 w-3.5" />
                  {phase === 'error' ? '重试' : '重新连接'}
                </Button>
              </>
            )}
          </div>
        )}

        {notice && (
          <div className="absolute inset-x-2 top-2 flex items-start gap-1.5 rounded-md bg-amber-100 px-2 py-1.5 text-[11px] text-amber-900 shadow dark:bg-amber-900/90 dark:text-amber-100">
            <span className="flex-1">{notice}</span>
            <button onClick={() => setNotice('')} title="知道了" className="shrink-0 opacity-70 hover:opacity-100">
              <X className="h-3 w-3" />
            </button>
          </div>
        )}
      </div>

      <div className="flex items-center justify-around border-t border-border py-1">
        <NavButton title="返回（也可以在画面上点右键）" onClick={pressBack} disabled={phase !== 'live'}>
          <ChevronLeft className="h-4 w-4" />
        </NavButton>
        <NavButton title="主页（也可以在画面上点中键）" onClick={() => pressKey(KEY.HOME)} disabled={phase !== 'live'}>
          <Circle className="h-3.5 w-3.5" />
        </NavButton>
        <NavButton title="最近任务" onClick={() => pressKey(KEY.APP_SWITCH)} disabled={phase !== 'live'}>
          <Square className="h-3.5 w-3.5" />
        </NavButton>
        <NavButton title="电源键：亮屏 / 锁屏" onClick={() => pressKey(KEY.POWER)} disabled={phase !== 'live'}>
          <Power className="h-3.5 w-3.5" />
        </NavButton>
      </div>
      {/* 取证现场要知道这一步对手机做了什么 */}
      <div className="border-t border-border px-2 py-1 text-[10px] leading-4 text-muted-foreground">
        在画面上点击会真的操作手机；投屏会往手机推一个临时程序，启动后它自己删掉
      </div>
    </div>
  )
}

function NavButton({
  title,
  onClick,
  disabled,
  children,
}: {
  title: string
  onClick: () => void
  disabled: boolean
  children: ReactNode
}) {
  return (
    <button
      title={title}
      aria-label={title}
      onClick={onClick}
      disabled={disabled}
      className="rounded-md px-3 py-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:opacity-40"
    >
      {children}
    </button>
  )
}
