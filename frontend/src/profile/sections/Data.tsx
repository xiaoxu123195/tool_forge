import { useEffect, useState } from 'react'
import {
  AlertTriangle,
  ClipboardList,
  Download,
  ExternalLink,
  FolderOpen,
  HardDrive,
  Keyboard,
  Pin,
  RefreshCw,
  RotateCcw,
  Star,
  Trash2,
  Upload,
} from 'lucide-react'
import {
  ExportData,
  GetDataStats,
  ImportData,
  OpenDataDir,
  ResetAllData,
} from '../../../wailsjs/go/main/App'
import type { system } from '../../../wailsjs/go/models'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { usePinnedToolsStore } from '@/stores/pinnedTools'
import { useRecentToolsStore } from '@/stores/recentTools'
import { useToolsStore } from '@/stores/tools'
import { cn } from '@/lib/utils'

type Stats = system.DataStats

const LS_PREFIX = 'tool-forge:'

function formatBytes(b: number): string {
  if (b <= 0) return '0 B'
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`
  if (b < 1024 ** 3) return `${(b / 1024 / 1024).toFixed(1)} MB`
  return `${(b / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function gatherLocalStorage(): string {
  const out: Record<string, string> = {}
  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i)
    if (!key || !key.startsWith(LS_PREFIX)) continue
    out[key] = localStorage.getItem(key) ?? ''
  }
  return JSON.stringify(out)
}

function applyLocalStorage(json: string) {
  if (!json) return
  try {
    const data = JSON.parse(json) as Record<string, string>
    for (const [k, v] of Object.entries(data)) {
      localStorage.setItem(k, v)
    }
  } catch (e) {
    console.error('apply localStorage failed', e)
  }
}

function clearLocalStorage() {
  const keys: string[] = []
  for (let i = 0; i < localStorage.length; i++) {
    const k = localStorage.key(i)
    if (k && k.startsWith(LS_PREFIX)) keys.push(k)
  }
  keys.forEach((k) => localStorage.removeItem(k))
}

export function DataSection() {
  const [stats, setStats] = useState<Stats | null>(null)
  const [busy, setBusy] = useState(false)
  const [flash, setFlash] = useState('')
  const confirm = useConfirm()

  const visibility = useToolsStore((s) => s.visibility)
  const order = useToolsStore((s) => s.order)
  const resetOrder = useToolsStore((s) => s.resetOrder)
  const setVisibility = useToolsStore((s) => s.setVisibility)
  const recentIds = useRecentToolsStore((s) => s.ids)
  const recentCounts = useRecentToolsStore((s) => s.counts)
  const clearRecents = useRecentToolsStore((s) => s.clear)
  const pinnedIds = usePinnedToolsStore((s) => s.ids)
  const clearPinned = usePinnedToolsStore((s) => s.clear)

  const refresh = async () => {
    const r = await GetDataStats()
    setStats(r)
  }
  useEffect(() => {
    refresh()
  }, [])

  const flashMsg = (m: string) => {
    setFlash(m)
    setTimeout(() => setFlash(''), 2400)
  }

  const onOpenDir = async () => {
    const err = await OpenDataDir()
    if (err) alert(err)
  }

  const onExport = async () => {
    if (busy) return
    setBusy(true)
    try {
      const [path, err] = await ExportData(gatherLocalStorage())
      if (err) {
        alert('导出失败: ' + err)
      } else if (path) {
        flashMsg(`已导出到 ${path}`)
      }
    } finally {
      setBusy(false)
    }
  }

  const onImport = async () => {
    const ok = await confirm({
      title: '导入备份',
      message: '导入会覆盖当前所有本地数据(剪贴板历史 / 收藏 / 主题等)。导入完成后需要重启 App 才能生效。',
      confirmLabel: '继续',
      cancelLabel: '取消',
      danger: true,
    })
    if (!ok || busy) return
    setBusy(true)
    try {
      const [ls, err] = await ImportData()
      if (err) {
        alert('导入失败: ' + err)
        return
      }
      if (!ls) {
        // 用户取消了选文件
        return
      }
      // 把备份里的 localStorage 写回
      clearLocalStorage()
      applyLocalStorage(ls)
      await confirm({
        title: '导入成功',
        message: '本地数据已恢复。请关闭 App 后重新启动以加载新配置。',
        confirmLabel: '我知道了',
        cancelLabel: '',
      })
    } finally {
      setBusy(false)
      refresh()
    }
  }

  const onResetPinned = async () => {
    const ok = await confirm({
      title: '清空侧栏收藏',
      message: '会清空所有已收藏到 dock 的工具。',
      danger: true,
    })
    if (!ok) return
    clearPinned()
    flashMsg('已清空侧栏收藏')
  }

  const onResetRecents = async () => {
    const ok = await confirm({
      title: '清空最近使用',
      message: '会清空"最近使用"列表和频次计数,工具排序不受影响。',
      danger: true,
    })
    if (!ok) return
    clearRecents()
    flashMsg('已清空最近使用')
  }

  const onResetVisibility = async () => {
    const ok = await confirm({
      title: '重置工具偏好',
      message: '会把所有工具的"可见 / 隐藏"和"自定义顺序"恢复成默认。',
      danger: true,
    })
    if (!ok) return
    resetOrder()
    // 把每个 tool 的 visibility 也清掉(置为默认 true)
    for (const id of Object.keys(visibility)) {
      setVisibility(id, true)
    }
    flashMsg('已重置工具偏好')
  }

  const onResetAll = async () => {
    const ok = await confirm({
      title: '重置全部本地数据',
      message:
        '将清空所有数据:剪贴板历史 / 收藏 / 主题 / 热键 / 工具偏好 / 昵称等等。此操作不可恢复,完成后需要重启 App。',
      confirmLabel: '我已了解,重置',
      cancelLabel: '取消',
      danger: true,
    })
    if (!ok || busy) return
    setBusy(true)
    try {
      const err = await ResetAllData()
      if (err) {
        alert('清空数据目录失败: ' + err)
        return
      }
      clearLocalStorage()
      await confirm({
        title: '已重置',
        message: '所有本地数据已清空。请关闭 App 后重新启动。',
        confirmLabel: '我知道了',
        cancelLabel: '',
      })
    } finally {
      setBusy(false)
      refresh()
    }
  }

  // —— 计算渲染用的小数字
  const pinnedCount = pinnedIds.length
  const recentCount = recentIds.length
  const totalLaunches = Object.values(recentCounts).reduce((a, b) => a + b, 0)
  const customizedTools = Object.values(visibility).filter((v) => v === false).length
  const hasOrder = order.length > 0

  return (
    <div className="mx-auto max-w-3xl space-y-8">
      <header className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold">数据</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            查看本地数据占用,导入 / 导出备份,或精细化清理。
          </p>
        </div>
        <Button variant="ghost" size="sm" onClick={refresh} title="刷新概览">
          <RefreshCw className="h-3.5 w-3.5" />
          刷新
        </Button>
      </header>

      {flash && (
        <div className="rounded-md border border-emerald-500/40 bg-emerald-500/10 px-3 py-2 text-sm text-emerald-700 dark:text-emerald-300">
          {flash}
        </div>
      )}

      {/* —— Block 1: 概览 —— */}
      <section className="space-y-3">
        <SectionTitle>数据概览</SectionTitle>
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <StatCard
            icon={<HardDrive className="h-4 w-4" />}
            label="数据目录总占用"
            value={stats ? formatBytes(stats.totalBytes) : '—'}
            subtitle={stats ? `${stats.totalFiles} 个文件` : ''}
          />
          <StatCard
            icon={<ClipboardList className="h-4 w-4" />}
            label="剪贴板历史"
            value={stats ? formatBytes(stats.clipboardSize) : '—'}
            subtitle={stats ? `${stats.clipboardImgs} 张图片` : ''}
          />
          <StatCard
            icon={<Pin className="h-4 w-4" />}
            label="侧栏收藏"
            value={`${pinnedCount} / 5`}
          />
          <StatCard
            icon={<Star className="h-4 w-4" />}
            label="最近使用"
            value={`${recentCount} 项`}
            subtitle={totalLaunches > 0 ? `累计打开 ${totalLaunches} 次` : ''}
          />
          <StatCard
            icon={<Keyboard className="h-4 w-4" />}
            label="自定义热键"
            value={stats?.hasHotkeys ? '有' : '无'}
          />
          <StatCard
            icon={<RefreshCw className="h-4 w-4" />}
            label="工具偏好"
            value={
              hasOrder || customizedTools > 0
                ? `已自定义`
                : '默认'
            }
            subtitle={
              hasOrder || customizedTools > 0
                ? `顺序 ${hasOrder ? '已改' : '默认'} · 隐藏 ${customizedTools} 个`
                : ''
            }
          />
        </div>
        <div className="flex items-center gap-2 rounded-lg border border-border bg-card/60 p-3 text-xs">
          <FolderOpen className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className="flex-1 truncate font-mono text-muted-foreground">
            {stats?.dataDir || '—'}
          </span>
          <Button size="sm" variant="outline" onClick={onOpenDir}>
            <ExternalLink className="h-3.5 w-3.5" />
            打开
          </Button>
        </div>
      </section>

      {/* —— Block 2: 导入 / 导出 —— */}
      <section className="space-y-3">
        <SectionTitle>导入 / 导出</SectionTitle>
        <div className="rounded-lg border border-border bg-card p-4">
          <p className="mb-3 text-sm text-muted-foreground">
            备份文件包含 <span className="font-mono">~/.toolforge/</span>{' '}
            目录(剪贴板 / 热键)和浏览器 localStorage 里所有 <span className="font-mono">tool-forge:*</span>{' '}
            条目。可用于跨机迁移或定期备份。
          </p>
          <div className="flex flex-wrap gap-2">
            <Button onClick={onExport} disabled={busy} variant="outline">
              <Download className="h-3.5 w-3.5" />
              导出为 ZIP
            </Button>
            <Button onClick={onImport} disabled={busy} variant="outline">
              <Upload className="h-3.5 w-3.5" />
              从 ZIP 导入
            </Button>
          </div>
        </div>
      </section>

      {/* —— Block 3: 危险区 —— */}
      <section className="space-y-3">
        <SectionTitle danger>危险区</SectionTitle>
        <div className="space-y-2">
          <DangerRow
            label="清空侧栏收藏"
            hint="清空 dock 上所有已固定的工具"
            onClick={onResetPinned}
            disabled={pinnedCount === 0}
          />
          <DangerRow
            label="清空最近使用"
            hint="清空最近使用列表和频次计数"
            onClick={onResetRecents}
            disabled={recentCount === 0}
          />
          <DangerRow
            label="重置工具偏好"
            hint="工具的可见性 / 排序恢复成默认"
            onClick={onResetVisibility}
            disabled={!hasOrder && customizedTools === 0}
          />
          <DangerRow
            label="重置全部本地数据"
            hint="清空所有 localStorage 和 ~/.toolforge 目录,需要重启 App"
            onClick={onResetAll}
            disabled={busy}
            kind="extreme"
          />
        </div>
      </section>
    </div>
  )
}

function SectionTitle({
  children,
  danger,
}: {
  children: React.ReactNode
  danger?: boolean
}) {
  return (
    <div
      className={cn(
        'flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider',
        danger ? 'text-red-600 dark:text-red-400' : 'text-muted-foreground'
      )}
    >
      {danger && <AlertTriangle className="h-3 w-3" />}
      {children}
    </div>
  )
}

function StatCard({
  icon,
  label,
  value,
  subtitle,
}: {
  icon: React.ReactNode
  label: string
  value: string
  subtitle?: string
}) {
  return (
    <div className="rounded-lg border border-border bg-card p-3">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span className="flex h-6 w-6 items-center justify-center rounded-md bg-gradient-to-br from-info/20 to-info/10 text-info">
          {icon}
        </span>
        {label}
      </div>
      <div className="mt-1.5 text-xl font-semibold tracking-tight">{value}</div>
      {subtitle && (
        <div className="mt-0.5 text-[11px] text-muted-foreground">{subtitle}</div>
      )}
    </div>
  )
}

function DangerRow({
  label,
  hint,
  onClick,
  disabled,
  kind,
}: {
  label: string
  hint: string
  onClick: () => void
  disabled?: boolean
  kind?: 'extreme'
}) {
  const extreme = kind === 'extreme'
  return (
    <div
      className={cn(
        'flex items-center gap-3 rounded-lg border p-3',
        extreme
          ? 'border-red-500/40 bg-red-500/5'
          : 'border-border bg-card'
      )}
    >
      <div className="min-w-0 flex-1">
        <div className={cn('text-sm font-medium', extreme && 'text-red-700 dark:text-red-300')}>
          {label}
        </div>
        <div className="text-[11px] text-muted-foreground">{hint}</div>
      </div>
      <Button
        size="sm"
        variant={extreme ? 'destructive' : 'outline'}
        onClick={onClick}
        disabled={disabled}
      >
        {extreme ? (
          <Trash2 className="h-3.5 w-3.5" />
        ) : (
          <RotateCcw className="h-3.5 w-3.5" />
        )}
        {extreme ? '重置' : '清空'}
      </Button>
    </div>
  )
}
