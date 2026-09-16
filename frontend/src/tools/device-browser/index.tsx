import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  CornerLeftUp,
  Download,
  Eye,
  Folder,
  FileText,
  FolderDown,
  HardDriveDownload,
  Link2,
  RefreshCw,
  Search,
  Unplug,
  X,
} from 'lucide-react'
import { ToolShell } from '@/components/tool/ToolShell'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { useDeviceBrowserStore } from '@/stores/device-browser'
import {
  ConnectDevice,
  DiffDeviceDir,
  DisconnectDevice,
  ExportDeviceDir,
  ExportDeviceFile,
  ListDeviceDir,
  PickDirectory,
  PreviewDeviceFile,
  SearchDeviceFiles,
} from '../../../wailsjs/go/main/App'
import type { devicefs } from '../../../wailsjs/go/models'
import { meta } from './meta'
import { ConnectPanel } from './ConnectPanel'
import { Breadcrumbs } from './Breadcrumbs'
import { PreviewPane, fmtSize } from './PreviewPane'
import { presetsFor } from './presets'
import { jumpTo } from '@/lib/jump'
import {
  ChangeMark,
  WATCH_MAX_EVENTS,
  WatchPanel,
  changeMarks,
  newWatch,
  type WatchEvent,
  type WatchKind,
  type WatchState,
} from './WatchPanel'

// 直接翻连着的手机,而不是"先导出再看"。
// 现场是先翻、翻到有价值的再取 —— 反过来意味着你得先猜对要导哪个目录。
//
// 连接活在 Go 那边(USB 转发 + SSH + SFTP),这里只拿着 sessionId。

