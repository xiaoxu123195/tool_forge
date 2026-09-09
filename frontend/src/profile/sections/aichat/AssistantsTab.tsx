import { useEffect, useState } from 'react'
import { GripVertical, Plus, Save, Trash2, X } from 'lucide-react'
import {
  DeleteAIAssistant,
  ListAIAssistants,
  ReorderAIAssistants,
  SaveAIAssistant,
} from '../../../../wailsjs/go/main/App'
import type { Assistant } from '@/tools/ai-chat/types'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { MarkdownPreview } from '@/components/tool/MarkdownPreview'
import { cn } from '@/lib/utils'

/**
 * 助手预设:一套可复用的「系统提示词 + 参数」。
 *
 * 以前系统提示词只能一条会话一条会话地填,想重复用同一个角色只能来回复制粘贴。
 * 这里存一份,新建会话时挑一个就带过去。
 */
export function AssistantsTab() {
  const dialog = useConfirm()
  const [list, setList] = useState<Assistant[]>([])
  const [activeId, setActiveId] = useState('')
  const [dragId, setDragId] = useState('')
  const [overId, setOverId] = useState('')

  const reload = async (preferId?: string) => {
    const l = ((await ListAIAssistants().catch(() => [])) ?? []) as unknown as Assistant[]
    setList(l)
    setActiveId((prev) => {
      const target = preferId ?? prev
      if (target && l.find((a) => a.id === target)) return target
      return l[0]?.id ?? ''
    })
  }

  useEffect(() => {
    void reload()
  }, [])

  const active = list.find((a) => a.id === activeId) ?? null

  const onNew = async () => {
    try {
      const created = (await SaveAIAssistant({
        id: '',
        name: '新预设',
        emoji: '🤖',
        system: '',
        createdAt: 0,
        updatedAt: 0,
      } as unknown as never)) as unknown as Assistant
      await reload(created?.id)
    } catch (e) {
      await dialog({ title: '创建失败', message: String(e), confirmLabel: '知道了' })
    }
  }

  const onDelete = async (a: Assistant) => {
    const ok = await dialog({
      title: '删除预设',
      message: `确认删除「${a.name}」?已经用它建的会话不受影响。`,
      danger: true,
      confirmLabel: '删除',
    })
    if (!ok) return
    try {
      await DeleteAIAssistant(a.id)
      await reload()
    } catch (e) {
      await dialog({ title: '删除失败', message: String(e), confirmLabel: '知道了' })
    }
  }

  const onDrop = async (targetId: string) => {
    const from = list.findIndex((a) => a.id === dragId)
    const to = list.findIndex((a) => a.id === targetId)
    setDragId('')
    setOverId('')
    if (from < 0 || to < 0 || from === to) return
    const next = [...list]
    const [moved] = next.splice(from, 1)
    next.splice(to, 0, moved)
    setList(next)
    await ReorderAIAssistants(next.map((a) => a.id)).catch(() => reload())
  }

  return (
    <div className="flex min-h-0 flex-1 overflow-hidden rounded-lg border border-border bg-card">
      <aside className="flex w-[220px] shrink-0 flex-col border-r border-border">
        <ul className="flex-1 overflow-auto p-2">
          {list.length === 0 ? (
            <li className="px-3 py-6 text-center text-xs text-muted-foreground">
              还没有预设,点下面「新建」
            </li>
          ) : (
            list.map((a) => (
              <li
                key={a.id}
                draggable
                onDragStart={() => setDragId(a.id)}
                onDragEnd={() => {
                  setDragId('')
                  setOverId('')
                }}
                onDragOver={(e) => {
                  if (!dragId) return
                  e.preventDefault()
                  if (overId !== a.id) setOverId(a.id)
                }}
                onDrop={(e) => {
                  e.preventDefault()
                  void onDrop(a.id)
                }}
                onClick={() => setActiveId(a.id)}
                className={cn(
                  'group/a mb-1 flex h-10 cursor-pointer items-center gap-1.5 rounded-md px-2 text-sm transition-colors',
                  activeId === a.id ? 'bg-info/15 text-info' : 'hover:bg-secondary',
                  dragId === a.id && 'opacity-40',
                  overId === a.id && dragId && dragId !== a.id && 'ring-1 ring-info',
                )}
              >
                <GripVertical className="h-3.5 w-3.5 shrink-0 cursor-grab text-muted-foreground opacity-0 transition-opacity group-hover/a:opacity-100" />
                <span className="shrink-0 text-base leading-none">{a.emoji || '🤖'}</span>
                <span className="min-w-0 flex-1 truncate">{a.name}</span>
              </li>
            ))
          )}
        </ul>
        <div className="border-t border-border p-2">
          <Button onClick={() => void onNew()} variant="outline" size="sm" className="w-full">
            <Plus className="h-3.5 w-3.5" />
            新建预设
          </Button>
        </div>
      </aside>

      <div className="min-w-0 flex-1 overflow-auto">
        {active ? (
          <AssistantEditor
            key={active.id}
            initial={active}
            onSaved={(a) => void reload(a.id)}
            onDelete={() => void onDelete(active)}
          />
        ) : (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            从左侧选一个预设,或点「新建预设」
          </div>
        )}
      </div>
    </div>
  )
}

