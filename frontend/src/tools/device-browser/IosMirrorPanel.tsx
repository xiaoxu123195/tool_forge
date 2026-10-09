import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import {
  Camera,
  Circle,
  CircleHelp,
  ClipboardList,
  Columns2,
  FolderOpen,
  Loader2,
  Lock,
  Maximize2,
  Minimize2,
  Moon,
  PackageOpen,
  RotateCcw,
  Sun,
  Video,
  Volume1,
  Volume2,
  X,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { useDeviceBrowserStore } from '@/stores/device-browser'
import {
  AppendMirrorRecording,
  BeginMirrorRecording,
  EndMirrorRecording,
  InstallTrollVNC,
  PickTrollVNCPackage,
  SaveMirrorShot,
  StartIOSMirror,
  StopIOSMirror,
  TrollVNCStatus,
} from '../../../wailsjs/go/main/App'
import type { mirror } from '../../../wailsjs/go/models'
import { ClipboardGetText, ClipboardSetText } from '../../../wailsjs/runtime/runtime'
import { typeable } from './mirror-input'
import { iosKeyAction, textKeys, XK } from './ios-input'
import { BUTTON, CanvasRecorder, VncLink, supportsRecording } from './vnc-link'
import {
  ClipboardCard,
  DEFAULT_ASPECT,
  HintBar,
  KeyboardCatcher,
  RAIL,
  RailButton,
  RecordIcon,
  ToastBar,
  displaySize,
  fmtDuration,
  preview,
  useCaptureDir,
  useDevicePixelRatio,
  useElementSize,
  useFullscreen,
  useTicker,
  useWindowShown,
  type Toast,
} from './mirror-ui'

/** 画质:VNC 里 JPEG 的压缩程度,0–9。手机发的是原尺寸,电脑这边再高质量缩小 */
const QUALITIES = [
  { id: 'smooth', label: '流畅', level: 4, note: '压得狠一些，线不好或者电脑慢时用' },
  { id: 'normal', label: '标准', level: 7, note: '字清楚，也不太占 USB' },
  { id: 'full', label: '原画', level: 9, note: '几乎不压缩，字最清楚，最吃 USB' },
] as const
type QualityId = (typeof QUALITIES)[number]['id']

type Phase = 'checking' | 'missing' | 'installing' | 'starting' | 'live' | 'ended' | 'error'

/** 往手机剪贴板里放了字以后,等多久再按 Command+V:手机那头写剪贴板是排着队做的 */
const PASTE_DELAY = 200
/** 按了 Ctrl+C 以后多久内回来的剪贴板内容算是这次复制的 */
const COPY_WAIT = 3000

const HELP = [
  '左键：点按、拖动，和手指一样（只能单指，没有双指缩放）',
  '右键：主页　　中键：锁屏 / 亮屏',
  '滚轮：上下滑',
  '点一下画面就能用键盘打字，中文经手机剪贴板粘贴过去',
  'Ctrl+C / Ctrl+X：手机上选中的字复制到电脑',
  'Ctrl+V：电脑剪贴板里的字粘到手机上',
  'Alt+按键：等于 iPhone 外接键盘上的 Command+按键',
].join('\n')

interface Props {
  /** 真机浏览那条连接:开关手机上的 TrollVNC 都经它的 SSH */
  deviceSession: string
  /** 真机浏览这一页是不是正在显示。切走就停,切回来自动重连(录着屏时除外) */
  active: boolean
  onClose: () => void
}

/**
 * 越狱 iPhone 的投屏面板,停在真机浏览的最右边,样子和用法跟安卓的一样。
 *
 * 手机上跑的是 TrollVNC:开面板时工具箱经 SSH 按这一次的设置把它启动起来
 * (只走 USB、这次专用的随机密码),关面板就停掉。电脑这头是 noVNC。
 * 手机上没装 TrollVNC 时,面板里教人怎么拿到安装包,再经 SSH 装上去
 */
export function IosMirrorPanel({ deviceSession, active, onClose }: Props) {
  const keepAwake = useDeviceBrowserStore((s) => s.mirrorKeepAwake)
  const setKeepAwake = useDeviceBrowserStore((s) => s.setMirrorKeepAwake)

  const [quality, setQuality] = useState<QualityId>('normal')
  const [attempt, setAttempt] = useState(0)
  const [phase, setPhase] = useState<Phase>('checking')
  const [message, setMessage] = useState('')
  const [status, setStatus] = useState<mirror.TrollStatus | null>(null)
  const [installing, setInstalling] = useState(false)
  const [installError, setInstallError] = useState('')
  const [hint, setHint] = useState('')
  const [screen, setScreen] = useState<{ w: number; h: number } | null>(null)
  const [toast, setToast] = useState<Toast | null>(null)
  const [recording, setRecording] = useState<{ since: number } | null>(null)
  const [shooting, setShooting] = useState(false)
  // 手机剪贴板:连上以后手机上最近一次复制的东西。iOS 读不到连上之前复制的
  const [clip, setClip] = useState<{ open: boolean; text: string | null }>({ open: false, text: null })
  const [kbdFocus, setKbdFocus] = useState(false)
  const [kbdAt, setKbdAt] = useState({ x: 0, y: 0 })

  const areaRef = useRef<HTMLDivElement>(null)
  const hostRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<HTMLCanvasElement>(null)
  const kbdRef = useRef<HTMLTextAreaElement>(null)
  const linkRef = useRef<VncLink | null>(null)
  // 截图、录屏文件名里的机型
  const labelRef = useRef('iPhone')
  const recRef = useRef<{ id: string; recorder: CanvasRecorder; since: number } | null>(null)
  const pendingCopy = useRef(0)
  const pastedOnce = useRef(false)
  // 发给手机的键盘操作排成一队:中文粘贴要等一下,后面打的英文不能插到它前面去
  const outQ = useRef<Promise<void>>(Promise.resolve())

  const windowShown = useWindowShown(active)
  const dpr = useDevicePixelRatio()
  const area = useElementSize(areaRef)
  const { fullscreen, enter: enterFullscreen, exit: exitFullscreen, fullscreenRef } = useFullscreen(active)
  const { captureDir, pickDir, failed, ensureDir } = useCaptureDir(setToast)
  useTicker(recording !== null)
  const running = (active && windowShown) || recording !== null
  const q = QUALITIES.find((x) => x.id === quality) ?? QUALITIES[1]

  // ---- 剪贴板、录屏的回信 ----
  const onClipboard = (text: string) => {
    setClip((c) => ({ ...c, text }))
    if (Date.now() - pendingCopy.current < COPY_WAIT) {
      pendingCopy.current = 0
      void ClipboardSetText(text)
      setToast({ text: `已复制到电脑：${preview(text)}` })
    }
  }
  const onClipboardRef = useRef(onClipboard)
  onClipboardRef.current = onClipboard

  const onRecorded = (r: mirror.Recording, error: string) => {
    const files = r.files ?? []
    if (files.length === 0) {
      setToast({ text: error || r.error || '没录到画面', error: true })
      return
    }
    const err = error || r.error || ''
    setToast({ text: `录屏已保存（${fmtDuration(r.durationMs)}）${err ? '。' + err : ''}`, path: files[0], error: !!err })
  }

  /** 停录屏、把文件收好。投屏断了、面板关了也走这里 */
  const stopRecording = async () => {
    const r = recRef.current
    if (!r) return
    recRef.current = null
    let error = ''
    try {
      await r.recorder.stop()
    } catch (e) {
      error = '有一段没写进文件：' + errText(e)
    }
    try {
      onRecorded(await EndMirrorRecording(r.id, Date.now() - r.since), error)
    } catch (e) {
      failed(e)
    } finally {
      setRecording(null)
    }
  }
  // 连接上的回调、卸载时的清理在早先就定下了,经 ref 转一道,用的永远是最新的那份
  const stopRecordingRef = useRef(stopRecording)
  stopRecordingRef.current = stopRecording
  useEffect(() => () => void stopRecordingRef.current(), [])

  // 换画质不用重连:开连接时取当前档,之后直接改
  const levelRef = useRef<number>(q.level)
  levelRef.current = q.level
  useEffect(() => {
    linkRef.current?.setQuality(q.level)
  }, [q.level])

  // ---- 开一路:查 TrollVNC → 按这次的设置启动 → noVNC 连上去 ----
  useEffect(() => {
    if (!running) return
    let cancelled = false
    let link: VncLink | null = null
    let sessionId = ''
    setPhase('checking')
    setMessage('')
    setScreen(null)
    void (async () => {
      try {
        const st = await TrollVNCStatus(deviceSession)
        if (cancelled) return
        setStatus(st)
        if (st.unsupported) {
          setPhase('error')
          setMessage(st.unsupported)
          return
        }
        if (!st.installed) {
          setPhase('missing')
          return
        }
        setPhase('starting')
        const s = await StartIOSMirror(deviceSession, { keepAwake } as mirror.IOSOptions)
        sessionId = s.id
        const host = hostRef.current
        const view = viewRef.current
        if (cancelled || !host || !view) {
          void StopIOSMirror(s.id).catch(() => {})
          return
        }
        labelRef.current = s.deviceName || 'iPhone'
        pastedOnce.current = false
        link = await VncLink.open(s.id, s.url, s.password, host, view, levelRef.current, {
          onSize: (w, h) => setScreen({ w, h }),
          onLive: () => setPhase('live'),
          onClipboard: (text) => onClipboardRef.current(text),
          onEnded: (reason) => {
            setPhase('ended')
            setMessage(reason)
            void stopRecordingRef.current()
          },
        })
        if (cancelled) {
          link.close()
          return
        }
        linkRef.current = link
      } catch (e) {
        if (cancelled) return
        setPhase('error')
        setMessage(errText(e))
      }
    })()
    return () => {
      cancelled = true
      linkRef.current = null
      if (link) link.close()
      else if (sessionId) void StopIOSMirror(sessionId).catch(() => {})
    }
  }, [deviceSession, keepAwake, attempt, running])

  // ---- 尺寸 ----
  const aspect = screen ? screen.w / screen.h : DEFAULT_ASPECT
  const display = displaySize(aspect, fullscreen, area)
  // 显示用的画布按屏幕实际像素建,缩小交给高质量插值
  useLayoutEffect(() => {
    const c = viewRef.current
    if (!c) return
    const w = Math.max(1, Math.round(display.width * dpr))
    const h = Math.max(1, Math.round(display.height * dpr))
    if (c.width !== w || c.height !== h) {
      c.width = w
      c.height = h
      linkRef.current?.redraw()
    }
  }, [display.width, display.height, dpr])

  // ---- 键盘、剪贴板 ----
  const enqueue = (task: () => void | Promise<void>) => {
    outQ.current = outQ.current.then(task).catch(() => {})
  }
  const press = (keys: number[]) => enqueue(() => linkRef.current?.press(keys))
  // 中文、表情手机上按键打不出来:放进手机剪贴板,再按 Command+V
  const paste = (s: string) => {
    if (!pastedOnce.current) {
      pastedOnce.current = true
      setHint('中文是经手机剪贴板粘贴的：手机剪贴板里原来的内容会被替换（iOS 读不到连上之前复制的东西）')
    }
    enqueue(async () => {
      const link = linkRef.current
      if (!link) return
      link.setClipboard(s)
      await sleep(PASTE_DELAY)
      link.press([XK.Command, 0x76])
    })
  }
  const sendText = (s: string) => {
    if (!s) return
    if (typeable(s)) for (const keys of textKeys(s)) press(keys)
    else paste(s)
  }
  const pasteFromPC = async () => {
    const s = await ClipboardGetText().catch(() => '')
    if (!s) {
      setToast({ text: '电脑剪贴板里没有文字' })
      return
    }
    paste(s)
  }
  const copyFromPhone = (cut: boolean) => {
    pendingCopy.current = Date.now()
    press([XK.Command, cut ? 0x78 : 0x63])
  }

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // 全屏时 Esc 是退出全屏,不发给手机
    if (e.key === 'Escape' && fullscreenRef.current) return
    const act = iosKeyAction(e)
    if (!act) {
      // 没认领的 Ctrl 组合键不发给手机,也不让输入框自己处理;照样往上冒,工具箱的快捷键还能用
      if (e.ctrlKey || e.metaKey) e.preventDefault()
      return
    }
    e.preventDefault()
    e.stopPropagation()
    if (act.kind === 'keys') press(act.keys)
    else if (act.kind === 'paste') void pasteFromPC()
    else copyFromPhone(act.cut)
  }

  // 点画面:键盘归手机,输入框挪到点的位置(输入法的候选框跟着出现在这儿)。
  // 鼠标本身交给 noVNC,它在上面那块透明画布上
  const focusKeyboard = (e: React.PointerEvent) => {
    const box = areaRef.current?.getBoundingClientRect()
    if (box) setKbdAt({ x: e.clientX - box.left, y: e.clientY - box.top })
    kbdRef.current?.focus({ preventScroll: true })
  }

  // ---- 按钮 ----
  const clickButton = (b: number) => enqueue(() => linkRef.current?.click(b))
  // 连按两下 Home 是多任务
  const appSwitcher = () =>
    enqueue(async () => {
      linkRef.current?.click(BUTTON.HOME)
      await sleep(150)
      linkRef.current?.click(BUTTON.HOME)
    })

  // ---- 截图、录屏 ----
  const screenshot = async () => {
    const link = linkRef.current
    if (!link || shooting) return
    const dir = await ensureDir()
    if (!dir) return
    setShooting(true)
    try {
      const s = await SaveMirrorShot(dir, labelRef.current, await link.snapshot())
      setToast({ text: `截图已保存（${s.width}×${s.height}）`, path: s.path, image: true })
    } catch (e) {
      failed(e)
    } finally {
      setShooting(false)
    }
  }
  const toggleRecording = async () => {
    if (recRef.current) {
      await stopRecording()
      return
    }
    const src = linkRef.current?.source()
    if (!src) return
    if (!supportsRecording()) {
      setToast({ text: '当前环境录不了屏：没有 MediaRecorder。更新一下 Edge WebView2 运行时再试', error: true })
      return
    }
    const dir = await ensureDir()
    if (!dir) return
    try {
      const recorder = new CanvasRecorder(src)
      const [id] = await BeginMirrorRecording(dir, labelRef.current, recorder.ext)
      recorder.start((b64) => AppendMirrorRecording(id, b64))
      const since = Date.now()
      recRef.current = { id, recorder, since }
      setRecording({ since })
    } catch (e) {
      failed(e)
    }
  }

  // ---- 没装 TrollVNC:选安装包,经 SSH 装上去 ----
  const install = async () => {
    const path = await PickTrollVNCPackage().catch(() => '')
    if (!path) return
    setInstalling(true)
    setInstallError('')
    try {
      const r = await InstallTrollVNC(deviceSession, path)
      setToast({ text: `TrollVNC ${r.version} 装好了，正在启动投屏` })
      setAttempt((n) => n + 1)
    } catch (e) {
      setInstallError(errText(e))
    } finally {
      setInstalling(false)
    }
  }

  // 已经装着:换一个安装包(比如自己改过、重新编的版本)。先断开投屏,装完重新开
  const reinstall = async () => {
    const path = await PickTrollVNCPackage().catch(() => '')
    if (!path) return
    linkRef.current?.close()
    linkRef.current = null
    setPhase('installing')
    try {
      const r = await InstallTrollVNC(deviceSession, path)
      setToast({ text: `TrollVNC 换成了 ${r.version}，正在重新投屏` })
    } catch (e) {
      setToast({ text: errText(e), error: true })
    } finally {
      setAttempt((n) => n + 1)
    }
  }

  const nextQuality = QUALITIES[(QUALITIES.indexOf(q) + 1) % QUALITIES.length]
  const live = phase === 'live'
  // 开关保持亮屏要按新设置重启手机上的服务,正在录的屏会断
  const restartLocked = recording !== null

  return (
    <div
      data-mirror-panel=""
      className={cn(
        'flex min-h-0 overflow-hidden',
        fullscreen ? 'fixed inset-0 z-[300] bg-black' : 'shrink-0 rounded-lg border border-border bg-black',
      )}
      style={fullscreen ? undefined : { width: Math.max(240, display.width) + RAIL + 2 }}
    >
      <div ref={areaRef} className="relative flex min-h-0 min-w-0 flex-1 items-center justify-center">
        <div
          className={cn(
            'relative shrink-0',
            !live && 'invisible',
            kbdFocus && live && 'outline outline-2 outline-sky-400/80',
          )}
          style={{ width: display.width, height: display.height }}
          onPointerDownCapture={focusKeyboard}
        >
          <canvas ref={viewRef} data-mirror-canvas="" className="absolute inset-0 h-full w-full" />
          {/* noVNC 的画布:透明地盖在上面接鼠标和滚轮;光标保持系统箭头(手机不发光标) */}
          <div
            ref={hostRef}
            data-vnc-host=""
            className="absolute inset-0 select-none [&_canvas]:!cursor-default [&_canvas]:opacity-0"
          />
        </div>
        <KeyboardCatcher ref={kbdRef} at={kbdAt} onKeyDown={onKeyDown} onText={sendText} onFocusChange={setKbdFocus} />

        {!live && phase !== 'missing' && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 p-4 text-center text-xs text-white/80">
            {phase === 'installing' && (
              <>
                <Loader2 className="h-5 w-5 animate-spin" />
                <span>正在把 TrollVNC 装到手机上…装完自动重新投屏</span>
              </>
            )}
            {(phase === 'checking' || phase === 'starting') && (
              <>
                <Loader2 className="h-5 w-5 animate-spin" />
                <span>{phase === 'checking' ? '正在查看手机上的 TrollVNC…' : '正在启动投屏…'}</span>
                {/* 取证现场要知道这一步对手机做了什么 */}
                <span className="text-[11px] leading-5 text-white/50">
                  会经 SSH 改写手机上 TrollVNC 的设置并启动它：只走 USB、用这一次专用的随机密码，关掉投屏就停；在画面上点击会真的操作手机
                </span>
                <span className="whitespace-pre-line text-[11px] leading-5 text-white/40">{HELP}</span>
              </>
            )}
            {(phase === 'error' || phase === 'ended') && (
              <>
                <span className="whitespace-pre-wrap break-words">{message}</span>
                {!status?.unsupported && (
                  <Button size="sm" variant="secondary" onClick={() => setAttempt((n) => n + 1)}>
                    <RotateCcw className="h-3.5 w-3.5" />
                    {phase === 'error' ? '重试' : '重新连接'}
                  </Button>
                )}
              </>
            )}
          </div>
        )}

        {phase === 'missing' && status && (
          <InstallGuide status={status} busy={installing} error={installError} onInstall={() => void install()} />
        )}

        <div className="pointer-events-none absolute inset-x-2 top-2 flex flex-col items-end gap-1.5">
          <HintBar hint={hint} onClose={() => setHint('')} />
          {live && display.landscape && !fullscreen && (
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
              text={clip.text}
              emptyText="连上以后手机上还没复制过东西（iOS 读不到连上之前复制的内容）"
              footer="手机上一复制，这里就跟着变；在画面上按 Ctrl+C 能把选中的字直接复制到电脑"
              onCopy={(text) => {
                void ClipboardSetText(text)
                setToast({ text: `已复制到电脑：${preview(text)}` })
              }}
              onClose={() => setClip((c) => ({ ...c, open: false }))}
            />
          )}
        </div>

        <div className="pointer-events-none absolute inset-x-2 bottom-2 flex flex-col gap-1.5">
          <ToastBar toast={toast} onChange={setToast} onFailed={failed} onPickDir={() => void pickDir()} />
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
        <RailButton
          title={`画质：${q.label}（${q.note}）· 点一下换成「${nextQuality.label}」`}
          onClick={() => setQuality(nextQuality.id)}
        >
          <span className="text-[10px] font-medium leading-none">{q.label}</span>
        </RailButton>
        <RailButton
          title={
            restartLocked
              ? '保持亮屏：录屏中不能改'
              : keepAwake
                ? '保持亮屏：开着。投屏期间手机不会自动锁屏（TrollVNC 每 30 秒按一下唤醒，不改手机设置）· 点一下关掉'
                : '保持亮屏：关着。手机按自己的设置到点锁屏，锁屏后画面会黑 · 点一下打开'
          }
          onClick={() => setKeepAwake(!keepAwake)}
          disabled={restartLocked}
          on={keepAwake}
        >
          {keepAwake ? <Sun className="h-3.5 w-3.5" /> : <Moon className="h-3.5 w-3.5" />}
        </RailButton>
        <RailButton
          title="关闭投屏（手机上的 TrollVNC 随之停掉）"
          onClick={() => {
            if (fullscreen) exitFullscreen()
            onClose()
          }}
        >
          <X className="h-3.5 w-3.5" />
        </RailButton>

        <div className="my-0.5 h-px w-5 shrink-0 bg-border" />
        <RailButton
          title={captureDir ? `截图：原尺寸 PNG，存到 ${captureDir}` : '截图：原尺寸 PNG。第一次会问存到哪个文件夹'}
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
          {recording ? <RecordIcon since={recording.since} /> : <Video className="h-3.5 w-3.5" />}
        </RailButton>
        <RailButton
          title="手机剪贴板：连上以后手机上复制过的东西"
          onClick={() => setClip((c) => ({ ...c, open: !c.open }))}
          disabled={!live}
          on={clip.open}
        >
          <ClipboardList className="h-3.5 w-3.5" />
        </RailButton>

        <div className="flex-1" />

        <RailButton title="音量 +" onClick={() => press([XK.VolumeUp])} disabled={!live}>
          <Volume2 className="h-3.5 w-3.5" />
        </RailButton>
        <RailButton title="音量 -" onClick={() => press([XK.VolumeDown])} disabled={!live}>
          <Volume1 className="h-3.5 w-3.5" />
        </RailButton>
        <RailButton title="主页（也可以在画面上点右键）" onClick={() => clickButton(BUTTON.HOME)} disabled={!live}>
          <Circle className="h-3.5 w-3.5" />
        </RailButton>
        <RailButton title="多任务（连按两下主页）" onClick={appSwitcher} disabled={!live}>
          <Columns2 className="h-3.5 w-3.5" />
        </RailButton>
        <RailButton title="锁屏 / 亮屏（也可以在画面上点中键）" onClick={() => clickButton(BUTTON.POWER)} disabled={!live}>
          <Lock className="h-3.5 w-3.5" />
        </RailButton>
        <RailButton
          title={
            restartLocked
              ? 'TrollVNC：录屏中不能换安装包'
              : `TrollVNC ${status?.version ?? ''}：换一个安装包（更新、重装），先断开投屏，装完重新开`
          }
          onClick={() => void reinstall()}
          disabled={restartLocked || phase === 'installing' || !status?.installed}
        >
          <PackageOpen className="h-3.5 w-3.5" />
        </RailButton>
        <RailButton title={'怎么操作：\n' + HELP} onClick={() => setHint(hint === HELP ? '' : HELP)}>
          <CircleHelp className="h-3.5 w-3.5" />
        </RailButton>
      </div>
    </div>
  )
}

