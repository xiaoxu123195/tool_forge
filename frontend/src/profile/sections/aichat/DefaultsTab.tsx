import { useEffect, useState } from 'react'
import { MessagesSquare, Save, Sparkles } from 'lucide-react'
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
  // 自动起标题。后端存的是"关"(老配置没有这个字段 → 零值 → 开着),
  // 这里翻成正向的"开"再给界面用,免得整个组件里到处都是双重否定
  const [autoTitle, setAutoTitle] = useState(true)
  const [titleProviderId, setTitleProviderId] = useState('')
  const [titleModelId, setTitleModelId] = useState('')
  const [savedFlash, setSavedFlash] = useState(false)

  useEffect(() => {
    void (async () => {
      const list = ((await ListAIProviders()) ?? []) as unknown as Provider[]
      setProviders(list)
      const cfg = (await GetAIConfig()) as unknown as AIConfig
      setProviderId(cfg.defaultProviderId ?? '')
      setModelId(cfg.defaultModelId ?? '')
      setAutoTitle(!cfg.autoTitleOff)
      setTitleProviderId(cfg.titleProviderId ?? '')
      setTitleModelId(cfg.titleModelId ?? '')
    })()
  }, [])

  const enabled = providers.filter((p) => p.enabled && p.models.length > 0)
  const currentProvider = enabled.find((p) => p.id === providerId)
  const modelOptions = currentProvider?.models ?? []
  const titleProvider = enabled.find((p) => p.id === titleProviderId)

  const onSave = async () => {
    const err = (await SaveAIConfig({
      defaultProviderId: providerId,
      defaultModelId: modelId,
      autoTitleOff: !autoTitle,
      // 只选了供应商没选模型等于没配。半套配置存下去,后端每次都要判一遍
      // "两个字段是不是都在" —— 干脆在这里就不让它成形
      titleProviderId: titleModelId ? titleProviderId : '',
      titleModelId: titleProviderId ? titleModelId : '',
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

      <div className="rounded-lg border border-border bg-card p-5">
        <div className="mb-3 flex items-center gap-2 text-sm font-medium">
          <Sparkles className="h-4 w-4 text-info" />
          自动起标题
        </div>
        <label className="flex cursor-pointer items-start gap-2">
          <input
            type="checkbox"
            checked={autoTitle}
            onChange={(e) => setAutoTitle(e.target.checked)}
            className="mt-0.5 h-3.5 w-3.5 shrink-0"
          />
          <span className="text-xs">
            首轮问答结束后,让模型给会话起个标题
            <span className="mt-0.5 block text-[11px] text-muted-foreground">
              只在第一轮之后起一次;你手动改过名字的会话不会被覆盖
            </span>
          </span>
        </label>

        {autoTitle && (
          <div className="mt-4 space-y-2">
            <div className="text-xs font-medium text-muted-foreground">
              专用模型
              <span className="ml-2 font-normal">· 留空就用会话自己的模型</span>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <select
                value={titleProviderId}
                onChange={(e) => {
                  setTitleProviderId(e.target.value)
                  setTitleModelId('')
                }}
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring"
              >
                <option value="">— 跟随会话 —</option>
                {enabled.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
              <select
                value={titleModelId}
                onChange={(e) => setTitleModelId(e.target.value)}
                disabled={!titleProvider}
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring disabled:opacity-50"
              >
                <option value="">— 未选择 —</option>
                {(titleProvider?.models ?? []).map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </select>
            </div>
            <p className="text-[11px] text-muted-foreground">
              起标题是一次额外的请求。挑个便宜的小模型专门干这件事,
              比让正在用的大模型顺手起要省得多。
            </p>
          </div>
        )}
      </div>
    </div>
  )
}
