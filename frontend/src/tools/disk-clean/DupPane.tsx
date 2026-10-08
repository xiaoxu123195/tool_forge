import { useEffect, useRef, useState } from 'react'
import { Copy, FolderSearch, Search, Wand2 } from 'lucide-react'
import {
  DeleteDuplicateFiles,
  DiskPlaces,
  RevealInExplorer,
  ScanDuplicateFiles,
} from '../../../wailsjs/go/main/App'
import type { diskclean } from '../../../wailsjs/go/models'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { cn } from '@/lib/utils'
import { errText, fmtBytes, fmtCount, fmtDate, useDiskJob } from './lib'
import { DeleteBar, DeleteSummary, DeniedNotice, Notice, ProgressLine, RootPicker } from './Shared'

/** 删除之后组会变,所以用自己的形状存,不直接改后端给的那个对象 */
interface Group {
  id: string
  size: number
  files: diskclean.DupFile[]
}

const SIZES = [
  { label: '100 KB', v: 100 << 10 },
  { label: '1 MB', v: 1 << 20 },
  { label: '10 MB', v: 10 << 20 },
  { label: '100 MB', v: 100 << 20 },
]

const PAGE = 100

/**
 * 每组留哪一份:
 *   - 删不掉的那份(系统保护的)本来就留着
 *   - 其次留带提醒的:微信、QQ 收到的文件被聊天记录引用着,网盘同步目录里的删了会同步到云端 ——
 *     同样的内容别处还有一份,删那一份才没有副作用
 *   - 再其次留修改时间最早的:原件一般最早,复制出来的晚;再一样就留路径短的
 */
function keeperOf(g: Group): diskclean.DupFile {
  return [...g.files].sort(
    (a, b) =>
      Number(b.blocked) - Number(a.blocked) ||
      Number(!!b.warn) - Number(!!a.warn) ||
      a.modTime - b.modTime ||
      a.path.length - b.path.length ||
      a.path.localeCompare(b.path),
  )[0]
}

