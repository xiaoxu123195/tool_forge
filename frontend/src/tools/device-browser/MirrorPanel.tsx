import { useCallback, useEffect, useLayoutEffect, useRef, useState, type HTMLAttributes, type ReactNode } from 'react'
import {
  Camera,
  ChevronLeft,
  Circle,
  CircleHelp,
  ClipboardList,
  Copy,
  FolderOpen,
  Loader2,
  Maximize2,
  Minimize2,
  Moon,
  PanelTopOpen,
  Power,
  RotateCcw,
  Square,
  Sun,
  Video,
  Volume1,
  Volume2,
  X,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { useNativeFileDrop } from '@/lib/useNativeFileDrop'
import { useDeviceBrowserStore } from '@/stores/device-browser'
import {
  CopyImageFile,
  MirrorDrop,
  MirrorScreenshot,
  MirrorStartRecording,
  MirrorStopRecording,
  PickDirectory,
  RevealInExplorer,
  StartMirror,
  StopMirror,
} from '../../../wailsjs/go/main/App'
import type { mirror } from '../../../wailsjs/go/models'
import {
  ClipboardGetText,
  ClipboardSetText,
  WindowFullscreen,
  WindowIsMinimised,
  WindowUnfullscreen,
} from '../../../wailsjs/runtime/runtime'
import { MirrorLink, supportsDecoding } from './mirror-link'
import { KEY, fitSize, toVideoPoint, wheelNotches, type ControlEvent } from './mirror-video'
import { keyAction, pinchFactor, pinchFingers, typeable } from './mirror-input'

/**
 * 画质档位。要看清字,宁可传大一点、在电脑这边高质量缩小,也不要让手机先缩一遍 ——
 * 手机缩、电脑再缩,细字就被插值插糊了
 */
const QUALITIES = [
  { id: 'smooth', label: '流畅', maxSize: 1280, bitRate: 4_000_000, maxFps: 30, note: '长边 1280、30 帧，老电脑或者线不好时用' },
  { id: 'normal', label: '标准', maxSize: 1920, bitRate: 8_000_000, maxFps: 60, note: '长边 1920、60 帧' },
  { id: 'full', label: '原画', maxSize: 0, bitRate: 16_000_000, maxFps: 60, note: '手机原始分辨率，字最清楚，最吃 USB 和电脑' },
] as const
type QualityId = (typeof QUALITIES)[number]['id']

type Phase = 'starting' | 'live' | 'ended' | 'error' | 'unsupported'

/** 还不知道手机多宽时先按常见的 9:19.5 占位,画面一到就换成真的 */
const DEFAULT_ASPECT = 9 / 19.5
/** 右侧按钮栏的宽度 */
const RAIL = 36
/** 停靠时横屏画面最多占窗口宽度的多少。横屏是扁的,宽度给少了就只剩一条缝 */
const LANDSCAPE_SHARE = 0.62
/** 读手机剪贴板最多等多久:剪贴板是空的时候手机不回话 */
const CLIP_WAIT = 2000
/** 安卓 7(API 24)起才有复制、粘贴键 */
const SDK_PASTE = 24

/**
 * 提示条多久后自己消失(毫秒):顺口一提的提醒、成功、失败。失败的留久一点,要看清是哪个文件、为什么。
 * 「手机拒绝了模拟点击」这种不处理就用不了的不在这里,一直留着等人关。
 * 导出是给冒烟测试调短用的,省得真等几秒
 */
export const DISMISS_MS = { hint: 6000, ok: 5000, error: 15000 }

const HELP = [
  '左键：点按、拖动，和手指一样',
  '右键：返回　　中键：主页',
  '滚轮：上下滑　　Ctrl+滚轮：双指缩放',
  '点一下画面就能用键盘打字，中文经手机剪贴板粘贴过去',
  'Ctrl+C / Ctrl+X：手机上选中的字复制到电脑',
  'Ctrl+V：电脑剪贴板里的字粘到手机上',
  '把文件拖到画面上：APK 直接安装，其它文件推到手机的 Download 文件夹',
].join('\n')

/** 面板底部的提示条 */
interface Toast {
  text: string
  /** 失败的标红,留得久一点 */
  error?: boolean
  /** 存下来的文件:给「打开所在文件夹」 */
  path?: string
  /** 存的是图片:多给一个「复制图片」 */
  image?: boolean
  /** 保存的文件夹出了问题:给一个「换文件夹」 */
  pickDir?: boolean
}

interface Clip {
  open: boolean
  loading: boolean
  /** 最近一次读到的;null = 还没读过,'' = 空的或者读不到 */
  text: string | null
  /** 第一次粘贴前手机剪贴板里原来的东西;null = 还没粘贴过 */
  before: string | null
}

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
 * 投屏面板,停靠在真机浏览的最右边:一边在画面上操作手机,一边看左边的文件和监视记录 ——
 * 「拍基线 → 去手机上操作 → 回来看变了什么」这一套不用再在手机和电脑之间来回切。
 *
 * 手机是竖长条,面板多宽由多高决定:所以面板从上到下占满,按钮都挪到右侧一条竖栏里,
 * 高度全留给画面。还嫌小就全屏。操作方式见 HELP
 */
export function MirrorPanel({ serial, adbPath, active, onClose }: Props) {
  const keepAwake = useDeviceBrowserStore((s) => s.mirrorKeepAwake)
  const setKeepAwake = useDeviceBrowserStore((s) => s.setMirrorKeepAwake)
  const captureDir = useDeviceBrowserStore((s) => s.captureDir)
  const setCaptureDir = useDeviceBrowserStore((s) => s.setCaptureDir)

  const [quality, setQuality] = useState<QualityId>('normal')
  // 重连就是让这个数变一下,重新开一路
  const [attempt, setAttempt] = useState(0)
  const [phase, setPhase] = useState<Phase>('starting')
  const [message, setMessage] = useState('')
  // 手机端发来的、不处理就用不了的提醒(比如不让模拟点击):一直留着,等人关
  const [warning, setWarning] = useState('')
  // 顺口一提的提醒(中文经剪贴板粘贴、操作说明):几秒后自己消失
  const [hint, setHint] = useState('')
  const [video, setVideo] = useState<{ w: number; h: number } | null>(null)
  const [area, setArea] = useState({ w: 0, h: 0 })
  const [fullscreen, setFullscreen] = useState(false)
  // 手机的安卓 API 级别,读不到时是 0
  const [sdk, setSdk] = useState(0)
  const [toast, setToast] = useState<Toast | null>(null)
  const [recording, setRecording] = useState<{ since: number } | null>(null)
  const [, setTick] = useState(0)
  const [shooting, setShooting] = useState(false)
  const [dropping, setDropping] = useState(false)
  const [transfer, setTransfer] = useState('')
  const [clip, setClip] = useState<Clip>({ open: false, loading: false, text: null, before: null })
  const [kbdFocus, setKbdFocus] = useState(false)
  // 输入框跟着点的位置走:中文输入法的候选框会出现在这儿,而不是窗口角落里
  const [kbdAt, setKbdAt] = useState({ x: 0, y: 0 })

  const canvasRef = useRef<HTMLCanvasElement>(null)
  const areaRef = useRef<HTMLDivElement>(null)
  const kbdRef = useRef<HTMLTextAreaElement>(null)
  const linkRef = useRef<MirrorLink | null>(null)
  const sessionRef = useRef('')
  const videoRef = useRef(video)
  videoRef.current = video
  const composing = useRef(false)
  // 按了 Ctrl+C 的时间:这之后回来的剪贴板内容要放进电脑剪贴板
  const pendingCopy = useRef(0)
  const pastedOnce = useRef(false)
  const clipTimer = useRef(0)

  const windowShown = useWindowShown(active)
  const dpr = useDevicePixelRatio()
  // 录着屏就不停:切到别的工具、最小化窗口,录像都得接着录
  const running = (active && windowShown) || recording !== null

  // ---- 剪贴板、录屏的回信(从投屏连接上来) ----
  const onClipboard = (text: string, code: string) => {
    if (code === 'replaced') {
      // 第一次粘贴前剪贴板里原来的东西:留着,在「手机剪贴板」里看得到
      setClip((c) => ({ ...c, before: text }))
      setHint(
        text
          ? '中文是经手机剪贴板粘贴的：手机剪贴板里原来的内容已被替换，原内容在右边「手机剪贴板」里能看到'
          : '中文是经手机剪贴板粘贴的：手机上的应用都读得到剪贴板里的字',
      )
      return
    }
    window.clearTimeout(clipTimer.current)
    setClip((c) => ({ ...c, loading: false, text }))
    if (Date.now() - pendingCopy.current < CLIP_WAIT + 1000) {
      pendingCopy.current = 0
      void ClipboardSetText(text)
      setToast({ text: `已复制到电脑：${preview(text)}` })
    }
  }

  const onRecorded = (files: string[], ms: number, error: string) => {
    setRecording(null)
    if (files.length === 0) {
      setToast({ text: error || '没录到画面：还没等到第一帧就停了', error: true })
      return
    }
    const parts = files.length > 1 ? `，中间转过屏，分成了 ${files.length} 个文件` : ''
    setToast({
      text: `录屏已保存（${fmtDuration(ms)}${parts}）${error ? '。中途出错：' + error : ''}`,
      path: files[0],
      error: !!error,
    })
  }
  // 连接上的回调在开连接时就定下了,经 ref 转一道,用的永远是最新的那份
  const onClipboardRef = useRef(onClipboard)
  onClipboardRef.current = onClipboard
  const onRecordedRef = useRef(onRecorded)
  onRecordedRef.current = onRecorded

  useEffect(() => {
    if (!supportsDecoding()) {
      setPhase('unsupported')
      return
    }
    if (!running) return
    let cancelled = false
    let link: MirrorLink | null = null
    setPhase('starting')
    setMessage('')
    setWarning('')
    setHint('')
    setVideo(null)
    setTransfer('')
    const q = QUALITIES.find((x) => x.id === quality) ?? QUALITIES[1]
    StartMirror({
      serial,
      adbPath,
      options: { maxSize: q.maxSize, bitRate: q.bitRate, maxFps: q.maxFps, keepAwake },
    } as mirror.StartRequest)
      .then((s) => {
        const canvas = canvasRef.current
        if (cancelled || !canvas) {
          // 等它启动的这一会儿面板已经关了(或者换了画质):开起来的这一路马上收掉
          void StopMirror(s.id).catch(() => {})
          return
        }
        sessionRef.current = s.id
        setSdk(s.sdk ?? 0)
        pastedOnce.current = false
        link = new MirrorLink(s.id, s.url, canvas, {
          onSize: (w, h) => setVideo({ w, h }),
          onLive: () => setPhase('live'),
          onNotice: setWarning,
          onClipboard: (text, code) => onClipboardRef.current(text, code),
          onRecorded: (files, ms, error) => onRecordedRef.current(files, ms, error),
          onTransfer: setTransfer,
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
      sessionRef.current = ''
    }
  }, [serial, adbPath, quality, keepAwake, attempt, running])

  // 画面区有多大决定了画面多大。停靠时只看高度(面板宽度反过来由画面定),全屏时宽高都看
  useLayoutEffect(() => {
    const el = areaRef.current
    if (!el) return
    const measure = () => setArea({ w: el.clientWidth, h: el.clientHeight })
    measure()
    window.addEventListener('resize', measure)
    const ro = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure)
    ro?.observe(el)
    return () => {
      window.removeEventListener('resize', measure)
      ro?.disconnect()
    }
  }, [])

  // ---- 全屏 ----
  // 画面盖住整个窗口,窗口也进系统全屏:竖屏手机能用上整块屏幕的高度
  const enterFullscreen = () => {
    setFullscreen(true)
    void WindowFullscreen()
  }
  const exitFullscreen = useCallback(() => {
    setFullscreen(false)
    void WindowUnfullscreen()
  }, [])
  useEffect(() => {
    if (!fullscreen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') exitFullscreen()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [fullscreen, exitFullscreen])
  // 切到别的工具(比如按了全局热键)、或者面板被关掉:不能把窗口留在全屏里
  useEffect(() => {
    if (fullscreen && !active) exitFullscreen()
  }, [fullscreen, active, exitFullscreen])
  const fullscreenRef = useRef(fullscreen)
  fullscreenRef.current = fullscreen
  useEffect(
    () => () => {
      if (fullscreenRef.current) void WindowUnfullscreen()
    },
    [],
  )

  // ---- 尺寸 ----
  const aspect = video ? video.w / video.h : DEFAULT_ASPECT
  const landscape = aspect > 1
  // 停靠时竖屏不超过 720、不超过窗口的 45%,别把左边的文件列表挤没了;
  // 横屏是扁的,按这个上限只剩巴掌大,放宽到窗口的六成
  const maxWidth = fullscreen
    ? area.w
    : landscape
      ? window.innerWidth * LANDSCAPE_SHARE
      : Math.min(720, window.innerWidth * 0.45)
  const display = fitSize(maxWidth, area.h, aspect)

  // 画布按屏幕实际像素建(CSS 尺寸 × 缩放比),缩小交给高质量插值去做。
  // 要是画布就用视频尺寸、让浏览器按 CSS 去缩,走的是最粗的那种缩放,字会发虚。
  // 缩放比也得跟着变:窗口拖到另一块缩放比例不同的显示器上,CSS 尺寸不变,像素变了
  useLayoutEffect(() => {
    const c = canvasRef.current
    if (!c) return
    const w = Math.max(1, Math.round(display.width * dpr))
    const h = Math.max(1, Math.round(display.height * dpr))
    if (c.width !== w || c.height !== h) {
      c.width = w
      c.height = h
      linkRef.current?.redraw()
    }
  }, [display.width, display.height, dpr])

  // ---- 按键 ----
  const send = useCallback((e: ControlEvent) => linkRef.current?.send(e), [])
  const pressKey = (k: number, m = 0) => {
    send({ t: 'key', a: 0, k, m })
    send({ t: 'key', a: 1, k, m })
  }
  const pressBack = () => {
    send({ t: 'back', a: 0 })
    send({ t: 'back', a: 1 })
  }

  // ---- 键盘、剪贴板 ----
  const sendPaste = (s: string) => {
    if (!pastedOnce.current) {
      pastedOnce.current = true
      if (sdk > 0 && sdk < SDK_PASTE) {
        setHint('这台手机是安卓 7 以下，没有粘贴键：字已经放进手机剪贴板，在输入框上长按选「粘贴」')
      }
    }
    send({ t: 'paste', s })
  }
  // 键盘上有的字符模拟按键打过去;中文、表情手机端打不出来,只能经剪贴板粘贴
  const sendText = (s: string) => {
    if (!s) return
    if (typeable(s)) send({ t: 'text', s })
    else sendPaste(s)
  }
  const pasteFromPC = async () => {
    const s = await ClipboardGetText().catch(() => '')
    if (!s) {
      setToast({ text: '电脑剪贴板里没有文字' })
      return
    }
    sendPaste(s)
  }
  const copyFromPhone = (cut: boolean) => {
    if (sdk > 0 && sdk < SDK_PASTE) {
      setToast({ text: '安卓 7 以下没有复制键：先在手机上长按选字复制，再点右边的「手机剪贴板」', error: true })
      return
    }
    pendingCopy.current = Date.now()
    send({ t: 'getclip', a: cut ? 2 : 1 })
  }
  const readClipboard = () => {
    setClip((c) => ({ ...c, open: true, loading: true }))
    send({ t: 'getclip', a: 0 })
    window.clearTimeout(clipTimer.current)
    clipTimer.current = window.setTimeout(
      () => setClip((c) => (c.loading ? { ...c, loading: false, text: '' } : c)),
      CLIP_WAIT,
    )
  }
  useEffect(() => () => window.clearTimeout(clipTimer.current), [])

  const flushTyped = () => {
    const ta = kbdRef.current
    if (!ta || !ta.value) return
    const s = ta.value
    ta.value = ''
    sendText(s)
  }
  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // 输入法正在拼字:按键归输入法,等它上屏再发
    if (e.nativeEvent.isComposing || e.keyCode === 229) return
    // 全屏时 Esc 是退出全屏(窗口上那个监听管),不发给手机
    if (e.key === 'Escape' && fullscreenRef.current) return
    const act = keyAction(e)
    if (!act) {
      // 没认领的 Ctrl 组合键不发给手机,也不让这个看不见的输入框自己处理(Ctrl+Z 之类);
      // 照样往上冒,工具箱自己的快捷键(Ctrl+K)还能用
      if (e.ctrlKey || e.metaKey) e.preventDefault()
      return
    }
    e.preventDefault()
    e.stopPropagation()
    if (act.kind === 'key') pressKey(act.code, act.meta)
    else if (act.kind === 'paste') void pasteFromPC()
    else copyFromPhone(act.cut)
  }

  // ---- 鼠标 → 触摸 ----
  // 移动和滚轮一帧只发一次:鼠标一秒能报好几百次位置,全发过去手机端反而跟不上
  const held = useRef({ left: false, right: false, middle: false })
  const lastPoint = useRef({ x: 0, y: 0 })
  const pendingMove = useRef(false)
  const wheel = useRef({ hs: 0, vs: 0, x: 0, y: 0 })
  const raf = useRef(0)
  const pinch = useRef<{ center: { x: number; y: number }; half: number; size: { w: number; h: number }; timer: number } | null>(
    null,
  )

  const pointAt = useCallback((clientX: number, clientY: number) => {
    const c = canvasRef.current
    const v = videoRef.current
    if (!c || !v) return null
    const p = toVideoPoint(c.getBoundingClientRect(), v, clientX, clientY)
    return { ...p, w: v.w, h: v.h }
  }, [])

  const flush = useCallback(() => {
    raf.current = 0
    const v = videoRef.current
    if (!v) return
    if (pendingMove.current && held.current.left) {
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

  // 左键抬起。拖到画面外松手也照样抬起:坐标会被钳在画面边上,手机那头不会留着一根按住的手指
  const releaseLeft = useCallback(
    (clientX?: number, clientY?: number) => {
      if (!held.current.left) return
      held.current.left = false
      pendingMove.current = false
      const v = videoRef.current
      const p = clientX === undefined || clientY === undefined ? null : pointAt(clientX, clientY)
      if (p) send({ t: 'touch', a: 1, ...p })
      else if (v) send({ t: 'touch', a: 1, ...lastPoint.current, w: v.w, h: v.h })
    },
    [pointAt, send],
  )
  // 所有按着的键都抬起来。右键是返回、中键是主页 —— 主页键一直按着,手机会当成长按,把语音助手叫出来
  const releaseAll = useCallback(
    (clientX?: number, clientY?: number) => {
      releaseLeft(clientX, clientY)
      if (held.current.right) {
        held.current.right = false
        send({ t: 'back', a: 1 })
      }
      if (held.current.middle) {
        held.current.middle = false
        send({ t: 'key', a: 1, k: KEY.HOME })
      }
    },
    [releaseLeft, send],
  )

  const endPinch = useCallback(() => {
    const st = pinch.current
    if (!st) return
    pinch.current = null
    window.clearTimeout(st.timer)
    const [a, b] = pinchFingers(st.center, st.half, st.size)
    send({ t: 'touch', a: 1, p: 2, ...b, ...sizeOf(st.size) })
    send({ t: 'touch', a: 1, p: 1, ...a, ...sizeOf(st.size) })
  }, [send])

  // Ctrl+滚轮 = 双指缩放:以鼠标位置为中心放两根手指,滚轮转一下张开或合拢一点,停了就抬起
  const pinchBy = useCallback(
    (p: { x: number; y: number }, deltaY: number) => {
      const v = videoRef.current
      if (!v || held.current.left) return
      const short = Math.min(v.w, v.h)
      let st = pinch.current
      if (!st) {
        st = { center: { x: p.x, y: p.y }, half: short * 0.08, size: { w: v.w, h: v.h }, timer: 0 }
        pinch.current = st
        const [a, b] = pinchFingers(st.center, st.half, st.size)
        send({ t: 'touch', a: 0, p: 1, ...a, ...sizeOf(st.size) })
        send({ t: 'touch', a: 0, p: 2, ...b, ...sizeOf(st.size) })
      }
      const from = st.half
      const to = Math.max(short * 0.02, Math.min(short * 0.45, from * pinchFactor(deltaY)))
      // 分几步挪过去:一下子跳太远,有的应用认不出这是在捏合
      for (let i = 1; i <= 3; i++) {
        st.half = from + ((to - from) * i) / 3
        const [a, b] = pinchFingers(st.center, st.half, st.size)
        send({ t: 'touch', a: 2, p: 1, ...a, ...sizeOf(st.size) })
        send({ t: 'touch', a: 2, p: 2, ...b, ...sizeOf(st.size) })
      }
      window.clearTimeout(st.timer)
      st.timer = window.setTimeout(endPinch, 300)
    },
    [send, endPinch],
  )

  // 窗口失去焦点(Alt+Tab 切走)时按着的键收不到抬起:先替它抬起来
  useEffect(() => {
    const onBlur = () => {
      releaseAll()
      endPinch()
    }
    window.addEventListener('blur', onBlur)
    return () => window.removeEventListener('blur', onBlur)
  }, [releaseAll, endPinch])

  const focusKeyboard = (clientX: number, clientY: number) => {
    const box = areaRef.current?.getBoundingClientRect()
    if (box) setKbdAt({ x: clientX - box.left, y: clientY - box.top })
    kbdRef.current?.focus({ preventScroll: true })
  }

  const onPointerDown = (e: React.PointerEvent<HTMLCanvasElement>) => {
    const p = pointAt(e.clientX, e.clientY)
    if (!p) return
    e.preventDefault()
    focusKeyboard(e.clientX, e.clientY)
    endPinch()
    // 哪个键按下都抓住指针:拖到画面外再松开,抬起照样回到这里
    e.currentTarget.setPointerCapture?.(e.pointerId)
    if (e.button === 0) {
      held.current.left = true
      lastPoint.current = { x: p.x, y: p.y }
      send({ t: 'touch', a: 0, ...p })
    } else if (e.button === 2) {
      held.current.right = true
      send({ t: 'back', a: 0 })
    } else if (e.button === 1) {
      held.current.middle = true
      send({ t: 'key', a: 0, k: KEY.HOME })
    }
  }

  const onPointerMove = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (!held.current.left) return
    // 左键在别的键还按着时松开:浏览器不报抬起,只能从 buttons 看出来
    if (!(e.buttons & 1)) {
      releaseLeft(e.clientX, e.clientY)
      return
    }
    const p = pointAt(e.clientX, e.clientY)
    if (!p) return
    lastPoint.current = { x: p.x, y: p.y }
    pendingMove.current = true
    schedule()
  }

  // 滚轮得自己挂监听:React 挂的是被动监听,preventDefault 不管用,整个页面会跟着滚
  useEffect(() => {
    const c = canvasRef.current
    if (!c) return
    const onWheel = (e: WheelEvent) => {
      const p = pointAt(e.clientX, e.clientY)
      if (!p) return
      e.preventDefault()
      if (e.ctrlKey) {
        pinchBy(p, e.deltaY)
        return
      }
      const n = wheelNotches(e)
      wheel.current = { hs: wheel.current.hs + n.hs, vs: wheel.current.vs + n.vs, x: p.x, y: p.y }
      schedule()
    }
    c.addEventListener('wheel', onWheel, { passive: false })
    return () => c.removeEventListener('wheel', onWheel)
  }, [pointAt, pinchBy, schedule])

  // 转屏了:正在捏的两根手指按旧尺寸算的,收掉
  useEffect(() => endPinch, [video?.w, video?.h, endPinch])

  // ---- 截图、录屏 ----
  const pickDir = async () => {
    const dir = await PickDirectory('截图、录屏存到哪个文件夹', captureDir).catch(() => '')
    if (dir) setCaptureDir(dir)
    return dir
  }
  const failed = (e: unknown) => {
    const text = e instanceof Error ? e.message : String(e)
    // 文件夹被删了、U 盘拔了:忘掉它,下次重新问
    if (text.includes('保存的文件夹不在了')) setCaptureDir('')
    setToast({ text, error: true, pickDir: text.includes('保存的文件夹') })
  }
  const screenshot = async () => {
    const id = sessionRef.current
    if (!id || shooting) return
    const dir = captureDir || (await pickDir())
    if (!dir) return
    setShooting(true)
    try {
      const s = await MirrorScreenshot(id, dir)
      setToast({ text: `截图已保存（${s.width}×${s.height}）`, path: s.path, image: true })
    } catch (e) {
      failed(e)
    } finally {
      setShooting(false)
    }
  }
  const toggleRecording = async () => {
    const id = sessionRef.current
    if (!id) return
    if (recording) {
      try {
        const r = await MirrorStopRecording(id)
        onRecorded(r.files ?? [], r.durationMs, r.error ?? '')
      } catch (e) {
        failed(e)
      } finally {
        setRecording(null)
      }
      return
    }
    const dir = captureDir || (await pickDir())
    if (!dir) return
    try {
      await MirrorStartRecording(id, dir)
      setRecording({ since: Date.now() })
    } catch (e) {
      failed(e)
    }
  }
  // 录着的时候每秒刷一下,按钮上的时长才会走
  useEffect(() => {
    if (!recording) return
    const t = window.setInterval(() => setTick((n) => n + 1), 1000)
    return () => window.clearInterval(t)
  }, [recording])

  // ---- 拖文件进来 ----
  const dropRef = useNativeFileDrop<HTMLDivElement>((paths) => {
    void dropFiles(paths)
  })
  const dropFiles = async (paths: string[]) => {
    const id = sessionRef.current
    if (!id || phase !== 'live') {
      setToast({ text: '投屏连上之后才能拖文件进来', error: true })
      return
    }
    if (dropping) {
      setToast({ text: '上一批文件还没处理完，等它弄完再拖', error: true })
      return
    }
    setDropping(true)
    setTransfer('正在准备…')
    try {
      setToast(summarizeDrop((await MirrorDrop(id, paths)) ?? []))
    } catch (e) {
      setToast({ text: e instanceof Error ? e.message : String(e), error: true })
    } finally {
      setDropping(false)
      setTransfer('')
    }
  }

  // 每来一条新提示(哪怕字一样,比如连着复制两次)都重新计时:给它一个新的 key
  const toastSeq = useRef(0)
  const lastToast = useRef<Toast | null>(null)
  if (toast !== lastToast.current) {
    lastToast.current = toast
    toastSeq.current++
  }

  const q = QUALITIES.find((x) => x.id === quality) ?? QUALITIES[1]
  const nextQuality = QUALITIES[(QUALITIES.indexOf(q) + 1) % QUALITIES.length]
  const live = phase === 'live'
  // 换画质、开关保持亮屏都要重开一路,正在录的屏会断成两截
  const restartLocked = recording !== null

  return (
    <div
      ref={dropRef}
      data-mirror-panel=""
      className={cn(
        'group/drop flex min-h-0 overflow-hidden',
        fullscreen ? 'fixed inset-0 z-[300] bg-black' : 'shrink-0 rounded-lg border border-border bg-black',
      )}
      // 停靠时的宽度 = 画面宽 + 按钮栏 + 两条边框
      style={{
        ...(fullscreen ? {} : { width: Math.max(240, display.width) + RAIL + 2 }),
        ['--wails-drop-target' as never]: 'drop',
      }}
    >
      <div ref={areaRef} className="relative flex min-h-0 min-w-0 flex-1 items-center justify-center">
        <canvas
          ref={canvasRef}
          data-mirror-canvas=""
          style={{ width: display.width, height: display.height }}
          className={cn(
            'touch-none select-none',
            !live && 'invisible',
            kbdFocus && live && 'outline outline-2 outline-sky-400/80',
          )}
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={(e) => releaseAll(e.clientX, e.clientY)}
          onPointerCancel={(e) => releaseAll(e.clientX, e.clientY)}
          onLostPointerCapture={() => releaseAll()}
          onContextMenu={(e) => e.preventDefault()}
        />
        {/* 键盘输入:看不见的输入框,点画面时拿到焦点。输入法的拼字、上屏都在它身上发生 */}
        <textarea
          ref={kbdRef}
          data-mirror-keyboard=""
          aria-label="键盘输入到手机"
          tabIndex={-1}
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="off"
          spellCheck={false}
          className="pointer-events-none absolute h-4 w-px resize-none overflow-hidden border-0 p-0 opacity-0"
          style={{ left: kbdAt.x, top: kbdAt.y }}
          onKeyDown={onKeyDown}
          onInput={() => {
            if (!composing.current) flushTyped()
          }}
          onCompositionStart={() => {
            composing.current = true
          }}
          onCompositionEnd={() => {
            composing.current = false
            flushTyped()
          }}
          onFocus={() => setKbdFocus(true)}
          onBlur={() => setKbdFocus(false)}
        />

        {!live && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 p-4 text-center text-xs text-white/80">
            {phase === 'starting' && (
              <>
                <Loader2 className="h-5 w-5 animate-spin" />
                <span>正在启动投屏…</span>
                {/* 取证现场要知道这一步对手机做了什么 */}
                <span className="text-[11px] leading-5 text-white/50">
                  会往手机推一个临时程序，启动后它自己删掉；在画面上点击会真的操作手机
                </span>
                <span className="whitespace-pre-line text-[11px] leading-5 text-white/40">{HELP}</span>
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

        <div className="pointer-events-none absolute inset-x-2 top-2 flex flex-col items-end gap-1.5">
          {warning && (
            <div
              data-mirror-warning=""
              className="pointer-events-auto flex w-full items-start gap-1.5 rounded-md bg-amber-100 px-2 py-1.5 text-[11px] text-amber-900 shadow dark:bg-amber-900/90 dark:text-amber-100"
            >
              <span className="flex-1 whitespace-pre-line">{warning}</span>
              <button onClick={() => setWarning('')} title="知道了" className="shrink-0 opacity-70 hover:opacity-100">
                <X className="h-3 w-3" />
              </button>
            </div>
          )}
          {hint && (
            // 换一条提醒就换一个计时:key 跟着内容走
            <AutoDismiss
              key={hint}
              ms={DISMISS_MS.hint}
              onDismiss={() => setHint('')}
              data-mirror-hint=""
              className="pointer-events-auto flex w-full items-start gap-1.5 rounded-md bg-sky-100 px-2 py-1.5 text-[11px] text-sky-900 shadow dark:bg-sky-900/90 dark:text-sky-100"
            >
              <span className="flex-1 whitespace-pre-line">{hint}</span>
              <button onClick={() => setHint('')} title="知道了" className="shrink-0 opacity-70 hover:opacity-100">
                <X className="h-3 w-3" />
              </button>
            </AutoDismiss>
          )}
          {/* 横过来的画面停靠着看还是小:给一个一键全屏 */}
          {live && landscape && !fullscreen && (
            <button
              onClick={enterFullscreen}
              className="pointer-events-auto flex items-center gap-1 rounded-md bg-black/60 px-2 py-1 text-[11px] text-white/90 hover:bg-black/80"
            >
              <Maximize2 className="h-3 w-3" />
              横屏了，全屏看更大
            </button>
          )}
          {clip.open && (
            <ClipboardCard
              clip={clip}
              onCopy={(text) => {
                void ClipboardSetText(text)
                setToast({ text: `已复制到电脑：${preview(text)}` })
              }}
              onReload={readClipboard}
              onClose={() => setClip((c) => ({ ...c, open: false }))}
            />
          )}
        </div>

        <div className="pointer-events-none absolute inset-x-2 bottom-2 flex flex-col gap-1.5">
          {transfer && (
            <div className="flex items-center gap-1.5 rounded-md bg-black/70 px-2 py-1.5 text-[11px] text-white/90">
              <Loader2 className="h-3 w-3 shrink-0 animate-spin" />
              <span className="min-w-0 flex-1 break-words">{transfer}</span>
            </div>
          )}
          {toast && (
            <AutoDismiss
              key={toastSeq.current}
              ms={toast.error ? DISMISS_MS.error : DISMISS_MS.ok}
              onDismiss={() => setToast(null)}
              data-mirror-toast=""
              className={cn(
                'pointer-events-auto rounded-md px-2 py-1.5 text-[11px] shadow',
                toast.error
                  ? 'bg-destructive/90 text-destructive-foreground'
                  : 'bg-emerald-100 text-emerald-900 dark:bg-emerald-900/90 dark:text-emerald-100',
              )}
            >
              <div className="flex items-start gap-1.5">
                <span className="min-w-0 flex-1 whitespace-pre-wrap break-words">{toast.text}</span>
                <button onClick={() => setToast(null)} title="知道了" className="shrink-0 opacity-70 hover:opacity-100">
                  <X className="h-3 w-3" />
                </button>
              </div>
              {(toast.path || toast.pickDir) && (
                <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1">
                  {toast.path && (
                    <ToastAction onClick={() => void RevealInExplorer(toast.path!)}>
                      <FolderOpen className="h-3 w-3" />
                      打开所在文件夹
                    </ToastAction>
                  )}
                  {toast.path && toast.image && (
                    <ToastAction
                      onClick={() =>
                        void CopyImageFile(toast.path!)
                          .then(() => setToast({ text: '截图已复制，可以直接粘到文档、聊天里' }))
                          .catch(failed)
                      }
                    >
                      <Copy className="h-3 w-3" />
                      复制图片
                    </ToastAction>
                  )}
                  <ToastAction onClick={() => void pickDir()}>换保存的文件夹</ToastAction>
                </div>
              )}
            </AutoDismiss>
          )}
        </div>

        {/* 拖着文件经过时 Wails 会给拖放区加上 wails-drop-target-active 这个类 */}
        <div className="pointer-events-none absolute inset-0 hidden items-center justify-center bg-black/60 p-6 text-center text-xs leading-6 text-white group-[.wails-drop-target-active]/drop:flex">
          松手：APK 安装到手机
          <br />
          其它文件推到手机的 Download 文件夹
        </div>
      </div>

      <div
        className="flex shrink-0 flex-col items-center gap-1 overflow-y-auto border-l border-white/10 bg-card py-1.5"
        style={{ width: RAIL }}
      >
        <RailButton
          title={fullscreen ? '退出全屏（Esc）' : '全屏：画面铺满整个屏幕'}
          onClick={fullscreen ? exitFullscreen : enterFullscreen}
        >
          {fullscreen ? <Minimize2 className="h-3.5 w-3.5" /> : <Maximize2 className="h-3.5 w-3.5" />}
        </RailButton>
        {/* 点一下换下一档。档位很少换,一个按钮比下拉框省地方 */}
        <RailButton
          title={
            restartLocked
              ? `画质：${q.label}（录屏中不能换，换了录像会断开）`
              : `画质：${q.label}（${q.note}）· 点一下换成「${nextQuality.label}」`
          }
          onClick={() => setQuality(nextQuality.id)}
          disabled={restartLocked}
        >
          <span className="text-[10px] font-medium leading-none">{q.label}</span>
        </RailButton>
        <RailButton
          title={
            restartLocked
              ? '保持亮屏：录屏中不能改'
              : keepAwake
                ? '保持亮屏：开着。投屏期间手机不会自己息屏（手机端定时报「有人在用」，不改手机设置）· 点一下关掉'
                : '保持亮屏：关着。手机按自己的设置到点息屏，息屏后画面会黑 · 点一下打开'
          }
          onClick={() => setKeepAwake(!keepAwake)}
          disabled={restartLocked}
          on={keepAwake}
        >
          {keepAwake ? <Sun className="h-3.5 w-3.5" /> : <Moon className="h-3.5 w-3.5" />}
        </RailButton>
        <RailButton
          title="关闭投屏"
          onClick={() => {
            if (fullscreen) exitFullscreen()
            onClose()
          }}
        >
          <X className="h-3.5 w-3.5" />
        </RailButton>

        <div className="my-0.5 h-px w-5 shrink-0 bg-border" />
        <RailButton
          title={
            captureDir
              ? `截图：手机原图，存到 ${captureDir}`
              : '截图：手机原图（不是投屏画面，没有压缩）。第一次会问存到哪个文件夹'
          }
          onClick={() => void screenshot()}
          disabled={!live || shooting}
        >
          {shooting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Camera className="h-3.5 w-3.5" />}
        </RailButton>
        <RailButton
          title={
            recording
              ? `停止录屏（已录 ${fmtDuration(Date.now() - recording.since)}）`
              : captureDir
                ? `录屏：存成 MP4，到 ${captureDir}`
                : '录屏：存成 MP4。第一次会问存到哪个文件夹'
          }
          onClick={() => void toggleRecording()}
          disabled={!live && !recording}
          on={recording !== null}
        >
          {recording ? (
            <span className="flex flex-col items-center leading-none text-red-500">
              <span className="h-2 w-2 animate-pulse rounded-full bg-red-500" />
              <span className="mt-0.5 text-[8px] tabular-nums">{fmtClock(Date.now() - recording.since)}</span>
            </span>
          ) : (
            <Video className="h-3.5 w-3.5" />
          )}
        </RailButton>
        <RailButton
          title="手机剪贴板：看看手机上复制着什么"
          onClick={() => (clip.open ? setClip((c) => ({ ...c, open: false })) : readClipboard())}
          disabled={!live}
          on={clip.open}
        >
          <ClipboardList className="h-3.5 w-3.5" />
        </RailButton>

        <div className="flex-1" />

        <RailButton title="下拉通知栏（再按返回收起来）" onClick={() => send({ t: 'panel', a: 0 })} disabled={!live}>
          <PanelTopOpen className="h-3.5 w-3.5" />
        </RailButton>
        <RailButton title="音量 +" onClick={() => pressKey(KEY.VOLUME_UP)} disabled={!live}>
          <Volume2 className="h-3.5 w-3.5" />
        </RailButton>
        <RailButton title="音量 -" onClick={() => pressKey(KEY.VOLUME_DOWN)} disabled={!live}>
          <Volume1 className="h-3.5 w-3.5" />
        </RailButton>
        <RailButton title="返回（也可以在画面上点右键）" onClick={pressBack} disabled={!live}>
          <ChevronLeft className="h-4 w-4" />
        </RailButton>
        <RailButton title="主页（也可以在画面上点中键）" onClick={() => pressKey(KEY.HOME)} disabled={!live}>
          <Circle className="h-3.5 w-3.5" />
        </RailButton>
        <RailButton title="最近任务" onClick={() => pressKey(KEY.APP_SWITCH)} disabled={!live}>
          <Square className="h-3.5 w-3.5" />
        </RailButton>
        <RailButton title="电源键：亮屏 / 锁屏" onClick={() => pressKey(KEY.POWER)} disabled={!live}>
          <Power className="h-3.5 w-3.5" />
        </RailButton>
        <RailButton title={'怎么操作：\n' + HELP} onClick={() => setHint(hint === HELP ? '' : HELP)}>
          <CircleHelp className="h-3.5 w-3.5" />
        </RailButton>
      </div>
    </div>
  )
}

function RailButton({
  title,
  onClick,
  disabled,
  on,
  children,
}: {
  title: string
  onClick: () => void
  disabled?: boolean
  /** 开关类按钮:开着的时候高亮 */
  on?: boolean
  children: ReactNode
}) {
  return (
    <button
      title={title}
      aria-label={title}
      aria-pressed={on}
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:opacity-40',
        on && 'bg-secondary text-foreground',
      )}
    >
      {children}
    </button>
  )
}

/**
 * 到点自己消失的提示条。鼠标停在上面时不计时,挪开后重新数 ——
 * 正要点上面的「打开所在文件夹」,它不能先没了
 */
function AutoDismiss({
  ms,
  onDismiss,
  children,
  ...rest
}: { ms: number; onDismiss: () => void; children: ReactNode } & Omit<HTMLAttributes<HTMLDivElement>, 'children'>) {
  const [hover, setHover] = useState(false)
  const done = useRef(onDismiss)
  done.current = onDismiss
  useEffect(() => {
    if (hover) return
    const t = window.setTimeout(() => done.current(), ms)
    return () => window.clearTimeout(t)
  }, [hover, ms])
  return (
    <div {...rest} onMouseEnter={() => setHover(true)} onMouseLeave={() => setHover(false)}>
      {children}
    </div>
  )
}

function ToastAction({ onClick, children }: { onClick: () => void; children: ReactNode }) {
  return (
    <button onClick={onClick} className="flex items-center gap-1 underline-offset-2 hover:underline">
      {children}
    </button>
  )
}

/** 手机剪贴板:最近一次读到的,加上第一次粘贴前原来的那份 */
function ClipboardCard({
  clip,
  onCopy,
  onReload,
  onClose,
}: {
  clip: Clip
  onCopy: (text: string) => void
  onReload: () => void
  onClose: () => void
}) {
  return (
    <div
      data-mirror-clipboard=""
      className="pointer-events-auto w-full rounded-md border border-border bg-card p-2 text-[11px] text-foreground shadow-lg"
    >
      <div className="mb-1 flex items-center gap-1.5">
        <span className="font-medium">手机剪贴板</span>
        <button onClick={onReload} title="再读一次" className="ml-auto opacity-70 hover:opacity-100">
          <RotateCcw className="h-3 w-3" />
        </button>
        <button onClick={onClose} title="收起" className="opacity-70 hover:opacity-100">
          <X className="h-3 w-3" />
        </button>
      </div>
      {clip.loading ? (
        <div className="flex items-center gap-1.5 text-muted-foreground">
          <Loader2 className="h-3 w-3 animate-spin" />
          正在读…
        </div>
      ) : clip.text ? (
        <ClipText text={clip.text} onCopy={onCopy} />
      ) : (
        <div className="text-muted-foreground">剪贴板是空的，或者这台手机不让读</div>
      )}
      {clip.before !== null && (
        <div className="mt-2 border-t border-border pt-1.5">
          <div className="mb-1 text-muted-foreground">第一次粘贴中文之前，手机剪贴板里原来是：</div>
          {clip.before ? (
            <ClipText text={clip.before} onCopy={onCopy} />
          ) : (
            <div className="text-muted-foreground">（空的）</div>
          )}
        </div>
      )}
      <div className="mt-1.5 text-[10px] text-muted-foreground">
        在画面上按 Ctrl+C 也能把手机上选中的字直接复制到电脑
      </div>
    </div>
  )
}

function ClipText({ text, onCopy }: { text: string; onCopy: (text: string) => void }) {
  return (
    <div className="flex items-start gap-1.5">
      <div className="max-h-28 min-w-0 flex-1 overflow-auto whitespace-pre-wrap break-words rounded bg-muted/60 px-1.5 py-1 font-mono">
        {text}
      </div>
      <button onClick={() => onCopy(text)} title="复制到电脑" className="shrink-0 opacity-70 hover:opacity-100">
        <Copy className="h-3.5 w-3.5" />
      </button>
    </div>
  )
}

/**
 * 窗口有没有被最小化。Wails 最小化窗口时网页收不到任何通知(页面照样以为自己在显示),
 * 只能隔一会儿问一次;页面自己变成不可见也算。连着两次问到最小化才算数 ——
 * 顺手点了最小化又马上点回来,不至于断一次再连
 */
function useWindowShown(enabled: boolean) {
  const [shown, setShown] = useState(true)
  useEffect(() => {
    if (!enabled) {
      setShown(true)
      return
    }
    let alive = true
    let misses = 0
    const check = async () => {
      let minimised = false
      try {
        minimised = !!(await WindowIsMinimised())
      } catch {
        // 不在 Wails 里(纯浏览器预览)
      }
      if (!alive) return
      misses = minimised ? misses + 1 : 0
      setShown(document.visibilityState !== 'hidden' && misses < 2)
    }
    const onVisibility = () => {
      if (document.visibilityState === 'hidden') setShown(false)
      else void check()
    }
    const timer = window.setInterval(() => void check(), 1500)
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      alive = false
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [enabled])
  return shown
}

/** 当前屏幕的缩放比。窗口拖到缩放比例不同的另一块显示器上会变 */
function useDevicePixelRatio() {
  const [dpr, setDpr] = useState(() => window.devicePixelRatio || 1)
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    // 这条查询只在「离开现在这个缩放比」时触发一次,所以每变一次就换一条新的
    const mq = window.matchMedia(`(resolution: ${dpr}dppx)`)
    const onChange = () => setDpr(window.devicePixelRatio || 1)
    mq.addEventListener?.('change', onChange)
    return () => mq.removeEventListener?.('change', onChange)
  }, [dpr])
  return dpr
}

const sizeOf = (s: { w: number; h: number }) => ({ w: s.w, h: s.h })

/** 提示里只放开头一段 */
function preview(text: string) {
  const one = text.replace(/\s+/g, ' ').trim()
  return one.length > 40 ? one.slice(0, 40) + '…' : one
}

function fmtDuration(ms: number) {
  const s = Math.max(0, Math.round(ms / 1000))
  return s < 60 ? `${s} 秒` : `${Math.floor(s / 60)} 分 ${s % 60} 秒`
}

function fmtClock(ms: number) {
  const s = Math.max(0, Math.floor(ms / 1000))
  return `${String(Math.floor(s / 60)).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`
}

function summarizeDrop(items: mirror.DropItem[]): Toast {
  const ok = items.filter((i) => i.ok)
  const bad = items.filter((i) => !i.ok)
  const installed = ok.filter((i) => i.kind === 'install').length
  const pushed = ok.filter((i) => i.kind !== 'install').reduce((n, i) => n + (i.kind === 'folder' ? (i.files ?? 0) : 1), 0)
  const done: string[] = []
  if (installed) done.push(`装好了 ${installed} 个应用`)
  if (pushed) done.push(`推了 ${pushed} 个文件到手机的 Download 文件夹`)
  const lines = [done.join('，') || '一个都没成功']
  for (const i of bad.slice(0, 3)) lines.push(`${i.name}：${i.error ?? '失败了'}`)
  if (bad.length > 3) lines.push(`还有 ${bad.length - 3} 项没成功`)
  return { text: lines.join('\n'), error: bad.length > 0 }
}
