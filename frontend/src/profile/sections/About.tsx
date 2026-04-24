import { useEffect } from 'react'
import {
  AlertTriangle,
  CheckCircle2,
  Download,
  ExternalLink,
  FolderOpen,
  Github,
  Info,
  Loader2,
  Mail,
  Power,
  RefreshCw,
  XCircle,
} from 'lucide-react'
import { GetAppInfo } from '../../../wailsjs/go/main/App'
import type { main } from '../../../wailsjs/go/models'
import { useState } from 'react'
import { toolRegistry } from '@/tools/registry'
import { CATEGORY_LABELS, type ToolCategory } from '@/stores/tools'
import { useUpdaterStore } from '@/stores/updater'
import logoUrl from '@/assets/logo.png'

const CATEGORY_ACCENT: Record<ToolCategory, string> = {
  forensic: 'bg-rose-500',
  data: 'bg-blue-500',
  ai: 'bg-indigo-500',
  codec: 'bg-violet-500',
  crypto: 'bg-amber-500',
  time: 'bg-sky-500',
  text: 'bg-emerald-500',
  network: 'bg-cyan-500',
  gen: 'bg-fuchsia-500',
  dev: 'bg-slate-500',
  system: 'bg-zinc-500',
}

const FEEDBACK_EMAIL = 'cherrytump@gmail.com'

function formatSize(bytes: number): string {
  if (!bytes || bytes <= 0) return '—'
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}