export function DupPane({ active }: { active: boolean }) {
  const dialog = useConfirm()
  const job = useDiskJob()
  const [places, setPlaces] = useState<diskclean.Places | null>(null)
  const [roots, setRoots] = useState<string[]>([])
  const [minSize, setMinSize] = useState(1 << 20)
  const [skipDev, setSkipDev] = useState(true)
  const [res, setRes] = useState<diskclean.DupResult | null>(null)
  const [groups, setGroups] = useState<Group[]>([])
  const [marked, setMarked] = useState<Set<string>>(new Set())
  const [failed, setFailed] = useState<Map<string, string>>(new Map())
  const [hint, setHint] = useState('')
  const [shown, setShown] = useState(PAGE)
  const [permanent, setPermanent] = useState(false)
  const [summary, setSummary] = useState<diskclean.DeleteResult | null>(null)
  const [err, setErr] = useState('')

  // 默认扫个人目录:整块盘里的重复大多在系统和程序目录,那些本来就删不得
  const loaded = useRef(false)
  useEffect(() => {
    if (!active || loaded.current) return
    loaded.current = true
    DiskPlaces()
      .then((p) => {
        setPlaces(p)
        setRoots((prev) => (prev.length || !p.home ? prev : [p.home]))
      })
      .catch((e) => setErr(errText(e)))
  }, [active])

  const scan = async () => {
    setErr('')
    setHint('')
    setSummary(null)
    setFailed(new Map())
    setMarked(new Set())
    setShown(PAGE)
    try {
      const r = await job.run((id) =>
        ScanDuplicateFiles({ jobId: id, roots, minSize, skipDevDirs: skipDev } as diskclean.DupOptions),
      )
      setRes(r)
      setGroups(r.groups.map((g) => ({ id: g.id, size: g.size, files: g.files })))
    } catch (e) {
      setErr(errText(e))
    }
  }

  const toggle = (g: Group, path: string) => {
    setHint('')
    const next = new Set(marked)
    if (next.has(path)) {
      next.delete(path)
    } else {
      // 一组里至少留一份。后端也拦这一条,前端先拦是为了当场说清楚为什么勾不上
      if (!g.files.some((f) => f.path !== path && !next.has(f.path))) {
        setHint('每组至少要留一份，不然这份内容就彻底没了')
        return
      }
      next.add(path)
    }
    setMarked(next)
  }

  const autoPick = () => {
    setHint('')
    const next = new Set<string>()
    for (const g of groups) {
      const keep = keeperOf(g)
      for (const f of g.files) if (f.path !== keep.path && !f.blocked) next.add(f.path)
    }
    setMarked(next)
  }

  const chosenGroups = groups.filter((g) => g.files.some((f) => marked.has(f.path)))
  const chosenCount = marked.size
  const chosenBytes = chosenGroups.reduce(
    (s, g) => s + g.size * g.files.filter((f) => marked.has(f.path)).length,
    0,
  )

  const del = async () => {
    if (chosenCount === 0) return
    const warns = groups.flatMap((g) => g.files.filter((f) => marked.has(f.path) && f.warn))
    const ok = await dialog({
      title: permanent ? '永久删除重复文件' : '把重复文件移到回收站',
      danger: true,
      confirmLabel: permanent ? '永久删除' : '移到回收站',
      message: (
        <div className="space-y-1.5">
          <p>
            从 {chosenGroups.length} 组里{permanent ? '永久删除' : '移走'} {chosenCount} 个文件，共{' '}
            <b className="text-foreground">{fmtBytes(chosenBytes)}</b>，每组都留着没勾的那份。
          </p>
          <p>删之前会逐个重新核对内容：扫描之后改过的、要留的那份已经不在的，都不会删。</p>
          {!permanent && <p>回收站和这些文件在同一块盘上：清空回收站之后，空间才会真正腾出来。</p>}
          {warns.length > 0 && (
            <p className="text-amber-700 dark:text-amber-400">
              其中 {warns.length} 个是需要特别注意的类型（{warns[0].name}：{warns[0].warn}）。
            </p>
          )}
        </div>
      ),
    })
    if (!ok) return
    setErr('')
    try {
      const r = await job.run((id) =>
        DeleteDuplicateFiles({
          jobId: id,
          permanent,
          groups: chosenGroups.map((g) => ({
            id: g.id,
            size: g.size,
            keep: g.files.filter((f) => !marked.has(f.path)).map((f) => f.path),
            delete: g.files.filter((f) => marked.has(f.path)).map((f) => f.path),
          })),
        } as diskclean.DupDeleteRequest),
      )
      const gone = new Set(r.items.filter((i) => i.ok).map((i) => i.path))
      setGroups((prev) =>
        prev
          .map((g) => ({ ...g, files: g.files.filter((f) => !gone.has(f.path)) }))
          .filter((g) => g.files.length > 1),
      )
      setFailed(new Map(r.items.filter((i) => !i.ok).map((i) => [i.path, i.reason ?? ''])))
      setMarked(new Set())
      setSummary(r)
    } catch (e) {
      setErr(errText(e))
    }
  }

  const wasted = groups.reduce((s, g) => s + g.size * (g.files.length - 1), 0)

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 space-y-2.5 border-b border-border px-4 py-3">
        <RootPicker places={places} roots={roots} onChange={setRoots} disabled={job.running} />
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-muted-foreground">只看大于</span>
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
          <span className="text-xs text-muted-foreground">的文件</span>
          <label className="ml-2 flex cursor-pointer items-center gap-1.5 text-xs text-muted-foreground">
            <input type="checkbox" checked={skipDev} onChange={(e) => setSkipDev(e.target.checked)} disabled={job.running} />
            跳过 node_modules、.git 这类目录（里面的重复是故意的，删了项目就坏）
          </label>
          <Button
            size="sm"
            className="ml-auto"
            disabled={job.running || roots.length === 0}
            onClick={() => void scan()}
          >
            <Search className="h-3.5 w-3.5" />
            开始查找
          </Button>
        </div>
      </div>

      {job.running && <ProgressLine p={job.progress} onCancel={job.cancel} />}

      <div className="min-h-0 flex-1 space-y-2 overflow-auto p-4">
        {err && <Notice tone="error">{err}</Notice>}
        {summary && <DeleteSummary r={summary} onClose={() => setSummary(null)} />}
        {res && (
          <>
            <Notice>
              {groups.length > 0 ? (
                <>
                  找到 <b>{fmtCount(groups.length)}</b> 组重复，每组只留一份能腾出 <b>{fmtBytes(wasted)}</b>
                </>
              ) : (
                '没有找到重复文件'
              )}
              {' · '}看了 {fmtCount(res.scanned)} 个文件，实际读了 {fmtBytes(res.hashedBytes)} 去比内容 · 用时{' '}
              {(res.elapsedMs / 1000).toFixed(1)} 秒
              {res.totalGroups > res.groups.length && `（组太多，只列出最占地方的 ${res.groups.length} 组）`}
              {res.cancelled && ' · 中途停止了，结果不完整'}
            </Notice>
            {(res.hardlinks > 0 || res.unreadable > 0 || res.skippedLinks > 0) && (
              <Notice>
                {res.hardlinks > 0 && <div>{fmtCount(res.hardlinks)} 个是同一个文件的另一个名字（硬链接），不算重复：删了腾不出空间。</div>}
                {res.unreadable > 0 && <div>{fmtCount(res.unreadable)} 个读不了内容（被占用或没权限），没参与比对。</div>}
                {res.skippedLinks > 0 && <div>跳过了 {fmtCount(res.skippedLinks)} 个链接、目录联接和网盘同步的文件。</div>}
              </Notice>
            )}
            <DeniedNotice d={res.denied} />
          </>
        )}

        {groups.length > 0 && (
          <div className="flex flex-wrap items-center gap-2">
            <Button size="sm" variant="outline" disabled={job.running} onClick={autoPick} title="每组保留修改时间最早的那份（一般就是原件），其余勾上">
              <Wand2 className="h-3.5 w-3.5" />
              每组只留一份
            </Button>
            <Button size="sm" variant="ghost" disabled={job.running || marked.size === 0} onClick={() => setMarked(new Set())}>
              全不选
            </Button>
            {hint && <span className="text-xs text-amber-600 dark:text-amber-400">{hint}</span>}
          </div>
        )}

        <div className="space-y-2" data-list="dup">
          {groups.slice(0, shown).map((g) => (
            <GroupCard
              key={g.id}
              g={g}
              marked={marked}
              failed={failed}
              disabled={job.running}
              onToggle={(p) => toggle(g, p)}
            />
          ))}
        </div>
        {groups.length > shown && (
          <Button size="sm" variant="ghost" className="w-full" onClick={() => setShown((n) => n + PAGE)}>
            再显示 {Math.min(PAGE, groups.length - shown)} 组（还有 {groups.length - shown} 组）
          </Button>
        )}
      </div>

      {chosenCount > 0 && (
        <DeleteBar
          count={chosenCount}
          bytes={chosenBytes}
          permanent={permanent}
          onPermanent={setPermanent}
          onDelete={() => void del()}
          disabled={job.running}
        >
          <span className="text-xs text-muted-foreground">来自 {chosenGroups.length} 组</span>
        </DeleteBar>
      )}
    </div>
  )
}

