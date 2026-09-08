import { useEffect, useState } from 'react'
import { MessagesSquare, Save } from 'lucide-react'
import {
  ListAIProviders,
  GetAIConfig,
  SaveAIConfig,
} from '../../../../wailsjs/go/main/App'
import type { Provider, AIConfig } from '@/tools/ai-chat/types'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'

export function DefaultsTab() {
  const dialog = useConfirm()
  const [providers, setProviders] = useState<Provider[]>([])
  const [providerId, setProviderId] = useState('')
  const [modelId, setModelId] = useState('')
  const [savedFlash, setSavedFlash] = useState(false)

  useEffect(() => {
    void (async () => {
      const list = ((await ListAIProviders()) ?? []) as unknown as Provider[]
      setProviders(list)
      const cfg = (await GetAIConfig()) as unknown as AIConfig
      setProviderId(cfg.defaultProviderId ?? '')
      setModelId(cfg.defaultModelId ?? '')
    })()
  }, [])

  const enabled = providers.filter((p) => p.enabled && p.models.length > 0)
  const currentProvider = enabled.find((p) => p.id === providerId)
  const modelOptions = currentProvider?.models ?? []

  const onSave = async () => {
    const err = (await SaveAIConfig({
      defaultProviderId: providerId,
      defaultModelId: modelId,
    } as unknown as never)) as unknown as string
    if (err) {
      await dialog({ title: '保存失败', message: err, confirmLabel: '知道了' })
      return
    }
    setSavedFlash(true)
    setTimeout(() => setSavedFlash(false), 1500)
  }

  return (
    <div className="max-w-2xl space-y-4">
      <div className="rounded-lg border border-border bg-card p-5">
        <div className="mb-3 flex items-center gap-2 text-sm font-medium">
          <MessagesSquare className="h-4 w-4 text-info" />
          默认助手模型
        </div>
        <p className="mb-4 text-xs text-muted-foreground">
          新建对话时使用的模型,可在对话顶部随时切换
        </p>

        {enabled.length === 0 ? (
          <div className="rounded-md border border-dashed border-border bg-secondary/30 p-6 text-center text-xs text-muted-foreground">
            还没有启用且选了模型的供应商,请先到「模型服务」配置
          </div>
        ) : (
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-muted-foreground">供应商</label>
              <select
                value={providerId}
                onChange={(e) => {
                  setProviderId(e.target.value)
                  const next = enabled.find((p) => p.id === e.target.value)
                  setModelId(next?.models[0] ?? '')
                }}
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring"
              >
                <option value="">— 未选择 —</option>
                {enabled.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>

            <div className="space-y-1.5">
              <label className="text-xs font-medium text-muted-foreground">模型</label>
              <select
                value={modelId}
                onChange={(e) => setModelId(e.target.value)}
                disabled={!currentProvider}
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring disabled:opacity-50"
              >
                <option value="">— 未选择 —</option>
                {modelOptions.map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </select>
            </div>
          </div>
        )}

        <div className="mt-4 flex items-center gap-2">
          <Button onClick={onSave} disabled={enabled.length === 0} size="sm">
            <Save className="h-3.5 w-3.5" />
            保存
          </Button>
          {savedFlash && <span className="text-xs text-success">已保存</span>}
        </div>
      </div>
    </div>
  )
}
