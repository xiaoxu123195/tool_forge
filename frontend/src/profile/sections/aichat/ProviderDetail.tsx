import { useEffect, useMemo, useState } from 'react'
import { ChevronDown, Minus, RefreshCw, Save, Trash2 } from 'lucide-react'
import { ListAIModelSpecs, SaveAIProvider } from '../../../../wailsjs/go/main/App'
import type { Capability, ModelSpec, Provider, ProviderType } from '@/tools/ai-chat/types'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { cn } from '@/lib/utils'
import { ApiKeyField } from './ApiKeyField'
import { ProviderAvatar } from './ProviderAvatar'

const DEFAULT_BASE_URL_BY_TYPE: Record<ProviderType, string> = {
  openai: 'https://api.openai.com/v1',
  'openai-compatible': 'https://api.openai.com/v1',
  gemini: 'https://generativelanguage.googleapis.com',
  anthropic: 'https://api.anthropic.com',
  xai: 'https://api.x.ai/v1',
}

const TYPE_LABEL: Record<ProviderType, string> = {
  openai: 'OpenAI(/responses)',
  'openai-compatible': 'OpenAI 兼容(/chat/completions)',
  gemini: 'Gemini',
  anthropic: 'Anthropic',
  xai: 'xAI Grok(/responses)',
}

/** 能力标签:文字比图标好认 —— 图标得先学一遍才知道哪个是"工具" */
const CAP_LABEL: Record<Capability, { text: string; cls: string }> = {
  vision: { text: '视觉', cls: 'bg-success/15 text-success' },
  pdf: { text: '文档', cls: 'bg-muted-foreground/15 text-muted-foreground' },
  reasoning: { text: '思考', cls: 'bg-violet-500/15 text-violet-500' },
  webSearch: { text: '联网', cls: 'bg-info/15 text-info' },
  imageGen: { text: '绘图', cls: 'bg-pink-500/15 text-pink-500' },
  tools: { text: '工具', cls: 'bg-amber-500/15 text-amber-600 dark:text-amber-400' },
}

const CAP_ORDER: Capability[] = ['vision', 'reasoning', 'tools', 'webSearch', 'imageGen', 'pdf']

function effectiveEndpoint(type: ProviderType, baseUrl: string): string {
  const url = baseUrl || DEFAULT_BASE_URL_BY_TYPE[type]
  switch (type) {
    case 'gemini':
      return `${url}/v1beta/models/{model}:streamGenerateContent`
    case 'anthropic':
      return `${url}/v1/messages`
    case 'openai-compatible':
      return `${url}/chat/completions`
    case 'openai':
    case 'xai':
    default:
      return `${url}/responses`
  }
}

/**
 * 模型归到哪一组。
 *
 * 先看 `/`、空格、冒号 —— 有这些的话前半截就是厂商/系列(`deepseek-ai/DeepSeek-V3`)。
 * 都没有再按 `-`/`_` 取前两段:`grok-4.20-fast` → `grok-4.20`、`mimo-v2-omni` → `mimo-v2`。
 * 取两段而不是一段,是因为只取一段会把 gpt-5.6 和 gpt-4o 混成一堆 "gpt"。
 */
function modelGroup(id: string): string {
  const s = id.toLowerCase()
  for (const d of ['/', ' ', ':']) {
    if (s.includes(d)) return s.split(d)[0]
  }
  for (const d of ['-', '_']) {
    if (s.includes(d)) {
      const parts = s.split(d)
      return parts.length > 1 ? parts[0] + d + parts[1] : parts[0]
    }
  }
  return s
}

