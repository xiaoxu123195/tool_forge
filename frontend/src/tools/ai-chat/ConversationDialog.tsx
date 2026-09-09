import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { Eye, Pencil, X } from 'lucide-react'
import { ListAIAssistants } from '../../../wailsjs/go/main/App'
import { MarkdownPreview } from '@/components/tool/MarkdownPreview'
import { useConfirm } from '@/components/ui/confirm'
import { cn } from '@/lib/utils'
import { EFFORT_LABELS, type Assistant, type ModelSpec, type ReasoningEffort } from './types'

/**
 * 「编辑会话」一次提交的内容。
 *
 * 以前还兼着"新建会话"。现在点新建就直接开一个空的「新对话」——
 * 想设人设、改参数再进这个弹窗,所以这里只剩编辑一种用法。
 */
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
  /**
   * 套了预设时带上它的三个开关。没套预设是 undefined ——
   * 调用方据此区分"预设要求关掉联网"和"这次压根没碰过联网",
   * 用 false 表示两者的话,进来编辑一下提示词就会把用户开着的联网顺手关了。
   */
  preset?: { reasoningEffort: string; webSearch: boolean; tools: boolean }
}

const PRESET_COUNTS: { label: string; value: number }[] = [
  { label: '5 条', value: 5 },
  { label: '10 条', value: 10 },
  { label: '20 条', value: 20 },
  { label: '40 条', value: 40 },
  { label: '不限', value: 0 },
]

