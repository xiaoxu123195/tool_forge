import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import {
  X,
  Search,
  Plus,
  Minus,
  RefreshCw,
  AlertCircle,
  Loader2,
  PencilLine,
  SlidersHorizontal,
} from 'lucide-react'
import {
  FetchAIModels,
  GetAIModelSpec,
  SaveAIProvider,
} from '../../../../wailsjs/go/main/App'
import type {
  Capability,
  FetchModelsResult,
  ModelInfo,
  ModelOverride,
  ModelSpec,
  Provider,
} from '@/tools/ai-chat/types'
import { useConfirm } from '@/components/ui/confirm'
import { cn } from '@/lib/utils'

/** 可手动勾选的能力。顺序即 UI 顺序 */
const CAPABILITY_ITEMS: { key: Capability; label: string; hint: string }[] = [
  { key: 'vision', label: '看图', hint: '能接收图片输入' },
  { key: 'pdf', label: '原生 PDF', hint: '能直接吃 PDF 二进制,不用先抽成文本' },
  { key: 'reasoning', label: '思考', hint: '会输出思考过程' },
  { key: 'webSearch', label: '联网', hint: '支持供应商内置的联网搜索' },
  { key: 'imageGen', label: '生图', hint: '能生成图片' },
]

export function ModelManageDrawer({
  provider,
  onClose,
}: {
  provider: Provider
  onClose: () => void
}) {
  const dialog = useConfirm()
  const [loading, setLoading] = useState(true)
  const [models, setModels] = useState<ModelInfo[]>([])
  const [error, setError] = useState('')
  const [filter, setFilter] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set(provider.models))
  const [pending, setPending] = useState<Set<string>>(new Set())
  const [manualOpen, setManualOpen] = useState(false)
  const [manualId, setManualId] = useState('')
  // 能力覆盖:本地保存一份,和 selected 一样属于"随手改随手落库"的状态。
  // 每次落库都要连 models 一起写回去 —— provider 是打开抽屉那一刻的快照,
  // 只写其中一半会把这次会话里改的另一半冲掉。
  const [overrides, setOverrides] = useState<Record<string, ModelOverride>>(
    provider.modelOverrides ?? {},
  )
  const [capOpen, setCapOpen] = useState('')
  const manualInputRef = useRef<HTMLInputElement>(null)

  /** 把当前的模型选择 + 能力覆盖整体写回;返回错误信息(空串 = 成功) */
  const persist = async (
    nextModels: Set<string>,
    nextOverrides: Record<string, ModelOverride>,
  ): Promise<string> => {
    // 出错时 Wails 直接 reject(第二个返回值是 error),不再走"错误字符串"那条路 ——
    // 以前那句解包永远得到空串,保存失败在界面上一点声音都没有
    try {
      await SaveAIProvider({
        ...provider,
        models: Array.from(nextModels),
        modelOverrides: nextOverrides,
      } as unknown as never)
      return ''
    } catch (e) {
      return String(e)
    }
  }

  const load = async () => {
    setLoading(true)
    setError('')
    try {
      const r = (await FetchAIModels(provider.id)) as unknown as FetchModelsResult
      if (!r.ok) {
        setError(r.message ?? '请求失败')
        setModels([])
        return
      }
      const list = r.models ?? []
      list.sort((a, b) => a.id.localeCompare(b.id))
      setModels(list)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [provider.id])

  // 合并:远端拉到的模型 + selected 里但远端没有的(用户手动添加的);
  // 让自定义模型也能在列表里看到并通过 - 按钮移除
  const mergedModels = useMemo(() => {
    const known = new Set(models.map((m) => m.id))
    const extras: ModelInfo[] = []
    for (const id of selected) {
      if (!known.has(id)) extras.push({ id, ownedBy: 'custom' })
    }
    return extras.length ? [...extras, ...models] : models
  }, [models, selected])

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (!q) return mergedModels
    return mergedModels.filter((m) => m.id.toLowerCase().includes(q))
  }, [mergedModels, filter])

  const grouped = useMemo(() => {
    const map = new Map<string, ModelInfo[]>()
    for (const m of filtered) {
      const groupKey = m.ownedBy === 'custom' ? '自定义' : inferGroup(m.id)
      const arr = map.get(groupKey) ?? []
      arr.push(m)
      map.set(groupKey, arr)
    }
    // 自定义组排最前
    return Array.from(map.entries()).sort((a, b) => {
      if (a[0] === '自定义') return -1
      if (b[0] === '自定义') return 1
      return a[0].localeCompare(b[0])
    })
  }, [filtered])

  const addManual = async () => {
    const id = manualId.trim()
    if (!id) return
    if (selected.has(id)) {
      setManualId('')
      setManualOpen(false)
      return
    }
    const next = new Set(selected)
    next.add(id)
    setSelected(next)
    setPending((p) => new Set(p).add(id))
    try {
      const err = await persist(next, overrides)
      if (err) {
        setSelected((prev) => {
          const back = new Set(prev)
          back.delete(id)
          return back
        })
        await dialog({ title: '添加模型失败', message: err, confirmLabel: '知道了' })
      } else {
        setManualId('')
        setManualOpen(false)
      }
    } finally {
      setPending((p) => {
        const next = new Set(p)
        next.delete(id)
        return next
      })
    }
  }

  /** 保存(next 为 null 表示清除)某个模型的能力覆盖 */
  const saveOverride = async (id: string, next: ModelOverride | null): Promise<string> => {
    const nextOverrides = { ...overrides }
    if (next) nextOverrides[id] = next
    else delete nextOverrides[id]
    const err = await persist(selected, nextOverrides)
    if (!err) setOverrides(nextOverrides)
    return err
  }

  // 单条点击就立即落库;期间防抖避免连点出问题
  const toggle = async (id: string) => {
    if (pending.has(id)) return
    const willAdd = !selected.has(id)
    const nextSelected = new Set(selected)
    if (willAdd) nextSelected.add(id)
    else nextSelected.delete(id)

    setSelected(nextSelected)
    setPending((p) => new Set(p).add(id))
    try {
      const err = await persist(nextSelected, overrides)
      if (err) {
        // 回滚 UI
        setSelected((prev) => {
          const back = new Set(prev)
          if (willAdd) back.delete(id)
          else back.add(id)
          return back
        })
        await dialog({
          title: willAdd ? '加入模型失败' : '移除模型失败',
          message: err,
          confirmLabel: '知道了',
        })
      }
    } finally {
      setPending((p) => {
        const next = new Set(p)
        next.delete(id)
        return next
      })
    }
  }

  return createPortal(
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center bg-black/40 p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div className="flex h-[78vh] w-[720px] max-w-full flex-col overflow-hidden rounded-xl border border-border bg-card shadow-2xl">
        <header className="flex h-12 shrink-0 items-center justify-between border-b border-border px-4">
          <h3 className="text-sm font-semibold">{provider.name} · 模型</h3>
          <div className="flex items-center gap-1">
            <button
              onClick={load}
              disabled={loading}
              title="重新拉取 /v1/models"
              className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:opacity-50"
            >
              <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />
            </button>
            <button
              onClick={onClose}
              className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        </header>

        <div className="space-y-2 border-b border-border p-3">
          <div className="flex items-center gap-2">
            <div className="relative flex-1">
              <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <input
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder="搜索模型 ID..."
                className="h-8 w-full rounded-md border border-input bg-background pl-7 pr-2 text-xs outline-none focus:ring-1 focus:ring-ring"
              />
            </div>
            <button
              type="button"
              onClick={() => {
                setManualOpen((v) => !v)
                setTimeout(() => manualInputRef.current?.focus(), 0)
              }}
              className={cn(
                'flex h-8 shrink-0 items-center gap-1 rounded-md border px-2 text-xs transition-colors',
                manualOpen
                  ? 'border-info/50 bg-info/10 text-info'
                  : 'border-input bg-background text-muted-foreground hover:bg-secondary hover:text-foreground',
              )}
              title="部分服务商不提供 /models 接口,可手动输入模型 ID"
            >
              <PencilLine className="h-3.5 w-3.5" />
              手动添加
            </button>
          </div>
          {manualOpen && (
            <div className="flex items-center gap-2">
              <input
                ref={manualInputRef}
                value={manualId}
                onChange={(e) => setManualId(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    void addManual()
                  } else if (e.key === 'Escape') {
                    setManualOpen(false)
                    setManualId('')
                  }
                }}
                placeholder="例如 grok-4.1-fast、glm-4.6 等"
                className="h-8 flex-1 rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
              />
              <button
                type="button"
                onClick={() => void addManual()}
                disabled={!manualId.trim()}
                className="h-8 rounded-md bg-info px-3 text-xs font-medium text-info-foreground transition-colors hover:bg-info/90 disabled:opacity-50"
              >
                添加
              </button>
            </div>
          )}
        </div>

        <div className="min-h-0 flex-1 overflow-auto">
          {loading ? (
            <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              拉取模型列表中...
            </div>
          ) : error ? (
            <div className="m-4 flex items-start gap-3 rounded-md border border-destructive/30 bg-destructive/5 p-4 text-sm">
              <AlertCircle className="h-4 w-4 shrink-0 text-destructive" />
              <div>
                <div className="font-medium text-destructive">无法拉取模型列表</div>
                <div className="mt-1 text-xs text-muted-foreground">{error}</div>
                <p className="mt-2 text-[11px] text-muted-foreground">
                  请检查 API 密钥与 API 地址是否正确,然后点右上刷新按钮重试。
                </p>
              </div>
            </div>
          ) : grouped.length === 0 ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              无匹配模型
            </div>
          ) : (
            <div className="space-y-3 p-3">
              {grouped.map(([group, list]) => (
                <div key={group}>
                  <div className="mb-1 px-2 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                    {group} <span className="ml-1 normal-case">{list.length}</span>
                  </div>
                  <ul className="space-y-1">
                    {list.map((m) => {
                      const checked = selected.has(m.id)
                      const busy = pending.has(m.id)
                      const overridden = !!overrides[m.id]
                      return (
                        <li key={m.id} className="space-y-1">
                          <div
                            onClick={() => void toggle(m.id)}
                            className={cn(
                              'flex h-9 cursor-pointer items-center gap-2 rounded-md border px-3 transition-colors',
                              checked
                                ? 'border-info/50 bg-info/10'
                                : 'border-border bg-card hover:bg-secondary/50',
                              busy && 'pointer-events-none opacity-60',
                            )}
                          >
                            <div className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-info/15 text-[10px] font-semibold text-info">
                              {m.id.slice(0, 1).toUpperCase()}
                            </div>
                            <span className="flex-1 truncate font-mono text-xs">{m.id}</span>
                            {/* 能力覆盖只对已选中的模型有意义 —— 没在用的模型不用操心它支持什么 */}
                            {checked && (
                              <button
                                type="button"
                                onClick={(e) => {
                                  e.stopPropagation()
                                  setCapOpen(capOpen === m.id ? '' : m.id)
                                }}
                                title="能力修正:推断不准时手动指定"
                                className={cn(
                                  'flex h-6 w-6 shrink-0 items-center justify-center rounded transition-colors hover:bg-secondary',
                                  overridden || capOpen === m.id
                                    ? 'text-info'
                                    : 'text-muted-foreground',
                                )}
                              >
                                <SlidersHorizontal className="h-3.5 w-3.5" />
                              </button>
                            )}
                            {busy ? (
                              <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-muted-foreground" />
                            ) : checked ? (
                              <Minus className="h-4 w-4 shrink-0 text-destructive/70" />
                            ) : (
                              <Plus className="h-4 w-4 shrink-0 text-muted-foreground" />
                            )}
                          </div>
                          {capOpen === m.id && (
                            <ModelCapabilityPanel
                              providerId={provider.id}
                              modelId={m.id}
                              override={overrides[m.id]}
                              onSave={(next) => saveOverride(m.id, next)}
                              onClose={() => setCapOpen('')}
                            />
                          )}
                        </li>
                      )
                    })}
                  </ul>
                </div>
              ))}
            </div>
          )}
        </div>

        <footer className="flex h-10 shrink-0 items-center justify-between border-t border-border bg-secondary/30 px-4 text-xs text-muted-foreground">
          <span>
            已选 {selected.size} 个模型(总 {models.length})· 点击即生效
          </span>
        </footer>
      </div>
    </div>,
    document.body,
  )
}