function AssistantEditor({
  initial,
  onSaved,
  onDelete,
}: {
  initial: Assistant
  onSaved: (a: Assistant) => void
  onDelete: () => void
}) {
  const dialog = useConfirm()
  const [name, setName] = useState(initial.name)
  const [emoji, setEmoji] = useState(initial.emoji ?? '')
  const [system, setSystem] = useState(initial.system)
  const [view, setView] = useState<'edit' | 'preview'>('edit')
  const [saving, setSaving] = useState(false)
  const [flash, setFlash] = useState(false)

  const dirty =
    name !== initial.name || (emoji || '') !== (initial.emoji ?? '') || system !== initial.system

  const onSave = async () => {
    setSaving(true)
    try {
      const saved = (await SaveAIAssistant({
        ...initial,
        name: name.trim() || '未命名',
        emoji: emoji.trim(),
        system,
      } as unknown as never)) as unknown as Assistant
      setFlash(true)
      setTimeout(() => setFlash(false), 1500)
      onSaved(saved)
    } catch (e) {
      await dialog({ title: '保存失败', message: String(e), confirmLabel: '知道了' })
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex h-full flex-col gap-3 p-4">
      <div className="flex items-center gap-2">
        <input
          value={emoji}
          onChange={(e) => setEmoji(e.target.value)}
          placeholder="🤖"
          maxLength={4}
          className="h-9 w-12 rounded-md border border-input bg-background text-center text-lg outline-none focus:ring-1 focus:ring-ring"
        />
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="预设名称"
          className="h-9 min-w-0 flex-1 rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring"
        />
        <Button variant="ghost" size="sm" onClick={onDelete} title="删除这个预设">
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      </div>

      <div className="flex items-center justify-between">
        <label className="text-xs font-medium text-muted-foreground">
          系统提示词 · {system.length} 字符
        </label>
        <div className="flex overflow-hidden rounded-md border border-border text-[11px]">
          {(['edit', 'preview'] as const).map((m) => (
            <button
              key={m}
              type="button"
              onClick={() => setView(m)}
              className={cn(
                'px-2 py-0.5 transition-colors',
                view === m ? 'bg-info/15 text-info' : 'text-muted-foreground hover:bg-secondary',
              )}
            >
              {m === 'edit' ? '编辑' : '预览'}
            </button>
          ))}
        </div>
      </div>

      {view === 'edit' ? (
        <textarea
          value={system}
          onChange={(e) => setSystem(e.target.value)}
          placeholder="这个角色是谁、该怎么说话、有什么禁忌…… 支持 Markdown。"
          className="min-h-0 flex-1 resize-none rounded-md border border-input bg-background px-3 py-2 font-mono text-sm leading-relaxed outline-none focus:ring-1 focus:ring-ring"
        />
      ) : (
        <div
          onDoubleClick={() => setView('edit')}
          title="双击回到编辑"
          className="min-h-0 flex-1 overflow-auto rounded-md border border-input bg-secondary/20 px-3 py-2"
        >
          {system.trim() ? (
            <MarkdownPreview value={system} className="markdown-preview text-sm" />
          ) : (
            <span className="text-sm text-muted-foreground">(空)</span>
          )}
        </div>
      )}

      <div className="flex shrink-0 items-center gap-2">
        <Button onClick={() => void onSave()} disabled={!dirty || saving} size="sm">
          <Save className="h-3.5 w-3.5" />
          {saving ? '保存中...' : '保存'}
        </Button>
        {flash && <span className="text-xs text-success">已保存</span>}
        {dirty && !flash && (
          <span className="flex items-center gap-1 text-xs text-muted-foreground">
            <X className="h-3 w-3" />
            有未保存的修改
          </span>
        )}
      </div>
    </div>
  )
}
