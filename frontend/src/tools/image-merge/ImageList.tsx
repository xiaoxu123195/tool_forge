import { useState } from 'react'
import { GripVertical, RotateCw, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { MergeImage } from './types'

interface Props {
  images: MergeImage[]
  onRemove: (id: string) => void
  onRotate: (id: string) => void
  onReorder: (from: number, to: number) => void
}

function formatSize(b: number): string {
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`
  return `${(b / 1024 / 1024).toFixed(1)} MB`
}

export function ImageList({ images, onRemove, onRotate, onReorder }: Props) {
  const [dragIndex, setDragIndex] = useState<number | null>(null)
  const [overIndex, setOverIndex] = useState<number | null>(null)

  if (images.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-border py-8 text-center text-xs text-muted-foreground">
        还没有图片，拖拽顺序即拼接顺序
      </div>
    )
  }

  return (
    <ul className="space-y-2">
      {images.map((img, index) => (
        <li
          key={img.id}
          draggable
          onDragStart={() => setDragIndex(index)}
          onDragEnd={() => {
            setDragIndex(null)
            setOverIndex(null)
          }}
          onDragOver={(e) => {
            e.preventDefault()
            if (overIndex !== index) setOverIndex(index)
          }}
          onDrop={(e) => {
            e.preventDefault()
            if (dragIndex !== null) onReorder(dragIndex, index)
            setDragIndex(null)
            setOverIndex(null)
          }}
          className={cn(
            'flex items-center gap-2.5 rounded-lg border bg-card p-2 transition-colors',
            dragIndex === index ? 'opacity-50' : 'opacity-100',
            overIndex === index && dragIndex !== index ? 'border-info' : 'border-border',
          )}
        >
          <GripVertical className="h-4 w-4 shrink-0 cursor-grab text-muted-foreground" />
          <span className="w-5 shrink-0 text-center text-[11px] tabular-nums text-muted-foreground">{index + 1}</span>
          <img src={img.thumb} alt={img.name} className="h-11 w-11 shrink-0 rounded object-cover" />
          <div className="min-w-0 flex-1">
            <div className="truncate text-xs font-medium" title={img.name}>
              {img.name}
            </div>
            <div className="text-[11px] text-muted-foreground">
              {img.width} × {img.height} · {formatSize(img.size)}
              {img.rotation !== 0 && <span className="ml-1 text-info">↻{img.rotation}°</span>}
            </div>
          </div>
          <button
            type="button"
            onClick={() => onRotate(img.id)}
            title="顺时针旋转 90°"
            className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          >
            <RotateCw className="h-3.5 w-3.5" />
          </button>
          <button
            type="button"
            onClick={() => onRemove(img.id)}
            title="移除"
            className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
          >
            <X className="h-4 w-4" />
          </button>
        </li>
      ))}
    </ul>
  )
}