export function ConversationDialog({
  initial,
  spec,
  onClose,
  onSave,
}: {
  initial: ConversationDraft
  /** 当前模型的能力画像;拿不到时不展示采样区(免得设一个不知道生不生效的值) */
  spec?: ModelSpec | null
  onClose: () => void
  onSave: (draft: ConversationDraft) => void
}) {
  const confirm = useConfirm()
  const [title, setTitle] = useState(initial.title)
  const [system, setSystem] = useState(initial.system)
  const [systemView, setSystemView] = useState<'edit' | 'preview'>('edit')
  const [contextCount, setContextCount] = useState(initial.contextCount)
  const [temperature, setTemperature] = useState<number | undefined>(initial.temperature)
  const [topP, setTopP] = useState<number | undefined>(initial.topP)
  const [maxTokens, setMaxTokens] = useState(initial.maxTokens ?? 0)
  const [assistants, setAssistants] = useState<Assistant[]>([])
  const [pickedId, setPickedId] = useState('')

  useEffect(() => {
    void (async () => {
      const l = ((await ListAIAssistants().catch(() => [])) ?? []) as unknown as Assistant[]
      setAssistants(l)
    })()
  }, [])

  // 预设里的思考档位 / 联网 / 工具。它们不在这个弹窗里显示成控件(本体是输入栏
  // 上的即时开关),但要捎给保存流程,并用一行字说明 —— 免得"选了却不知道生效了什么"
  const [preset, setPreset] = useState<ConversationDraft['preset']>()

  /**
   * 套一个预设。
   *
   * 已经写了提示词的会话要先问一句:预设是整段替换,不是追加。以前这个弹窗
   * 干脆不给编辑态看预设,就是怕这一下冲掉用户写的东西 —— 现在新建走的是
   * "直接开一个空会话",预设只剩这一个入口,不能再藏着了,改成问一句。
   */
  const applyAssistant = async (a: Assistant | null) => {
    if (!a) {
      setPickedId('')
      setPreset(undefined)
      return
    }
    if (system.trim() && system.trim() !== a.system.trim()) {
      const ok = await confirm({
        title: '替换系统提示词',
        message: `当前会话已经有提示词了,套用「${a.name}」会把它整段换掉。`,
        danger: true,
        confirmLabel: '替换',
      })
      if (!ok) return
    }
    setPickedId(a.id)
    setSystem(a.system)
    // 不拿预设名当会话标题:标题留给首轮问答后自动生成,
    // 在这里填上等于提前把它锁死(改过标题的会话不再自动起名)
    if (a.contextCount) setContextCount(a.contextCount)
    if (a.temperature !== undefined) setTemperature(a.temperature)
    if (a.topP !== undefined) setTopP(a.topP)
    if (a.maxTokens) setMaxTokens(a.maxTokens)
    setPreset({
      reasoningEffort: a.reasoningEffort || '',
      webSearch: !!a.webSearch,
      tools: !!a.tools,
    })
  }

  const effortLabel = preset?.reasoningEffort
    ? (EFFORT_LABELS[preset.reasoningEffort as ReasoningEffort] ?? preset.reasoningEffort)
    : ''
  const extraSummary = !preset
    ? ''
    : [
        effortLabel ? '思考档位设为「' + effortLabel + '」' : '',
        preset.webSearch ? '开启联网' : '',
        preset.tools ? '开启工具' : '',
      ]
        .filter(Boolean)
        .join(' · ')

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
      // 模型不收的采样参数原样退回初值 —— 界面上没给控件,就不该悄悄改掉它
      temperature: canTemp ? temperature : initial.temperature,
      topP: canTopP ? topP : initial.topP,
      maxTokens,
      preset,
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
          <h3 className="text-sm font-semibold">会话设置</h3>
          <button
            onClick={onClose}
            className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </header>

        <div className="min-h-0 flex-1 space-y-4 overflow-auto p-4">
          {assistants.length > 0 && (
            <div className="space-y-1.5">
              <label className="text-xs font-medium">
                助手预设
                <span className="ml-2 font-normal text-muted-foreground">
                  · 选一个套用它的提示词和参数,下面还能继续改
                </span>
              </label>
              <div className="flex flex-wrap gap-1.5">
                <button
                  type="button"
                  onClick={() => void applyAssistant(null)}
                  className={cn(
                    'h-7 rounded-md border px-2.5 text-xs transition-colors',
                    pickedId === ''
                      ? 'border-info/50 bg-info/10 text-info'
                      : 'border-border hover:bg-secondary',
                  )}
                >
                  不使用
                </button>
                {assistants.map((a) => (
                  <button
                    key={a.id}
                    type="button"
                    onClick={() => void applyAssistant(a)}
                    title={a.system.slice(0, 200)}
                    className={cn(
                      'flex h-7 items-center gap-1 rounded-md border px-2.5 text-xs transition-colors',
                      pickedId === a.id
                        ? 'border-info/50 bg-info/10 text-info'
                        : 'border-border hover:bg-secondary',
                    )}
                  >
                    <span className="text-sm leading-none">{a.emoji || '🤖'}</span>
                    {a.name}
                  </button>
                ))}
              </div>
              {extraSummary && (
                <p className="text-[11px] text-info">该预设还会:{extraSummary}</p>
              )}
            </div>
          )}

          <div className="space-y-1.5">
            <label className="text-xs font-medium">会话名称</label>
            <input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="留空则由模型在首轮问答后自动生成"
              className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring"
            />
          </div>

          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium">系统提示词</label>
              <div className="flex items-center gap-2">
                <span className="text-[11px] text-muted-foreground">{system.length} 字符</span>
                {/* 提示词长了以后基本都是 Markdown(标题分段、列表、代码块),
                    纯文本框里看是一坨;给个预览开关,写和读各用各的视图 */}
                <div className="flex overflow-hidden rounded-md border border-border text-[11px]">
                  {(['edit', 'preview'] as const).map((m) => (
                    <button
                      key={m}
                      type="button"
                      onClick={() => setSystemView(m)}
                      className={cn(
                        'flex items-center gap-1 px-2 py-0.5 transition-colors',
                        systemView === m
                          ? 'bg-info/15 text-info'
                          : 'text-muted-foreground hover:bg-secondary',
                      )}
                    >
                      {m === 'edit' ? <Pencil className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                      {m === 'edit' ? '编辑' : '预览'}
                    </button>
                  ))}
                </div>
              </div>
            </div>
            {systemView === 'edit' ? (
              <textarea
                value={system}
                onChange={(e) => setSystem(e.target.value)}
                placeholder="例如:你是一个简洁、严谨的中文编程助手,只返回必要的代码,不要寒暄。支持 Markdown。"
                rows={8}
                className="w-full resize-none rounded-md border border-input bg-background px-3 py-2 font-mono text-sm leading-relaxed outline-none focus:ring-1 focus:ring-ring"
              />
            ) : (
              <div
                onDoubleClick={() => setSystemView('edit')}
                title="双击回到编辑"
                className="min-h-[168px] w-full overflow-auto rounded-md border border-input bg-secondary/20 px-3 py-2"
              >
                {system.trim() ? (
                  <MarkdownPreview value={system} className="markdown-preview text-sm" />
                ) : (
                  <span className="text-sm text-muted-foreground">(空)</span>
                )}
              </div>
            )}
            <p className="text-[11px] text-muted-foreground">
              作为 system 角色注入到每次请求最前;留空则不发送。原文按 Markdown 原样发给模型,
              预览只影响这里怎么显示。
            </p>
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
            保存
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
