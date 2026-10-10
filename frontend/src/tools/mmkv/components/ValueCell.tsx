import { ChevronsRight, Copy } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { mmkv } from '../../../../wailsjs/go/models'
import { TYPE_ORDER, bgOf, defaultTypeOf, labelOf, looseNote, readingOf } from '../valueTypes'

interface Props {
  value: mmkv.Value
  type: string
  onCycle: () => void
  onExpand: () => void
}

export function ValueCell({ value, type, onCycle, onExpand }: Props) {
  const r = readingOf(value, type)
  // 打开时显示的就是自动识别出的类型;点着换过一圈的人得知道哪个是它认出来的
  const auto = type === defaultTypeOf(value)
  const title = `点击换成下一种类型看（共 ${TYPE_ORDER.length} 种）`

  const copy = async (e: React.MouseEvent) => {
    e.stopPropagation()
    await navigator.clipboard.writeText(r.text)
  }

  return (
    <div
      className={cn(
        'group flex items-center gap-2 rounded-sm px-3 py-1.5 font-mono text-[12.5px] transition-colors',
        r.kind === 'none' ? 'bg-muted/30' : bgOf(type)
      )}
    >
      {/* 类型徽章：点击换下一种 */}
      <button
        onClick={(e) => {
          e.stopPropagation()
          onCycle()
        }}
        className="shrink-0 text-[11px] text-orange-600 transition-opacity hover:opacity-70 dark:text-orange-400"
        title={title}
      >
        ({labelOf(type)})
      </button>
      {auto && (
        <span
          className="shrink-0 rounded-sm bg-background/60 px-1 font-sans text-[10px] text-muted-foreground"
          title="打开时自动识别出的类型"
        >
          自动
        </span>
      )}
      {r.kind === 'loose' && (
        <span
          className="shrink-0 rounded-sm bg-background/60 px-1 font-sans text-[10px] text-amber-700 dark:text-amber-400"
          title={looseNote(value, r)}
        >
          只用前 {r.used} 字节
        </span>
      )}

      {/* 值本体：单行截断；点击也是换类型（展开看完整值请点右侧按钮） */}
      <div onClick={onCycle} className="min-w-0 flex-1 cursor-pointer truncate" title={title}>
        {r.kind === 'none' ? (
          <span className="text-muted-foreground" title="按这个类型读不通">
            N/A
          </span>
        ) : (
          r.text
        )}
      </div>

      {/* 复制：hover 时浮出；读不通的没东西可复制 */}
      {r.kind !== 'none' && (
        <button
          onClick={copy}
          title="复制当前解码结果"
          className="shrink-0 rounded p-1 text-muted-foreground/70 opacity-0 transition-opacity hover:bg-background/60 hover:text-foreground group-hover:opacity-100"
        >
          <Copy className="h-3.5 w-3.5" />
        </button>
      )}

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