/** 手机上没装 TrollVNC:说清楚去哪儿拿安装包,选好了经 SSH 装上去 */
function InstallGuide({
  status,
  busy,
  error,
  onInstall,
}: {
  status: mirror.TrollStatus
  busy: boolean
  error: string
  onInstall: () => void
}) {
  return (
    <div className="absolute inset-0 overflow-y-auto p-4 text-xs leading-5 text-white/80">
      <div className="mb-2 text-sm font-medium text-white">这台 {status.model} 上还没装 TrollVNC</div>
      <p className="mb-2 text-white/60">
        iOS 投屏要在手机上跑 TrollVNC（开源的 VNC 服务端）。它的安装包得自己编，在 GitHub 上免费：
      </p>
      <ol className="mb-3 list-decimal space-y-1 pl-4">
        <li>
          打开 <span className="font-mono">github.com/OwnGoalStudio/TrollVNC</span>，点 Fork（去掉「只复制 main 分支」那一项）
        </li>
        <li>在你的 fork 里打开 Actions，启用后手动运行「Build TrollVNC」，分支选 release</li>
        <li>
          跑完以后在产物里下载 <span className="font-mono text-white">{status.artifact}</span>（zip 不用解压）
        </li>
        <li>点下面的按钮选它，工具箱经 SSH 装到手机上</li>
      </ol>
      {!status.loader && (
        <div className="mb-3 rounded-md bg-amber-500/20 px-2 py-1.5 text-amber-100">
          手机上还缺 PreferenceLoader（TrollVNC 的包依赖它）：先在手机的 Sileo 里搜「PreferenceLoader」装上
        </div>
      )}
      <Button size="sm" variant="secondary" onClick={onInstall} disabled={busy}>
        {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <FolderOpen className="h-3.5 w-3.5" />}
        {busy ? '正在装…' : '选择安装包并安装'}
      </Button>
      {error && <div className="mt-2 whitespace-pre-wrap break-words text-red-300">{error}</div>}
      <p className="mt-3 text-[11px] text-white/40">
        装的时候先写好设置再启动：只听手机本机、不在局域网广播、不开网页端；之后每次投屏换一个随机密码，不投屏时服务停着
      </p>
    </div>
  )
}

function errText(e: unknown) {
  return e instanceof Error ? e.message : String(e)
}

function sleep(ms: number) {
  return new Promise<void>((r) => setTimeout(r, ms))
}
