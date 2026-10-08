import type { ReactNode } from 'react'
import { AlertTriangle, FolderPlus, HardDrive, Info, Square, X } from 'lucide-react'
import { PickDirectory } from '../../../wailsjs/go/main/App'
import type { diskclean } from '../../../wailsjs/go/models'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { fmtBytes, fmtCount, type Progress } from './lib'

/** 进度:在干什么、看了多少、正看到哪儿。几十秒没动静的界面会让人以为卡死了 */
export function ProgressLine({ p, onCancel }: { p: Progress | null; onCancel: () => void }) {
  const pct = p && p.total > 0 ? Math.min(100, Math.round((p.done / p.total) * 100)) : null
  return (
    <div className="shrink-0 space-y-1.5 border-b border-border bg-muted/30 px-4 py-2">
      <div className="flex items-center gap-3 text-xs">
        <span className="font-medium">{p?.phase || '准备中'}</span>
        {p && p.files > 0 && (
          <span className="text-muted-foreground">
            已看 {fmtCount(p.files)} 个文件（{fmtBytes(p.bytes)}）
          </span>
        )}
        {pct !== null && <span className="tabular-nums text-muted-foreground">{pct}%</span>}
        <Button size="sm" variant="outline" className="ml-auto h-7" onClick={onCancel}>
          <Square className="h-3 w-3" />
          停止
        </Button>
      </div>
      <div className="h-1 w-full overflow-hidden rounded-full bg-secondary">
        {pct === null ? (
          <div className="h-full w-1/3 animate-pulse rounded-full bg-info/60" />
        ) : (
          <div className="h-full bg-info transition-all" style={{ width: `${pct}%` }} />
        )}
      </div>
      {p?.current && (
        <div className="truncate font-mono text-[11px] text-muted-foreground" title={p.current}>
          {p.current}
        </div>
      )}
    </div>
  )
}

/** 扫描起点:点盘符选整块盘,也能另加目录 */
export function RootPicker({
  places,
  roots,
  onChange,
  disabled,
}: {
  places: diskclean.Places | null
  roots: string[]
  onChange: (roots: string[]) => void
  disabled?: boolean
}) {
  const volumes = places?.volumes ?? []
  const volPaths = new Set(volumes.map((v) => v.path))
  const custom = roots.filter((r) => !volPaths.has(r))
  const toggle = (p: string) =>
    onChange(roots.includes(p) ? roots.filter((r) => r !== p) : [...roots, p])

  const addDir = async () => {
    const p = await PickDirectory('选择要扫描的目录', '').catch(() => '')
    if (p && !roots.includes(p)) onChange([...roots, p])
  }

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {volumes.map((v) => {
        const on = roots.includes(v.path)
        const used = v.total > 0 ? Math.round(((v.total - v.free) / v.total) * 100) : 0
        return (
          <button
            key={v.path}
            type="button"
            disabled={disabled}
            onClick={() => toggle(v.path)}
            title={`${v.path}${v.label ? ' ' + v.label : ''}：剩 ${fmtBytes(v.free)}，共 ${fmtBytes(v.total)}`}
            className={cn(
              'flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs transition-colors disabled:opacity-50',
              on ? 'border-info bg-info/10 text-info' : 'border-border text-muted-foreground hover:bg-secondary',
            )}
          >
            <HardDrive className="h-3 w-3" />
            <span className="font-medium">{v.path.replace(/\\$/, '')}</span>
            {v.system && <span className="text-[10px] opacity-70">系统</span>}
            {v.removable && <span className="text-[10px] opacity-70">可移动</span>}
            <span className="text-[10px] opacity-70">
              剩 {fmtBytes(v.free)} · 已用 {used}%
            </span>
          </button>
        )
      })}
      {custom.map((p) => (
        <span
          key={p}
          className="flex max-w-[320px] items-center gap-1 rounded-md border border-info bg-info/10 px-2 py-1 text-xs text-info"
        >
          <span className="truncate font-mono" title={p}>
            {p}
          </span>
          <button
            type="button"
            disabled={disabled}
            onClick={() => toggle(p)}
            title={`不扫 ${p}`}
            className="shrink-0 hover:text-destructive"
          >
            <X className="h-3 w-3" />
          </button>
        </span>
      ))}
      <Button size="sm" variant="ghost" className="h-7" disabled={disabled} onClick={() => void addDir()}>
        <FolderPlus className="h-3.5 w-3.5" />
        添加目录
      </Button>
    </div>
  )
}

