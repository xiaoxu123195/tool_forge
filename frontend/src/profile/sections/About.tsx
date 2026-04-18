import { useEffect, useState } from 'react'
import { CheckCircle2, ExternalLink, Github, Info, Mail, RefreshCw } from 'lucide-react'
import { GetAppInfo } from '../../../wailsjs/go/main/App'
import type { main } from '../../../wailsjs/go/models'
import { toolRegistry } from '@/tools/registry'
import { CATEGORY_LABELS, type ToolCategory } from '@/stores/tools'
import logoUrl from '@/assets/logo.png'

const CATEGORY_ACCENT: Record<ToolCategory, string> = {
  forensic: 'bg-rose-500',
  data: 'bg-blue-500',
  codec: 'bg-violet-500',
  crypto: 'bg-amber-500',
  time: 'bg-sky-500',
  text: 'bg-emerald-500',
  network: 'bg-cyan-500',
  gen: 'bg-indigo-500',
  dev: 'bg-slate-500',
}

const FEEDBACK_EMAIL = 'cherrytump@gmail.com'

export function AboutSection() {
  const [info, setInfo] = useState<main.AppInfo | null>(null)
  const [checking, setChecking] = useState(false)
  const [lastChecked, setLastChecked] = useState<Date | null>(null)

  useEffect(() => {
    GetAppInfo().then(setInfo)
  }, [])

  const categoryCounts = toolRegistry.reduce<Partial<Record<ToolCategory, number>>>((acc, t) => {
    acc[t.category] = (acc[t.category] ?? 0) + 1
    return acc
  }, {})

  const checkUpdate = () => {
    if (checking) return
    setChecking(true)
    // TODO: 接入 GitHub release API
    setTimeout(() => {
      setChecking(false)
      setLastChecked(new Date())
    }, 800)
  }

  return (
    <div className="mx-auto max-w-2xl space-y-5">
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

      <div className="rounded-lg border border-border bg-card">
        <div className="flex items-center justify-between px-4 py-3">
          <div className="flex items-center gap-2.5">
            <CheckCircle2 className="h-4 w-4 text-emerald-500" />
            <div>
              <div className="text-sm">当前版本 v{info?.version ?? '…'}</div>
              <div className="text-xs text-muted-foreground">
                {lastChecked
                  ? `上次检查 ${lastChecked.toLocaleTimeString()}`
                  : '点击检查是否有新版本'}
              </div>
            </div>
          </div>
          <button
            onClick={checkUpdate}
            disabled={checking}
            className="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-3 text-xs font-medium transition-colors hover:bg-accent disabled:opacity-60"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${checking ? 'animate-spin' : ''}`} />
            {checking ? '检查中' : '检查更新'}
          </button>
        </div>
      </div>

      <p className="text-center text-xs text-muted-foreground">
        © {new Date().getFullYear()} Tool Forge · MIT · Made with ♥
      </p>
    </div>
  )
}
