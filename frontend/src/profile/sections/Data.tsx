import { useEffect, useState } from 'react'
import {
  AlertTriangle,
  Bot,
  ClipboardList,
  Download,
  ExternalLink,
  FileJson,
  FolderOpen,
  Globe,
  HardDrive,
  Keyboard,
  RefreshCw,
  RotateCcw,
  Sparkles,
  Trash2,
  Upload,
} from 'lucide-react'
import {
  ClearAIDataModule,
  ExportData,
  GetDataStats,
  ImportData,
  OpenDataDir,
  OpenInExplorer,
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
type Module = system.ModuleStorage

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

const MODULE_META: Record<
  string,
  { icon: React.ReactNode; needRestart?: boolean; emptyHint?: string }
> = {
  'ai-chat': { icon: <Bot className="h-4 w-4" />, emptyHint: '没有 AI 会话' },
  clipboard: { icon: <ClipboardList className="h-4 w-4" />, needRestart: true, emptyHint: '剪贴板未启用' },
  'http-test': { icon: <Globe className="h-4 w-4" />, emptyHint: '没有 HTTP 历史' },
  'provider-switch': { icon: <Sparkles className="h-4 w-4" />, emptyHint: '没有 Provider 配置' },
  hotkeys: { icon: <Keyboard className="h-4 w-4" />, needRestart: true, emptyHint: '使用默认热键' },
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

  const onOpenPath = async (path: string) => {
    try {
      await OpenInExplorer(path)
    } catch (e) {
      alert('打开失败: ' + String(e))
    }
  }

  const onClearModule = async (m: Module) => {
    const meta = MODULE_META[m.key] ?? {}
    const ok = await confirm({
      title: `清空 ${m.label}`,
      message: meta.needRestart
        ? `将删除「${m.label}」的所有本地数据(${formatBytes(m.bytes)})。该模块涉及后台服务,清空后请重启 App 以避免数据被立刻再写回。`
        : `将删除「${m.label}」的所有本地数据(${formatBytes(m.bytes)})。该操作不可恢复。`,
      danger: true,
      confirmLabel: '清空',
    })
    if (!ok) return
    const err = (await ClearAIDataModule(m.key)) as unknown as string
    if (err) {
      alert('清空失败: ' + err)
      return
    }
    flashMsg(`已清空 ${m.label}`)
    refresh()
  }

  const onExport = async () => {
    if (busy) return
    setBusy(true)
    try {
      // 返回的就是路径字符串。以前解构成 [path, err] 会把字符串拆成首字符和次字符,
      // 于是 err 永远非空 —— 导出成功也会弹"导出失败"
      const path = (await ExportData(gatherLocalStorage())) as unknown as string
      if (path) flashMsg(`已导出到 ${path}`)
    } finally {
      setBusy(false)
    }
  }

  const onImport = async () => {
    const ok = await confirm({
      title: '导入备份',
      message:
        '导入会覆盖当前所有本地数据(剪贴板历史 / AI 对话 / 收藏 / 主题等)。导入完成后需要重启 App 才能生效。',
      confirmLabel: '继续',
      cancelLabel: '取消',
      danger: true,
    })
    if (!ok || busy) return
    setBusy(true)
    try {
      const ls = (await ImportData()) as unknown as string
      if (!ls) return // 用户取消
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
    for (const id of Object.keys(visibility)) {
      setVisibility(id, true)
    }
    flashMsg('已重置工具偏好')
  }

  const onResetAll = async () => {
    const ok = await confirm({
      title: '重置全部本地数据',
      message:
        '将清空所有数据:剪贴板历史 / AI 对话 / 收藏 / 主题 / 热键 / 工具偏好 / 昵称等等。此操作不可恢复,完成后需要重启 App。',
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
            查看本地数据占用,导入 / 导出备份,或按模块清理。
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

      {/* —— 1. 基础数据 —— */}
      <Section title="基础数据">
        <div className="rounded-lg border border-border bg-card p-4">
          <div className="flex items-start gap-3">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-info/15 text-info">
              <HardDrive className="h-4 w-4" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-baseline justify-between gap-2">
                <div className="text-sm font-medium">数据目录</div>
                <div className="text-xs text-muted-foreground">
                  {stats ? `${formatBytes(stats.totalBytes)} · ${stats.totalFiles} 个文件` : '—'}
                </div>
              </div>
              <div className="mt-1 flex items-center gap-2">
                <code className="min-w-0 flex-1 truncate rounded bg-secondary/40 px-2 py-1 font-mono text-[11px] text-muted-foreground">
                  {stats?.dataDir || '—'}
                </code>
                <Button size="sm" variant="ghost" onClick={onOpenDir} title="在资源管理器中打开">
                  <FolderOpen className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>
          </div>
        </div>
      </Section>

      {/* —— 2. 模块占用 —— */}
      <Section title="模块占用" hint="每项独立可清理;清理某模块不影响其他">
        <div className="overflow-hidden rounded-lg border border-border bg-card">
          {(stats?.modules ?? []).length === 0 ? (
            <div className="px-4 py-8 text-center text-xs text-muted-foreground">没有数据</div>
          ) : (
            <ul className="divide-y divide-border">
              {(stats?.modules ?? []).map((m) => (
                <ModuleRow
                  key={m.key}
                  m={m}
                  onOpen={() => void onOpenPath(m.path)}
                  onClear={() => void onClearModule(m)}
                />
              ))}
            </ul>
          )}
        </div>
      </Section>

      {/* —— 3. 备份还原 —— */}
      <Section title="备份还原">
        <div className="rounded-lg border border-border bg-card p-4">
          <p className="mb-3 text-sm text-muted-foreground">
            备份包含 <code className="rounded bg-secondary/40 px-1 font-mono text-[11px]">~/.toolforge/</code>{' '}
            目录全部内容 + 浏览器 localStorage 中{' '}
            <code className="rounded bg-secondary/40 px-1 font-mono text-[11px]">tool-forge:*</code> 条目。可用于跨机迁移或定期备份。
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
      </Section>

      {/* —— 4. 偏好清理(轻量) —— */}
      <Section title="偏好清理" hint="只清理 UI 级偏好,不影响工具数据">
        <div className="space-y-2">
          <PrefRow
            label="清空侧栏收藏"
            hint={`Dock 上 ${pinnedCount} / 5 个`}
            disabled={pinnedCount === 0}
            onClick={onResetPinned}
          />
          <PrefRow
            label="清空最近使用"
            hint={
              recentCount > 0
                ? `${recentCount} 项 · 累计打开 ${totalLaunches} 次`
                : '尚无记录'
            }
            disabled={recentCount === 0}
            onClick={onResetRecents}
          />
          <PrefRow
            label="重置工具偏好"
            hint={
              hasOrder || customizedTools > 0
                ? `顺序 ${hasOrder ? '已改' : '默认'} · 隐藏 ${customizedTools} 个`
                : '当前为默认'
            }
            disabled={!hasOrder && customizedTools === 0}
            onClick={onResetVisibility}
          />
        </div>
      </Section>

      {/* —— 5. 危险区 —— */}
      <Section title="危险区" danger>
        <div className="flex items-center gap-3 rounded-lg border border-red-500/40 bg-red-500/5 p-4">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-red-700 dark:text-red-300">
              重置全部本地数据
            </div>
            <div className="mt-0.5 text-[11px] text-muted-foreground">
              清空所有 localStorage 和 ~/.toolforge 目录,需要重启 App
            </div>
          </div>
          <Button size="sm" variant="destructive" onClick={onResetAll} disabled={busy}>
            <Trash2 className="h-3.5 w-3.5" />
            重置
          </Button>
        </div>
      </Section>
    </div>
  )
}

function Section({
  title,
  hint,
  danger,
  children,
}: {
  title: string
  hint?: string
  danger?: boolean
  children: React.ReactNode
}) {
  return (
    <section className="space-y-3">
      <div className="flex items-baseline justify-between gap-2">
        <div
          className={cn(
            'flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider',
            danger ? 'text-red-600 dark:text-red-400' : 'text-muted-foreground',
          )}
        >
          {danger && <AlertTriangle className="h-3 w-3" />}
          {title}
        </div>
        {hint && <span className="text-[11px] text-muted-foreground">{hint}</span>}
      </div>
      {children}
    </section>
  )
}

function ModuleRow({
  m,
  onOpen,
  onClear,
}: {
  m: Module
  onOpen: () => void
  onClear: () => void
}) {
  const meta = MODULE_META[m.key] ?? { icon: <FileJson className="h-4 w-4" /> }
  const empty = !m.exists || m.bytes === 0
  return (
    <li className="flex items-center gap-3 px-4 py-3">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-info/15 text-info">
        {meta.icon}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <span className="truncate text-sm font-medium">{m.label}</span>
          {m.subInfo && (
            <span className="shrink-0 text-[11px] text-muted-foreground">{m.subInfo}</span>
          )}
        </div>
        <code className="block truncate font-mono text-[11px] text-muted-foreground" title={m.path}>
          {m.path}
        </code>
      </div>
      <div className="shrink-0 text-right">
        <div className="text-sm font-semibold tabular-nums">
          {empty ? <span className="text-muted-foreground">—</span> : formatBytes(m.bytes)}
        </div>
        {!empty && m.isDir && (
          <div className="text-[11px] text-muted-foreground">{m.files} 文件</div>
        )}
        {empty && (
          <div className="text-[11px] text-muted-foreground">{meta.emptyHint ?? '空'}</div>
        )}
      </div>
      <div className="ml-2 flex shrink-0 items-center gap-1">
        <Button
          size="sm"
          variant="ghost"
          onClick={onOpen}
          title="在资源管理器中打开"
          disabled={!m.exists}
        >
          <ExternalLink className="h-3.5 w-3.5" />
        </Button>
        <Button
          size="sm"
          variant="ghost"
          onClick={onClear}
          title={m.exists ? '清空该模块数据' : '当前无数据'}
          disabled={empty}
          className="text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      </div>
    </li>
  )
}

function PrefRow({
  label,
  hint,
  onClick,
  disabled,
}: {
  label: string
  hint: string
  onClick: () => void
  disabled?: boolean
}) {
  return (
    <div className="flex items-center gap-3 rounded-lg border border-border bg-card p-3">
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium">{label}</div>
        <div className="text-[11px] text-muted-foreground">{hint}</div>
      </div>
      <Button size="sm" variant="outline" onClick={onClick} disabled={disabled}>
        <RotateCcw className="h-3.5 w-3.5" />
        清空
      </Button>
    </div>
  )
}
