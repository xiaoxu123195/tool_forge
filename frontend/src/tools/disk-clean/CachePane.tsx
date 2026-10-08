import { useEffect, useRef, useState } from 'react'
import { ChevronDown, FolderOpen, FolderPlus, RefreshCw, Trash2, X } from 'lucide-react'
import {
  CleanCacheRules,
  DeleteCustomCacheRule,
  OpenInExplorer,
  PickDirectory,
  SaveCustomCacheRule,
  ScanCacheRules,
} from '../../../wailsjs/go/main/App'
import type { diskclean } from '../../../wailsjs/go/models'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { cn } from '@/lib/utils'
import { errText, fmtBytes, fmtCount, useDiskJob } from './lib'
import { Badge, Notice, ProgressLine } from './Shared'

type Item = diskclean.CacheItem

/** 能不能勾:这台机器上有、有东西可清、权限够 */
const selectable = (it?: Item) => !!it && it.found && !it.needAdmin && it.files > 0

export function CachePane({ active }: { active: boolean }) {
  const dialog = useConfirm()
  const job = useDiskJob()
  const [scan, setScan] = useState<diskclean.CacheScanResult | null>(null)
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [open, setOpen] = useState<Set<string>>(new Set())
  const [err, setErr] = useState('')
  const [result, setResult] = useState<diskclean.CacheCleanResult | null>(null)
  // 用户动过勾选之后,重新扫描不再套回默认 —— 清完自动重扫一遍时,人家的选择不能被冲掉
  const touched = useRef(false)

  const doScan = async () => {
    setErr('')
    try {
      const r = await job.run((id) => ScanCacheRules(id))
      setScan(r)
      setPicked((prev) => {
        const base = touched.current ? prev : new Set(r.items.filter((i) => i.default).map((i) => i.id))
        return new Set([...base].filter((id) => selectable(r.items.find((i) => i.id === id))))
      })
    } catch (e) {
      setErr(errText(e))
    }
  }

  // 第一次切到这一页时自动统计一遍:只是数文件,几秒钟的事,省得人对着一片空白
  const started = useRef(false)
  useEffect(() => {
    if (active && !started.current) {
      started.current = true
      void doScan()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active])

  const items = scan?.items ?? []
  // 自己加的目录就算没了也要列出来:不然想移除这条规则都找不到地方点
  const shown = items.filter((i) => i.found || i.custom)
  const hidden = items.length - shown.length
  const byGroup = new Map<string, Item[]>()
  for (const it of shown) byGroup.set(it.group, [...(byGroup.get(it.group) ?? []), it])
  const groups = [...byGroup.entries()]
  const chosen = shown.filter((i) => picked.has(i.id))
  const chosenBytes = chosen.reduce((s, i) => s + i.size, 0)
  const availBytes = shown.filter(selectable).reduce((s, i) => s + i.size, 0)
  const needAdmin = shown.some((i) => i.needAdmin && i.files > 0)

  const toggle = (id: string) => {
    touched.current = true
    setPicked((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const clean = async () => {
    if (chosen.length === 0) return
    const running = [...new Set(chosen.flatMap((i) => i.running))]
    const ok = await dialog({
      title: '清理缓存',
      danger: true,
      confirmLabel: '清理',
      message: (
        <div className="space-y-1.5">
          <p>
            清理 {chosen.length} 项，约 <b className="text-foreground">{fmtBytes(chosenBytes)}</b>。
          </p>
          <p>缓存直接删除，不进回收站——回收站和缓存在同一块盘上，进回收站等于没腾出空间。</p>
          {running.length > 0 && (
            <p>{running.join('、')} 正在运行，它们占着的文件会跳过；关掉再清能清得更干净。</p>
          )}
          {chosen.some((i) => i.recycleBin) && (
            <p className="text-destructive">回收站会被清空，里面的东西就找不回来了。</p>
          )}
        </div>
      ),
    })
    if (!ok) return
    setErr('')
    try {
      const r = await job.run((id) => CleanCacheRules(id, chosen.map((i) => i.id)))
      setResult(r)
    } catch (e) {
      setErr(errText(e))
      return
    }
    // 清完再量一遍,列表上的数字才对得上
    await doScan()
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 flex-wrap items-center gap-3 border-b border-border px-4 py-3">
        <div className="text-sm">
          {scan ? (
            <>
              可清理约 <b>{fmtBytes(availBytes)}</b>
              <span className="text-muted-foreground">，已选 </span>
              <b>{fmtBytes(chosenBytes)}</b>
            </>
          ) : (
            <span className="text-muted-foreground">还没统计</span>
          )}
        </div>
        <div className="ml-auto flex gap-2">
          <Button size="sm" variant="outline" disabled={job.running} onClick={() => void doScan()}>
            <RefreshCw className="h-3.5 w-3.5" />
            重新统计
          </Button>
          <Button
            size="sm"
            variant="destructive"
            disabled={job.running || chosen.length === 0}
            onClick={() => void clean()}
          >
            <Trash2 className="h-3.5 w-3.5" />
            清理已选
          </Button>
        </div>
      </div>

      {job.running && <ProgressLine p={job.progress} onCancel={job.cancel} />}

      <div className="min-h-0 flex-1 space-y-3 overflow-auto p-4">
        {err && <Notice tone="error">{err}</Notice>}
        {result && <CleanSummary r={result} onClose={() => setResult(null)} />}
        {scan && !scan.supported && (
          <Notice>这个系统上还没有内置的缓存规则，可以在下面自己加目录；大文件和重复文件两页照常能用。</Notice>
        )}
        {scan && scan.supported && !scan.elevated && needAdmin && (
          <Notice tone="warn">
            当前不是管理员身份：标着「需要管理员」的几项只能看不能清。要清的话，关掉工具箱，右键 → 以管理员身份运行。
          </Notice>
        )}

        {groups.map(([group, list]) => (
          <section key={group} className="space-y-1.5">
            <h3 className="px-1 text-xs font-medium text-muted-foreground">{group}</h3>
            {list.map((it) => (
              <CacheRow
                key={it.id}
                it={it}
                picked={picked.has(it.id)}
                expanded={open.has(it.id)}
                disabled={job.running}
                onToggle={() => toggle(it.id)}
                onExpand={() =>
                  setOpen((prev) => {
                    const next = new Set(prev)
                    if (next.has(it.id)) next.delete(it.id)
                    else next.add(it.id)
                    return next
                  })
                }
                onRemove={
                  it.custom
                    ? () => {
                        // 只是不再清它,目录本身不动,用不着确认
                        void DeleteCustomCacheRule(it.id.replace(/^custom:/, ''))
                          .then(doScan)
                          .catch((e) => setErr(errText(e)))
                      }
                    : undefined
                }
              />
            ))}
          </section>
        ))}

        <AddCustomRule disabled={job.running} onAdded={() => void doScan()} />

        {hidden > 0 && (
          <p className="px-1 text-[11px] text-muted-foreground">另有 {hidden} 项这台机器上没装，没列出来。</p>
        )}
        <p className="px-1 text-[11px] text-muted-foreground">
          只清缓存、临时文件、日志和转储。Cookie、历史记录、密码、登录状态一概不碰——那是隐私擦除，不是清理。
        </p>
      </div>
    </div>
  )
}

function CacheRow({
  it,
  picked,
  expanded,
  disabled,
  onToggle,
  onExpand,
  onRemove,
}: {
  it: Item
  picked: boolean
  expanded: boolean
  disabled: boolean
  onToggle: () => void
  onExpand: () => void
  onRemove?: () => void
}) {
  const can = selectable(it)
  return (
    <div className={cn('rounded-md border border-border bg-card', !can && 'opacity-70')}>
      <div className="flex items-start gap-3 px-3 py-2">
        <label className={cn('flex min-w-0 flex-1 items-start gap-2.5', can && 'cursor-pointer')}>
          <input
            type="checkbox"
            className="mt-1"
            checked={picked}
            disabled={!can || disabled}
            onChange={onToggle}
          />
          <span className="min-w-0">
            <span className="flex flex-wrap items-center gap-1.5 text-sm font-medium">
              {it.name}
              {it.needAdmin && <Badge tone="warn">需要管理员</Badge>}
              {it.running.length > 0 && (
                <Badge tone="warn" title="开着的时候，它占着的文件会跳过">
                  {it.running.join('、')} 正在运行
                </Badge>
              )}
            </span>
            <span className="block text-[11px] leading-relaxed text-muted-foreground">{it.desc}</span>
            {it.custom && !it.found && (
              <span className="block text-[11px] text-amber-600 dark:text-amber-400">这个目录现在不在了</span>
            )}
            {it.note && <span className="block break-all text-[11px] text-amber-600 dark:text-amber-400">{it.note}</span>}
          </span>
        </label>
        <div className="shrink-0 text-right">
          <div className="text-sm tabular-nums">{fmtBytes(it.size)}</div>
          <div className="text-[11px] text-muted-foreground">{fmtCount(it.files)} 个</div>
        </div>
        {onRemove && (
          <button
            type="button"
            onClick={onRemove}
            disabled={disabled}
            title="不再清这个目录（目录本身不动）"
            className="mt-0.5 shrink-0 text-muted-foreground hover:text-destructive disabled:opacity-50"
          >
            <X className="h-4 w-4" />
          </button>
        )}
        {it.paths.length > 0 && (
          <button
            type="button"
            onClick={onExpand}
            title="看看清的是哪些目录"
            className="mt-0.5 shrink-0 text-muted-foreground hover:text-foreground"
          >
            <ChevronDown className={cn('h-4 w-4 transition-transform', expanded && 'rotate-180')} />
          </button>
        )}
      </div>
      {expanded && (
        <div className="space-y-1 border-t border-border/60 px-3 py-2">
          {it.paths.map((p) => (
            <div key={p} className="flex items-center gap-2">
              <code className="min-w-0 flex-1 truncate font-mono text-[11px] text-muted-foreground" title={p}>
                {p}
              </code>
              <button
                type="button"
                title="在资源管理器里打开"
                onClick={() => void OpenInExplorer(p)}
                className="shrink-0 text-muted-foreground hover:text-foreground"
              >
                <FolderOpen className="h-3.5 w-3.5" />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

/**
 * 自己加一个当缓存清的目录:内置规则覆盖不到的(模拟器缓存、某个工具的下载目录之类)。
 * 能不能加由后端的守卫说了算,这里只把它给的原因原样显示出来
 */
function AddCustomRule({ disabled, onAdded }: { disabled: boolean; onAdded: () => void }) {
  const [open, setOpen] = useState(false)
  const [dir, setDir] = useState('')
  const [name, setName] = useState('')
  const [days, setDays] = useState(0)
  const [err, setErr] = useState('')
  const [saving, setSaving] = useState(false)

  const reset = () => {
    setOpen(false)
    setDir('')
    setName('')
    setDays(0)
    setErr('')
  }

  if (!open) {
    return (
      <Button size="sm" variant="outline" disabled={disabled} onClick={() => setOpen(true)}>
        <FolderPlus className="h-3.5 w-3.5" />
        添加自定义目录
      </Button>
    )
  }

  const pick = async () => {
    const p = await PickDirectory('选择要当缓存清的目录', '').catch(() => '')
    if (p) {
      setDir(p)
      setErr('')
    }
  }

  const save = async () => {
    setSaving(true)
    setErr('')
    try {
      await SaveCustomCacheRule({ id: '', name: name.trim(), dir, minAgeDays: days } as diskclean.CustomRule)
      reset()
      onAdded()
    } catch (e) {
      setErr(errText(e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-2 rounded-md border border-dashed border-border p-3" data-form="custom-rule">
      <div className="flex items-center gap-2">
        <Button size="sm" variant="outline" onClick={() => void pick()}>
          选目录
        </Button>
        <code className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground" title={dir}>
          {dir || '还没选'}
        </code>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="名字（不填就用目录名）"
          className="h-8 min-w-0 flex-1 rounded-md border border-input bg-background px-2 text-xs outline-none focus:ring-1 focus:ring-ring"
        />
        <select
          value={days}
          onChange={(e) => setDays(Number(e.target.value))}
          className="h-8 rounded-md border border-input bg-background px-2 text-xs"
        >
          <option value={0}>清掉里面的全部内容</option>
          <option value={1}>只清 1 天以前的</option>
          <option value={7}>只清 7 天以前的</option>
          <option value={30}>只清 30 天以前的</option>
        </select>
        <Button size="sm" disabled={!dir || saving} onClick={() => void save()}>
          保存
        </Button>
        <Button size="sm" variant="ghost" onClick={reset}>
          取消
        </Button>
      </div>
      {err && <p className="text-xs text-destructive">{err}</p>}
      <p className="text-[11px] text-muted-foreground">
        系统目录、程序目录、个人目录、文档这些地方加不进来。清的时候和内置规则一样：只清目录里面的东西，目录本身留着，而且是直接删、不进回收站。
      </p>
    </div>
  )
}

function CleanSummary({ r, onClose }: { r: diskclean.CacheCleanResult; onClose: () => void }) {
  const sum = (k: 'inUse' | 'denied' | 'recent') => r.items.reduce((s, i) => s + i[k], 0)
  const inUse = sum('inUse')
  const denied = sum('denied')
  const recent = sum('recent')
  const errors = r.items.filter((i) => i.error)
  return (
    <Notice tone={errors.length > 0 ? 'warn' : 'ok'} onClose={onClose}>
      <div>
        清掉了 <b>{fmtBytes(r.freed)}</b>（{fmtCount(r.deleted)} 个文件）
        {r.cancelled && '，中途停止了'}
      </div>
      {inUse > 0 && <div>{fmtCount(inUse)} 个文件正被占用，跳过了——一般是对应的程序开着，关掉再清就行。</div>}
      {recent > 0 && <div>{fmtCount(recent)} 个是一天以内的临时文件，按规则留着。</div>}
      {denied > 0 && <div>{fmtCount(denied)} 个没有权限删。</div>}
      {errors.map((i) => (
        <div key={i.id} className="text-destructive">
          {i.name}：{i.error}
        </div>
      ))}
    </Notice>
  )
}
