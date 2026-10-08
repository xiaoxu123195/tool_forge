import { useEffect, useState } from 'react'
import { ChevronRight, File as FileIcon, Folder, FolderOpen, Loader2 } from 'lucide-react'
import { DiskUsageChildren, OpenInExplorer } from '../../../wailsjs/go/main/App'
import type { diskclean } from '../../../wailsjs/go/models'
import { cn } from '@/lib/utils'
import { errText, fmtBytes, fmtCount } from './lib'
import { Notice } from './Shared'

const baseName = (p: string) => p.replace(/[\\/]+$/, '').split(/[\\/]/).pop() || p

/**
 * 按目录看占用,一层层点进去找"C 盘被谁吃了"。
 *
 * 数据是大文件扫描时顺带建的目录树,点进哪一层就当场从树上取,不再碰磁盘
 */
export function UsageView({ usageId, onShowFiles }: { usageId: string; onShowFiles: (dir: string) => void }) {
  // 走过的路:第一项是最顶层(扫描的那几个起点)
  const [trail, setTrail] = useState<string[]>([''])
  const [level, setLevel] = useState<diskclean.UsageLevel | null>(null)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  const dir = trail[trail.length - 1]

  // 换了一次扫描就回到最顶层:旧的那条路在新树上不一定还在
  useEffect(() => setTrail(['']), [usageId])

  useEffect(() => {
    let alive = true
    setLoading(true)
    setErr('')
    DiskUsageChildren(usageId, dir)
      .then((lv) => alive && setLevel(lv))
      .catch((e) => alive && setErr(errText(e)))
      .finally(() => alive && setLoading(false))
    return () => {
      alive = false
    }
  }, [usageId, dir])

  return (
    <div className="space-y-2 px-4 pb-4" data-view="usage">
      <div className="flex flex-wrap items-center gap-0.5 text-xs">
        {trail.map((p, i) => (
          <span key={i} className="flex items-center gap-0.5">
            {i > 0 && <ChevronRight className="h-3 w-3 text-muted-foreground" />}
            <button
              type="button"
              disabled={i === trail.length - 1}
              onClick={() => setTrail(trail.slice(0, i + 1))}
              className={cn(
                'rounded px-1.5 py-0.5',
                i === trail.length - 1 ? 'font-medium' : 'text-info hover:bg-info/10',
              )}
            >
              {i === 0 ? '全部' : i === 1 ? p : baseName(p)}
            </button>
          </span>
        ))}
        {loading && <Loader2 className="ml-1 h-3 w-3 animate-spin text-muted-foreground" />}
      </div>

      {err && <Notice tone="error">{err}</Notice>}
      {level && (
        <>
          <div className="text-xs text-muted-foreground">
            共 <b className="text-foreground">{fmtBytes(level.size)}</b>，{fmtCount(level.files)} 个文件
            {level.partial && '（有的目录进不去，实际可能更大）'}
          </div>
          <div className="overflow-hidden rounded-lg border border-border">
            {level.entries.map((e) => (
              <UsageRow
                key={e.path + (e.isDir ? '' : '#files')}
                e={e}
                total={level.size}
                onOpen={() => (e.isDir ? setTrail([...trail, e.path]) : onShowFiles(e.path))}
              />
            ))}
            {level.more > 0 && (
              <div className="px-3 py-1.5 text-[11px] text-muted-foreground">
                其余 {fmtCount(level.more)} 项，共 {fmtBytes(level.moreSize)}
              </div>
            )}
            {level.entries.length === 0 && (
              <div className="p-6 text-center text-xs text-muted-foreground">这个目录是空的</div>
            )}
          </div>
        </>
      )}
    </div>
  )
}

function UsageRow({ e, total, onOpen }: { e: diskclean.UsageEntry; total: number; onOpen: () => void }) {
  const pct = total > 0 ? (e.size / total) * 100 : 0
  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(ev) => ev.key === 'Enter' && onOpen()}
      title={e.isDir ? `点进去看 ${e.path}` : '在文件列表里看这个目录里的大文件'}
      data-usage={e.isDir ? e.path : 'files'}
      className="flex cursor-pointer items-center gap-3 border-b border-border/50 px-3 py-1.5 last:border-b-0 hover:bg-muted/40"
    >
      {e.isDir ? (
        <Folder className="h-4 w-4 shrink-0 text-amber-500" />
      ) : (
        <FileIcon className="h-4 w-4 shrink-0 text-muted-foreground" />
      )}
      <div className="min-w-0 flex-1">
        <div className={cn('truncate text-sm', !e.isDir && 'text-muted-foreground')}>{e.name}</div>
        {e.note && <div className="truncate text-[11px] text-muted-foreground">{e.note}</div>}
      </div>
      <div className="hidden w-32 shrink-0 sm:block">
        <div className="h-1.5 overflow-hidden rounded-full bg-secondary">
          <div className="h-full rounded-full bg-info" style={{ width: `${Math.max(pct, 0.5)}%` }} />
        </div>
      </div>
      <div className="w-12 shrink-0 text-right text-[11px] tabular-nums text-muted-foreground">
        {pct >= 0.1 ? `${pct.toFixed(1)}%` : '<0.1%'}
      </div>
      <div className="w-20 shrink-0 text-right text-sm tabular-nums">
        {e.partial && <span title="有的目录进不去,实际可能更大">≥</span>}
        {fmtBytes(e.size)}
      </div>
      <div className="w-20 shrink-0 text-right text-[11px] text-muted-foreground">{fmtCount(e.files)} 个</div>
      {e.isDir ? (
        <button
          type="button"
          title="在资源管理器里打开"
          onClick={(ev) => {
            ev.stopPropagation()
            void OpenInExplorer(e.path)
          }}
          className="shrink-0 text-muted-foreground hover:text-foreground"
        >
          <FolderOpen className="h-3.5 w-3.5" />
        </button>
      ) : (
        <span className="w-3.5 shrink-0" />
      )}
    </div>
  )
}