function GroupCard({
  g,
  marked,
  failed,
  disabled,
  onToggle,
}: {
  g: Group
  marked: Set<string>
  failed: Map<string, string>
  disabled: boolean
  onToggle: (path: string) => void
}) {
  return (
    <div className="rounded-lg border border-border bg-card" data-group={g.id}>
      <div className="flex items-center gap-2 border-b border-border/60 px-3 py-1.5 text-xs">
        <Copy className="h-3.5 w-3.5 text-info" />
        <span>
          <b>{g.files.length}</b> 份 · 每份 {fmtBytes(g.size)}
        </span>
        <span className="text-muted-foreground">只留一份能腾出 {fmtBytes(g.size * (g.files.length - 1))}</span>
        <code className="ml-auto font-mono text-[10px] text-muted-foreground" title={`内容的 SHA-256：${g.id}`}>
          {g.id.slice(0, 12)}
        </code>
      </div>
      {g.files.map((f) => {
        const del = marked.has(f.path)
        const why = failed.get(f.path)
        return (
          <div key={f.path} className="flex items-start gap-2 px-3 py-1 hover:bg-muted/30">
            <input
              type="checkbox"
              className="mt-0.5"
              checked={del}
              disabled={f.blocked || disabled}
              onChange={() => onToggle(f.path)}
              title={f.blocked ? f.reason : del ? '取消，保留这份' : '勾上，删掉这份'}
            />
            <div className="min-w-0 flex-1">
              <div className={cn('truncate font-mono text-xs', del && 'text-muted-foreground line-through')} title={f.path}>
                {f.path}
              </div>
              {f.blocked && <div className="text-[11px] text-muted-foreground">不能删：{f.reason}</div>}
              {f.warn && <div className="text-[11px] text-amber-600 dark:text-amber-400">{f.warn}</div>}
              {why && <div className="text-[11px] text-destructive">没删：{why}</div>}
            </div>
            <span className="shrink-0 text-[11px] text-muted-foreground">{fmtDate(f.modTime)}</span>
            <span className={cn('w-8 shrink-0 text-right text-[11px]', del ? 'text-destructive' : 'text-success')}>
              {del ? '删' : '留'}
            </span>
            <button
              type="button"
              title="在文件夹中显示"
              onClick={() => void RevealInExplorer(f.path)}
              className="shrink-0 text-muted-foreground hover:text-foreground"
            >
              <FolderSearch className="h-3.5 w-3.5" />
            </button>
          </div>
        )
      })}
    </div>
  )
}