export default function DeviceBrowser() {
  const sessionId = useDeviceBrowserStore((s) => s.sessionId)
  const platform = useDeviceBrowserStore((s) => s.platform)
  const adbPath = useDeviceBrowserStore((s) => s.adbPath)
  const rooted = useDeviceBrowserStore((s) => s.rooted)
  const cwd = useDeviceBrowserStore((s) => s.cwd)
  const startPath = useDeviceBrowserStore((s) => s.startPath)
  const user = useDeviceBrowserStore((s) => s.user)
  const deviceId = useDeviceBrowserStore((s) => s.deviceId)
  const setSession = useDeviceBrowserStore((s) => s.setSession)
  const clearSession = useDeviceBrowserStore((s) => s.clearSession)
  const setCwd = useDeviceBrowserStore((s) => s.setCwd)
  const setUser = useDeviceBrowserStore((s) => s.setUser)
  const setDeviceId = useDeviceBrowserStore((s) => s.setDeviceId)
  const setPlatform = useDeviceBrowserStore((s) => s.setPlatform)
  const setAdbPath = useDeviceBrowserStore((s) => s.setAdbPath)

  const [connecting, setConnecting] = useState(false)
  const [connectError, setConnectError] = useState('')
  const [listing, setListing] = useState<devicefs.Listing | null>(null)
  const [listError, setListError] = useState('')
  const [listLoading, setListLoading] = useState(false)

  const [selected, setSelected] = useState('')
  const [preview, setPreview] = useState<devicefs.Preview | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [previewError, setPreviewError] = useState('')
  const [exporting, setExporting] = useState(false)
  const [exportedTo, setExportedTo] = useState('')

  const [deviceLabel, setDeviceLabel] = useState('')
  const [query, setQuery] = useState('')
  const [hits, setHits] = useState<devicefs.SearchResult | null>(null)
  const [searching, setSearching] = useState(false)
  // 搜索是从哪个目录发起的 —— 它是递归的,结果里会出现别的层级的路径,
  // 不记住起点的话人会以为这些东西都在当前目录里
  const [searchRoot, setSearchRoot] = useState('')

  // 文件夹导出:进行中的那个路径 + 上一次的结果
  const [dirExporting, setDirExporting] = useState('')
  const [dirResult, setDirResult] = useState<devicefs.ExportDirResult | null>(null)
  const [dirError, setDirError] = useState('')

  // 监视模式:定时给一棵目录树拍快照比差异。看的是 watch.dir,不跟着 cwd 走 ——
  // 拍着基线去别处翻一翻很正常,监视的对象不能因此变了
  const [watch, setWatch] = useState<WatchState | null>(null)
  const watchRef = useRef(watch)
  watchRef.current = watch
  const cwdRef = useRef(cwd)
  cwdRef.current = cwd
  const navigate = useNavigate()

  const load = useCallback(
    async (dir: string) => {
      if (!sessionId) return
      setListLoading(true)
      setListError('')
      try {
        const l = await ListDeviceDir(sessionId, dir)
        setListing(l)
        setCwd(l.path)
        setHits(null)
      } catch (e) {
        setListError(String(e))
      } finally {
        setListLoading(false)
      }
    },
    [sessionId, setCwd]
  )

  // 连上之后(或者从别的工具切回来)自动列一次当前目录
  useEffect(() => {
    if (sessionId && !listing) void load(cwd)
  }, [sessionId, listing, cwd, load])

  const pollWatch = useCallback(
    async (reset = false) => {
      const w = watchRef.current
      if (!w || !sessionId) return
      setWatch((s) => s && { ...s, busy: true })
      try {
        const r = await DiffDeviceDir(sessionId, w.dir, reset)
        const now = Date.now()
        const fresh: WatchEvent[] = (r.changes ?? []).map((c) => ({
          at: now,
          kind: c.kind as WatchKind,
          path: c.path,
          name: c.name,
          isDir: c.isDir,
          size: c.size,
          sizeDelta: c.sizeDelta ?? 0,
        }))
        setWatch(
          (s) =>
            s && {
              ...s,
              busy: false,
              error: '',
              ticks: s.ticks + 1,
              lastAt: now,
              total: r.total,
              truncated: r.truncated,
              events: fresh.length
                ? [...fresh, ...s.events].slice(0, WATCH_MAX_EVENTS)
                : s.events,
            },
        )
        // 当前正看着的目录就在被监视的树里:刷新列表,新文件才会出现在眼前
        if (fresh.length > 0 && isWithin(cwdRef.current, w.dir)) void load(cwdRef.current)
      } catch (e) {
        setWatch((s) => s && { ...s, busy: false, error: String(e) })
      }
    },
    [sessionId, load],
  )

  // 轮询:上一次查完再排下一次。setTimeout 串起来而不是 setInterval ——
  // 设备慢的时候一次 find 可能超过间隔,不能让请求叠起来
  useEffect(() => {
    if (!watch || watch.paused || !sessionId) return
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    const tick = async () => {
      if (cancelled) return
      await pollWatch(false)
      if (!cancelled) timer = setTimeout(tick, watch.interval * 1000)
    }
    if (watch.ticks === 0) {
      // 刚开始:先拍基线,再排定时
      void pollWatch(true).then(() => {
        if (!cancelled) timer = setTimeout(tick, watch.interval * 1000)
      })
    } else {
      timer = setTimeout(tick, watch.interval * 1000)
    }
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
    // ticks 故意不在依赖里:每次检查都会变,放进去就成了查完立刻再查
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [watch?.dir, watch?.interval, watch?.paused, sessionId, pollWatch])

  const connect = async (password: string) => {
    setConnecting(true)
    setConnectError('')
    try {
      const s = await ConnectDevice({
        platform,
        deviceId,
        user,
        password,
        adbPath,
        remotePort: 0,
      } as devicefs.ConnectOptions)
      setSession(s.id, s.startPath, s.rooted)
      setDeviceLabel([s.model, s.deviceId].filter(Boolean).join(' · '))
      setListing(null)
    } catch (e) {
      setConnectError(String(e))
    } finally {
      setConnecting(false)
    }
  }

  const disconnect = async () => {
    if (sessionId) await DisconnectDevice(sessionId).catch(() => {})
    clearSession()
    setListing(null)
    setPreview(null)
    setSelected('')
    setHits(null)
    setWatch(null)
  }

  const open = async (e: devicefs.Entry) => {
    if (e.isDir) {
      void load(e.path)
      return
    }
    setSelected(e.path)
    setPreviewLoading(true)
    setPreviewError('')
    setPreview(null)
    setExportedTo('')
    try {
      setPreview(await PreviewDeviceFile(sessionId, e.path))
    } catch (err) {
      setPreviewError(String(err))
    } finally {
      setPreviewLoading(false)
    }
  }

  const runSearch = async () => {
    if (!query.trim() || !sessionId) return
    setSearching(true)
    setListError('')
    setSearchRoot(cwd)
    try {
      setHits(await SearchDeviceFiles(sessionId, cwd, query, 0))
    } catch (e) {
      setListError(String(e))
    } finally {
      setSearching(false)
    }
  }

  const exportSelected = async () => {
    if (!preview) return
    const dir = await PickDirectory('导出到哪个目录', '')
    if (!dir) return
    setExporting(true)
    try {
      const at = await ExportDeviceFile(sessionId, preview.path, dir)
      setPreviewError('')
      setExportedTo(at)
    } catch (e) {
      setPreviewError(String(e))
    } finally {
      setExporting(false)
    }
  }

  const exportDir = async (remote: string) => {
    const dir = await PickDirectory('把这个文件夹导出到哪里', '')
    if (!dir) return
    setDirExporting(remote)
    setDirError('')
    setDirResult(null)
    try {
      setDirResult(await ExportDeviceDir(sessionId, remote, dir))
    } catch (e) {
      setDirError(String(e))
    } finally {
      setDirExporting('')
    }
  }

  const startWatch = (dir: string) => setWatch(newWatch(dir, watch?.interval ?? 5))
  const stopWatch = () => setWatch(null)
  // 点一处变化:跳到它所在的目录并选中它;目录本身变了就进去看
  const goToChange = (ev: WatchEvent) => {
    const parent = ev.path.slice(0, ev.path.lastIndexOf('/')) || '/'
    void load(ev.isDir && ev.kind !== 'removed' ? ev.path : parent)
    if (!ev.isDir) setSelected(ev.path)
  }
  const marks = useMemo(() => changeMarks(watch?.events ?? []), [watch?.events])
  // 带着路径跳去移动取证:平台跟着这条会话走,路径填进那边的「指定路径」。
  // 现场的顺序就是先翻到、再整个取下来;以前这一步要手抄路径
  const forensicExport = (path: string) =>
    jumpTo(navigate, {
      to: 'mobile-forensic',
      platform: platform === 'android' ? 'android' : 'ios',
      paths: [path],
    })

  if (!sessionId) {
    return (
      <ToolShell title={meta.title} description={meta.description}>
        <ConnectPanel
          platform={platform}
          user={user}
          deviceId={deviceId}
          adbPath={adbPath}
          busy={connecting}
          error={connectError}
          onPlatformChange={setPlatform}
          onUserChange={setUser}
          onDeviceIdChange={setDeviceId}
          onAdbPathChange={setAdbPath}
          onConnect={connect}
        />
      </ToolShell>
    )
  }

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      actions={
        <div className="flex items-center gap-1.5">
          {/* 有没有 root 决定了看不看得到 /data —— 这是最需要一眼看到的状态 */}
          <span className="mr-1 flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <span className="rounded-sm bg-muted px-1.5 py-0.5 font-medium">
              {platform === 'android' ? 'Android' : 'iOS'}
            </span>
            {platform === 'android' && (
              <span
                className={cn(
                  'rounded-sm px-1.5 py-0.5 font-medium',
                  rooted
                    ? 'bg-emerald-200/60 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300'
                    : 'bg-amber-200/60 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300'
                )}
                title={rooted ? '可以读 /data 下面的应用数据' : '没有 root,只看得到 /sdcard'}
              >
                {rooted ? 'root' : '无 root'}
              </span>
            )}
            {deviceLabel && <span className="max-w-[180px] truncate">{deviceLabel}</span>}
          </span>
          <Button variant="ghost" size="sm" onClick={() => load(cwd)} disabled={listLoading}>
            <RefreshCw className={cn('h-3.5 w-3.5', listLoading && 'animate-spin')} />
            刷新
          </Button>
          <Button variant="outline" size="sm" onClick={disconnect}>
            <Unplug className="h-3.5 w-3.5" />
            断开
          </Button>
        </div>
      }
    >
      <div className="flex h-full min-h-0 flex-col gap-2">
        {/* 路径 + 搜索 */}
        <div className="flex items-center gap-1.5 rounded-lg border border-border bg-card px-2 py-1.5">
          <Button
            variant="ghost"
            size="sm"
            className="h-7 shrink-0 px-2"
            onClick={() => listing?.parent && load(listing.parent)}
            disabled={!listing?.parent}
            title="上一级"
          >
            <CornerLeftUp className="h-3.5 w-3.5" />
          </Button>
          <Breadcrumbs path={cwd} startPath={startPath} onGo={load} />
          {/* 当前目录也能整个导出 —— 以前只有选中单个文件才导得了 */}
          <Button
            variant="ghost"
            size="sm"
            className="h-7 shrink-0 px-2"
            onClick={() => exportDir(cwd)}
            disabled={!!dirExporting || cwd === '/'}
            title={`把 ${cwd} 整个导出到本地`}
          >
            <FolderDown className={cn('h-3.5 w-3.5', dirExporting === cwd && 'animate-pulse')} />
            <span className="text-[11px]">导出此目录</span>
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 shrink-0 px-2"
            onClick={() => forensicExport(cwd)}
            disabled={cwd === '/'}
            title={`把 ${cwd} 填进移动取证的「指定路径」，走取证流程整个取下来`}
          >
            <HardDriveDownload className="h-3.5 w-3.5" />
            <span className="text-[11px]">用移动取证导出</span>
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className={cn('h-7 shrink-0 px-2', watch?.dir === cwd && 'text-info')}
            onClick={() => (watch?.dir === cwd ? stopWatch() : startWatch(cwd))}
            title={
              watch?.dir === cwd
                ? '停止监视这个目录'
                : `监视 ${cwd}：每隔几秒对比一次整棵子树，列出新增、修改、删除的文件`
            }
          >
            <Eye className={cn('h-3.5 w-3.5', watch?.dir === cwd && !watch.paused && 'animate-pulse')} />
            <span className="text-[11px]">{watch?.dir === cwd ? '监视中' : '监视此目录'}</span>
          </Button>
          <div className="relative shrink-0">
            <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') void runSearch()
                if (e.key === 'Escape') {
                  setQuery('')
                  setHits(null)
                }
              }}
              // 它是递归的:从这里往下翻整棵子树。写"在当前目录下"会让人
              // 以为只看这一层,然后对着一堆别处的结果发懵
              placeholder="从这里往下找（回车搜索）"
              spellCheck={false}
              className="h-7 w-52 rounded-md border border-input bg-background pl-7 pr-7 text-xs outline-none focus:ring-1 focus:ring-ring"
            />
            {query && (
              <button
                onClick={() => {
                  setQuery('')
                  setHits(null)
                }}
                title="清空（Esc）"
                className="absolute right-1.5 top-1/2 -translate-y-1/2 rounded p-0.5 text-muted-foreground hover:bg-secondary hover:text-foreground"
              >
                <X className="h-3 w-3" />
              </button>
            )}
          </div>
        </div>

        {/* 常用位置:iOS 的路径又长又容易打错,让人每次手敲是这个功能没人用的主因 */}
        <div className="flex flex-wrap gap-1">
          {presetsFor(platform, rooted).map((p) => (
            <button
              key={p.path}
              onClick={() => load(p.path)}
              title={p.note ?? p.path}
              className="rounded-sm bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            >
              {p.label}
            </button>
          ))}
        </div>

        {watch && (
          <WatchPanel
            watch={watch}
            onPause={() => setWatch((s) => s && { ...s, paused: !s.paused })}
            onCheckNow={() => void pollWatch(false)}
            onClear={() => setWatch((s) => s && { ...s, events: [] })}
            onStop={stopWatch}
            onInterval={(sec) => setWatch((s) => s && { ...s, interval: sec })}
            onGo={goToChange}
          />
        )}

        {dirError && (
          <div className="whitespace-pre-wrap break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
            {dirError}
          </div>
        )}
        {dirResult && (
          <div className="rounded-md border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-[11px] text-emerald-700 dark:text-emerald-400">
            已导出 {dirResult.files} 个文件 / {fmtSize(dirResult.bytes)}，用时{' '}
            {(dirResult.elapsedMs / 1000).toFixed(1)} 秒 —— {dirResult.rootPath}
            {/* 改过名必须让人看见:取证里文件名本身就是证据 */}
            {dirResult.renamed > 0 && (
              <div className="mt-1 text-amber-700 dark:text-amber-400">
                有 {dirResult.renamed} 个名字在本地文件系统上非法，已替换其中的字符
                {dirResult.renameSamples.length > 0 && (
                  <span className="opacity-80">（例如 {dirResult.renameSamples[0]}）</span>
                )}
              </div>
            )}
            {dirResult.skipped > 0 && (
              <div className="mt-0.5 opacity-80">跳过 {dirResult.skipped} 个成员（软链、设备节点之类）</div>
            )}
          </div>
        )}
        {listError && (
          <div className="whitespace-pre-wrap break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
            {listError}
          </div>
        )}

        <div className="grid min-h-0 flex-1 grid-cols-[minmax(280px,2fr)_3fr] gap-2">
          <div className="min-h-0 overflow-auto rounded-lg border border-border bg-card">
            {hits ? (
              <SearchResults
                res={hits}
                root={searchRoot}
                query={query}
                searching={searching}
                selected={selected}
                onPick={open}
                onBack={() => {
                  setHits(null)
                  setQuery('')
                }}
              />
            ) : (
              <EntryList
                listing={listing}
                loading={listLoading}
                selected={selected}
                onOpen={open}
                onExportDir={exportDir}
                onForensic={forensicExport}
                exportingDir={dirExporting}
                marks={marks}
              />
            )}
          </div>
          <div className="min-h-0 overflow-hidden rounded-lg border border-border bg-card">
            <PreviewPane
              preview={preview}
              loading={previewLoading}
              error={previewError}
              onExport={exportSelected}
              exporting={exporting}
              exportedTo={exportedTo}
            />
          </div>
        </div>
      </div>
    </ToolShell>
  )
}

