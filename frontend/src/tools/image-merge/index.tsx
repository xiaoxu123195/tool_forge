import { useEffect } from 'react'
import { ToolShell } from '@/components/tool/ToolShell'
import { meta } from './meta'
import { useMergeStore } from './store'
import { useImages } from './useImages'
import { ControlPanel } from './ControlPanel'
import { ImageList } from './ImageList'
import { PreviewPane } from './PreviewPane'

export default function ImageMerge() {
  const { images, addFiles, remove, clear, reorder, rotate } = useImages()
  const settings = useMergeStore()

  // 全局粘贴:Ctrl+V 把剪贴板里的图片加进来
  useEffect(() => {
    const onPaste = (e: ClipboardEvent) => {
      const files = Array.from(e.clipboardData?.items ?? [])
        .filter((it) => it.kind === 'file' && it.type.startsWith('image/'))
        .map((it) => it.getAsFile())
        .filter((f): f is File => f != null)
      if (files.length) {
        e.preventDefault()
        void addFiles(files)
      }
    }
    window.addEventListener('paste', onPaste)
    return () => window.removeEventListener('paste', onPaste)
  }, [addFiles])

  return (
    <ToolShell title={meta.title} description={meta.description} fullBleed>
      <div className="flex min-h-0 flex-1">
        {/* 左:设置 + 图片列表 */}
        <div className="w-[340px] shrink-0 space-y-5 overflow-auto border-r border-border p-4" data-tool-scroll="true">
          <ControlPanel settings={settings} set={settings.set} onAddFiles={(f) => void addFiles(f)} />
          <div className="space-y-2">
            <div className="text-xs font-medium text-foreground/80">
              图片列表 <span className="text-muted-foreground">({images.length})</span>
            </div>
            <ImageList images={images} onRemove={remove} onRotate={rotate} onReorder={reorder} />
          </div>
        </div>
        {/* 右:预览 + 导出 */}
        <div className="min-w-0 flex-1 overflow-auto p-4" data-tool-scroll="true">
          <PreviewPane images={images} settings={settings} onClear={clear} />
        </div>
      </div>
    </ToolShell>
  )
}
