import { useEffect, useMemo, useRef, useState } from 'react'
import { AlertTriangle, File as FileIcon, FolderSearch, Lock, Search } from 'lucide-react'
import { DeleteDiskFiles, DiskPlaces, RevealInExplorer, ScanLargeFiles } from '../../../wailsjs/go/main/App'
import type { diskclean } from '../../../wailsjs/go/models'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { cn } from '@/lib/utils'
import { errText, fmtBytes, fmtCount, fmtDate, useDiskJob } from './lib'
import { DeleteBar, DeleteSummary, DeniedNotice, Notice, ProgressLine, RootPicker } from './Shared'

type Big = diskclean.LargeFile

const SIZES = [
  { label: '50 MB', v: 50 << 20 },
  { label: '100 MB', v: 100 << 20 },
  { label: '500 MB', v: 500 << 20 },
  { label: '1 GB', v: 1 << 30 },
  { label: '5 GB', v: 5 * (1 << 30) },
]

/** 超过这个大小的文件,多半放不进回收站 —— 那时系统会再弹窗问一次 */
const RECYCLE_HINT = 4 * (1 << 30)

export function LargePane({ active }: { active: boolean }) {
  const dialog = useConfirm()
  const job = useDiskJob()
  const [places, setPlaces] = useState<diskclean.Places | null>(null)
  const [roots, setRoots] = useState<string[]>([])
  const [minSize, setMinSize] = useState(100 << 20)
  // 扫描的汇总和文件列表分开放:列表会随着删除变,汇总是那一次扫描的事实
  const [res, setRes] = useState<diskclean.LargeResult | null>(null)
  const [files, setFiles] = useState<Big[]>([])
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [failed, setFailed] = useState<Map<string, string>>(new Map())
  const [filter, setFilter] = useState('')
  const [sort, setSort] = useState<'size' | 'date' | 'name'>('size')
  const [permanent, setPermanent] = useState(false)
  const [summary, setSummary] = useState<diskclean.DeleteResult | null>(null)
  const [err, setErr] = useState('')

  // 默认扫系统盘:「C 盘满了」是最常见的来由
  const loaded = useRef(false)
  useEffect(() => {
    if (!active || loaded.current) return
    loaded.current = true
    DiskPlaces()
      .then((p) => {
        setPlaces(p)
        const sys = p.volumes.find((v) => v.system)
        setRoots((prev) => (prev.length ? prev : sys ? [sys.path] : p.home ? [p.home] : []))
      })
      .catch((e) => setErr(errText(e)))
  }, [active])

  const scan = async () => {
    setErr('')
    setSummary(null)
    setFailed(new Map())
    setPicked(new Set())
    try {
      const r = await job.run((id) =>
        ScanLargeFiles({ jobId: id, roots, minSize, limit: 500 } as diskclean.LargeOptions),
      )
      setRes(r)
      setFiles(r.files)
    } catch (e) {
      setErr(errText(e))
    }
  }

  const view = useMemo(() => {
    const q = filter.trim().toLowerCase()
    const list = q ? files.filter((f) => f.path.toLowerCase().includes(q)) : [...files]
    if (sort === 'size') list.sort((a, b) => b.size - a.size)
    if (sort === 'date') list.sort((a, b) => a.modTime - b.modTime)
    if (sort === 'name') list.sort((a, b) => a.name.localeCompare(b.name))
    return list
  }, [files, filter, sort])

  const chosen = files.filter((f) => picked.has(f.path))
  const chosenBytes = chosen.reduce((s, f) => s + f.size, 0)
  // 全选不带上有提醒的(虚拟机磁盘、Outlook 数据):那几类要一个个看清楚了单独勾
  const pickable = view.filter((f) => !f.blocked && !f.warn)
  const allPicked = pickable.length > 0 && pickable.every((f) => picked.has(f.path))

  const toggle = (p: string) =>
    setPicked((prev) => {
      const next = new Set(prev)
      if (next.has(p)) next.delete(p)
      else next.add(p)
      return next
    })

  const del = async () => {
    if (chosen.length === 0) return
    const warns = chosen.filter((f) => f.warn)
    const ok = await dialog({
      title: permanent ? '永久删除' : '移到回收站',
      danger: true,
      confirmLabel: permanent ? '永久删除' : '移到回收站',
      message: (
        <div className="space-y-1.5">
          <p>
            {permanent ? '永久删除' : '把'} {chosen.length} 个文件（共{' '}
            <b className="text-foreground">{fmtBytes(chosenBytes)}</b>）
            {permanent ? '，删了就找不回来了。' : '移到回收站。'}
          </p>
          {!permanent && <p>回收站和这些文件在同一块盘上：清空回收站之后，空间才会真正腾出来。</p>}
          {!permanent && chosen.some((f) => f.size > RECYCLE_HINT) && (
            <p>其中有超过 4 GB 的文件，可能放不进回收站——那时 Windows 会再弹窗问一次要不要直接删。</p>
          )}
          {warns.length > 0 && (
            <div className="space-y-0.5 rounded-md border border-amber-500/30 bg-amber-500/10 p-2 text-amber-700 dark:text-amber-400">
              <p className="font-medium">这几个要特别注意：</p>
              {warns.slice(0, 5).map((f) => (
                <p key={f.path} className="break-all">
                  {f.name}：{f.warn}
                </p>
              ))}
              {warns.length > 5 && <p>……还有 {warns.length - 5} 个</p>}
            </div>
          )}
        </div>
      ),
    })
    if (!ok) return
    setErr('')
    try {
      const r = await job.run((id) =>
        DeleteDiskFiles({
          jobId: id,
          files: chosen.map((f) => ({ path: f.path, size: f.size, modTime: f.modTime })),
          permanent,
        } as diskclean.DeleteRequest),
      )
      const gone = new Set(r.items.filter((i) => i.ok).map((i) => i.path))
      setFiles((prev) => prev.filter((f) => !gone.has(f.path)))
      setFailed(new Map(r.items.filter((i) => !i.ok).map((i) => [i.path, i.reason ?? ''])))
      setPicked(new Set())
      setSummary(r)
    } catch (e) {
      setErr(errText(e))
    }
  }

  const minLabel = SIZES.find((s) => s.v === minSize)?.label ?? fmtBytes(minSize)

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 space-y-2.5 border-b border-border px-4 py-3">
        <RootPicker places={places} roots={roots} onChange={setRoots} disabled={job.running} />
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-muted-foreground">大于</span>
          <select
            value={minSize}
            onChange={(e) => setMinSize(Number(e.target.value))}
            disabled={job.running}
            className="h-8 rounded-md border border-input bg-background px-2 text-xs"
          >
            {SIZES.map((s) => (
              <option key={s.v} value={s.v}>
                {s.label}
              </option>
            ))}
          </select>
          <span className="text-xs text-muted-foreground">的文件，列出最大的 500 个</span>
          <Button
            size="sm"
            className="ml-auto"
            disabled={job.running || roots.length === 0}
            onClick={() => void scan()}
          >
            <Search className="h-3.5 w-3.5" />
            开始扫描
          </Button>
        </div>
      </div>

      {job.running && <ProgressLine p={job.progress} onCancel={job.cancel} />}

      <div className="min-h-0 flex-1 overflow-auto">
        <div className="space-y-2 p-4 pb-2">
          {err && <Notice tone="error">{err}</Notice>}
          {summary && <DeleteSummary r={summary} onClose={() => setSummary(null)} />}
          {res && (
            <>
              <Notice>
                超过 {minLabel} 的有 <b>{fmtCount(res.matched)}</b> 个，共 <b>{fmtBytes(res.matchedBytes)}</b>
                {res.matched > res.files.length && `（只列出最大的 ${res.files.length} 个）`}
                {' · '}看了 {fmtCount(res.scanned)} 个文件 · 用时 {(res.elapsedMs / 1000).toFixed(1)} 秒
                {res.cancelled && ' · 中途停止了，结果不完整'}
              </Notice>
              <DeniedNotice d={res.denied} />
              {res.skippedLinks > 0 && (
                <Notice>
                  跳过了 {fmtCount(res.skippedLinks)} 个链接、目录联接和网盘同步的文件：前两种跟进去会走到别处，网盘文件删了会连云端一起删。
                </Notice>
              )}
            </>
          )}
        </div>

        {res && files.length > 0 && (
          <>
            <div className="sticky top-0 z-10 flex items-center gap-3 border-y border-border bg-card px-4 py-1.5 text-[11px] text-muted-foreground">
              <input
                type="checkbox"
                title="全选当前列表里能删的（带提醒的要单独勾）"
                checked={allPicked}
                disabled={job.running || pickable.length === 0}
                onChange={() =>
                  setPicked(allPicked ? new Set() : new Set(pickable.map((f) => f.path)))
                }
              />
              <input
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder="按路径筛选，比如 .iso 或 Downloads"
                className="h-7 min-w-0 flex-1 rounded-md border border-input bg-background px-2 text-xs text-foreground outline-none focus:ring-1 focus:ring-ring"
              />
              <span>排序</span>
              {(
                [
                  ['size', '大小'],
                  ['date', '最旧'],
                  ['name', '名称'],
                ] as const
              ).map(([k, label]) => (
                <button
                  key={k}
                  type="button"
                  onClick={() => setSort(k)}
                  className={cn('rounded px-1.5 py-0.5', sort === k ? 'bg-secondary text-foreground' : 'hover:bg-secondary/60')}
                >
                  {label}
                </button>
              ))}
            </div>
            <div data-list="large">
              {view.map((f) => (
                <LargeRow
                  key={f.path}
                  f={f}
                  picked={picked.has(f.path)}
                  failed={failed.get(f.path)}
                  disabled={job.running}
                  onToggle={() => toggle(f.path)}
                />
              ))}
              {view.length === 0 && (
                <p className="p-6 text-center text-xs text-muted-foreground">没有路径里带「{filter}」的</p>
              )}
            </div>
          </>
        )}
        {res && files.length === 0 && !res.cancelled && (
          <p className="p-6 text-center text-sm text-muted-foreground">没有超过 {minLabel} 的文件</p>
        )}
      </div>

      {chosen.length > 0 && (
        <DeleteBar
          count={chosen.length}
          bytes={chosenBytes}
          permanent={permanent}
          onPermanent={setPermanent}
          onDelete={() => void del()}
          disabled={job.running}
        />
      )}
    </div>
  )
}

