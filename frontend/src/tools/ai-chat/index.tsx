import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { MessagesSquare, Settings as SettingsIcon } from 'lucide-react'
import {
  ListAIProviders,
  ListAIConversations,
  CreateAIConversation,
  DeleteAIConversation,
  GetAIConversation,
  GetAIConfig,
  ReorderAIConversations,
  UpdateAIConversationMeta,
  UpdateAIConversationOptions,
} from '../../../wailsjs/go/main/App'
import type {
  AIConfig,
  Conversation,
  ConversationSummary,
  Provider,
} from './types'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { ConversationList } from './ConversationList'
import { ConversationDialog, type ConversationDraft } from './ConversationDialog'
import { ExportDialog } from './ExportDialog'
import { ChatPane } from './ChatPane'

/** 新会话默认带多少条历史给模型。跟以前新建弹窗里的默认值保持一致 */
const DEFAULT_CONTEXT_COUNT = 10

export default function AIChat() {
  const navigate = useNavigate()
  const dialog = useConfirm()
  const [providers, setProviders] = useState<Provider[]>([])
  const [conversations, setConversations] = useState<ConversationSummary[]>([])
  const [activeId, setActiveId] = useState('')
  const [defaults, setDefaults] = useState<AIConfig>({
    defaultProviderId: '',
    defaultModelId: '',
  })
  const [dialogState, setDialogState] = useState<
    { convId: string; initial: ConversationDraft } | null
  >(null)
  // 编辑弹窗保存后要让正在显示的 ChatPane 重新读一次盘。
  // 用计数器而不是给 ChatPane 换 key:换 key 是整块重挂,输入框里的草稿和
  // 滚动位置全没了 —— 改个系统提示词不该有这种代价
  const [convRev, setConvRev] = useState(0)
  // 要导出的会话。放在页面级而不是 ChatPane 里:正文区的按钮和侧边栏右键
  // 是同一件事的两个入口,各自持一份状态迟早会不一致
  const [exportId, setExportId] = useState('')

  const reloadAll = async () => {
    const [provList, convList, cfg] = await Promise.all([
      ListAIProviders(),
      ListAIConversations(),
      GetAIConfig(),
    ])
    setProviders(((provList ?? []) as unknown) as Provider[])
    setConversations(((convList ?? []) as unknown) as ConversationSummary[])
    setDefaults(cfg as unknown as AIConfig)
  }

  useEffect(() => {
    void reloadAll()
  }, [])

  // 自动选第一个会话
  useEffect(() => {
    if (!activeId && conversations.length > 0) {
      setActiveId(conversations[0].id)
    } else if (activeId && !conversations.find((c) => c.id === activeId)) {
      setActiveId(conversations[0]?.id ?? '')
    }
  }, [conversations, activeId])

  const usable = providers.filter((p) => p.enabled && p.models.length > 0)

  const goConfig = () => navigate('/profile', { state: { section: 'ai' } })

  /**
   * 新建对话:直接开一条空的「新对话」,不弹窗。
   *
   * 以前先弹一个要填标题和提示词的表单 —— 可绝大多数时候用户只是想立刻开始问,
   * 标题首轮答完会自动生成,人设想设的话进「会话设置」随时能改。
   * 把这一步挪到事后,常见路径就少了一次打断。
   */
  const onNewConversation = async () => {
    if (usable.length === 0) {
      await dialog({
        title: '没有可用模型',
        message: '请先到「个人中心 → AI 配置」启用至少一个供应商并选择模型',
        confirmLabel: '去配置',
      })
      goConfig()
      return
    }
    // 已经停在一条一句话都没说的会话上就不再建 ——
    // 连点几下"新建"不该在侧边栏攒出一串一模一样的空「新对话」
    const active = conversations.find((c) => c.id === activeId)
    if (active && active.messageCount === 0) return

    let providerId = defaults.defaultProviderId
    let modelId = defaults.defaultModelId
    const def = usable.find((p) => p.id === providerId)
    if (!def || !def.models.includes(modelId)) {
      providerId = usable[0].id
      modelId = usable[0].models[0]
    }
    try {
      // 标题传空串:后端据此把 titleAuto 置上,首轮答完才轮到模型起名
      const created = (await CreateAIConversation(
        providerId,
        modelId,
        '',
        '',
        DEFAULT_CONTEXT_COUNT,
      )) as unknown as Conversation
      await reloadAll()
      if (created?.id) setActiveId(created.id)
    } catch (e) {
      await dialog({ title: '创建失败', message: String(e), confirmLabel: '知道了' })
    }
  }

  const onEditConversation = async (id: string) => {
    let conv: Conversation
    try {
      conv = (await GetAIConversation(id)) as unknown as Conversation
    } catch (e) {
      await dialog({ title: '加载失败', message: String(e), confirmLabel: '知道了' })
      return
    }
    setDialogState({
      convId: id,
      initial: {
        title: conv.title,
        system: conv.system ?? '',
        contextCount: conv.contextCount ?? 0,
        temperature: conv.temperature,
        topP: conv.topP,
        maxTokens: conv.maxTokens ?? 0,
      },
    })
  }

  const onDialogSave = async (draft: ConversationDraft) => {
    if (!dialogState) return
    const { convId } = dialogState
    // 标题原样传:清空了就是"交回给自动起名",后端见到空串不动标题也不动 titleAuto
    const err =
      ((await UpdateAIConversationMeta(convId, {
        title: draft.title,
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
    // 只有真套了预设才动这三个开关 —— 没套预设时它们是 undefined,
    // 照着写会把用户在输入栏上开着的联网悄悄关掉
    if (draft.preset) {
      await UpdateAIConversationOptions(
        convId,
        draft.preset.reasoningEffort,
        draft.preset.webSearch,
        draft.preset.tools,
      ).catch(() => {})
    }
    setDialogState(null)
    await reloadAll()
    if (convId === activeId) setConvRev((n) => n + 1)
  }

  // 拖动排序:先动本地(拖放要跟手),再落盘
  const onReorder = async (ids: string[]) => {
    const byId = new Map(conversations.map((c) => [c.id, c]))
    setConversations(ids.map((id) => byId.get(id)!).filter(Boolean))
    try {
      await ReorderAIConversations(ids)
    } catch (e) {
      await dialog({ title: '排序保存失败', message: String(e), confirmLabel: '知道了' })
      await reloadAll()
    }
  }

  const onDelete = async (id: string) => {
    const err = (await DeleteAIConversation(id)) as unknown as string
    if (err) {
      await dialog({ title: '删除失败', message: err, confirmLabel: '知道了' })
      return
    }
    if (activeId === id) setActiveId('')
    await reloadAll()
  }

  const empty = usable.length === 0

  return (
    <div className="flex h-full flex-col">
      <header className="flex h-12 shrink-0 items-center justify-between border-b border-border bg-card px-4">
        <div className="flex items-center gap-2">
          <MessagesSquare className="h-4 w-4 text-info" />
          <span className="text-sm font-medium">AI 问答</span>
        </div>
        <Button size="sm" variant="ghost" onClick={goConfig} title="去个人中心 → AI 配置">
          <SettingsIcon className="h-4 w-4" />
          配置
        </Button>
      </header>

      {empty ? (
        <div className="flex flex-1 items-center justify-center">
          <div className="flex max-w-md flex-col items-center gap-4 text-center">
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-info/15 text-info">
              <MessagesSquare className="h-6 w-6" />
            </div>
            <div>
              <h2 className="text-base font-semibold">还没有可用的 AI 供应商</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                请到「个人中心 → AI 配置」添加供应商,
                <br />
                填入 API Key,选择模型
              </p>
            </div>
            <Button onClick={goConfig}>
              <SettingsIcon className="h-4 w-4" />
              去配置
            </Button>
          </div>
        </div>
      ) : (
        <div className="flex min-h-0 flex-1">
          <ConversationList
            list={conversations}
            activeId={activeId}
            onSelect={setActiveId}
            onNew={() => void onNewConversation()}
            onDelete={onDelete}
            onEdit={(id) => void onEditConversation(id)}
            onExport={setExportId}
            onReorder={(ids) => void onReorder(ids)}
          />
          {activeId ? (
            <ChatPane
              key={activeId}
              conversationId={activeId}
              onTitleChange={() => void reloadAll()}
              onExport={() => setExportId(activeId)}
              refreshToken={convRev}
              onForked={(id) => {
                void reloadAll().then(() => setActiveId(id))
              }}
            />
          ) : (
            <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
              选一个对话,或点左上「新建对话」
            </div>
          )}
        </div>
      )}

      {dialogState && (
        <ConversationDialog
          initial={dialogState.initial}
          onClose={() => setDialogState(null)}
          onSave={(d) => void onDialogSave(d)}
        />
      )}

      {exportId && (
        <ExportDialog
          conversationId={exportId}
          title={conversations.find((c) => c.id === exportId)?.title ?? '会话'}
          onClose={() => setExportId('')}
          onError={(m) =>
            void dialog({ title: '导出失败', message: m, confirmLabel: '知道了' })
          }
        />
      )}
    </div>
  )
}
