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
import { ChatPane } from './ChatPane'

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
    | { mode: 'create'; initial: ConversationDraft }
    | { mode: 'edit'; convId: string; initial: ConversationDraft }
    | null
  >(null)

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

  const onNewConversation = () => {
    if (usable.length === 0) {
      void dialog({
        title: '没有可用模型',
        message: '请先到「个人中心 → AI 配置」启用至少一个供应商并选择模型',
        confirmLabel: '去配置',
      }).then(() => goConfig())
      return
    }
    setDialogState({
      mode: 'create',
      initial: { title: '', system: '', contextCount: 10 },
    })
  }

  const onEditConversation = async (id: string) => {
    const r = (await GetAIConversation(id)) as any
    const conv =
      (Array.isArray(r) ? r[0] : r?.['0'] ?? (r && 'id' in r ? r : null)) as
        | Conversation
        | null
    const err = (Array.isArray(r) ? r[1] : r?.['1']) as string | undefined
    if (err || !conv) {
      await dialog({ title: '加载失败', message: err ?? '未知错误', confirmLabel: '知道了' })
      return
    }
    setDialogState({
      mode: 'edit',
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
    if (dialogState.mode === 'create') {
      let providerId = defaults.defaultProviderId
      let modelId = defaults.defaultModelId
      const def = usable.find((p) => p.id === providerId)
      if (!def || !def.models.includes(modelId)) {
        providerId = usable[0].id
        modelId = usable[0].models[0]
      }
      const r = (await CreateAIConversation(
        providerId,
        modelId,
        draft.title,
        draft.system,
        draft.contextCount,
      )) as any
      const created =
        (Array.isArray(r) ? r[0] : r?.['0'] ?? (r && 'id' in r ? r : null)) as
          | Conversation
          | null
      const err = (Array.isArray(r) ? r[1] : r?.['1']) as string | undefined
      if (err) {
        await dialog({ title: '创建失败', message: err, confirmLabel: '知道了' })
        return
      }
      setDialogState(null)
      await reloadAll()
      if (created?.id) setActiveId(created.id)
    } else {
      const err =
        ((await UpdateAIConversationMeta(dialogState.convId, {
          title: draft.title || '新对话',
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
      setDialogState(null)
      await reloadAll()
    }
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
            onNew={onNewConversation}
            onDelete={onDelete}
            onEdit={(id) => void onEditConversation(id)}
            onReorder={(ids) => void onReorder(ids)}
          />
          {activeId ? (
            <ChatPane
              key={activeId}
              conversationId={activeId}
              onTitleChange={() => void reloadAll()}
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
          mode={dialogState.mode}
          initial={dialogState.initial}
          onClose={() => setDialogState(null)}
          onSave={(d) => void onDialogSave(d)}
        />
      )}
    </div>
  )
}