function LargeRow({
  f,
  picked,
  failed,
  disabled,
  onToggle,
}: {
  f: Big
  picked: boolean
  failed?: string
  disabled: boolean
  onToggle: () => void
}) {
  const Icon = f.blocked ? Lock : f.warn ? AlertTriangle : FileIcon
  return (
    <div
      data-path={f.path}
      className={cn(
        'flex items-start gap-3 border-b border-border/50 px-4 py-1.5 hover:bg-muted/30',
        f.blocked && 'opacity-60',
      )}
    >
      <input
        type="checkbox"
        className="mt-1"
        checked={picked}
        disabled={f.blocked || disabled}
        onChange={onToggle}
        title={f.blocked ? f.reason : undefined}
      />
      <Icon
        className={cn(
          'mt-0.5 h-4 w-4 shrink-0',
          f.blocked ? 'text-muted-foreground' : f.warn ? 'text-amber-500' : 'text-info',
        )}
      />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm" title={f.path}>
          {f.name}
        </div>
        <div className="truncate font-mono text-[11px] text-muted-foreground" title={f.dir}>
          {f.dir}
        </div>
        {f.blocked && <div className="text-[11px] text-muted-foreground">不能删：{f.reason}</div>}
        {f.warn && <div className="text-[11px] text-amber-600 dark:text-amber-400">{f.warn}</div>}
        {failed && <div className="text-[11px] text-destructive">没删：{failed}</div>}
      </div>
      <div className="w-20 shrink-0 pt-0.5 text-right text-[11px] text-muted-foreground">{fmtDate(f.modTime)}</div>
      <div className="w-20 shrink-0 text-right text-sm tabular-nums">{fmtBytes(f.size)}</div>
      <button
        type="button"
        title="在文件夹中显示"
        onClick={() => void RevealInExplorer(f.path)}
        className="mt-0.5 shrink-0 text-muted-foreground hover:text-foreground"
      >
        <FolderSearch className="h-4 w-4" />
      </button>
    </div>
  )
}
