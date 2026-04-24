import { useEffect, useState } from 'react'
import { ClipboardList, Image as ImageIcon, Power, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import {
  ClearClipboardAll,
  ListClipboard,
  SetClipboardEnabled,
  SetClipboardLimit,
  SetClipboardMaxImageBytes,
} from '../../../wailsjs/go/main/App'

const LIMIT_PRESETS = [50, 100, 200, 500]
const IMAGE_LIMIT_PRESETS_MB = [5, 10, 20, 50]
const MB = 1024 * 1024

export function ClipboardSection() {
  const confirm = useConfirm()
  const [enabled, setEnabled] = useState(true)
  const [limit, setLimit] = useState(100)
  const [maxImageMB, setMaxImageMB] = useState(10)
  const [count, setCount] = useState(0)
  const [busy, setBusy] = useState(false)

  const refresh = async () => {
    const r = await ListClipboard()
    setEnabled(r.enabled)
    setLimit(r.limit)
    setMaxImageMB(Math.max(1, Math.round((r.maxImageBytes || 10 * MB) / MB)))
    setCount((r.items ?? []).length)
  }

  useEffect(() => {
    refresh()
  }, [])

  const toggleEnabled = async () => {
    setBusy(true)
    try {
      await SetClipboardEnabled(!enabled)
      setEnabled(!enabled)
    } finally {
      setBusy(false)
    }
  }

  const setLimitPreset = async (n: number) => {
    setBusy(true)
    try {
      await SetClipboardLimit(n)
      await refresh()
    } finally {
      setBusy(false)
    }
  }

  const setMaxImagePreset = async (mb: number) => {
    setBusy(true)
    try {
      await SetClipboardMaxImageBytes(mb * MB)
      await refresh()
    } finally {
      setBusy(false)
    }
  }

  const clearAll = async () => {
    const ok = await confirm({
      title: '清空所有剪贴板历史',
      message: '将删除所有条目(包括置顶项)以及缓存的图片文件。此操作不可恢复。',
      confirmLabel: '全部清空',
      danger: true,
    })
    if (!ok) return
    setBusy(true)
    try {
      await ClearClipboardAll()
      await refresh()
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mx-auto max-w-2xl space-y-6">
      <header>
        <h1 className="text-xl font-semibold">剪贴板</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          配置剪贴板历史的监听、容量与数据。Tool Forge 关闭后监听同时停止,数据不出本机。
        </p>
      </header>

      <div className="rounded-lg border border-border bg-card p-5">
        <div className="mb-4 flex items-center justify-between">
          <div>
            <h2 className="flex items-center gap-2 text-sm font-semibold">
              <Power className="h-4 w-4" />
              监听状态
            </h2>
            <p className="mt-1 text-xs text-muted-foreground">
              开启后 Tool Forge 会在后台记录你的复制内容(文本与图片)。
            </p>
          </div>
          <button
            onClick={toggleEnabled}
            disabled={busy}
            className={
              'relative inline-flex h-6 w-11 items-center rounded-full transition-colors ' +
              (enabled ? 'bg-indigo-500' : 'bg-muted')
            }
          >
            <span
              className={
                'inline-block h-5 w-5 rounded-full bg-white shadow transition-transform ' +
                (enabled ? 'translate-x-[22px]' : 'translate-x-0.5')
              }
            />
          </button>
        </div>
      </div>

      <div className="rounded-lg border border-border bg-card p-5">
        <div className="mb-3">
          <h2 className="flex items-center gap-2 text-sm font-semibold">
            <ClipboardList className="h-4 w-4" />
            历史上限
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            超过上限时自动按时间清理最旧的非置顶项。当前已存 <strong>{count}</strong> 条。
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {LIMIT_PRESETS.map((n) => (
            <button
              key={n}
              onClick={() => setLimitPreset(n)}
              disabled={busy}
              className={
                'inline-flex h-8 min-w-[64px] items-center justify-center rounded-md border px-3 text-xs font-medium transition-colors ' +
                (limit === n
                  ? 'border-indigo-500 bg-indigo-500/10 text-indigo-600 dark:text-indigo-300'
                  : 'border-border text-muted-foreground hover:border-foreground/30 hover:text-foreground')
              }
            >
              {n}
            </button>
          ))}
        </div>
      </div>

      <div className="rounded-lg border border-border bg-card p-5">
        <div className="mb-3">
          <h2 className="flex items-center gap-2 text-sm font-semibold">
            <ImageIcon className="h-4 w-4" />
            图片大小上限
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            单张图片超过此大小则不入库（避免大截图占满磁盘）。文本上限固定为 1 MB。
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {IMAGE_LIMIT_PRESETS_MB.map((mb) => (
            <button
              key={mb}
              onClick={() => setMaxImagePreset(mb)}
              disabled={busy}
              className={
                'inline-flex h-8 min-w-[64px] items-center justify-center rounded-md border px-3 text-xs font-medium transition-colors ' +
                (maxImageMB === mb
                  ? 'border-indigo-500 bg-indigo-500/10 text-indigo-600 dark:text-indigo-300'
                  : 'border-border text-muted-foreground hover:border-foreground/30 hover:text-foreground')
              }
            >
              {mb} MB
            </button>
          ))}
        </div>
      </div>

      <div className="rounded-lg border border-border bg-card p-5">
        <div className="mb-3">
          <h2 className="flex items-center gap-2 text-sm font-semibold text-destructive">
            <Trash2 className="h-4 w-4" />
            清空数据
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            一次性删除所有历史(包括置顶项)及缓存图片文件。
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={clearAll} disabled={busy} className="text-destructive hover:bg-destructive/10">
          <Trash2 className="h-3.5 w-3.5" />
          清空所有历史
        </Button>
      </div>

      <div className="rounded-lg border border-dashed border-border bg-muted/20 p-4 text-xs text-muted-foreground">
        <strong className="text-foreground/80">快捷键</strong> · 按 <kbd className="rounded border border-border bg-background px-1.5 py-0.5 font-mono text-[10px]">Ctrl + Shift + V</kbd> 可以从任何应用唤起并跳转到剪贴板页。
      </div>
    </div>
  )
}
