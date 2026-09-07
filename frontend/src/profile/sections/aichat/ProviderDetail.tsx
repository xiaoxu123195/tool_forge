import { useEffect, useState } from 'react'
import { Eye, EyeOff, Trash2, FlaskConical, Settings2, Save, Minus } from 'lucide-react'
import { SaveAIProvider } from '../../../../wailsjs/go/main/App'
import type { Provider, ProviderType } from '@/tools/ai-chat/types'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
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
  const [apiKey, setApiKey] = useState(provider.apiKey)
  const [showKey, setShowKey] = useState(false)
  const [saving, setSaving] = useState(false)
  const [savedFlash, setSavedFlash] = useState(false)

  useEffect(() => {
    setName(provider.name)
    setBaseUrl(provider.baseUrl)
    setApiKey(provider.apiKey)
  }, [provider.id])

  const dirty =
    name !== provider.name ||
    baseUrl !== provider.baseUrl ||
    apiKey !== provider.apiKey

  const onSave = async () => {
    setSaving(true)
    try {
      const r = (await SaveAIProvider({
        ...provider,
        name: name.trim() || '未命名',
        baseUrl: baseUrl.trim() || DEFAULT_BASE_URL_BY_TYPE[provider.type],
        apiKey: apiKey.trim(),
      } as unknown as never)) as any
      const err = (r?.[1] as string) || ''
      if (err) {
        await dialog({ title: '保存失败', message: err, confirmLabel: '知道了' })
        return
      }
      setSavedFlash(true)
      setTimeout(() => setSavedFlash(false), 1500)
      onSaved()
    } finally {
      setSaving(false)
    }
  }

  const onRemoveModel = async (modelID: string) => {
    const ok = await dialog({
      title: '移除模型',
      message: `从供应商「${provider.name}」中移除模型 ${modelID}?`,
      confirmLabel: '移除',
    })
    if (!ok) return
    const r = (await SaveAIProvider({
      ...provider,
      models: provider.models.filter((m) => m !== modelID),
    } as unknown as never)) as any
    const err = (r?.[1] as string) || ''
    if (err) {
      await dialog({ title: '移除失败', message: err, confirmLabel: '知道了' })
      return
    }
    onSaved()
  }

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

      <div className="space-y-1.5">
        <label className="text-xs font-medium text-muted-foreground">API 密钥</label>
        <div className="flex gap-2">
          <div className="relative flex-1">
            <input
              type={showKey ? 'text' : 'password'}
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder="sk-..."
              className="h-9 w-full rounded-md border border-input bg-background pl-3 pr-9 font-mono text-sm outline-none focus:ring-1 focus:ring-ring"
            />
            <button
              type="button"
              onClick={() => setShowKey((v) => !v)}
              className="absolute right-1 top-1/2 flex h-7 w-7 -translate-y-1/2 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              title={showKey ? '隐藏' : '显示'}
            >
              {showKey ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
            </button>
          </div>
          <Button variant="outline" size="sm" onClick={onTest}>
            <FlaskConical className="h-3.5 w-3.5" />
            检测
          </Button>
        </div>
      </div>

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
            <Settings2 className="h-3.5 w-3.5" />
            管理
          </Button>
        </div>
        {provider.models.length === 0 ? (
          <div className="rounded-md border border-dashed border-border p-6 text-center text-xs text-muted-foreground">
            还没选模型,点击「管理」从 /v1/models 拉取并选入
          </div>
        ) : (
          <ul className="space-y-1.5">
            {provider.models.map((m) => (
              <li
                key={m}
                className="group/model flex items-center gap-2 rounded-md border border-border bg-card px-3 py-2"
              >
                <div className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-info/15 text-[10px] font-semibold text-info">
                  {m.slice(0, 1).toUpperCase()}
                </div>
                <span className="flex-1 truncate font-mono text-xs">{m}</span>
                <button
                  type="button"
                  onClick={() => onRemoveModel(m)}
                  title="移除该模型"
                  className="flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground opacity-0 transition-all hover:bg-destructive/10 hover:text-destructive group-hover/model:opacity-100"
                >
                  <Minus className="h-3.5 w-3.5" />
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
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
