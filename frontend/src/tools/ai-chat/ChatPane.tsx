import { useEffect, useRef, useState } from 'react'
import { ChevronDown, Settings2 } from 'lucide-react'
import {
  DeleteAIChatMessage,
  EditAndResendAIChat,
  GetAIConversation,
  GetAIModelSpec,
  InsertAIClearMarker,
  ListAIProviders,
  RegenerateAILastChat,
  SendAIChat,
  StopAIChat,
  UpdateAIConversationMeta,
  UpdateAIConversationModel,
  UpdateAIConversationOptions,
} from '../../../wailsjs/go/main/App'
import {
  thinkingText,
  type Conversation,
  type FileBlock,
  type ImageBlock,
  type Message,
  type ModelSpec,
  type Provider,
  type ReasoningEffort,
} from './types'
import { useConfirm } from '@/components/ui/confirm'
import { ChatModelPicker } from './ChatModelPicker'
import { ConversationDialog, type ConversationDraft } from './ConversationDialog'
import { ChatComposer } from './ChatComposer'
import { ClearDivider, MessageItem } from './ChatMessage'
import { WelcomeScreen } from './ChatWelcome'
import { FilePreviewModal, ImagePreviewModal } from './ChatPreviews'
import { pickFirst, pickSecond } from './chat-utils'
import { useChatStream } from './useChatStream'
import { useAttachments } from './useAttachments'
import { cn } from '@/lib/utils'

interface Props {
  conversationId: string
  onTitleChange: () => void
}

