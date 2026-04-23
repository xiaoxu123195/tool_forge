import type { ReactNode } from 'react'
import { Eraser, FileCode2 } from 'lucide-react'
import { Button } from '@/components/ui/button'

interface ToolShellProps {
  title: string
  description?: string
  actions?: ReactNode
  onClear?: () => void
  onLoadExample?: () => void
  children: ReactNode
}

export function ToolShell({
  title,
  description,
  actions,
  onClear,
  onLoadExample,
  children,
}: ToolShellProps) {
  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center justify-between border-b border-border bg-card px-5">
        <div className="min-w-0">
          <h1 className="truncate text-base font-semibold">{title}</h1>
          {description && (
            <p className="truncate text-xs text-muted-foreground">{description}</p>
          )}
        </div>
        <div className="flex items-center gap-1.5">
          {actions}
          {onLoadExample && (
            <Button variant="ghost" size="sm" onClick={onLoadExample}>
              <FileCode2 className="h-3.5 w-3.5" />
              示例
            </Button>
          )}
          {onClear && (
            <Button variant="ghost" size="sm" onClick={onClear}>
              <Eraser className="h-3.5 w-3.5" />
              清空
            </Button>
          )}
        </div>
      </header>
      <div className="flex-1 overflow-auto p-5" data-tool-scroll="true">{children}</div>
    </div>
  )
}
