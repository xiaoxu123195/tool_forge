import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  DndContext,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core'
import {
  SortableContext,
  arrayMove,
  rectSortingStrategy,
  useSortable,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { GripVertical, RotateCcw } from 'lucide-react'
import {
  CATEGORY_LABELS,
  getAllTools,
  isVisible,
  useToolsStore,
  type ToolMeta,
} from '@/stores/tools'
import { useLayoutStore } from '@/stores/layout'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { iconClassForCategory } from '@/lib/categoryColors'

export function Home() {
  const visibility = useToolsStore((s) => s.visibility)
  const order = useToolsStore((s) => s.order)
  const setOrder = useToolsStore((s) => s.setOrder)
  const setVisibility = useToolsStore((s) => s.setVisibility)
  const resetOrder = useToolsStore((s) => s.resetOrder)
  const styleId = useLayoutStore((s) => s.styleId)

  const allTools = useMemo(() => getAllTools(order), [order])
  const [isManaging, setIsManaging] = useState(false)

  const displayedTools = isManaging
    ? allTools
    : allTools.filter((t) => isVisible(t.id, visibility, t.defaultVisible))

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } })
  )

  const handleDragEnd = (e: DragEndEvent) => {
    const { active, over } = e
    if (!over || active.id === over.id) return
    const ids = allTools.map((t) => t.id)
    const oldIndex = ids.indexOf(String(active.id))
    const newIndex = ids.indexOf(String(over.id))
    if (oldIndex < 0 || newIndex < 0) return
    setOrder(arrayMove(ids, oldIndex, newIndex))
  }

  const visibleCount = allTools.filter((t) =>
    isVisible(t.id, visibility, t.defaultVisible)
  ).length

  return (
    <div className="ambient min-h-full">
      <div className="mx-auto max-w-6xl p-6">
        <header className="mb-6 flex items-end justify-between gap-4">
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">
              Tool{' '}
              <span className={cn('bg-clip-text', titleAccent(styleId))}>
                Forge
              </span>
            </h1>
            <p className="mt-1 text-sm text-muted-foreground">
              {isManaging
                ? `全部 ${allTools.length} 个工具 · 拖动卡片排序，点击开关启用 / 隐藏`
                : `共 ${visibleCount} 个工具 · 拖动卡片可调整顺序`}
            </p>
          </div>
          <div className="flex gap-2">
            {isManaging && order.length > 0 && (
              <Button variant="ghost" size="sm" onClick={resetOrder}>
                <RotateCcw className="h-3.5 w-3.5" />
                重置顺序
              </Button>
            )}
            <Button
              variant={isManaging ? 'default' : 'outline'}
              size="sm"
              onClick={() => setIsManaging((v) => !v)}
            >
              {isManaging ? '完成' : '管理'}
            </Button>
          </div>
        </header>

        {displayedTools.length === 0 ? (
          <div className="rounded-lg border border-dashed border-border p-12 text-center text-sm text-muted-foreground">
            没有启用的工具，点右上角「管理」开启一些吧
          </div>
        ) : (
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragEnd={handleDragEnd}
          >
            <SortableContext
              items={displayedTools.map((t) => t.id)}
              strategy={rectSortingStrategy}
            >
              <div className="grid auto-rows-fr grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {displayedTools.map((tool) => (
                  <SortableCard
                    key={tool.id}
                    tool={tool}
                    visible={isVisible(tool.id, visibility, tool.defaultVisible)}
                    managing={isManaging}
                    styleId={styleId}
                    onToggle={(v) => setVisibility(tool.id, v)}
                  />
                ))}
              </div>
            </SortableContext>
          </DndContext>
        )}
      </div>
    </div>
  )
}

function titleAccent(styleId: string): string {
  switch (styleId) {
    case 'nebula':
      return 'bg-gradient-to-r from-violet-500 to-fuchsia-500 text-transparent'
    case 'ocean':
      return 'bg-gradient-to-r from-cyan-500 to-sky-500 text-transparent'
    case 'forest':
      return 'bg-gradient-to-r from-emerald-500 to-green-500 text-transparent'
    default:
      // minimal:不做渐变,直接用 foreground
      return 'text-foreground'
  }
}

function SortableCard({
  tool,
  visible,
  managing,
  styleId,
  onToggle,
}: {
  tool: ToolMeta
  visible: boolean
  managing: boolean
  styleId: string
  onToggle: (v: boolean) => void
}) {
  const navigate = useNavigate()
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id: tool.id })

  const Icon = tool.icon
  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  }

  const handleClick = () => {
    if (managing) return
    navigate(tool.path)
  }

  const themed = styleId !== 'minimal'

  return (
    <div
      ref={setNodeRef}
      style={style}
      onClick={handleClick}
      {...attributes}
      {...listeners}
      className={cn(
        'group relative flex h-full flex-col rounded-lg border bg-card p-4 transition-all duration-200 touch-none',
        themed ? 'border-border/60 card-soft' : 'border-border',
        !managing &&
          'cursor-pointer hover:-translate-y-0.5 hover:border-foreground/30 hover:shadow-lg hover:shadow-foreground/5',
        managing && 'cursor-grab active:cursor-grabbing',
        !visible && 'opacity-50',
        isDragging && 'z-10 cursor-grabbing shadow-xl'
      )}
    >
      <div className="flex items-center gap-3">
        <div
          className={cn(
            'flex h-9 w-9 shrink-0 items-center justify-center rounded-md transition-transform duration-200',
            iconClassForCategory(tool.category, styleId),
            !managing && 'group-hover:scale-110'
          )}
        >
          <Icon className="h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{tool.title}</div>
          <div className="text-[11px] text-muted-foreground">
            {CATEGORY_LABELS[tool.category]}
          </div>
        </div>
        {managing && (
          <div
            className="flex items-center gap-1.5"
            onClick={(e) => e.stopPropagation()}
            onPointerDown={(e) => e.stopPropagation()}
          >
            <VisibilitySwitch checked={visible} onChange={onToggle} />
            <GripVertical className="h-4 w-4 text-muted-foreground/60" />
          </div>
        )}
      </div>
      <p className="mt-3 text-xs leading-relaxed text-muted-foreground line-clamp-2">
        {tool.description}
      </p>
    </div>
  )
}

function VisibilitySwitch({
  checked,
  onChange,
}: {
  checked: boolean
  onChange: (v: boolean) => void
}) {
  return (
    <button
      onClick={() => onChange(!checked)}
      role="switch"
      aria-checked={checked}
      title={checked ? '点击隐藏' : '点击显示'}
      className={cn(
        'inline-flex h-5 w-[34px] shrink-0 cursor-pointer items-center rounded-full p-0.5 transition-colors',
        checked
          ? 'bg-emerald-500 hover:bg-emerald-600'
          : 'bg-zinc-300 hover:bg-zinc-400 dark:bg-zinc-700 dark:hover:bg-zinc-600'
      )}
    >
      <span
        className={cn(
          'block h-4 w-4 rounded-full bg-white shadow-sm ring-0 transition-transform duration-150',
          checked ? 'translate-x-[14px]' : 'translate-x-0'
        )}
      />
    </button>
  )
}