function EntryList({
  listing,
  loading,
  selected,
  onOpen,
  onExportDir,
  onForensic,
  exportingDir,
  marks,
}: {
  listing: devicefs.Listing | null
  loading: boolean
  selected: string
  onOpen: (e: devicefs.Entry) => void
  onExportDir: (path: string) => void
  onForensic: (path: string) => void
  exportingDir: string
  marks: ReturnType<typeof changeMarks>
}) {
  if (!listing) {
    return <Hint>{loading ? '读取中…' : '还没有内容'}</Hint>
  }
  if (listing.entries.length === 0) {
    return <Hint>这个目录是空的</Hint>
  }
  // 目录排前面。翻目录时找的多半是下一层,把它们和几百个文件混在一起没法用
  const dirs = listing.entries.filter((e) => e.isDir).length
  return (
    <>
      <div className="sticky top-0 z-10 flex items-center gap-2 border-b border-border bg-muted/60 px-3 py-1 text-[10px] text-muted-foreground backdrop-blur">
        <span>
          {dirs} 个文件夹 · {listing.entries.length - dirs} 个文件
        </span>
        {listing.truncated && (
          <span className="ml-auto text-amber-700 dark:text-amber-400">
            共 {listing.total} 条，只列出了前面一部分
          </span>
        )}
      </div>
      <ul className="text-[12.5px]">
        {listing.entries.map((e) => (
          <Row
            key={e.path}
            e={e}
            active={e.path === selected}
            onClick={() => onOpen(e)}
            onExportDir={onExportDir}
            onForensic={onForensic}
            exporting={exportingDir === e.path}
            mark={marks.exact.get(e.path) ?? (marks.inside.has(e.path) ? 'inside' : undefined)}
          />
        ))}
      </ul>
    </>
  )
}