export function Notice({
  tone = 'info',
  children,
  onClose,
}: {
  tone?: 'info' | 'warn' | 'error' | 'ok'
  children: ReactNode
  onClose?: () => void
}) {
  const Icon = tone === 'info' || tone === 'ok' ? Info : AlertTriangle
  return (
    <div
      className={cn(
        'flex items-start gap-2 rounded-lg border px-3 py-2 text-xs',
        tone === 'info' && 'border-border bg-muted/30 text-muted-foreground',
        tone === 'ok' && 'border-success/40 bg-success/5',
        tone === 'warn' && 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-400',
        tone === 'error' && 'border-destructive/40 bg-destructive/5 text-destructive',
      )}
    >
      <Icon className="mt-0.5 h-3.5 w-3.5 shrink-0" />
      <div className="min-w-0 flex-1 space-y-0.5 break-words">{children}</div>
      {onClose && (
        <button type="button" onClick={onClose} title="关掉" className="shrink-0 opacity-60 hover:opacity-100">
          <X className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  )
}

export function Badge({ tone, title, children }: { tone: 'warn' | 'muted'; title?: string; children: ReactNode }) {
  return (
    <span
      title={title}
      className={cn(
        'shrink-0 rounded px-1.5 py-0.5 text-[10px] font-normal',
        tone === 'warn' && 'bg-amber-500/15 text-amber-700 dark:text-amber-300',
        tone === 'muted' && 'bg-secondary text-muted-foreground',
      )}
    >
      {children}
    </span>
  )
}

/**
 * 底部删除条。永久删除是个显眼的开关而不是藏在确认框里:
 * 点删除之前就该知道这一下会不会进回收站
 */
export function DeleteBar({
  count,
  bytes,
  permanent,
  onPermanent,
  onDelete,
  disabled,
  children,
}: {
  count: number
  bytes: number
  permanent: boolean
  onPermanent: (v: boolean) => void
  onDelete: () => void
  disabled?: boolean
  children?: ReactNode
}) {
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-3 border-t border-border bg-card px-4 py-2.5">
      <span className="text-xs">
        已选 <b>{fmtCount(count)}</b> 个，共 <b>{fmtBytes(bytes)}</b>
      </span>
      {children}
      <label className="ml-auto flex cursor-pointer items-center gap-1.5 text-xs text-muted-foreground">
        <input type="checkbox" checked={permanent} onChange={(e) => onPermanent(e.target.checked)} />
        永久删除（不进回收站）
      </label>
      <Button size="sm" variant="destructive" disabled={disabled || count === 0} onClick={onDelete}>
        {permanent ? '永久删除' : '移到回收站'}
      </Button>
    </div>
  )
}

/**
 * 没权限进的目录。
 *
 * 怎么说取决于两件事:当时是不是管理员(已经是了还劝人提权就是瞎指挥),
 * 以及进不去的在哪儿(系统保护的位置里本来也删不了,别处的才是真漏了)
 */
export function DeniedNotice({ d }: { d: diskclean.DeniedInfo }) {
  if (d.count === 0) return null
  const missed = d.count - d.protected
  const who = d.elevated ? '连管理员身份也进不去' : '没权限进去'
  return (
    <Notice tone={missed > 0 ? 'warn' : 'info'}>
      <div>
        有 {fmtCount(d.count)} 个目录{who}
        {missed === 0
          ? '，都在系统保护的位置——那里的东西本来也删不了，不影响能清理的结果。'
          : `，其中 ${fmtCount(missed)} 个不在系统目录里，那几处的文件没扫到。`}
        {!d.elevated && '以管理员身份运行工具箱能看到这些目录。'}
      </div>
      {d.dirs.length > 0 && (
        <details>
          <summary className="cursor-pointer select-none">看看是哪些</summary>
          <div className="mt-1 space-y-0.5">
            {d.dirs.map((p) => (
              <div key={p} className="break-all font-mono text-[11px]">
                {p}
              </div>
            ))}
            {d.count > d.dirs.length && <div>……还有 {fmtCount(d.count - d.dirs.length)} 个</div>}
          </div>
        </details>
      )}
    </Notice>
  )
}

/** 删完之后的交代:删了多少、进没进回收站、哪些没删 */
export function DeleteSummary({ r, onClose }: { r: diskclean.DeleteResult; onClose: () => void }) {
  return (
    <Notice tone={r.failed > 0 ? 'warn' : 'ok'} onClose={onClose}>
      <div>
        {r.recycled ? '移到了回收站' : '永久删除了'} <b>{fmtCount(r.deleted)}</b> 个文件，共{' '}
        <b>{fmtBytes(r.bytes)}</b>
        {r.cancelled && '，中途停止了'}
      </div>
      {r.recycled && r.deleted > 0 && <div>回收站和这些文件在同一块盘上：清空回收站之后，这些空间才会真正腾出来。</div>}
      {r.failed > 0 && <div>{fmtCount(r.failed)} 个没删，原因标在各自那一行上。</div>}
    </Notice>
  )
}
