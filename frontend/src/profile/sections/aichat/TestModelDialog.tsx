import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { X, Loader2, CheckCircle2, XCircle, Circle } from 'lucide-react'
import { CheckAIProviderKeys, ListAIProviderKeys } from '../../../../wailsjs/go/main/App'
import type { APIKeyEntry, KeyCheckResult, Provider } from '@/tools/ai-chat/types'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { cn } from '@/lib/utils'
import { maskKey } from './ApiKeyField'

/**
 * 用某个模型逐把检测密钥。
 *
 * 两个维度都要能选:密钥可能只有一把坏了,模型也可能只有某一个没开通 ——
 * 把它们混在一次"检测"里,结果出来根本分不清是哪边的问题。
 */
export function TestModelDialog({
  provider,
  onClose,
}: {
  provider: Provider
  onClose: () => void
}) {
  const dialog = useConfirm()
  const [model, setModel] = useState(provider.models[0] ?? '')
  const [keys, setKeys] = useState<APIKeyEntry[]>([])
  // '' = 全部启用中的密钥
  const [keyID, setKeyID] = useState('')
  const [testing, setTesting] = useState(false)
  const [results, setResults] = useState<KeyCheckResult[]>([])

  useEffect(() => {
    void (async () => {
      const list = await ListAIProviderKeys(provider.id)
      setKeys(Array.isArray(list) ? list : [])
    })()
  }, [provider.id])

  const noModels = provider.models.length === 0
  const noKeys = keys.length === 0

  const onTest = async () => {
    if (!model) {
      await dialog({ title: '提示', message: '请先选择一个模型', confirmLabel: '知道了' })
      return
    }
    setTesting(true)
    setResults([])
    try {
      const ids = keyID ? [keyID] : keys.filter((k) => !k.disabled).map((k) => k.id)
      const raw = (await CheckAIProviderKeys(provider.id, model, ids)) as unknown
      setResults(Array.isArray(raw) ? (raw as KeyCheckResult[]) : [])
    } finally {
      setTesting(false)
    }
  }

  const labelOf = (id: string) => {
    const k = keys.find((x) => x.id === id)
    if (!k) return id
    return k.label ? `${k.label} · ${maskKey(k.key)}` : maskKey(k.key)
  }

  return createPortal(
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center bg-black/40 p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div className="flex max-h-[80vh] w-[480px] max-w-full flex-col overflow-hidden rounded-xl border border-border bg-card shadow-2xl">
        <header className="flex h-12 shrink-0 items-center justify-between border-b border-border px-4">
          <h3 className="text-sm font-semibold">检测 API 密钥</h3>
          <button
            onClick={onClose}
            className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </header>

        <div className="min-h-0 flex-1 space-y-4 overflow-auto p-5">
          {noModels ? (
            <p className="rounded-md border border-dashed border-border bg-secondary/30 p-4 text-center text-xs text-muted-foreground">
              当前供应商还没选择模型,请先点「获取模型列表」拉一份并选入
            </p>
          ) : (
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-muted-foreground">用哪个模型检测</label>
              <select
                value={model}
                onChange={(e) => {
                  setModel(e.target.value)
                  setResults([])
                }}
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring"
              >
                {provider.models.map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </select>
            </div>
          )}

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">检测哪把密钥</label>
            <select
              value={keyID}
              onChange={(e) => {
                setKeyID(e.target.value)
                setResults([])
              }}
              disabled={noKeys}
              className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring disabled:opacity-50"
            >
              <option value="">全部启用中的密钥({keys.filter((k) => !k.disabled).length} 把)</option>
              {keys.map((k) => (
                <option key={k.id} value={k.id}>
                  {labelOf(k.id)}
                  {k.disabled ? '(已停用)' : ''}
                </option>
              ))}
            </select>
            <p className="text-[11px] text-muted-foreground">
              {noKeys
                ? '这个供应商还没有密钥'
                : '会用选中的模型发一次最小的流式请求,收到首个 chunk 即视为成功;多把并发但最多 4 路,避免把好密钥打成限流'}
            </p>
          </div>

          {results.length > 0 && (
            <ul className="space-y-1.5">
              {results.map((r) => (
                <li
                  key={r.keyId}
                  className={cn(
                    'flex items-start gap-2.5 rounded-md border p-2.5 text-xs',
                    r.ok ? 'border-success/30 bg-success/5' : 'border-destructive/30 bg-destructive/5',
                  )}
                >
                  {r.ok ? (
                    <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" />
                  ) : (
                    <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
                  )}
                  <div className="min-w-0 flex-1 space-y-0.5">
                    <div className="flex items-center gap-2">
                      <span className="truncate font-mono text-[11px]">{labelOf(r.keyId)}</span>
                      <span
                        className={cn(
                          'shrink-0 font-medium',
                          r.ok ? 'text-success' : 'text-destructive',
                        )}
                      >
                        {r.ok ? '可用' : '不可用'}
                      </span>
                      <span className="shrink-0 text-[10px] text-muted-foreground">
                        {r.statusCode ? `HTTP ${r.statusCode} · ` : ''}
                        {r.durationMs} ms
                      </span>
                    </div>
                    {r.message && (
                      <div className="whitespace-pre-wrap break-words text-muted-foreground">
                        {r.message}
                      </div>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )}

          {testing && results.length === 0 && (
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Circle className="h-3 w-3 animate-pulse" />
              正在检测 {keyID ? '1' : keys.filter((k) => !k.disabled).length} 把密钥...
            </div>
          )}
        </div>

        <footer className="flex h-12 shrink-0 items-center justify-end gap-2 border-t border-border bg-secondary/30 px-4">
          <Button variant="ghost" size="sm" onClick={onClose}>
            关闭
          </Button>
          <Button onClick={() => void onTest()} disabled={testing || noModels || noKeys} size="sm">
            {testing ? (
              <>
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                检测中...
              </>
            ) : (
              '开始检测'
            )}
          </Button>
        </footer>
      </div>
    </div>,
    document.body,
  )
}
