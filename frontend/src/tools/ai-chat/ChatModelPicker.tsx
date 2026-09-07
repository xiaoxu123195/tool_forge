import { useEffect, useMemo, useState } from 'react'
import { createPortal } from 'react-dom'
import { X, Search, Check } from 'lucide-react'
import { ListAIProviders } from '../../../wailsjs/go/main/App'
import type { Provider } from './types'
import { ProviderAvatar } from './ProviderAvatar'
import { cn } from '@/lib/utils'

interface Props {
  /** 当前选中的 (providerId, modelId) — 用于在列表上高亮 */
  current: { providerId: string; modelId: string }
  onClose: () => void
  onPick: (providerId: string, modelId: string) => void
}

/** 模型选择弹窗:居中模态框 + 搜索 + 按 provider 分组 */
export function ChatModelPicker({ current, onClose, onPick }: Props) {
  const [providers, setProviders] = useState<Provider[]>([])
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let alive = true
    void (async () => {
      const list = ((await ListAIProviders()) ?? []) as unknown as Provider[]
      if (alive) {
        setProviders(list)
        setLoading(false)
      }
    })()
    return () => {
      alive = false
    }
  }, [])

  // 只显示已启用且选了模型的供应商
  const groups = useMemo(() => {
    const q = search.trim().toLowerCase()
    return providers
      .filter((p) => p.enabled && p.models.length > 0)
      .map((p) => ({
        provider: p,
        models: q
          ? p.models.filter((m) => m.toLowerCase().includes(q) || p.name.toLowerCase().includes(q))
          : p.models,
      }))
      .filter((g) => g.models.length > 0)
  }, [providers, search])

  const total = groups.reduce((acc, g) => acc + g.models.length, 0)

  return createPortal(
    <div
      className="fixed inset-0 z-[80] flex items-center justify-center bg-black/40 p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div className="flex h-[70vh] w-[560px] max-w-full flex-col overflow-hidden rounded-xl border border-border bg-card shadow-2xl">
        <header className="flex h-12 shrink-0 items-center justify-between border-b border-border px-4">
          <h3 className="text-sm font-semibold">选择模型</h3>
          <button
            onClick={onClose}
            className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </header>

        <div className="border-b border-border p-3">
          <div className="relative">
            <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              autoFocus
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="搜索供应商或模型 ID..."
              className="h-9 w-full rounded-md border border-input bg-background pl-7 pr-2 text-sm outline-none focus:ring-1 focus:ring-ring"
            />
          </div>
        </div>

        <div className="min-h-0 flex-1 overflow-auto">
          {loading ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              加载中...
            </div>
          ) : groups.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
              <p>{providers.length === 0 ? '还没有任何供应商' : '没有可用模型'}</p>
              <p className="text-xs">请到「个人中心 → AI 配置」启用供应商并选择模型</p>
            </div>
          ) : (
            <div className="space-y-3 p-2">
              {groups.map(({ provider: p, models }) => (
                <div key={p.id}>
                  <div className="flex items-center gap-2 px-2 py-1">
                    <ProviderAvatar logo={p.logo} name={p.name} size={20} />
                    <span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                      {p.name}
                    </span>
                    <span className="text-[10px] text-muted-foreground">{models.length}</span>
                  </div>
                  <ul>
                    {models.map((m) => {
                      const active = current.providerId === p.id && current.modelId === m
                      return (
                        <li key={m}>
                          <button
                            type="button"
                            onClick={() => onPick(p.id, m)}
                            className={cn(
                              'flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm transition-colors',
                              active
                                ? 'bg-info/15 text-info'
                                : 'hover:bg-secondary',
                            )}
                          >
                            <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-info/15 text-[10px] font-semibold text-info">
                              {m.charAt(0).toUpperCase()}
                            </span>
                            <span className="min-w-0 flex-1 truncate font-mono text-xs">{m}</span>
                            {active && <Check className="h-4 w-4 shrink-0 text-info" />}
                          </button>
                        </li>
                      )
                    })}
                  </ul>
                </div>
              ))}
            </div>
          )}
        </div>

        <footer className="flex h-9 shrink-0 items-center border-t border-border bg-secondary/30 px-4 text-[11px] text-muted-foreground">
          共 {total} 个模型
        </footer>
      </div>
    </div>,
    document.body,
  )
}