function SearchResults({
  res,
  root,
  query,
  searching,
  selected,
  onPick,
  onBack,
}: {
  res: devicefs.SearchResult
  root: string
  query: string
  searching: boolean
  selected: string
  onPick: (e: devicefs.Entry) => void
  onBack: () => void
}) {
  return (
    <>
      {/* 搜索是递归的,命中会来自各个层级。不写明从哪儿开始搜的,
          人会以为这些文件都在当前目录里 */}
      <div className="sticky top-0 z-10 border-b border-border bg-muted/60 px-3 py-1.5 text-[10px] text-muted-foreground backdrop-blur">
        <div className="flex items-center gap-2">
          <span className="font-medium text-foreground">
            {searching ? '查找中…' : `找到 ${res.hits.length} 条`}
          </span>
          {res.truncated && <span className="text-amber-700 dark:text-amber-400">已截断</span>}
          <button
            onClick={onBack}
            className="ml-auto rounded px-1.5 py-0.5 hover:bg-secondary hover:text-foreground"
          >
            返回目录
          </button>
        </div>
        <div className="mt-0.5 truncate" title={root}>
          「{query}」· 从 <span className="font-mono">{root}</span> 往下递归查找
        </div>
      </div>
      {res.hits.length === 0 && !searching ? (
        <Hint>没有名字含「{query}」的文件</Hint>
      ) : (
        <ul className="text-[12.5px]">
          {res.hits.map((h) => (
            <Row
              key={h.path}
              e={hitToEntry(h)}
              active={h.path === selected}
              relativeTo={root}
              onClick={() => onPick(hitToEntry(h))}
            />
          ))}
        </ul>
      )}
    </>
  )
}

