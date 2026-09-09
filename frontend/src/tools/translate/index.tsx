import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowLeftRight,
  ChevronDown,
  Clock,
  Copy,
  Image as ImageIcon,
  Languages,
  Loader2,
  Paperclip,
  Send,
  Settings,
  Square,
  Check,
  X,
} from 'lucide-react'
import {
  ListAIProviders,
  StartAITranslate,
  StopAITranslate,
} from '../../../wailsjs/go/main/App'
import { EventsOn } from '../../../wailsjs/runtime/runtime'
import type { ImageBlock, Provider } from '@/tools/ai-chat/types'
import {
  fileToFileBlock,
  fileToImageBlock,
  formatFileSize,
  imageSrc,
  isImageFile,
  MAX_FILES_PER_MESSAGE,
  MAX_IMAGE_BYTES,
} from '@/tools/ai-chat/file-parsers'
import { ChatModelPicker } from '@/tools/ai-chat/ChatModelPicker'
import { ProviderAvatar } from '@/tools/ai-chat/ProviderAvatar'
import { MarkdownPreview } from '@/components/tool/MarkdownPreview'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { cn } from '@/lib/utils'

import { AUTO_DETECT_ID, findLang } from './languages'
import { LanguageSelect } from './LanguageSelect'
import { Flag } from './Flag'
import { useTranslateStore } from './store'
import { SettingsDialog } from './SettingsDialog'
import { MoreSettingsDialog } from './MoreSettingsDialog'
import { HistoryDrawer } from './HistoryDrawer'
import { detectAuto, detectByFranc, detectByLLM } from './detect'

const EV_CHUNK = 'translate:chunk:'
const EV_DONE = 'translate:done:'
const EV_ERR = 'translate:error:'

// multiImage 模式下一次最多贴几张图(单张模式忽略此值)
const MAX_TRANSLATE_IMAGES = 6

/** Wails 多返回值兼容:array / {0,1} / 直传字符串(只有一个值时) */

