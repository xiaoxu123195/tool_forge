import { ChevronsRight, Copy } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { mmkv } from '../../../../wailsjs/go/models'
import { bgOf, displayOf, labelOf, optionsOf } from '../valueTypes'

interface Props {
  value: mmkv.Value
  type: string
  onCycle: () => void
  onExpand: () => void
}

export function ValueCell({ value, type, onCycle, onExpand }: Props) {
  const text = displayOf(value, type)
  // 只有一种读法时徽章不该看起来能点 —— 点了什么也不会变
  const cyclable = optionsOf(value).length > 1
  const title = cyclable ? '点击循环切换类型' : '这个值只有这一种读法'

  const copy = async (e: React.MouseEvent) => {
    e.stopPropagation()
    await navigator.clipboard.writeText(text)
  }

  return (
    <div
      className={cn(
        'group flex items-center gap-2 rounded-sm px-3 py-1.5 font-mono text-[12.5px] transition-colors',
        bgOf(type)
      )}
    >
      {/* 类型徽章：点击循环切换 */}
      <button
        onClick={(e) => {
          e.stopPropagation()
          if (cyclable) onCycle()
        }}
        className={cn(
          'shrink-0 text-[11px] text-orange-600 dark:text-orange-400',
          cyclable ? 'transition-opacity hover:opacity-70' : 'cursor-default opacity-70'
        )}
        title={title}
      >
        ({labelOf(type)})
      </button>

      {/* 值本体：单行截断；点击循环切换类型（展开看完整值请点右侧按钮） */}
      <div
        onClick={cyclable ? onCycle : undefined}
        className={cn('min-w-0 flex-1 truncate', cyclable && 'cursor-pointer')}
        title={title}
      >
        {text}
      </div>

      {/* 复制：hover 时浮出 */}
      <button
        onClick={copy}
        title="复制当前解码结果"
        className="shrink-0 rounded p-1 text-muted-foreground/70 opacity-0 transition-opacity hover:bg-background/60 hover:text-foreground group-hover:opacity-100"
      >
        <Copy className="h-3.5 w-3.5" />
      </button>

      {/* 展开：常驻，位于值的末尾 */}
      <button
        onClick={(e) => {
          e.stopPropagation()
          onExpand()
        }}
        title="展开查看完整值"
        className="shrink-0 rounded px-1.5 py-1 text-[10px] font-medium text-muted-foreground/80 transition-colors hover:bg-background/60 hover:text-foreground"
      >
        <ChevronsRight className="h-3.5 w-3.5" />
      </button>
    </div>
  )
}