/** 搜索命中转成列表行认识的形状 */
function hitToEntry(h: devicefs.SearchHit): devicefs.Entry {
  return {
    name: h.path.split('/').pop() ?? h.path,
    path: h.path,
    isDir: h.isDir,
    size: h.size,
    modTime: h.modTime,
    mode: '',
  } as devicefs.Entry
}

function Row({
  e,
  active,
  relativeTo,
  onClick,
  onExportDir,
  onForensic,
  exporting,
  mark,
}: {
  e: devicefs.Entry
  active: boolean
  /** 给了就显示相对这里的路径 —— 搜索结果里绝对路径太长,前缀还都一样 */
  relativeTo?: string
  onClick: () => void
  onExportDir?: (path: string) => void
  onForensic?: (path: string) => void
  exporting?: boolean
  /** 监视模式打的标:这一行自己变了,或者它底下有变化 */
  mark?: WatchKind | 'inside'
}) {
  const sub = relativeTo ? relPath(e.path, relativeTo) : ''
  return (
    <li className="group/row border-b border-border/40 last:border-0">
      <div
        className={cn(
          'flex w-full items-center gap-2 px-3 py-1.5 transition-colors hover:bg-accent/60',
          active && 'bg-accent'
        )}
      >
        <button onClick={onClick} className="flex min-w-0 flex-1 items-center gap-2 text-left">
          {e.isDir ? (
            <Folder className="h-3.5 w-3.5 shrink-0 fill-sky-500/20 text-sky-600 dark:text-sky-400" />
          ) : (
            <FileText className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          )}
          <span className="min-w-0 flex-1">
            <span className="flex min-w-0 items-center gap-1.5">
              <span className="truncate font-mono" title={e.path}>
                {e.name}
              </span>
              {mark && <ChangeMark mark={mark} />}
            </span>
            {sub && (
              <span className="block truncate font-mono text-[10px] text-muted-foreground" title={e.path}>
                {sub}
              </span>
            )}
            {/* 软链要标出来:iOS 上 /var 就是 /private/var 的软链,
                不标的话人会以为自己在两个不同的地方看到了同一份数据 */}
            {e.symlink && (
              <span className="flex items-center gap-1 text-[10px] text-muted-foreground">
                <Link2 className="h-3 w-3" />
                {e.symlink}
              </span>
            )}
            {e.err && <span className="block text-[10px] text-destructive">{e.err}</span>}
          </span>
        </button>

        {/* 文件夹的导出按钮平时不显示,悬停才出来 —— 常驻的话每一行都多一个图标,
            几百条列表会很吵 */}
        {e.isDir && onExportDir && (
          <button
            onClick={(ev) => {
              ev.stopPropagation()
              onExportDir(e.path)
            }}
            disabled={exporting}
            title={`把 ${e.name} 整个导出到本地`}
            className={cn(
              'shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground',
              exporting ? 'opacity-100' : 'opacity-0 group-hover/row:opacity-100'
            )}
          >
            <Download className={cn('h-3.5 w-3.5', exporting && 'animate-pulse')} />
          </button>
        )}
        {e.isDir && onForensic && (
          <button
            onClick={(ev) => {
              ev.stopPropagation()
              onForensic(e.path)
            }}
            title={`把 ${e.name} 填进移动取证的「指定路径」`}
            className="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition-colors hover:bg-secondary hover:text-foreground group-hover/row:opacity-100"
          >
            <HardDriveDownload className="h-3.5 w-3.5" />
          </button>
        )}

        <span className="w-14 shrink-0 text-right text-[10px] tabular-nums text-muted-foreground">
          {e.isDir ? '' : fmtSize(e.size)}
        </span>
        <span className="w-[84px] shrink-0 text-right text-[10px] tabular-nums text-muted-foreground">
          {fmtTime(e.modTime)}
        </span>
      </div>
    </li>
  )
}

/** 把绝对路径压成相对 root 的形式,root 之外的原样返回 */
function relPath(p: string, root: string): string {
  const base = root.endsWith('/') ? root : root + '/'
  if (!p.startsWith(base)) return p
  const rest = p.slice(base.length)
  const cut = rest.lastIndexOf('/')
  return cut > 0 ? rest.slice(0, cut) : ''
}

/** p 是不是 dir 自己或它底下的路径 */
function isWithin(p: string, dir: string): boolean {
  if (p === dir) return true
  const base = dir.endsWith('/') ? dir : dir + '/'
  return p.startsWith(base)
}

function Hint({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-4 py-8 text-center text-xs text-muted-foreground">{children}</div>
  )
}

function fmtTime(unixSec: number): string {
  if (!unixSec) return ''
  const d = new Date(unixSec * 1000)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear() % 100}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
