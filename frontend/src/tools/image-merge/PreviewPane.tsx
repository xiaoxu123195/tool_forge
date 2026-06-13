import { useEffect, useMemo, useRef, useState } from 'react'
import { Download, ImageOff, Loader2, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { SaveImageFile } from '../../../wailsjs/go/main/App'
import { computeLayout } from './layout'
import {
  renderToCanvas,
  previewScale,
  canvasToBlob,
  blobToBase64,
  formatToExt,
} from './render'
import type { MergeImage, MergeSettings, OutputFormat } from './types'

interface Props {
  images: MergeImage[]
  settings: MergeSettings
  onClear: () => void
}

const FORMAT_LABEL: Record<OutputFormat, string> = {
  png: 'PNG 图片',
  jpeg: 'JPEG 图片',
  webp: 'WebP 图片',
}

export function PreviewPane({ images, settings, onClear }: Props) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const [saving, setSaving] = useState(false)
  const [status, setStatus] = useState<{ kind: 'ok' | 'err' | 'info'; text: string } | null>(null)

  const layout = useMemo(() => computeLayout(images, settings), [images, settings])

  // 实时预览(降采样渲染),带轻量防抖避免拖动滑块时狂刷。
  // 画到 React 托管的 <canvas ref> 上,不手动增删 DOM。
  useEffect(() => {
    if (images.length === 0) return
    const t = setTimeout(() => {
      const canvas = canvasRef.current
      if (!canvas) return
      renderToCanvas(images, settings, layout, previewScale(layout), canvas)
    }, 60)
    return () => clearTimeout(t)
  }, [images, settings, layout])

  const transparent = settings.bg === 'transparent'

  const exportW = Math.round(layout.width * layout.exportScale)
  const exportH = Math.round(layout.height * layout.exportScale)
  const willScale = layout.exportScale < 1

  async function handleSave() {
    if (images.length === 0 || saving) return
    setSaving(true)
    setStatus(null)
    try {
      // 导出走全分辨率(exportScale 只在超上限时 <1)
      const { canvas } = renderToCanvas(images, settings, layout, layout.exportScale)
      const blob = await canvasToBlob(canvas, settings.format, settings.quality)
      const b64 = await blobToBase64(blob)
      const ext = formatToExt(settings.format)
      const name = `merged_${Date.now()}.${ext}`
      const path = await SaveImageFile(name, ext, FORMAT_LABEL[settings.format], b64)
      if (path) setStatus({ kind: 'ok', text: `已保存：${path}` })
      else setStatus({ kind: 'info', text: '已取消保存' })
    } catch (e) {
      setStatus({ kind: 'err', text: e instanceof Error ? e.message : String(e) })
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-1 items-center justify-center overflow-auto rounded-lg border border-border bg-secondary/30 p-4">
        {images.length === 0 ? (
          <div className="flex flex-col items-center gap-2 text-muted-foreground">
            <ImageOff className="h-10 w-10" />
            <div className="text-sm">添加图片后这里显示拼接预览</div>
          </div>
        ) : (
          <canvas
            ref={canvasRef}
            className="max-h-full max-w-full rounded-md object-contain shadow-lg"
            style={
              transparent
                ? {
                    backgroundImage:
                      'conic-gradient(#e5e5e5 90deg,#fff 90deg 180deg,#e5e5e5 180deg 270deg,#fff 270deg)',
                    backgroundSize: '16px 16px',
                  }
                : undefined
            }
          />
        )}
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-3">
        {images.length > 0 && (
          <div className="text-xs text-muted-foreground">
            成品尺寸 <span className="font-mono tabular-nums text-foreground">{exportW} × {exportH}</span>
            {willScale && (
              <span className="ml-1 text-amber-600 dark:text-amber-400">
                （已从 {layout.width}×{layout.height} 缩小以适配上限）
              </span>
            )}
            <span className="ml-2">· {images.length} 张</span>
          </div>
        )}
        <div className="ml-auto flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={onClear} disabled={images.length === 0 || saving}>
            <Trash2 className="h-3.5 w-3.5" />
            清空
          </Button>
          <Button size="sm" onClick={handleSave} disabled={images.length === 0 || saving}>
            {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
            保存图片
          </Button>
        </div>
      </div>

      {status && (
        <div
          className={
            'mt-2 truncate text-xs ' +
            (status.kind === 'ok'
              ? 'text-emerald-600 dark:text-emerald-400'
              : status.kind === 'err'
                ? 'text-destructive'
                : 'text-muted-foreground')
          }
          title={status.text}
        >
          {status.text}
        </div>
      )}
    </div>
  )
}
