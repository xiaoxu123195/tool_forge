import { useEffect, useRef, useState } from 'react'
import { FolderMinus, FolderSearch, Link2Off, Search, Trash2 } from 'lucide-react'
import {
  DeleteBrokenShortcuts,
  DeleteEmptyFolders,
  DiskPlaces,
  RevealInExplorer,
  ScanBrokenShortcuts,
  ScanEmptyFolders,
} from '../../../wailsjs/go/main/App'
import type { diskclean } from '../../../wailsjs/go/models'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { ModeToggle } from '@/components/tool/ModeToggle'
import { cn } from '@/lib/utils'
import { errText, fmtCount, fmtDate, useDiskJob } from './lib'
import { Badge, DeleteBar, DeleteSummary, DeniedNotice, Notice, ProgressLine, RootPicker } from './Shared'

type Kind = 'empty' | 'shortcut'

export function MorePane({ active }: { active: boolean }) {
  const [kind, setKind] = useState<Kind>('empty')
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center gap-3 border-b border-border px-4 py-2">
        <ModeToggle
          value={kind}
          onChange={setKind}
          options={[
            { value: 'empty', label: '空文件夹' },
            { value: 'shortcut', label: '无效快捷方式' },
          ]}
        />
        <span className="text-[11px] text-muted-foreground">
          {kind === 'empty'
            ? '腾不出多少空间，图的是整洁：清掉程序在「文档」里随手建了又不用的空目录'
            : '桌面、开始菜单、任务栏、「发送到」里，指向的东西已经不在了的快捷方式'}
        </span>
      </div>
      {/* 两页都挂着:切过去再切回来,结果还在 */}
      <div className={cn('min-h-0 flex-1 flex-col', kind === 'empty' ? 'flex' : 'hidden')}>
        <EmptyView active={active && kind === 'empty'} />
      </div>
      <div className={cn('min-h-0 flex-1 flex-col', kind === 'shortcut' ? 'flex' : 'hidden')}>
        <ShortcutView active={active && kind === 'shortcut'} />
      </div>
    </div>
  )
}