export function ProviderDetail({
  provider,
  onSaved,
  onToggle,
  onDelete,
  onTest,
  onManage,
}: {
  provider: Provider
  onSaved: () => void
  onToggle: (next: boolean) => void
  onDelete: () => void
  onTest: () => void
  onManage: () => void
}) {
  const dialog = useConfirm()
  const [name, setName] = useState(provider.name)
  const [baseUrl, setBaseUrl] = useState(provider.baseUrl)
  const [saving, setSaving] = useState(false)
  const [savedFlash, setSavedFlash] = useState(false)
  const [specs, setSpecs] = useState<Record<string, ModelSpec>>({})
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())

  useEffect(() => {
    setName(provider.name)
    setBaseUrl(provider.baseUrl)
    setCollapsed(new Set())
  }, [provider.id])

  // 能力画像一次批量取回:逐个模型问一遍就是十几次 IPC 往返
  useEffect(() => {
    let alive = true
    void (async () => {
      const list = await ListAIModelSpecs(provider.id)
      if (!alive) return
      const arr = Array.isArray(list) ? (list as unknown as ModelSpec[]) : []
      setSpecs(Object.fromEntries(arr.map((s) => [s.id, s])))
    })()
    return () => {
      alive = false
    }
  }, [provider.id, provider.models.join(',')])

  const groups = useMemo(() => {
    const m = new Map<string, string[]>()
    for (const id of provider.models) {
      const g = modelGroup(id)
      const list = m.get(g)
      if (list) list.push(id)
      else m.set(g, [id])
    }
    return [...m.entries()]
  }, [provider.models])

  const dirty = name !== provider.name || baseUrl !== provider.baseUrl

  const onSave = async () => {
    setSaving(true)
    try {
      // 出错时 Wails 会 reject,由下面的 catch 接住 —— 以前那个 err 字符串
      // 根本到不了前端(见 app.go SaveAIProvider 的注释)
      await SaveAIProvider({
        ...provider,
        name: name.trim() || '未命名',
        baseUrl: baseUrl.trim() || DEFAULT_BASE_URL_BY_TYPE[provider.type],
      } as unknown as never)
      setSavedFlash(true)
      setTimeout(() => setSavedFlash(false), 1500)
      onSaved()
    } catch (e) {
      await dialog({ title: '保存失败', message: String(e), confirmLabel: '知道了' })
    } finally {
      setSaving(false)
    }
  }

  // 移除模型不弹确认:它只是把模型从选中列表里拿掉,再点「获取模型列表」就能加回来 ——
  // 为一个可逆操作拦一道确认,只是让常规整理变得很烦
  const onRemoveModel = async (modelID: string) => {
    try {
      await SaveAIProvider({
        ...provider,
        models: provider.models.filter((m) => m !== modelID),
      } as unknown as never)
    } catch (e) {
      await dialog({ title: '移除失败', message: String(e), confirmLabel: '知道了' })
    }
    onSaved()
  }

  const toggleGroup = (g: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (next.has(g)) next.delete(g)
      else next.add(g)
      return next
    })

  return (
    <div className="space-y-5 p-6">
      <div className="flex items-center gap-3">
        <ProviderAvatar logo={provider.logo} name={provider.name} size={40} />
        <div className="min-w-0 flex-1">
          <h3 className="truncate text-lg font-semibold">{provider.name}</h3>
          <p className="text-[11px] text-muted-foreground">
            {TYPE_LABEL[provider.type]}
            {provider.isSystem && ' · 系统预设'}
          </p>
        </div>
        <ToggleSwitch checked={provider.enabled} onChange={onToggle} />
        <Button variant="ghost" size="sm" onClick={onDelete} title="删除供应商">
          <Trash2 className="h-3.5 w-3.5" />
          删除
        </Button>
      </div>

      <ApiKeyField provider={provider} onChanged={onSaved} onTest={onTest} />

      <div className="space-y-1.5">
        <label className="text-xs font-medium text-muted-foreground">API 地址</label>
        <input
          value={baseUrl}
          onChange={(e) => setBaseUrl(e.target.value)}
          placeholder={DEFAULT_BASE_URL_BY_TYPE[provider.type]}
          className="h-9 w-full rounded-md border border-input bg-background px-3 font-mono text-sm outline-none focus:ring-1 focus:ring-ring"
        />
        <p className="text-[11px] text-muted-foreground">
          {effectiveEndpoint(provider.type, baseUrl)} · 留空走默认
        </p>
      </div>

      <div className="space-y-1.5">
        <label className="text-xs font-medium text-muted-foreground">名称</label>
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring"
        />
      </div>

      {dirty && (
        <div className="flex items-center gap-2">
          <Button onClick={onSave} disabled={saving} size="sm">
            <Save className="h-3.5 w-3.5" />
            {saving ? '保存中...' : '保存修改'}
          </Button>
          {savedFlash && <span className="text-xs text-success">已保存</span>}
        </div>
      )}
      {!dirty && savedFlash && <div className="text-xs text-success">已保存</div>}

      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <label className="text-xs font-medium text-muted-foreground">
            模型({provider.models.length})
          </label>
          <Button variant="outline" size="sm" onClick={onManage}>
            <RefreshCw className="h-3.5 w-3.5" />
            获取模型列表
          </Button>
        </div>
        {provider.models.length === 0 ? (
          <div className="rounded-md border border-dashed border-border p-6 text-center text-xs text-muted-foreground">
            还没选模型,点「获取模型列表」从 /v1/models 拉取并选入
          </div>
        ) : (
          <div className="space-y-2">
            {groups.map(([group, ids]) => {
              const open = !collapsed.has(group)
              return (
                <div key={group} className="overflow-hidden rounded-lg border border-border">
                  <button
                    type="button"
                    onClick={() => toggleGroup(group)}
                    className="flex w-full items-center gap-2 bg-secondary/40 px-3 py-1.5 text-left text-xs font-medium transition-colors hover:bg-secondary/60"
                  >
                    <ChevronDown
                      className={cn('h-3.5 w-3.5 transition-transform', !open && '-rotate-90')}
                    />
                    <span className="truncate">{group}</span>
                    <span className="ml-auto text-[10px] font-normal text-muted-foreground">
                      {ids.length}
                    </span>
                  </button>
                  {open && (
                    <ul className="divide-y divide-border/60">
                      {ids.map((m) => (
                        <li
                          key={m}
                          className="group/model flex items-center gap-2 bg-card px-3 py-2"
                        >
                          <span className="min-w-0 flex-1 truncate font-mono text-xs">{m}</span>
                          <CapabilityChips caps={specs[m]?.capabilities} />
                          <button
                            type="button"
                            onClick={() => void onRemoveModel(m)}
                            title="从列表中移除"
                            className="flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground opacity-0 transition-all hover:bg-destructive/10 hover:text-destructive group-hover/model:opacity-100"
                          >
                            <Minus className="h-3.5 w-3.5" />
                          </button>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}

function CapabilityChips({ caps }: { caps?: Capability[] }) {
  if (!caps || caps.length === 0) return null
  const shown = CAP_ORDER.filter((c) => caps.includes(c))
  if (shown.length === 0) return null
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-1">
      {shown.map((c) => (
        <span
          key={c}
          className={cn('rounded px-1.5 py-0.5 text-[10px] font-medium', CAP_LABEL[c].cls)}
        >
          {CAP_LABEL[c].text}
        </span>
      ))}
    </div>
  )
}

function ToggleSwitch({
  checked,
  onChange,
}: {
  checked: boolean
  onChange: (v: boolean) => void
}) {
  return (
    <button
      onClick={() => onChange(!checked)}
      role="switch"
      aria-checked={checked}
      className={
        'relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full p-0.5 transition-colors ' +
        (checked ? 'bg-success' : 'bg-secondary')
      }
    >
      <span
        className={
          'inline-block h-5 w-5 rounded-full bg-white shadow-sm transition-transform ' +
          (checked ? 'translate-x-[20px]' : 'translate-x-0')
        }
      />
    </button>
  )
}
