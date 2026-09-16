import { useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { AlertTriangle, CheckCircle2, Database, FolderOpen, XCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { OpenInExplorer } from '../../../wailsjs/go/main/App'
import { cn } from '@/lib/utils'
import { jumpTo } from '@/lib/jump'
import type { LogEntry, RunStatus } from './types'

interface Props {
  status: RunStatus
  logs: LogEntry[]
  outputDir: string
  onCancel: () => void
  onClear: () => void
}

export function OutputPane({ status, logs, outputDir, onCancel, onClear }: Props) {
  const paneRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = paneRef.current
    if (!el) return
    el.scrollTop = el.scrollHeight
  }, [logs.length])

  return (
    <div className="flex flex-col overflow-hidden rounded-lg border border-border">
      <StatusBar status={status} outputDir={outputDir} onCancel={onCancel} onClear={onClear} />
      <div
        ref={paneRef}
        className="max-h-[360px] min-h-[180px] overflow-auto bg-zinc-950 p-3 font-mono text-[12px] leading-[1.5] text-zinc-200"
      >
        {logs.length === 0 ? (
          <div className="text-zinc-500">
            {status === 'idle' ? '执行后日志将显示在这里…' : '等待输出…'}
          </div>
        ) : (
          logs.map((l, i) => (
            <div
              key={i}
              className={cn(
                'whitespace-pre-wrap break-words',
                l.stream === 'stderr' && 'text-red-300'
              )}
            >
              {l.line}
            </div>
          ))
        )}
      </div>
    </div>
  )
}

function StatusBar({
  status,
  outputDir,
  onCancel,
  onClear,
}: {
  status: RunStatus
  outputDir: string
  onCancel: () => void
  onClear: () => void
}) {
  const navigate = useNavigate()
  if (status === 'idle') {
    return (
      <div className="flex h-9 items-center justify-between border-b border-border bg-muted/30 px-3 text-xs text-muted-foreground">
        <span>输出</span>
      </div>
    )
  }
  if (status === 'running') {
    return (
      <div className="flex h-10 items-center justify-between border-b border-border bg-amber-500/10 px-3 text-sm">
        <span className="flex items-center gap-2 text-amber-700 dark:text-amber-300">
          <span className="h-2 w-2 animate-pulse rounded-full bg-amber-500" />
          正在执行…
        </span>
        <Button variant="destructive" size="sm" onClick={onCancel}>
          <XCircle className="h-3.5 w-3.5" />
          取消
        </Button>
      </div>
    )
  }
  if (status === 'success') {
    return (
      <div className="flex h-10 items-center justify-between border-b border-border bg-emerald-500/10 px-3 text-sm">
        <span className="flex items-center gap-2 text-emerald-700 dark:text-emerald-300">
          <CheckCircle2 className="h-4 w-4" />
          取证完成
        </span>
        <div className="flex gap-1.5">
          {outputDir && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => OpenInExplorer(outputDir)}
            >
              <FolderOpen className="h-3.5 w-3.5" />
              打开输出目录
            </Button>
          )}
          {/* 取下来之后下一步多半是翻里面的库:把输出目录直接带过去,不用再抄一遍路径 */}
          {outputDir && (
            <Button
              variant="outline"
              size="sm"
              title="把输出目录填进 SQLite 搜索，直接搜里面的数据库"
              onClick={() => jumpTo(navigate, { to: 'sqlite-search', root: outputDir })}
            >
              <Database className="h-3.5 w-3.5" />
              用 SQLite 搜索
            </Button>
          )}
          <Button variant="ghost" size="sm" onClick={onClear}>
            清空日志
          </Button>
        </div>
      </div>
    )
  }
  if (status === 'canceled') {
    return (
      <div className="flex h-10 items-center justify-between border-b border-border bg-zinc-500/10 px-3 text-sm text-muted-foreground">
        <span>已取消</span>
        <Button variant="ghost" size="sm" onClick={onClear}>
          清空日志
        </Button>
      </div>
    )
  }
  return (
    <div className="flex h-10 items-center justify-between border-b border-border bg-destructive/10 px-3 text-sm">
      <span className="flex items-center gap-2 text-destructive">
        <AlertTriangle className="h-4 w-4" />
        取证失败
      </span>
      <Button variant="ghost" size="sm" onClick={onClear}>
        清空日志
      </Button>
    </div>
  )
}