/**
 * 单个模型的能力修正面板。
 *
 * 能力目录是按模型 ID 前缀猜的,中转把模型改了名就猜不出来。这里先把当前生效的推断结果
 * 摊开给用户看,再给两条纠正路径:填「实际模型」一次性拿回全部正确推断,或者直接勾能力。
 */
function ModelCapabilityPanel({
  providerId,
  modelId,
  override,
  onSave,
  onClose,
}: {
  providerId: string
  modelId: string
  override: ModelOverride | undefined
  onSave: (next: ModelOverride | null) => Promise<string>
  onClose: () => void
}) {
  const [spec, setSpec] = useState<ModelSpec | null>(null)
  const [alias, setAlias] = useState(override?.aliasOf ?? '')
  const [caps, setCaps] = useState<Set<Capability>>(new Set())
  // 只有用户真的动过复选框才写 capabilitiesSet —— 否则改个别名就把能力集冻住了,
  // 之后我们更新推断规则也不会再生效
  const [capsTouched, setCapsTouched] = useState(false)
  const [maxOutput, setMaxOutput] = useState(
    override?.maxOutput ? String(override.maxOutput) : '',
  )
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')

  // 拉当前生效的能力画像(已经算上现有覆盖),既用来展示也用来给复选框做初值
  useEffect(() => {
    let alive = true
    void (async () => {
      const r = (await GetAIModelSpec(providerId, modelId)) as any
      if (!alive) return
      const got = (Array.isArray(r) ? r[0] : r?.['0']) as ModelSpec | null
      if (got?.id) {
        setSpec(got)
        setCaps(new Set(got.capabilities))
      }
    })()
    return () => {
      alive = false
    }
  }, [providerId, modelId])

  const toggleCap = (key: Capability) => {
    setCapsTouched(true)
    setCaps((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const submit = async (clear: boolean) => {
    setSaving(true)
    setErr('')
    let payload: ModelOverride | null = null
    if (!clear) {
      const next: ModelOverride = {}
      if (alias.trim()) next.aliasOf = alias.trim()
      if (capsTouched) {
        next.capabilitiesSet = true
        next.capabilities = CAPABILITY_ITEMS.filter((c) => caps.has(c.key)).map((c) => c.key)
      }
      const n = Number.parseInt(maxOutput, 10)
      if (Number.isFinite(n) && n > 0) next.maxOutput = n
      // 什么都没填等于没有覆盖,不留一条空记录
      if (Object.keys(next).length > 0) payload = next
    }
    const e = await onSave(payload)
    setSaving(false)
    if (e) {
      setErr(e)
      return
    }
    onClose()
  }

  const effortText = spec?.reasoning?.efforts?.length
    ? spec.reasoning.efforts.join(' / ')
    : '无可调档位'

  return (
    <div className="space-y-3 rounded-md border border-info/30 bg-secondary/20 p-3 text-xs">
      <div className="text-muted-foreground">
        当前推断:
        <span className="ml-1 font-mono">{spec ? spec.endpoint : '读取中...'}</span>
        {spec && (
          <>
            <span className="mx-1">·</span>
            输出上限 {spec.maxOutput}
            <span className="mx-1">·</span>
            思考档位 {effortText}
          </>
        )}
      </div>

      <label className="block space-y-1">
        <span className="text-muted-foreground">
          实际模型(中转改过名时填,按它推断能一次拿回正确的思考方言与预算)
        </span>
        <input
          value={alias}
          onChange={(e) => setAlias(e.target.value)}
          placeholder="例如 claude-sonnet-4-5 / gpt-5 / gemini-2.5-pro"
          className="h-8 w-full rounded-md border border-input bg-background px-2 font-mono outline-none focus:ring-1 focus:ring-ring"
        />
      </label>

      <div className="space-y-1">
        <span className="text-muted-foreground">能力(勾选后以你的选择为准,不再自动推断)</span>
        <div className="flex flex-wrap gap-1.5">
          {CAPABILITY_ITEMS.map((c) => (
            <button
              key={c.key}
              type="button"
              onClick={() => toggleCap(c.key)}
              title={c.hint}
              className={cn(
                'rounded-md border px-2 py-1 transition-colors',
                caps.has(c.key)
                  ? 'border-info/50 bg-info/10 text-info'
                  : 'border-border text-muted-foreground hover:bg-secondary',
              )}
            >
              {c.label}
            </button>
          ))}
        </div>
      </div>

      <label className="block space-y-1">
        <span className="text-muted-foreground">单次回复 token 上限(留空用推断值)</span>
        <input
          value={maxOutput}
          onChange={(e) => setMaxOutput(e.target.value.replace(/\D/g, ''))}
          placeholder={spec ? String(spec.maxOutput) : ''}
          inputMode="numeric"
          className="h-8 w-40 rounded-md border border-input bg-background px-2 font-mono outline-none focus:ring-1 focus:ring-ring"
        />
      </label>

      {err && <div className="text-destructive">{err}</div>}

      <div className="flex items-center gap-2 pt-1">
        <button
          type="button"
          onClick={() => void submit(false)}
          disabled={saving}
          className="h-7 rounded-md bg-info px-3 font-medium text-info-foreground transition-colors hover:bg-info/90 disabled:opacity-50"
        >
          {saving ? '保存中...' : '保存'}
        </button>
        {override && (
          <button
            type="button"
            onClick={() => void submit(true)}
            disabled={saving}
            className="h-7 rounded-md border border-border px-3 text-muted-foreground transition-colors hover:bg-secondary disabled:opacity-50"
          >
            清除覆盖
          </button>
        )}
        <button
          type="button"
          onClick={onClose}
          className="h-7 rounded-md px-3 text-muted-foreground transition-colors hover:bg-secondary"
        >
          取消
        </button>
      </div>
    </div>
  )
}

function inferGroup(id: string): string {
  const lower = id.toLowerCase()
  if (lower.startsWith('gpt-4o')) return 'GPT-4o'
  if (lower.startsWith('gpt-4.1')) return 'GPT-4.1'
  if (lower.startsWith('gpt-4')) return 'GPT-4'
  if (lower.startsWith('gpt-5')) return 'GPT-5'
  if (lower.startsWith('gpt-3.5')) return 'GPT-3.5'
  if (lower.startsWith('o1')) return 'o1'
  if (lower.startsWith('o3')) return 'o3'
  if (lower.startsWith('o4')) return 'o4'
  if (lower.includes('embedding')) return 'Embedding'
  if (lower.includes('whisper')) return 'Whisper'
  if (lower.includes('tts')) return 'TTS'
  if (lower.includes('dall')) return 'DALL·E'
  if (lower.startsWith('claude')) return 'Claude'
  if (lower.startsWith('gemini')) return 'Gemini'
  if (lower.startsWith('deepseek')) return 'DeepSeek'
  if (lower.startsWith('glm')) return 'GLM'
  if (lower.startsWith('qwen')) return 'Qwen'
  if (lower.startsWith('moonshot')) return 'Moonshot'
  return '其他'
}