export function AboutSection() {
  const [info, setInfo] = useState<main.AppInfo | null>(null)
  const upd = useUpdaterStore()

  useEffect(() => {
    GetAppInfo().then(setInfo)
    // 打开 About 时静默刷一次;若已经在下载/刚下载完,就不打扰
    if (upd.status === 'idle' || upd.status === 'latest' || upd.status === 'available' || upd.status === 'error') {
      upd.check({ silent: true })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const categoryCounts = toolRegistry.reduce<Partial<Record<ToolCategory, number>>>((acc, t) => {
    acc[t.category] = (acc[t.category] ?? 0) + 1
    return acc
  }, {})

  return (
    <div className="mx-auto max-w-2xl space-y-5">
      {/* 头图 */}
      <div className="group flex flex-col items-center gap-3 rounded-lg border border-border bg-card p-6 text-center transition-all duration-300 hover:-translate-y-0.5 hover:shadow-lg">
        <div className="relative">
          <div className="absolute inset-0 rounded-2xl bg-gradient-to-br from-violet-500/40 to-fuchsia-500/40 opacity-40 blur-xl transition-opacity duration-500 group-hover:opacity-80" />
          <img
            src={logoUrl}
            alt="Tool Forge"
            className="relative h-16 w-16 rounded-2xl shadow-sm transition-transform duration-300 group-hover:scale-105"
          />
        </div>
        <div>
          <h1 className="text-lg font-semibold">Tool Forge</h1>
          <p className="text-xs text-muted-foreground">
            v{info?.version ?? '…'} · 给程序员的一站式桌面工具箱
          </p>
        </div>
        <div className="flex gap-2 pt-1">
          <a
            href={`mailto:${FEEDBACK_EMAIL}?subject=Tool%20Forge%20反馈`}
            className="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-3 text-xs font-medium transition-colors hover:bg-accent"
          >
            <Mail className="h-3.5 w-3.5" />
            问题反馈
            <ExternalLink className="h-3 w-3 opacity-60" />
          </a>
          <span
            className="group/gh relative inline-flex h-8 cursor-not-allowed items-center gap-1.5 rounded-md border border-input bg-background px-3 text-xs font-medium opacity-60"
            aria-disabled="true"
          >
            <Github className="h-3.5 w-3.5" />
            GitHub
            <span className="pointer-events-none absolute left-1/2 top-full z-10 mt-1.5 -translate-x-1/2 whitespace-nowrap rounded bg-popover px-2 py-1 text-[11px] text-popover-foreground opacity-0 shadow-md ring-1 ring-border transition-opacity duration-200 group-hover/gh:opacity-100">
              暂未开源
            </span>
          </span>
        </div>
      </div>

      {/* 工具总览 */}
      <div className="rounded-lg border border-border bg-card">
        <div className="flex items-center gap-1.5 border-b border-border px-4 py-2 text-xs font-medium text-muted-foreground">
          <Info className="h-3.5 w-3.5" />
          工具总览 · 共 {toolRegistry.length} 个
        </div>
        <div className="space-y-3 p-4">
          <div className="flex h-2 w-full overflow-hidden rounded-full bg-muted/40">
            {(Object.keys(CATEGORY_LABELS) as ToolCategory[])
              .filter((cat) => categoryCounts[cat])
              .map((cat) => {
                const count = categoryCounts[cat] ?? 0
                const pct = (count / toolRegistry.length) * 100
                return (
                  <div
                    key={cat}
                    className={`${CATEGORY_ACCENT[cat]} transition-opacity hover:opacity-80`}
                    style={{ width: `${pct}%` }}
                    title={`${CATEGORY_LABELS[cat]} · ${count}`}
                  />
                )
              })}
          </div>
          <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 pt-1 sm:grid-cols-3">
            {(Object.keys(CATEGORY_LABELS) as ToolCategory[])
              .filter((cat) => categoryCounts[cat])
              .map((cat) => (
                <div key={cat} className="flex items-center gap-1.5 text-xs">
                  <span className={`h-2 w-2 shrink-0 rounded-full ${CATEGORY_ACCENT[cat]}`} />
                  <span className="flex-1 truncate text-foreground/80">
                    {CATEGORY_LABELS[cat]}
                  </span>
                  <span className="font-mono text-[11px] text-muted-foreground tabular-nums">
                    {categoryCounts[cat]}
                  </span>
                </div>
              ))}
          </div>
        </div>
      </div>

      {/* 更新检查 */}
      <UpdateCard currentVersion={info?.version ?? null} />

      <p className="text-center text-xs text-muted-foreground">
        © {new Date().getFullYear()} Tool Forge · MIT · Made with ♥
      </p>
    </div>
  )
}

function UpdateCard({ currentVersion }: { currentVersion: string | null }) {
  const upd = useUpdaterStore()
  const s = upd.status

  // 顶部图标 + 主描述
  const iconAndText = (() => {
    switch (s) {
      case 'checking':
        return {
          icon: <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />,
          title: '检查中…',
          sub: '正在连接更新服务器',
        }
      case 'latest':
        return {
          icon: <CheckCircle2 className="h-4 w-4 text-emerald-500" />,
          title: `当前已是最新版本 v${currentVersion ?? '…'}`,
          sub: upd.lastCheckedAt
            ? `上次检查 ${upd.lastCheckedAt.toLocaleTimeString()}`
            : '',
        }
      case 'available':
        return {
          icon: <AlertTriangle className="h-4 w-4 text-amber-500" />,
          title: `发现新版本 v${upd.latestVersion ?? '…'}`,
          sub: upd.manifest?.size_bytes
            ? `安装包 ${formatSize(upd.manifest.size_bytes)}`
            : '',
        }
      case 'downloading':
        return {
          icon: <Loader2 className="h-4 w-4 animate-spin text-primary" />,
          title: `下载新版本 v${upd.latestVersion ?? '…'} 中…`,
          sub: `${formatSize(upd.progressLoaded)} / ${formatSize(upd.progressTotal)}`,
        }
      case 'downloaded':
        return {
          icon: <CheckCircle2 className="h-4 w-4 text-emerald-500" />,
          title: '新版本已下载',
          sub: '点击"立即安装并重启"即可切换到新版',
        }
      case 'error':
        return {
          icon: <XCircle className="h-4 w-4 text-destructive" />,
          title: '检查更新失败',
          sub: upd.errorMessage ?? '网络异常,稍后再试',
        }
      case 'download-error':
        return {
          icon: <XCircle className="h-4 w-4 text-destructive" />,
          title: '下载失败',
          sub: upd.errorMessage ?? '请重试',
        }
      default:
        return {
          icon: <CheckCircle2 className="h-4 w-4 text-muted-foreground" />,
          title: `当前版本 v${currentVersion ?? '…'}`,
          sub: '点击检查是否有新版本',
        }
    }
  })()

  return (
    <div className="rounded-lg border border-border bg-card">
      <div className="flex items-start gap-2.5 p-4">
        <div className="mt-0.5">{iconAndText.icon}</div>
        <div className="min-w-0 flex-1">
          <div className="text-sm">{iconAndText.title}</div>
          {iconAndText.sub && (
            <div className="mt-0.5 text-xs text-muted-foreground">
              {iconAndText.sub}
            </div>
          )}
          {s === 'downloading' && (
            <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-muted">
              <div
                className="h-full bg-primary transition-all duration-150"
                style={{ width: `${upd.progressPercent}%` }}
              />
            </div>
          )}
          {upd.manifest?.changelog && (s === 'available' || s === 'downloading' || s === 'downloaded') && (
            <details className="mt-2">
              <summary className="cursor-pointer text-xs text-muted-foreground hover:text-foreground">
                查看更新日志
              </summary>
              <pre className="mt-1.5 whitespace-pre-wrap rounded border border-border bg-muted/40 p-2 font-sans text-xs text-foreground/80">
                {upd.manifest.changelog}
              </pre>
            </details>
          )}
        </div>
        <UpdateActions />
      </div>
    </div>
  )
}

function UpdateActions() {
  const upd = useUpdaterStore()
  const s = upd.status
  const baseBtn =
    'inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-3 text-xs font-medium transition-colors hover:bg-accent disabled:opacity-60'
  const primaryBtn =
    'inline-flex h-8 items-center gap-1.5 rounded-md bg-primary text-primary-foreground px-3 text-xs font-medium transition-colors hover:bg-primary/90 disabled:opacity-60'

  if (s === 'available') {
    return (
      <button onClick={() => upd.download()} className={primaryBtn}>
        <Download className="h-3.5 w-3.5" />
        下载更新
      </button>
    )
  }
  if (s === 'downloading') {
    return (
      <button disabled className={baseBtn}>
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        下载中
      </button>
    )
  }
  if (s === 'downloaded') {
    return (
      <div className="flex flex-col gap-1.5">
        <button onClick={() => upd.installAndRestart()} className={primaryBtn}>
          <Power className="h-3.5 w-3.5" />
          立即安装并重启
        </button>
        <button onClick={() => upd.openDownloadsFolder()} className={baseBtn}>
          <FolderOpen className="h-3.5 w-3.5" />
          打开下载目录
        </button>
      </div>
    )
  }
  // idle / checking / latest / error / download-error
  return (
    <button
      onClick={() => upd.check()}
      disabled={s === 'checking'}
      className={baseBtn}
    >
      <RefreshCw className={`h-3.5 w-3.5 ${s === 'checking' ? 'animate-spin' : ''}`} />
      {s === 'checking' ? '检查中' : s === 'error' || s === 'download-error' ? '重试' : '检查更新'}
    </button>
  )
}