function EmptyView({ active }: { active: boolean }) {
  const dialog = useConfirm()
  const job = useDiskJob()
  const [places, setPlaces] = useState<diskclean.Places | null>(null)
  const [roots, setRoots] = useState<string[]>([])
  const [skipDev, setSkipDev] = useState(true)
  const [res, setRes] = useState<diskclean.EmptyResult | null>(null)
  const [dirs, setDirs] = useState<diskclean.EmptyDir[]>([])
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [failed, setFailed] = useState<Map<string, string>>(new Map())
  const [summary, setSummary] = useState<diskclean.DeleteResult | null>(null)
  const [err, setErr] = useState('')

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
    setSummary(null)
    setFailed(new Map())
    setPicked(new Set())
    try {
      const r = await job.run((id) =>
        ScanEmptyFolders({ jobId: id, roots, skipDevDirs: skipDev } as diskclean.EmptyOptions),
      )
      setRes(r)
      setDirs(r.dirs)
    } catch (e) {
      setErr(errText(e))
    }
  }

  const allPicked = dirs.length > 0 && dirs.every((d) => picked.has(d.path))
  const toggle = (p: string) =>
    setPicked((prev) => {
      const next = new Set(prev)
      if (next.has(p)) next.delete(p)
      else next.add(p)
      return next
    })

  const del = async () => {
    const chosen = dirs.filter((d) => picked.has(d.path))
    if (chosen.length === 0) return
    const ok = await dialog({
      title: '删除空文件夹',
      danger: true,
      confirmLabel: '删除',
      message: (
        <div className="space-y-1.5">
          <p>删除 {chosen.length} 个空文件夹，连同里面套着的空子文件夹。</p>
          <p>直接删，不进回收站——里面什么都没有。删之前会逐个确认还是空的，这期间放进了东西的不会删。</p>
        </div>
      ),
    })
    if (!ok) return
    setErr('')
    try {
      const r = await job.run((id) =>
        DeleteEmptyFolders({ jobId: id, paths: chosen.map((d) => d.path) } as diskclean.EmptyDeleteRequest),
      )
      const gone = new Set(r.items.filter((i) => i.ok).map((i) => i.path))
      setDirs((prev) => prev.filter((d) => !gone.has(d.path)))
      setFailed(new Map(r.items.filter((i) => !i.ok).map((i) => [i.path, i.reason ?? ''])))
      setPicked(new Set())
      setSummary(r)
    } catch (e) {
      setErr(errText(e))
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 space-y-2.5 border-b border-border px-4 py-3">
        <RootPicker places={places} roots={roots} onChange={setRoots} disabled={job.running} />
        <div className="flex flex-wrap items-center gap-2">
          <label className="flex cursor-pointer items-center gap-1.5 text-xs text-muted-foreground">
            <input
              type="checkbox"
              checked={skipDev}
              onChange={(e) => setSkipDev(e.target.checked)}
              disabled={job.running}
            />
            跳过 node_modules、.git 这类目录
          </label>
          <Button
            size="sm"
            className="ml-auto"
            disabled={job.running || roots.length === 0}
            onClick={() => void scan()}
          >
            <Search className="h-3.5 w-3.5" />
            查找空文件夹
          </Button>
        </div>
        <p className="text-[11px] text-muted-foreground">
          只找连同子目录一个文件都没有的。程序自己的数据目录不进去：AppData、以 . 开头的目录、微信 QQ 放在文档里的目录、网盘的同步目录（在里面删会同步到云端）。一天以内建的也不算，多半是马上要用的。
        </p>
      </div>

      {job.running && <ProgressLine p={job.progress} onCancel={job.cancel} />}

      <div className="min-h-0 flex-1 space-y-2 overflow-auto p-4">
        {err && <Notice tone="error">{err}</Notice>}
        {summary && <DeleteSummary r={summary} onClose={() => setSummary(null)} />}
        {res && (
          <>
            <Notice>
              {dirs.length > 0 ? (
                <>
                  找到 <b>{fmtCount(dirs.length)}</b> 个空文件夹
                </>
              ) : (
                '没有找到空文件夹'
              )}
              {res.recent > 0 && `（另有 ${fmtCount(res.recent)} 个是一天以内动过的，没列出来）`}
              {' · '}看了 {fmtCount(res.scanned)} 个目录
              {res.truncated && ' · 太多了，只列出前面的一部分'}
              {res.cancelled && ' · 中途停止了，没有给出结果'}
            </Notice>
            <DeniedNotice d={res.denied} />
          </>
        )}
        {dirs.length > 0 && (
          <div className="overflow-hidden rounded-lg border border-border" data-list="empty">
            <div className="flex items-center gap-2 border-b border-border bg-muted/30 px-3 py-1.5 text-[11px] text-muted-foreground">
              <input
                type="checkbox"
                title="全选"
                checked={allPicked}
                disabled={job.running}
                onChange={() => setPicked(allPicked ? new Set() : new Set(dirs.map((d) => d.path)))}
              />
              全选
            </div>
            {dirs.map((d) => (
              <div key={d.path} data-path={d.path} className="flex items-start gap-2 border-b border-border/50 px-3 py-1.5 last:border-b-0 hover:bg-muted/30">
                <input
                  type="checkbox"
                  className="mt-1"
                  checked={picked.has(d.path)}
                  disabled={job.running}
                  onChange={() => toggle(d.path)}
                />
                <FolderMinus className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <div className="truncate font-mono text-xs" title={d.path}>
                    {d.path}
                  </div>
                  {d.nested > 0 && (
                    <div className="text-[11px] text-muted-foreground">里面还套着 {d.nested} 个空文件夹，会一起删掉</div>
                  )}
                  {failed.get(d.path) && (
                    <div className="text-[11px] text-destructive">没删：{failed.get(d.path)}</div>
                  )}
                </div>
                <span className="shrink-0 text-[11px] text-muted-foreground">{fmtDate(d.modTime)}</span>
                <button
                  type="button"
                  title="在文件夹中显示"
                  onClick={() => void RevealInExplorer(d.path)}
                  className="shrink-0 text-muted-foreground hover:text-foreground"
                >
                  <FolderSearch className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {picked.size > 0 && (
        <div className="flex shrink-0 items-center gap-3 border-t border-border bg-card px-4 py-2.5">
          <span className="text-xs">
            已选 <b>{fmtCount(picked.size)}</b> 个空文件夹
          </span>
          <Button size="sm" variant="destructive" className="ml-auto" disabled={job.running} onClick={() => void del()}>
            <Trash2 className="h-3.5 w-3.5" />
            删除选中的空文件夹
          </Button>
        </div>
      )}
    </div>
  )
}

function ShortcutView({ active }: { active: boolean }) {
  const dialog = useConfirm()
  const job = useDiskJob()
  const [res, setRes] = useState<diskclean.ShortcutResult | null>(null)
  const [items, setItems] = useState<diskclean.BrokenShortcut[]>([])
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [failed, setFailed] = useState<Map<string, string>>(new Map())
  const [permanent, setPermanent] = useState(false)
  const [summary, setSummary] = useState<diskclean.DeleteResult | null>(null)
  const [err, setErr] = useState('')

  const scan = async () => {
    setErr('')
    setSummary(null)
    setFailed(new Map())
    setPicked(new Set())
    try {
      const r = await job.run((id) => ScanBrokenShortcuts(id))
      setRes(r)
      setItems(r.shortcuts)
    } catch (e) {
      setErr(errText(e))
    }
  }

  // 第一次切过来就查一遍:几百个快捷方式,一两秒的事
  const started = useRef(false)
  useEffect(() => {
    if (active && !started.current) {
      started.current = true
      void scan()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active])

  const pickable = items.filter((s) => !s.blocked)
  const allPicked = pickable.length > 0 && pickable.every((s) => picked.has(s.path))
  const chosen = items.filter((s) => picked.has(s.path))

  const del = async () => {
    if (chosen.length === 0) return
    const ok = await dialog({
      title: permanent ? '永久删除快捷方式' : '把快捷方式移到回收站',
      danger: true,
      confirmLabel: permanent ? '永久删除' : '移到回收站',
      message: (
        <div className="space-y-1.5">
          <p>
            {permanent ? '永久删除' : '把'} {chosen.length} 个无效的快捷方式{permanent ? '。' : '移到回收站。'}
          </p>
          <p>删的只是快捷方式本身。删之前会再看一次，它指向的东西要是又找得到了（盘接上了、软件重装了），就不删。</p>
        </div>
      ),
    })
    if (!ok) return
    setErr('')
    try {
      const r = await job.run((id) =>
        DeleteBrokenShortcuts({
          jobId: id,
          files: chosen.map((s) => ({ path: s.path, size: s.size, modTime: s.modTime })),
          permanent,
        } as diskclean.DeleteRequest),
      )
      const gone = new Set(r.items.filter((i) => i.ok).map((i) => i.path))
      setItems((prev) => prev.filter((s) => !gone.has(s.path)))
      setFailed(new Map(r.items.filter((i) => !i.ok).map((i) => [i.path, i.reason ?? ''])))
      setPicked(new Set())
      setSummary(r)
    } catch (e) {
      setErr(errText(e))
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b border-border px-4 py-3">
        <span className="text-xs text-muted-foreground">
          判断不准的一律不报：指向网络位置、U 盘、这会儿没接上的盘，或者控制面板、应用商店应用这类不是文件的
        </span>
        <Button size="sm" variant="outline" className="ml-auto" disabled={job.running} onClick={() => void scan()}>
          <Search className="h-3.5 w-3.5" />
          重新检查
        </Button>
      </div>

      {job.running && <ProgressLine p={job.progress} onCancel={job.cancel} />}

      <div className="min-h-0 flex-1 space-y-2 overflow-auto p-4">
        {err && <Notice tone="error">{err}</Notice>}
        {summary && <DeleteSummary r={summary} onClose={() => setSummary(null)} />}
        {res && !res.supported && <Notice>快捷方式(.lnk)是 Windows 上的东西，这个系统上没有可查的。</Notice>}
        {res && res.supported && (
          <Notice>
            检查了 {fmtCount(res.scanned)} 个快捷方式，无效的 <b>{fmtCount(items.length)}</b> 个
            {res.unknown > 0 && `；另有 ${fmtCount(res.unknown)} 个判断不了，没列出来`}
            {res.cancelled && ' · 中途停止了，结果不完整'}
          </Notice>
        )}
        {items.length > 0 && (
          <div className="overflow-hidden rounded-lg border border-border" data-list="shortcuts">
            <div className="flex items-center gap-2 border-b border-border bg-muted/30 px-3 py-1.5 text-[11px] text-muted-foreground">
              <input
                type="checkbox"
                title="全选"
                checked={allPicked}
                disabled={job.running || pickable.length === 0}
                onChange={() => setPicked(allPicked ? new Set() : new Set(pickable.map((s) => s.path)))}
              />
              全选
            </div>
            {items.map((s) => (
              <div
                key={s.path}
                data-path={s.path}
                className={cn(
                  'flex items-start gap-2 border-b border-border/50 px-3 py-1.5 last:border-b-0 hover:bg-muted/30',
                  s.blocked && 'opacity-60',
                )}
              >
                <input
                  type="checkbox"
                  className="mt-1"
                  checked={picked.has(s.path)}
                  disabled={s.blocked || job.running}
                  title={s.blocked ? s.reason : undefined}
                  onChange={() =>
                    setPicked((prev) => {
                      const next = new Set(prev)
                      if (next.has(s.path)) next.delete(s.path)
                      else next.add(s.path)
                      return next
                    })
                  }
                />
                <Link2Off className="mt-0.5 h-4 w-4 shrink-0 text-amber-500" />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5 text-sm">
                    <span className="truncate">{s.name}</span>
                    <Badge tone="muted">{s.location}</Badge>
                  </div>
                  <div className="truncate text-[11px] text-muted-foreground" title={s.target}>
                    指向 <span className="font-mono">{s.target}</span>，已经不在了
                  </div>
                  {s.blocked && <div className="text-[11px] text-muted-foreground">不能删：{s.reason}</div>}
                  {failed.get(s.path) && (
                    <div className="text-[11px] text-destructive">没删：{failed.get(s.path)}</div>
                  )}
                </div>
                <button
                  type="button"
                  title="在文件夹中显示这个快捷方式"
                  onClick={() => void RevealInExplorer(s.path)}
                  className="shrink-0 text-muted-foreground hover:text-foreground"
                >
                  <FolderSearch className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {chosen.length > 0 && (
        <DeleteBar
          count={chosen.length}
          bytes={chosen.reduce((n, s) => n + s.size, 0)}
          permanent={permanent}
          onPermanent={setPermanent}
          onDelete={() => void del()}
          disabled={job.running}
        />
      )}
    </div>
  )
}