export default function TranslatePage() {
  const dialog = useConfirm()
  const [providers, setProviders] = useState<Provider[]>([])
  const [source, setSource] = useState('')
  const [target, setTarget] = useState('')
  const [streaming, setStreaming] = useState(false)
  const [jobId, setJobId] = useState('')
  const [pickerOpen, setPickerOpen] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [moreOpen, setMoreOpen] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const [detectedHint, setDetectedHint] = useState('') // "检测到:中文" 之类
  const [pendingFile, setPendingFile] = useState<{ name: string; size: number } | null>(null)
  const [pendingImages, setPendingImages] = useState<ImageBlock[]>([])
  const [dragOver, setDragOver] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const sourceRef = useRef<HTMLTextAreaElement>(null)
  const targetRef = useRef<HTMLDivElement>(null)
  const sourceTextRef = useRef('') // 给 onDone 回调用的最新值
  sourceTextRef.current = source
  const lastWasImageRef = useRef(false) // 本次翻译是否为贴图(给 onDone 写历史用)

  const s = useTranslateStore()
  const setMany = s.setMany

  // —— 加载 providers
  useEffect(() => {
    void (async () => {
      const list = ((await ListAIProviders()) ?? []) as unknown as Provider[]
      setProviders(list)
    })()
  }, [])
  const usable = useMemo(
    () => providers.filter((p) => p.enabled && p.models.length > 0),
    [providers],
  )
  const provider = providers.find((p) => p.id === s.providerId)

  // 默认挑第一个可用的 provider/model
  useEffect(() => {
    if (s.providerId && s.modelId) {
      const def = usable.find((p) => p.id === s.providerId)
      if (def && def.models.includes(s.modelId)) return
    }
    if (usable.length > 0) {
      setMany({ providerId: usable[0].id, modelId: usable[0].models[0] })
    }
  }, [usable])

  // —— 滚动同步
  useEffect(() => {
    if (!s.scrollSync) return
    const src = sourceRef.current
    const tgt = targetRef.current
    if (!src || !tgt) return
    let lock = false
    const ratio = (el: HTMLElement) => {
      const max = el.scrollHeight - el.clientHeight
      return max > 0 ? el.scrollTop / max : 0
    }
    const apply = (from: HTMLElement, to: HTMLElement) => {
      if (lock) return
      lock = true
      const r = ratio(from)
      const m = to.scrollHeight - to.clientHeight
      to.scrollTop = r * m
      requestAnimationFrame(() => (lock = false))
    }
    const a = () => apply(src, tgt)
    const b = () => apply(tgt, src)
    src.addEventListener('scroll', a)
    tgt.addEventListener('scroll', b)
    return () => {
      src.removeEventListener('scroll', a)
      tgt.removeEventListener('scroll', b)
    }
  }, [s.scrollSync])

  // —— 流事件订阅
  useEffect(() => {
    if (!jobId) return
    const offChunk = EventsOn(EV_CHUNK + jobId, (delta: string) => {
      setTarget((prev) => prev + delta)
    })
    const offDone = EventsOn(EV_DONE + jobId, async () => {
      setStreaming(false)
      setJobId('')
      // 自动复制
      const finalText = await new Promise<string>((resolve) => {
        // 用一次状态读 — 直接从 DOM 读最稳
        setTimeout(() => resolve(targetRef.current?.innerText ?? ''), 0)
      })
      if (s.autoCopy && finalText) {
        try {
          await navigator.clipboard.writeText(finalText)
        } catch {}
      }
      // 写历史(贴图翻译无源文本,用占位符,不存原图)
      const sText = lastWasImageRef.current ? '🖼 贴图' : sourceTextRef.current.trim()
      if (sText && finalText.trim()) {
        useTranslateStore.getState().pushHistory({
          source: sText,
          target: finalText.trim(),
          sourceLangId: s.sourceLang,
          targetLangId: s.targetLang,
          providerId: s.providerId,
          providerName: provider?.name ?? '',
          modelId: s.modelId,
        })
      }
    })
    const offErr = EventsOn(EV_ERR + jobId, async (err: string) => {
      setStreaming(false)
      setJobId('')
      await dialog({ title: '翻译失败', message: err || '未知错误', confirmLabel: '知道了' })
    })
    return () => {
      offChunk()
      offDone()
      offErr()
    }
  }, [jobId, s.autoCopy, s.sourceLang, s.targetLang, s.providerId, s.modelId, provider?.name])

  // —— 双向翻译:若源语言已是目标语言,触发反向
  const computeFinalLangs = (): { src: string; tgt: string } => {
    if (!s.bidirectional) return { src: s.sourceLang, tgt: s.targetLang }
    if (s.sourceLang === s.targetLang) {
      // 退化:源 = 目标,无意义
      return { src: s.sourceLang, tgt: s.targetLang }
    }
    return { src: s.sourceLang, tgt: s.targetLang }
  }

  const onTranslate = async () => {
    const text = source.trim()
    const hasImage = pendingImages.length > 0
    if (!text && !hasImage) return
    lastWasImageRef.current = hasImage
    // 校验当前选中的 provider/model 仍然有效(可能 localStorage 留了旧 provider 已被禁用)
    let useProviderId = s.providerId
    let useModelId = s.modelId
    const valid = usable.find(
      (p) => p.id === useProviderId && p.models.includes(useModelId),
    )
    if (!valid) {
      if (usable.length === 0) {
        await dialog({
          title: '未配置模型',
          message: '请先到「个人主页 → AI 配置」启用至少一个供应商并选择模型',
          confirmLabel: '知道了',
        })
        return
      }
      // 当前选中无效 → 自动切到第一个可用,并提示一次
      const first = usable[0]
      useProviderId = first.id
      useModelId = first.models[0]
      setMany({ providerId: useProviderId, modelId: useModelId })
    }
    setTarget('')
    setStreaming(true)
    setDetectedHint('')

    // —— 解析最终目标语言(双向 + auto 检测)
    // 贴图翻译:图里没有可供检测的纯文本,跳过源语言检测与双向反转,直接译到目标语言
    let finalTargetId = s.targetLang
    let finalSourceId = s.sourceLang
    if (!hasImage && finalSourceId === AUTO_DETECT_ID) {
      let detected: ReturnType<typeof findLang> | undefined
      if (s.detectMethod === 'algorithm') detected = await detectByFranc(text)
      else if (s.detectMethod === 'llm')
        detected = await detectByLLM(useProviderId, useModelId, text)
      else detected = await detectAuto(useProviderId, useModelId, text)
      if (detected) {
        finalSourceId = detected.id
        setDetectedHint(`检测到:${detected.label}`)
      }
    }
    // 双向:源已是目标语言 → 反向(取上一次的 source);贴图模式不参与
    if (!hasImage && s.bidirectional && finalSourceId === finalTargetId) {
      // 简单策略:目标默认改成英文(如目标也是 en,改成中文)
      finalTargetId = finalTargetId === 'en' ? 'zh' : 'en'
    }
    const tgtLang = findLang(finalTargetId)
    if (!tgtLang) {
      setStreaming(false)
      await dialog({ title: '错误', message: '无法识别目标语言', confirmLabel: '知道了' })
      return
    }

    // 返回任务 ID 字符串;失败走 reject
    const r = await StartAITranslate({
      providerId: useProviderId,
      modelId: useModelId,
      text,
      targetLang: tgtLang.name,
      prompt: s.prompt,
      // 贴图翻译:带上图片,后端走视觉模型(图优先,忽略 text)
      images: hasImage ? pendingImages : undefined,
      // wails 生成的 ts class 还有这两个静态方法,但运行时直接传普通对象就行
    } as any).then(
      (v) => ({ id: v as unknown as string, err: '' }),
      (e) => ({ id: '', err: String(e) }),
    )
    const { id, err } = r
    if (err) {
      setStreaming(false)
      await dialog({ title: '启动失败', message: err, confirmLabel: '知道了' })
      return
    }
    if (!id) {
      setStreaming(false)
      await dialog({
        title: '启动失败',
        message: '后端未返回任务 ID(可能模型未启用或网络问题)',
        confirmLabel: '知道了',
      })
      return
    }
    setJobId(id)
  }

  const onStop = async () => {
    if (jobId) await StopAITranslate(jobId)
  }

  const onSwap = () => {
    if (s.sourceLang === AUTO_DETECT_ID) return // auto 不可交换
    setMany({ sourceLang: s.targetLang, targetLang: s.sourceLang })
    // 顺手把内容也换一下
    setSource(target)
    setTarget(source)
  }

  const onCopy = async () => {
    const txt = targetRef.current?.innerText ?? target
    if (!txt) return
    await navigator.clipboard.writeText(txt)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  const onClear = () => {
    setSource('')
    setTarget('')
    setDetectedHint('')
    setPendingFile(null)
    setPendingImages([])
  }

  // —— 贴图:把图片 File 收进 pendingImages(单张模式替换,多张模式累积到上限)
  const ingestImages = async (files: FileList | File[]) => {
    const imgs = Array.from(files).filter(isImageFile)
    if (imgs.length === 0) return
    const blocks: ImageBlock[] = []
    const errs: string[] = []
    for (const f of imgs) {
      if (f.size > MAX_IMAGE_BYTES) {
        errs.push(`「${f.name || '图片'}」超过 5 MB`)
        continue
      }
      try {
        blocks.push(await fileToImageBlock(f))
      } catch (e: any) {
        errs.push(`「${f.name || '图片'}」${e?.message ?? e}`)
      }
    }
    if (blocks.length > 0) {
      setPendingImages((prev) =>
        s.multiImage
          ? [...prev, ...blocks].slice(0, MAX_TRANSLATE_IMAGES)
          : [blocks[blocks.length - 1]], // 单张:用最后一张替换
      )
    }
    if (errs.length > 0) {
      await dialog({ title: '部分图片无法添加', message: errs.join('\n'), confirmLabel: '知道了' })
    }
  }

  // Ctrl+V 粘贴图片(纯文本粘贴走默认)
  const onSourcePaste = (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const items = e.clipboardData?.items
    if (!items) return
    const files: File[] = []
    for (let i = 0; i < items.length; i++) {
      const it = items[i]
      if (it.kind === 'file') {
        const f = it.getAsFile()
        if (f && isImageFile(f)) files.push(f)
      }
    }
    if (files.length > 0) {
      e.preventDefault()
      void ingestImages(files)
    }
  }

  // 拖拽图片到源面板
  const onSourceDrop = (e: React.DragEvent<HTMLDivElement>) => {
    setDragOver(false)
    if (e.dataTransfer.files.length === 0) return
    const imgs = Array.from(e.dataTransfer.files).filter(isImageFile)
    if (imgs.length > 0) {
      e.preventDefault()
      void ingestImages(imgs)
    }
  }

  const removePendingImage = (idx: number) => {
    setPendingImages((prev) => prev.filter((_, i) => i !== idx))
  }

  // —— 文件上传:抽文本到 source
  const onPickFiles = async (e: React.ChangeEvent<HTMLInputElement>) => {
    if (!e.target.files || e.target.files.length === 0) return
    const file = e.target.files[0]
    e.target.value = ''
    if (isImageFile(file)) {
      await dialog({ title: '不支持图片', message: '翻译目前只支持文本类附件', confirmLabel: '知道了' })
      return
    }
    try {
      const block = await fileToFileBlock(file)
      if (!block.text) {
        await dialog({
          title: '无法翻译',
          message: 'PDF 等二进制文件请先在 AI 问答中转文本,或上传 docx / 文本文件',
          confirmLabel: '知道了',
        })
        return
      }
      setSource(block.text)
      setPendingFile({ name: block.name, size: block.sizeBytes ?? 0 })
    } catch (err: any) {
      await dialog({ title: '读取文件失败', message: String(err?.message ?? err), confirmLabel: '知道了' })
    }
  }

  // —— 模型选择
  const onPickModel = async (providerId: string, modelId: string) => {
    setMany({ providerId, modelId })
    setPickerOpen(false)
  }

  // —— 源框键盘处理:Enter 翻译 / Shift+Enter 换行(默认) / Ctrl+Enter 换行(强制)
  const onSourceKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key !== 'Enter') return
    if (e.nativeEvent.isComposing) return // IME 拼音确认时不要触发
    if (e.shiftKey) return // Shift+Enter:走默认换行
    if (e.ctrlKey || e.metaKey) {
      // Ctrl/⌘+Enter:textarea 默认不会插换行,这里手动插一个
      e.preventDefault()
      const ta = e.currentTarget
      const start = ta.selectionStart
      const end = ta.selectionEnd
      setSource((cur) => cur.slice(0, start) + '\n' + cur.slice(end))
      requestAnimationFrame(() => {
        ta.selectionStart = ta.selectionEnd = start + 1
      })
      return
    }
    // 普通 Enter:翻译(流式中不响应,避免误触取消)
    e.preventDefault()
    if (!streaming) void onTranslate()
  }

  // —— 头部"源"卡片图标:auto 时不显示具体国旗(Flag 组件会画地球)
  const sourceFlagCode = s.sourceLang === AUTO_DETECT_ID ? undefined : findLang(s.sourceLang)?.country

  return (
    <div className="flex h-full flex-col">
      {/* 顶部工具栏 */}
      <header className="flex h-12 shrink-0 items-center gap-3 border-b border-border bg-card px-4">
        <Languages className="h-4 w-4 text-info" />
        <span className="text-sm font-medium">翻译</span>

        <div className="ml-3 flex items-center gap-2">
          <LanguageSelect
            value={s.sourceLang}
            onChange={(id) => setMany({ sourceLang: id })}
            showAuto
          />
          <button
            type="button"
            onClick={onSwap}
            disabled={s.sourceLang === AUTO_DETECT_ID}
            className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:opacity-40"
            title={s.sourceLang === AUTO_DETECT_ID ? '"自动"模式无法交换' : '交换源/目标语言'}
          >
            <ArrowLeftRight className="h-3.5 w-3.5" />
          </button>
          <LanguageSelect
            value={s.targetLang}
            onChange={(id) => setMany({ targetLang: id })}
          />
        </div>

        <div className="ml-auto flex items-center gap-1">
          <button
            type="button"
            onClick={() => setPickerOpen(true)}
            className="flex h-8 items-center gap-2 rounded-md border border-input bg-background px-2.5 text-xs transition-colors hover:bg-secondary"
            title="切换模型"
          >
            {provider ? (
              <ProviderAvatar logo={provider.logo} name={provider.name} size={16} />
            ) : (
              <span>—</span>
            )}
            <span className="max-w-[180px] truncate font-medium">
              {provider?.name ? `${provider.name} · ${s.modelId}` : '未选模型'}
            </span>
            <ChevronDown className="h-3 w-3 text-muted-foreground" />
          </button>
          <button
            type="button"
            onClick={() => setSettingsOpen(true)}
            className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            title="设置"
          >
            <Settings className="h-3.5 w-3.5" />
          </button>
          <button
            type="button"
            onClick={() => setHistoryOpen(true)}
            className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            title="历史"
          >
            <Clock className="h-3.5 w-3.5" />
          </button>
        </div>
      </header>

      {/* 双面板:两张等高卡片,中间留 gap,外层加微弱底色衬托 */}
      <div className="grid min-h-0 flex-1 grid-cols-2 gap-3 bg-secondary/20 p-3">
        {/* —— 源面板 —— */}
        <div
          className={cn(
            'relative flex min-h-0 flex-col overflow-hidden rounded-lg border bg-card shadow-sm transition-colors',
            dragOver ? 'border-info ring-1 ring-info' : 'border-border',
          )}
          onDragOver={(e) => {
            if (Array.from(e.dataTransfer.items).some((it) => it.kind === 'file')) {
              e.preventDefault()
              setDragOver(true)
            }
          }}
          onDragLeave={(e) => {
            if (e.currentTarget === e.target) setDragOver(false)
          }}
          onDrop={onSourceDrop}
        >
          {dragOver && (
            <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center bg-info/10 text-sm font-medium text-info">
              松开以添加图片
            </div>
          )}
          {/* 卡片头:语言徽标 + 字数 */}
          <div className="flex h-9 shrink-0 items-center gap-2 border-b border-border/60 px-3 text-xs">
            <Flag code={sourceFlagCode} />
            <span className="font-medium">
              {s.sourceLang === AUTO_DETECT_ID ? '源 · 自动检测' : `源 · ${findLang(s.sourceLang)?.label ?? s.sourceLang}`}
            </span>
            <div className="ml-auto flex items-center gap-2 text-muted-foreground">
              {detectedHint && (
                <span className="rounded bg-info/10 px-1.5 py-0.5 text-info">{detectedHint}</span>
              )}
              <span>{source.length} 字</span>
            </div>
          </div>

          {pendingFile && (
            <div className="flex items-center justify-between gap-2 border-b border-border/60 bg-secondary/30 px-3 py-1 text-[11px]">
              <div className="flex min-w-0 items-center gap-1.5 text-muted-foreground">
                <Paperclip className="h-3 w-3 shrink-0" />
                <span className="truncate" title={pendingFile.name}>
                  {pendingFile.name}
                </span>
                <span className="shrink-0">
                  {pendingFile.size > 0 ? `· ${formatFileSize(pendingFile.size)}` : ''}
                </span>
              </div>
              <button
                type="button"
                onClick={() => setPendingFile(null)}
                className="flex h-5 w-5 items-center justify-center rounded text-muted-foreground hover:bg-secondary hover:text-foreground"
              >
                <X className="h-3 w-3" />
              </button>
            </div>
          )}

          {pendingImages.length > 0 && (
            <div className="space-y-1.5 border-b border-border/60 bg-secondary/30 px-3 py-2">
              <div className="flex flex-wrap gap-2">
                {pendingImages.map((img, i) => (
                  <div key={i} className="group/thumb relative">
                    <img
                      src={imageSrc(img)}
                      alt=""
                      className="h-16 w-16 rounded-md border border-border object-cover"
                    />
                    <button
                      type="button"
                      onClick={() => removePendingImage(i)}
                      className="absolute -right-1.5 -top-1.5 flex h-4 w-4 items-center justify-center rounded-full bg-background/90 text-muted-foreground shadow ring-1 ring-border transition-colors hover:text-destructive"
                      title="移除"
                    >
                      <X className="h-3 w-3" />
                    </button>
                  </div>
                ))}
              </div>
              <div className="flex items-center gap-1 text-[11px] text-info">
                <ImageIcon className="h-3 w-3 shrink-0" />
                贴图翻译需选支持视觉的模型
              </div>
            </div>
          )}

          <textarea
            ref={sourceRef}
            value={source}
            onChange={(e) => setSource(e.target.value)}
            onKeyDown={onSourceKeyDown}
            onPaste={onSourcePaste}
            placeholder="输入文本,或 Ctrl+V 粘贴 / 拖入图片… (Enter 翻译 · Shift/Ctrl+Enter 换行)"
            className="min-h-0 flex-1 resize-none bg-transparent p-4 text-sm leading-relaxed outline-none"
          />

          {/* 卡片底:文件按钮 + 清空 + 翻译/停止 */}
          <div className="flex h-11 shrink-0 items-center justify-between gap-2 border-t border-border/60 px-3">
            <button
              type="button"
              onClick={() => fileInputRef.current?.click()}
              className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              title="上传文件(docx / xlsx / pptx / 文本 / 代码)"
            >
              <Paperclip className="h-3.5 w-3.5" />
            </button>
            <input
              ref={fileInputRef}
              type="file"
              accept=".pdf,.docx,.xlsx,.pptx,.txt,.md,.markdown,.csv,.tsv,.json,.yaml,.yml,.toml,.xml,.html,.css,.log,.go,.mod,.sum,.js,.jsx,.ts,.tsx,.mjs,.cjs,.py,.rb,.php,.rs,.c,.cc,.cpp,.h,.hpp,.java,.kt,.swift,.scala,.cs,.lua,.dart,.sh,.bash,.zsh,.ps1,.sql"
              hidden
              onChange={(e) => void onPickFiles(e)}
            />
            <div className="flex items-center gap-1">
              {(source || target || pendingImages.length > 0) && !streaming && (
                <Button onClick={onClear} size="sm" variant="ghost">
                  清空
                </Button>
              )}
              <Button
                onClick={() => (streaming ? void onStop() : void onTranslate())}
                disabled={!streaming && !source.trim() && pendingImages.length === 0}
                size="sm"
                variant={streaming ? 'outline' : 'default'}
                className="min-w-[88px]"
              >
                {streaming ? (
                  <>
                    <Square className="h-3 w-3" />
                    停止
                  </>
                ) : (
                  <>
                    <Send className="h-3 w-3" />
                    翻译
                  </>
                )}
              </Button>
            </div>
          </div>
        </div>

        {/* —— 目标面板 —— */}
        <div className="flex min-h-0 flex-col overflow-hidden rounded-lg border border-border bg-card shadow-sm">
          <div className="flex h-9 shrink-0 items-center gap-2 border-b border-border/60 px-3 text-xs">
            <Flag code={findLang(s.targetLang)?.country} />
            <span className="font-medium">
              译文 · {findLang(s.targetLang)?.label ?? s.targetLang}
            </span>
            <div className="ml-auto flex items-center gap-2 text-muted-foreground">
              {streaming && (
                <span className="flex items-center gap-1 text-info">
                  <Loader2 className="h-3 w-3 animate-spin" />
                  翻译中
                </span>
              )}
              <span>{target.length} 字</span>
            </div>
          </div>

          <div ref={targetRef} className="min-h-0 flex-1 overflow-auto p-4">
            {target ? (
              s.showMarkdown ? (
                <MarkdownPreview value={target} className="markdown-preview text-sm" />
              ) : (
                <pre className="whitespace-pre-wrap break-words font-sans text-sm leading-relaxed">
                  {target}
                </pre>
              )
            ) : streaming ? (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                正在翻译…
              </div>
            ) : (
              <div className="text-sm text-muted-foreground">翻译结果将显示在这里</div>
            )}
          </div>

          <div className="flex h-11 shrink-0 items-center justify-end gap-2 border-t border-border/60 px-3">
            <button
              type="button"
              onClick={onCopy}
              disabled={!target}
              className={cn(
                'flex h-7 items-center gap-1 rounded-md px-2.5 text-xs transition-colors disabled:opacity-40',
                copied
                  ? 'bg-success/15 text-success'
                  : 'text-muted-foreground hover:bg-secondary hover:text-foreground',
              )}
            >
              {copied ? (
                <>
                  <Check className="h-3 w-3" />
                  已复制
                </>
              ) : (
                <>
                  <Copy className="h-3 w-3" />
                  复制
                </>
              )}
            </button>
          </div>
        </div>
      </div>

      {pickerOpen && (
        <ChatModelPicker
          current={{ providerId: s.providerId, modelId: s.modelId }}
          onClose={() => setPickerOpen(false)}
          onPick={(pid, mid) => void onPickModel(pid, mid)}
        />
      )}

      {settingsOpen && (
        <SettingsDialog
          onClose={() => setSettingsOpen(false)}
          onOpenMore={() => setMoreOpen(true)}
        />
      )}

      {moreOpen && <MoreSettingsDialog onClose={() => setMoreOpen(false)} />}

      {historyOpen && (
        <HistoryDrawer
          onClose={() => setHistoryOpen(false)}
          onRestore={(item) => {
            setSource(item.source)
            setTarget(item.target)
            setMany({
              sourceLang: item.sourceLangId,
              targetLang: item.targetLangId,
            })
            setHistoryOpen(false)
          }}
        />
      )}
    </div>
  )
}
