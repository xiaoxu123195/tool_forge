import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import {
  GripVertical,
  MessageSquare,
  PanelLeftClose,
  PanelLeftOpen,
  Pencil,
  Plus,
  Trash2,
} from 'lucide-react'
import type { ConversationSummary } from './types'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { cn } from '@/lib/utils'

interface Props {
  list: ConversationSummary[]
  activeId: string
  onSelect: (id: string) => void
  onNew: () => void
  onDelete: (id: string) => void
  onEdit: (id: string) => void
  /** 拖动结束后的新顺序 */
  onReorder: (ids: string[]) => void
}

interface MenuState {
  id: string
  x: number
  y: number
}

const COLLAPSE_KEY = 'ai-chat.sidebar-collapsed'

export function ConversationList({
  list,
  activeId,
  onSelect,
  onNew,
  onDelete,
  onEdit,
  onReorder,
}: Props) {
  const dialog = useConfirm()
  const [menu, setMenu] = useState<MenuState | null>(null)
  // 收起状态记在本地:长对话时把这 240px 让给正文是个会反复用的动作,
  // 每次进来都要重新收一次就没意义了
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem(COLLAPSE_KEY) === '1'
    } catch {
      return false
    }
  })
  const [dragId, setDragId] = useState('')
  const [overId, setOverId] = useState('')

  useEffect(() => {
    try {
      localStorage.setItem(COLLAPSE_KEY, collapsed ? '1' : '0')
    } catch {
      // 隐私模式下 localStorage 会抛;记不住就算了,不该因此崩掉
    }
  }, [collapsed])

  const askDelete = async (c: ConversationSummary) => {
    const ok = await dialog({
      title: '删除会话',
      message: `确认删除「${c.title}」?该会话所有消息都会丢失。`,
      danger: true,
      confirmLabel: '删除',
    })
    if (ok) onDelete(c.id)
  }

  const openMenu = (e: React.MouseEvent, id: string) => {
    e.preventDefault()
    setMenu({ id, x: e.clientX, y: e.clientY })
  }

  const drop = (targetId: string) => {
    const from = list.findIndex((c) => c.id === dragId)
    const to = list.findIndex((c) => c.id === targetId)
    setDragId('')
    setOverId('')
    if (from < 0 || to < 0 || from === to) return
    const next = list.map((c) => c.id)
    const [moved] = next.splice(from, 1)
    next.splice(to, 0, moved)
    onReorder(next)
  }

  if (collapsed) {
    return (
      <aside className="flex h-full w-12 shrink-0 flex-col items-center gap-1 border-r border-border bg-card py-3">
        <button
          type="button"
          onClick={() => setCollapsed(false)}
          title="展开对话列表"
          className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
        >
          <PanelLeftOpen className="h-4 w-4" />
        </button>
        <button
          type="button"
          onClick={onNew}
          title="新建对话"
          className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
        >
          <Plus className="h-4 w-4" />
        </button>
        <div className="my-1 h-px w-6 bg-border" />
        {/* 收起后仍然能切会话:只画一列小圆点,当前那条高亮 */}
        <div className="flex min-h-0 flex-1 flex-col items-center gap-1 overflow-auto">
          {list.map((c) => (
            <button
              key={c.id}
              type="button"
              onClick={() => onSelect(c.id)}
              title={c.title}
              className={cn(
                'flex h-8 w-8 shrink-0 items-center justify-center rounded-md transition-colors',
                activeId === c.id
                  ? 'bg-info/15 text-info'
                  : 'text-muted-foreground hover:bg-secondary hover:text-foreground',
              )}
            >
              <MessageSquare className="h-3.5 w-3.5" />
            </button>
          ))}
        </div>
      </aside>
    )
  }

  return (
    <aside className="flex h-full w-[240px] shrink-0 flex-col border-r border-border bg-card">
      <div className="flex items-center gap-2 border-b border-border p-3">
        <Button onClick={onNew} size="sm" className="flex-1">
          <Plus className="h-3.5 w-3.5" />
          新建对话
        </Button>
        <button
          type="button"
          onClick={() => setCollapsed(true)}
          title="收起对话列表"
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
        >
          <PanelLeftClose className="h-4 w-4" />
        </button>
      </div>
      <ul className="flex-1 overflow-auto px-2 py-2">
        {list.length === 0 ? (
          <li className="px-3 py-6 text-center text-xs text-muted-foreground">
            还没有对话,点上方「新建」开始
          </li>
        ) : (
          list.map((c) => (
            <li
              key={c.id}
              draggable
              onDragStart={() => setDragId(c.id)}
              onDragEnd={() => {
                setDragId('')
                setOverId('')
              }}
              onDragOver={(e) => {
                if (!dragId) return
                e.preventDefault()
                if (overId !== c.id) setOverId(c.id)
              }}
              onDrop={(e) => {
                e.preventDefault()
                drop(c.id)
              }}
              onClick={() => onSelect(c.id)}
              onContextMenu={(e) => openMenu(e, c.id)}
              className={cn(
                'group/conv mb-1 flex cursor-pointer items-center gap-1.5 rounded-md px-2 py-2 text-sm transition-colors',
                activeId === c.id ? 'bg-info/15 text-info' : 'hover:bg-secondary',
                dragId === c.id && 'opacity-40',
                overId === c.id && dragId && dragId !== c.id && 'ring-1 ring-info',
              )}
            >
              <GripVertical className="h-3.5 w-3.5 shrink-0 cursor-grab text-muted-foreground opacity-0 transition-opacity group-hover/conv:opacity-100 active:cursor-grabbing" />
              <MessageSquare className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              <span className="min-w-0 flex-1 truncate" title={c.title}>
                {c.title}
              </span>
            </li>
          ))
        )}
      </ul>
      {menu && (
        <ContextMenu
          x={menu.x}
          y={menu.y}
          onClose={() => setMenu(null)}
          onEdit={() => {
            onEdit(menu.id)
            setMenu(null)
          }}
          onDelete={() => {
            const c = list.find((x) => x.id === menu.id)
            setMenu(null)
            if (c) void askDelete(c)
          }}
        />
      )}
    </aside>
  )
}

function ContextMenu({
  x,
  y,
  onClose,
  onEdit,
  onDelete,
}: {
  x: number
  y: number
  onClose: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  // 点击外部 / Esc 关闭
  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose()
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [onClose])

  // 防溢出:估算菜单尺寸
  const W = 160
  const H = 76
  const left = Math.min(x, window.innerWidth - W - 4)
  const top = Math.min(y, window.innerHeight - H - 4)

  return createPortal(
    <div
      ref={ref}
      style={{ left, top }}
      className="fixed z-[80] w-40 overflow-hidden rounded-md border border-border bg-popover py-1 text-sm shadow-lg"
      onContextMenu={(e) => e.preventDefault()}
    >
      <button
        type="button"
        onClick={onEdit}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-left transition-colors hover:bg-secondary"
      >
        <Pencil className="h-3.5 w-3.5 text-muted-foreground" />
        编辑
      </button>
      <button
        type="button"
        onClick={onDelete}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-destructive transition-colors hover:bg-destructive/10"
      >
        <Trash2 className="h-3.5 w-3.5" />
        删除
      </button>
    </div>,
    document.body,
  )
}
