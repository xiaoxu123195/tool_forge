import { useState } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import type { ModelSpec } from './types'

/** 用于"新建会话"和"编辑会话"两个场景 */
export interface ConversationDraft {
  title: string
  system: string
  /** 0 = 不限 */
  contextCount: number
  /** undefined = 不指定,由模型自己决定(0 是合法取值,所以不能用 0 当"未设置") */
  temperature?: number
  topP?: number
  /** 0 = 不指定 */
  maxTokens?: number
}

const PRESET_COUNTS: { label: string; value: number }[] = [
  { label: '5 条', value: 5 },
  { label: '10 条', value: 10 },
  { label: '20 条', value: 20 },
  { label: '40 条', value: 40 },
  { label: '不限', value: 0 },
]

export function ConversationDialog({
  mode,
  initial,
  spec,
  onClose,
  onSave,
}: {
  mode: 'create' | 'edit'
  initial: ConversationDraft
  /** 当前模型的能力画像;新建会话时还没有模型,传 undefined 即可(此时不展示采样区) */
  spec?: ModelSpec | null
  onClose: () => void
  onSave: (draft: ConversationDraft) => void
}) {
  const [title, setTitle] = useState(initial.title)
  const [system, setSystem] = useState(initial.system)
  const [contextCount, setContextCount] = useState(initial.contextCount)
  const [temperature, setTemperature] = useState<number | undefined>(initial.temperature)
  const [topP, setTopP] = useState<number | undefined>(initial.topP)
  const [maxTokens, setMaxTokens] = useState(initial.maxTokens ?? 0)

  // 采样参数得模型接受才有意义:o 系列 / gpt-5 / Kimi K2.5+ 完全不收,发过去会报错。
  // 拿不到 spec 时(新建会话还没选模型)整块不展示,免得设了个不知道生不生效的值。
  const canTemp = !!spec?.sampling?.temperature
  const canTopP = !!spec?.sampling?.topP
  const maxTemp = spec?.sampling?.maxTemp || 2
  const showSampling = !!spec && (canTemp || canTopP)

  const submit = () => {
    onSave({
      title: title.trim(),
      system: system.trim(),
      contextCount,
      temperature: canTemp ? temperature : initial.temperature,
      topP: canTopP ? topP : initial.topP,
      maxTokens,
    })
  }

  return createPortal(
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center bg-black/40 p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div className="flex max-h-[80vh] w-[640px] max-w-full flex-col overflow-hidden rounded-xl border border-border bg-card shadow-2xl">
        <header className="flex h-12 shrink-0 items-center justify-between border-b border-border px-4">
          <h3 className="text-sm font-semibold">
            {mode === 'create' ? '新建会话' : '编辑会话'}
          </h3>
          <button
            onClick={onClose}
            className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </header>

        <div className="min-h-0 flex-1 space-y-4 overflow-auto p-4">
          <div className="space-y-1.5">
            <label className="text-xs font-medium">会话名称</label>
            <input
              autoFocus={mode === 'create'}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={mode === 'create' ? '留空则用首条消息生成' : ''}
              className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring"
            />
          </div>

          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium">系统提示词</label>
              <span className="text-[11px] text-muted-foreground">{system.length} 字符</span>
            </div>
            <textarea
              value={system}
              onChange={(e) => setSystem(e.target.value)}
              placeholder="例如:你是一个简洁、严谨的中文编程助手,只返回必要的代码,不要寒暄。"
              rows={6}
              className="w-full resize-none rounded-md border border-input bg-background px-3 py-2 text-sm leading-relaxed outline-none focus:ring-1 focus:ring-ring"
            />
            <p className="text-[11px] text-muted-foreground">作为 system 角色注入到每次请求最前;留空则不发送。</p>
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium">上下文条数</label>
            <div className="flex flex-wrap items-center gap-2">
              {PRESET_COUNTS.map((p) => (
                <button
                  key={p.value}
                  type="button"
                  onClick={() => setContextCount(p.value)}
                  className={
                    contextCount === p.value
                      ? 'h-7 rounded-md border border-info/50 bg-info/10 px-3 text-xs font-medium text-info'
                      : 'h-7 rounded-md border border-input bg-background px-3 text-xs text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground'
                  }
                >
                  {p.label}
                </button>
              ))}
              <input
                type="number"
                min={0}
                max={200}
                value={contextCount}
                onChange={(e) => {
                  const v = Number(e.target.value)
                  if (Number.isFinite(v) && v >= 0) setContextCount(v)
                }}
                className="h-7 w-20 rounded-md border border-input bg-background px-2 text-xs outline-none focus:ring-1 focus:ring-ring"
                title="自定义条数(0 = 不限)"
              />
            </div>
            <p className="text-[11px] text-muted-foreground">
              发给模型时只保留最近 N 条消息;0 表示不限。值越大,token 消耗越高。
            </p>
          </div>

          {showSampling && (
            <div className="space-y-3 rounded-md border border-border bg-secondary/20 p-3">
              <div className="text-xs font-medium">采样参数</div>

              {canTemp && (
                <SliderRow
                  label="Temperature"
                  hint={`0 = 每次都一样,${maxTemp} = 最发散。不设则用模型自己的默认值。`}
                  value={temperature}
                  min={0}
                  max={maxTemp}
                  step={0.1}
                  onChange={setTemperature}
                />
              )}

              {canTopP && (
                <SliderRow
                  label="Top P"
                  hint="按累积概率截断候选词。一般和 Temperature 只调一个。"
                  value={topP}
                  min={0}
                  max={1}
                  step={0.05}
                  onChange={setTopP}
                />
              )}

              <div className="space-y-1.5">
                <label className="text-xs font-medium">回复长度上限</label>
                <div className="flex items-center gap-2">
                  <input
                    type="number"
                    min={0}
                    max={spec?.maxOutput ?? 0}
                    value={maxTokens || ''}
                    onChange={(e) => {
                      const v = Number(e.target.value)
                      setMaxTokens(Number.isFinite(v) && v > 0 ? Math.floor(v) : 0)
                    }}
                    placeholder="不限"
                    className="h-7 w-28 rounded-md border border-input bg-background px-2 text-xs outline-none focus:ring-1 focus:ring-ring"
                  />
                  <span className="text-[11px] text-muted-foreground">
                    token · 模型上限 {spec?.maxOutput ?? '未知'}
                  </span>
                </div>
                <p className="text-[11px] text-muted-foreground">
                  留空表示不指定,由模型自己决定;设小了长回答会被截断。
                </p>
              </div>
            </div>
          )}
        </div>

        <footer className="flex h-12 shrink-0 items-center justify-end gap-2 border-t border-border bg-secondary/30 px-3 text-xs">
          <button
            type="button"
            onClick={onClose}
            className="h-7 rounded-md px-3 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          >
            取消
          </button>
          <button
            type="button"
            onClick={submit}
            className="h-7 rounded-md bg-info px-3 font-medium text-info-foreground transition-colors hover:bg-info/90"
          >
            {mode === 'create' ? '创建' : '保存'}
          </button>
        </footer>
      </div>
    </div>,
    document.body,
  )
}

/**
 * 一个"可以不设置"的滑块。
 *
 * 采样参数的 0 是合法取值(完全确定性输出),所以"没设置"不能用 0 表示 ——
 * 用一个显式开关区分:关掉就是不发这个字段,让模型用自己的默认值。
 */
function SliderRow({
  label,
  hint,
  value,
  min,
  max,
  step,
  onChange,
}: {
  label: string
  hint: string
  value: number | undefined
  min: number
  max: number
  step: number
  onChange: (v: number | undefined) => void
}) {
  const enabled = value !== undefined
  const shown = value ?? (min + max) / 2

  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between">
        <label className="flex items-center gap-2 text-xs font-medium">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => onChange(e.target.checked ? shown : undefined)}
            className="h-3.5 w-3.5 accent-[hsl(var(--info))]"
          />
          {label}
        </label>
        <span className="font-mono text-[11px] text-muted-foreground">
          {enabled ? shown.toFixed(2) : '默认'}
        </span>
      </div>
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={shown}
        disabled={!enabled}
        onChange={(e) => onChange(Number(e.target.value))}
        className="w-full accent-[hsl(var(--info))] disabled:opacity-40"
      />
      <p className="text-[11px] text-muted-foreground">{hint}</p>
    </div>
  )
}
