import { Upload } from 'lucide-react'
import { useNativeFileDrop } from '@/lib/useNativeFileDrop'
import { cn } from '@/lib/utils'

/**
 * 文件拖放区:同时支持拖入(原生路径)与点击选择对话框。
 * 内联 --wails-drop-target:drop 把自己(及后代,自定义属性会继承)标记为拖放目标。
 */
export function DropZone({
  onPaths,
  onPick,
  hint,
  className,
}: {
  onPaths: (paths: string[]) => void
  onPick?: () => void
  hint?: string
  className?: string
}) {
  useNativeFileDrop(onPaths)
  return (
    <div
      style={{ ['--wails-drop-target' as never]: 'drop' }}
      onClick={onPick}
      className={cn(
        'flex cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed border-border bg-secondary/20 px-6 py-7 text-center text-sm text-muted-foreground transition-colors hover:border-info/60 hover:bg-secondary/40',
        className,
      )}
    >
      <Upload className="h-6 w-6 opacity-70" />
      <div>{hint ?? '拖入文件，或点击选择'}</div>
    </div>
  )
}