export function ChatPane({ conversationId, onTitleChange }: Props) {
  const dialog = useConfirm()
  const [conv, setConv] = useState<Conversation | null>(null)
  const [providers, setProviders] = useState<Provider[]>([])
  const [draft, setDraft] = useState('')
  const [streaming, setStreaming] = useState(false)
  const [pickerOpen, setPickerOpen] = useState(false)
  const [systemOpen, setSystemOpen] = useState(false)
  const [previewImage, setPreviewImage] = useState<ImageBlock | null>(null)
  const [previewFile, setPreviewFile] = useState<FileBlock | null>(null)
  // 当前模型的能力画像:决定输入栏上给不给思考档位、联网开关
  const [spec, setSpec] = useState<ModelSpec | null>(null)
  const [effortOpen, setEffortOpen] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  // 待发送附件(选文件 / 粘贴 / 拖入三条入口的收集与校验)
  const attach = useAttachments({
    spec,
    modelId: conv?.modelId ?? '',
    onError: (title, message) => dialog({ title, message, confirmLabel: '知道了' }),
  })

  const reloadProviders = async () => {
    const list = ((await ListAIProviders()) ?? []) as unknown as Provider[]
    setProviders(list)
  }

  const load = async () => {
    const r = (await GetAIConversation(conversationId)) as any
    const c = pickFirst<Conversation>(r)
    if (c?.id) setConv(c)
  }

  useEffect(() => {
    setConv(null)
    void load()
    void reloadProviders()
  }, [conversationId])

  // 自动跟随到底部 — 但用户主动往上滑就停;再滑回底部就恢复跟随
  const [stickToBottom, setStickToBottom] = useState(true)
  // 区分"用户滚动"与"我们 setScrollTop 引起的滚动",后者不应改变 stick 状态
  const programmaticScrollRef = useRef(false)

  const lastMsg = conv?.messages[conv.messages.length - 1]
  const lastLen =
    (lastMsg?.content.length ?? 0) + (lastMsg ? thinkingText(lastMsg).length : 0)
  useEffect(() => {
    const el = scrollRef.current
    if (!el || !stickToBottom) return
    programmaticScrollRef.current = true
    el.scrollTop = el.scrollHeight
    // 下一帧解锁,避免误把这次滚动当成用户行为
    requestAnimationFrame(() => {
      programmaticScrollRef.current = false
    })
  }, [conv?.messages.length, lastLen, stickToBottom])

  const onScroll = (e: React.UIEvent<HTMLDivElement>) => {
    if (programmaticScrollRef.current) return
    const el = e.currentTarget
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight
    setStickToBottom(distanceFromBottom < 40)
  }

  const jumpToBottom = () => {
    setStickToBottom(true)
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }

  // 换会话或换模型时重新拉一次能力画像(后端按模型 ID 推断,很便宜)
  useEffect(() => {
    const providerId = conv?.providerId
    const modelId = conv?.modelId
    if (!providerId || !modelId) {
      setSpec(null)
      return
    }
    let alive = true
    void (async () => {
      const r = (await GetAIModelSpec(providerId, modelId)) as any
      if (!alive) return
      const got = pickFirst(r) as ModelSpec | null
      setSpec(got && got.id ? got : null)
    })()
    return () => {
      alive = false
    }
  }, [conv?.providerId, conv?.modelId])

  // 流事件订阅(chunk / thinking / citation / image / done / error)
  useChatStream({
    conversationId,
    setConv,
    onStreamEnd: () => setStreaming(false),
    onDone: onTitleChange,
    onError: (err) => {
      void dialog({ title: '请求失败', message: err || '未知错误', confirmLabel: '知道了' })
      void load()
    },
  })

  const provider = providers.find((p) => p.id === conv?.providerId) ?? null

  const onSend = async (override?: string) => {
    const content = (override ?? draft).trim()
    if (
      (!content && attach.count === 0) ||
      streaming ||
      !conv
    )
      return
    const imagesToSend = attach.pendingImages
    const filesToSend = attach.pendingFiles
    setDraft('')
    attach.clear()
    setStreaming(true)

    // 乐观更新:先把 user + assistant 占位放进本地状态,
    // 这样后端 goroutine 立刻发的 chunk 一定能找到对的 last 消息(避免 race)
    const now = Date.now()
    const tmpUser: Message = {
      id: 'tmp-u-' + now,
      role: 'user',
      content,
      images: imagesToSend.length > 0 ? imagesToSend : undefined,
      files: filesToSend.length > 0 ? filesToSend : undefined,
      createdAt: now,
    }
    const tmpAsst: Message = {
      id: 'tmp-a-' + now,
      role: 'assistant',
      content: '',
      thinking: [],
      model: conv.modelId,
      createdAt: now + 1,
    }
    setConv((prev) =>
      prev ? { ...prev, messages: [...prev.messages, tmpUser, tmpAsst] } : prev,
    )

    const r = (await SendAIChat(conv.id, content, imagesToSend, filesToSend)) as any
    const err = pickSecond(r)
    if (err) {
      setStreaming(false)
      // 回滚乐观更新
      setConv((prev) =>
        prev
          ? {
              ...prev,
              messages: prev.messages.filter(
                (m) => m.id !== tmpUser.id && m.id !== tmpAsst.id,
              ),
            }
          : prev,
      )
      // 失败也把附件塞回去,避免用户刚拖了文件就丢了
      attach.restore(imagesToSend, filesToSend)
      await dialog({ title: '发送失败', message: err, confirmLabel: '知道了' })
      return
    }
    // 不 setConv(next) — 保留乐观状态,避免覆盖期间到达的 chunk;
    // 流结束时 onDone 会用最终内容兜底纠正
    onTitleChange()
  }

  const onStop = async () => {
    if (!conv) return
    await StopAIChat(conv.id)
  }

  const onEditResend = async (msgId: string, newContent: string) => {
    if (!conv || streaming) return
    setStreaming(true)
    // 乐观更新:截断该消息之后的所有,改写其内容,并加占位 assistant
    setConv((prev) => {
      if (!prev) return prev
      const idx = prev.messages.findIndex((m) => m.id === msgId)
      if (idx < 0) return prev
      const kept = prev.messages.slice(0, idx + 1).map((m) =>
        m.id === msgId ? { ...m, content: newContent } : m,
      )
      const tmpAsst: Message = {
        id: 'tmp-edit-' + Date.now(),
        role: 'assistant',
        content: '',
        thinking: [],
        model: prev.modelId,
        createdAt: Date.now(),
      }
      return { ...prev, messages: [...kept, tmpAsst] }
    })
    const r = (await EditAndResendAIChat(conv.id, msgId, newContent)) as any
    const err = pickSecond(r)
    if (err) {
      setStreaming(false)
      await dialog({ title: '重新发送失败', message: err, confirmLabel: '知道了' })
      void load()
    }
  }

  const onRegenerate = async () => {
    if (!conv || streaming) return
    setStreaming(true)
    // 乐观更新:把最后一条 assistant 的 content / thinking 清空,前端立刻进入"思考中"
    setConv((prev) => {
      if (!prev) return prev
      const msgs = [...prev.messages]
      const last = msgs[msgs.length - 1]
      if (last?.role === 'assistant') {
        msgs[msgs.length - 1] = {
          ...last,
          content: '',
          thinking: [],
          citations: undefined,
          toolCalls: undefined,
          model: prev.modelId,
        }
      }
      return { ...prev, messages: msgs }
    })
    const r = (await RegenerateAILastChat(conv.id)) as any
    const err = pickSecond(r)
    if (err) {
      setStreaming(false)
      await dialog({ title: '重新生成失败', message: err, confirmLabel: '知道了' })
      void load()
    }
  }

  const onSaveMeta = async (draft: ConversationDraft) => {
    if (!conv) return
    const err =
      ((await UpdateAIConversationMeta(conv.id, {
        title: draft.title || conv.title,
        system: draft.system,
        contextCount: draft.contextCount,
        temperature: draft.temperature,
        topP: draft.topP,
        maxTokens: draft.maxTokens ?? 0,
      } as never)) as string) || ''
    if (err) {
      await dialog({ title: '保存失败', message: err, confirmLabel: '知道了' })
      return
    }
    setConv((prev) =>
      prev
        ? {
            ...prev,
            title: draft.title || prev.title,
            system: draft.system,
            contextCount: draft.contextCount,
            temperature: draft.temperature,
            topP: draft.topP,
            maxTokens: draft.maxTokens ?? 0,
          }
        : prev,
    )
    setSystemOpen(false)
    onTitleChange()
  }

  const onDeleteMessage = async (msgId: string) => {
    if (!conv) return
    const ok = await dialog({
      title: '删除消息',
      message: '确认删除这条消息?该操作不可撤销。',
      danger: true,
      confirmLabel: '删除',
    })
    if (!ok) return
    // 乐观删除
    setConv((prev) =>
      prev ? { ...prev, messages: prev.messages.filter((m) => m.id !== msgId) } : prev,
    )
    const err = ((await DeleteAIChatMessage(conv.id, msgId)) as string) || ''
    if (err) {
      await dialog({ title: '删除失败', message: err, confirmLabel: '知道了' })
      void load() // 失败时回查后端真实状态
    }
  }

  const onClearContext = async () => {
    if (!conv || streaming) return
    const err = ((await InsertAIClearMarker(conv.id)) as string) || ''
    if (err) {
      await dialog({ title: '操作失败', message: err, confirmLabel: '知道了' })
      return
    }
    void load()
  }

  const onPickModel = async (providerId: string, modelId: string) => {
    if (!conv) return
    setPickerOpen(false)
    await UpdateAIConversationModel(conv.id, providerId, modelId)
    setConv({ ...conv, providerId, modelId })
    textareaRef.current?.focus()
  }

  if (!conv) {
    return (
      <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
        加载中...
      </div>
    )
  }

  // 输入栏上的两个开关只在模型真的支持时出现 —— 给一个发出去必然被拒的开关不如不给
  const effortOptions = (spec?.reasoning?.efforts ?? []) as ReasoningEffort[]
  const canTuneReasoning = effortOptions.length > 0
  const canWebSearch = !!spec?.capabilities?.includes('webSearch')
  const currentEffort = (conv.reasoningEffort || 'default') as ReasoningEffort

  const canUseTools = !!spec?.capabilities?.includes('tools')

  const setOptions = async (effort: string, webSearch: boolean, tools: boolean) => {
    setConv((prev) => (prev ? { ...prev, reasoningEffort: effort, webSearch, tools } : prev))
    await UpdateAIConversationOptions(conv.id, effort, webSearch, tools)
  }

  const visibleMessages = conv.messages.filter((m) => m.role !== 'system')
  const isEmpty = visibleMessages.length === 0
  const lastVisible = visibleMessages[visibleMessages.length - 1]

  return (
    <div className="relative flex h-full min-w-0 flex-1 flex-col">
      <header className="flex h-12 shrink-0 items-center gap-3 border-b border-border bg-card px-4">
        <h3 className="min-w-0 flex-1 truncate text-sm font-semibold">{conv.title}</h3>
        <button
          type="button"
          onClick={() => setSystemOpen(true)}
          className={cn(
            'flex h-7 items-center gap-1.5 rounded-md border px-2 text-xs transition-colors',
            conv.system
              ? 'border-info/40 bg-info/10 text-info'
              : 'border-border text-muted-foreground hover:bg-secondary hover:text-foreground',
          )}
          title="编辑会话(标题/系统提示/上下文)"
        >
          <Settings2 className="h-3.5 w-3.5" />
          会话设置
          {conv.system && <span className="ml-0.5 h-1.5 w-1.5 rounded-full bg-info" />}
        </button>
      </header>

      <div
        ref={scrollRef}
        onScroll={onScroll}
        className="relative min-h-0 flex-1 overflow-auto bg-background"
      >
        {isEmpty ? (
          <WelcomeScreen
            providerName={provider?.name ?? ''}
            modelId={conv.modelId}
            onPick={(s) => {
              setDraft(s)
              textareaRef.current?.focus()
            }}
          />
        ) : (
          <ul className="mx-auto max-w-3xl space-y-6 px-4 py-6">
            {visibleMessages.map((m) => {
              if (m.role === 'clear') {
                return (
                  <ClearDivider
                    key={m.id}
                    onDelete={!streaming ? () => void onDeleteMessage(m.id) : undefined}
                  />
                )
              }
              const isLast = m === lastVisible
              const isAssistant = m.role === 'assistant'
              const canMutate = !streaming && !(streaming && isLast && isAssistant)
              return (
                <MessageItem
                  key={m.id}
                  message={m}
                  fallbackModel={conv.modelId}
                  streaming={streaming && isLast && isAssistant}
                  onRegenerate={
                    isLast && isAssistant && !streaming ? onRegenerate : undefined
                  }
                  onEditResend={
                    m.role === 'user' && !streaming
                      ? (newContent) => void onEditResend(m.id, newContent)
                      : undefined
                  }
                  onDelete={canMutate ? () => void onDeleteMessage(m.id) : undefined}
                  onPreviewImage={setPreviewImage}
                  onPreviewFile={setPreviewFile}
                />
              )
            })}
          </ul>
        )}
      </div>

      {!stickToBottom && !isEmpty && (
        <button
          type="button"
          onClick={jumpToBottom}
          className="pointer-events-auto absolute bottom-[148px] left-1/2 z-10 flex h-8 -translate-x-1/2 items-center gap-1.5 rounded-full border border-border bg-card px-3 text-xs text-muted-foreground shadow-lg transition-colors hover:bg-secondary hover:text-foreground"
          title="滚动到最新"
        >
          <ChevronDown className="h-3.5 w-3.5" />
          回到最新
        </button>
      )}

      <ChatComposer
        draft={draft}
        setDraft={setDraft}
        streaming={streaming}
        isEmpty={isEmpty}
        attach={attach}
        spec={spec}
        provider={provider}
        modelId={conv.modelId}
        currentEffort={currentEffort}
        effortOptions={effortOptions}
        canTuneReasoning={canTuneReasoning}
        canWebSearch={canWebSearch}
        canUseTools={canUseTools}
        webSearch={!!conv.webSearch}
        toolsOn={!!conv.tools}
        textareaRef={textareaRef}
        onSend={() => void onSend()}
        onStop={() => void onStop()}
        onClearContext={() => void onClearContext()}
        onOpenPicker={() => setPickerOpen(true)}
        onSetOptions={(effort: string, web: boolean, tools: boolean) =>
          void setOptions(effort, web, tools)
        }
        onPreviewImage={setPreviewImage}
        onPreviewFile={setPreviewFile}
      />

      {pickerOpen && (
        <ChatModelPicker
          current={{ providerId: conv.providerId, modelId: conv.modelId }}
          onClose={() => setPickerOpen(false)}
          onPick={(pid, mid) => void onPickModel(pid, mid)}
        />
      )}

      {previewImage && (
        <ImagePreviewModal img={previewImage} onClose={() => setPreviewImage(null)} />
      )}

      {previewFile && (
        <FilePreviewModal file={previewFile} onClose={() => setPreviewFile(null)} />
      )}

      {systemOpen && (
        <ConversationDialog
          mode="edit"
          spec={spec}
          initial={{
            title: conv.title,
            system: conv.system ?? '',
            contextCount: conv.contextCount ?? 0,
            temperature: conv.temperature,
            topP: conv.topP,
            maxTokens: conv.maxTokens ?? 0,
          }}
          onClose={() => setSystemOpen(false)}
          onSave={(d) => void onSaveMeta(d)}
        />
      )}
    </div>
  )
}

