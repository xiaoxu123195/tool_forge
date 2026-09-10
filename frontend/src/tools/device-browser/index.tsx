import { useCallback, useEffect, useState } from 'react'
import {
  ChevronRight,
  CornerLeftUp,
  Folder,
  FileText,
  Link2,
  RefreshCw,
  Search,
  Unplug,
} from 'lucide-react'
import { ToolShell } from '@/components/tool/ToolShell'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { useDeviceBrowserStore } from '@/stores/device-browser'
import {
  ConnectDevice,
  DisconnectDevice,
  ExportDeviceFile,
  ListDeviceDir,
  PickDirectory,
  PreviewDeviceFile,
  SearchDeviceFiles,
} from '../../../wailsjs/go/main/App'
import type { devicefs } from '../../../wailsjs/go/models'
import { meta } from './meta'
import { ConnectPanel } from './ConnectPanel'
import { PreviewPane, fmtSize } from './PreviewPane'
import { presetsFor } from './presets'

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
        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => listing?.parent && load(listing.parent)}
            disabled={!listing?.parent}
            title="上一级"
          >
            <CornerLeftUp className="h-3.5 w-3.5" />
          </Button>
          <Breadcrumbs path={cwd} onGo={load} />
          <div className="relative ml-auto">
            <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && runSearch()}
              placeholder="在当前目录下按名字找"
              spellCheck={false}
              className="h-7 w-56 rounded-md border border-input bg-background pl-7 pr-2 text-xs outline-none focus:ring-1 focus:ring-ring"
            />
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
                searching={searching}
                selected={selected}
                onPick={open}
                onBack={() => setHits(null)}
              />
            ) : (
              <EntryList
                listing={listing}
                loading={listLoading}
                selected={selected}
                onOpen={open}
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
}: {
  listing: devicefs.Listing | null
  loading: boolean
  selected: string
  onOpen: (e: devicefs.Entry) => void
}) {
  if (!listing) {
    return <Hint>{loading ? '读取中…' : '还没有内容'}</Hint>
  }
  if (listing.entries.length === 0) {
    return <Hint>空目录</Hint>
  }
  return (
    <>
      {listing.truncated && (
        <div className="border-b border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-[11px] text-amber-700 dark:text-amber-400">
          这个目录有 {listing.total} 条，只列出了前面一部分
        </div>
      )}
      <ul className="text-[12.5px]">
        {listing.entries.map((e) => (
          <Row key={e.path} e={e} active={e.path === selected} onClick={() => onOpen(e)} />
        ))}
      </ul>
    </>
  )
}

function SearchResults({
  res,
  searching,
  selected,
  onPick,
  onBack,
}: {
  res: devicefs.SearchResult
  searching: boolean
  selected: string
  onPick: (e: devicefs.Entry) => void
  onBack: () => void
}) {
  return (
    <>
      <div className="flex items-center justify-between gap-2 border-b border-border px-3 py-1.5 text-[11px] text-muted-foreground">
        <span>
          {searching ? '查找中…' : `找到 ${res.hits.length} 条`}
          {res.truncated && '（已截断）'}
        </span>
        <button onClick={onBack} className="hover:text-foreground">
          返回目录
        </button>
      </div>
      {res.hits.length === 0 && !searching ? (
        <Hint>没有匹配的文件</Hint>
      ) : (
        <ul className="text-[12.5px]">
          {res.hits.map((h) => (
            <Row
              key={h.path}
              e={
                {
                  name: h.path.split('/').pop() ?? h.path,
                  path: h.path,
                  isDir: h.isDir,
                  size: h.size,
                  modTime: h.modTime,
                  mode: '',
                } as devicefs.Entry
              }
              active={h.path === selected}
              showPath
              onClick={() =>
                onPick({
                  name: h.path.split('/').pop() ?? h.path,
                  path: h.path,
                  isDir: h.isDir,
                  size: h.size,
                  modTime: h.modTime,
                  mode: '',
                } as devicefs.Entry)
              }
            />
          ))}
        </ul>
      )}
    </>
  )
}

function Row({
  e,
  active,
  showPath,
  onClick,
}: {
  e: devicefs.Entry
  active: boolean
  showPath?: boolean
  onClick: () => void
}) {
  return (
    <li>
      <button
        onClick={onClick}
        className={cn(
          'flex w-full items-center gap-2 px-3 py-1.5 text-left transition-colors hover:bg-accent',
          active && 'bg-accent'
        )}
      >
        {e.isDir ? (
          <Folder className="h-3.5 w-3.5 shrink-0 text-sky-600 dark:text-sky-400" />
        ) : (
          <FileText className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        <span className="min-w-0 flex-1">
          <span className="block truncate font-mono" title={e.path}>
            {showPath ? e.path : e.name}
          </span>
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
        <span className="shrink-0 text-[10px] tabular-nums text-muted-foreground">
          {e.isDir ? '' : fmtSize(e.size)}
        </span>
        <span className="w-[88px] shrink-0 text-right text-[10px] tabular-nums text-muted-foreground">
          {fmtTime(e.modTime)}
        </span>
      </button>
    </li>
  )
}

function Breadcrumbs({ path, onGo }: { path: string; onGo: (p: string) => void }) {
  const parts = path.split('/').filter(Boolean)
  return (
    <div className="flex min-w-0 flex-1 flex-wrap items-center gap-0.5 text-[11px] text-muted-foreground">
      <button onClick={() => onGo('/')} className="hover:text-foreground">
        /
      </button>
      {parts.map((seg, i) => {
        const full = '/' + parts.slice(0, i + 1).join('/')
        return (
          <span key={full} className="flex items-center gap-0.5">
            <ChevronRight className="h-3 w-3 opacity-50" />
            <button
              onClick={() => onGo(full)}
              className={cn(
                'max-w-[160px] truncate hover:text-foreground',
                i === parts.length - 1 && 'font-medium text-foreground'
              )}
              title={full}
            >
              {seg}
            </button>
          </span>
        )
      })}
    </div>
  )
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
