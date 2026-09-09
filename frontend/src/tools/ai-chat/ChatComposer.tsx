import { useEffect, useRef, useState } from 'react'
import {
  Send,
  Square,
  Sparkles,
  ChevronDown,
  Brain,
  Eraser,
  Paperclip,
  Globe,
  Wrench,
  X,
} from 'lucide-react'
import { ListAIChatTools } from '../../../wailsjs/go/main/App'
import { formatFileSize, imageSrc, MAX_FILES_PER_MESSAGE } from './file-parsers'
import { Button } from '@/components/ui/button'
import { ProviderAvatar } from './ProviderAvatar'
import { CHAT_COLUMN, fileIcon } from './chat-utils'
import type { useAttachments } from './useAttachments'
import {
  EFFORT_LABELS,
  type FileBlock,
  type ImageBlock,
  type ModelSpec,
  type Provider,
  type ReasoningEffort,
  type ChatToolsView,
} from './types'
import { cn } from '@/lib/utils'

/**
 * 输入栏:草稿框 + 附件预览条 + 工具栏(模型选择 / 附件 / 思考档位 / 联网)+ 发送。
 *
 * 纯展示 + 事件上抛,自己只留一个"档位弹层开着没有"的状态。
 * 附件那一摊直接收 useAttachments 的返回值,免得把六七个字段拆开一个个传。
 */
export function ChatComposer({
  draft,
  setDraft,
  streaming,
  isEmpty,
  attach,
  spec,
  provider,
  modelId,
  currentEffort,
  effortOptions,
  canTuneReasoning,
  canWebSearch,
  canUseTools,
  webSearch,
  toolsOn,
  textareaRef,
  onSend,
  onStop,
  onClearContext,
  onOpenPicker,
  onSetOptions,
  onPreviewImage,
  onPreviewFile,
}: {
  draft: string
  setDraft: (v: string) => void
  streaming: boolean
  /** 会话里一条消息都没有:此时不显示"清除上下文" */
  isEmpty: boolean
  attach: ReturnType<typeof useAttachments>
  spec: ModelSpec | null
  provider: Provider | null
  modelId: string
  currentEffort: ReasoningEffort
  effortOptions: ReasoningEffort[]
  canTuneReasoning: boolean
  canWebSearch: boolean
  canUseTools: boolean
  webSearch: boolean
  toolsOn: boolean
  textareaRef: React.RefObject<HTMLTextAreaElement>
  onSend: () => void
  onStop: () => void
  onClearContext: () => void
  onOpenPicker: () => void
  onSetOptions: (effort: string, webSearch: boolean, tools: boolean) => void
  onPreviewImage: (img: ImageBlock) => void
  onPreviewFile: (f: FileBlock) => void
}) {
  const [effortOpen, setEffortOpen] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)

  return (
    <footer className="shrink-0 border-t border-border bg-card">
      <div className={cn(CHAT_COLUMN, 'p-3')}>
        {!isEmpty && !streaming && (
          <div className="mb-2 flex items-center justify-end">
            <button
              type="button"
              onClick={() => void onClearContext()}
              className="flex h-6 items-center gap-1 rounded-md border border-border bg-card px-2 text-[11px] text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              title="插入分隔线,后续问答不再带上之前的上下文"
            >
              <Eraser className="h-3 w-3" />
              清除上下文
            </button>
          </div>
        )}
        <div
          className="rounded-xl border border-input bg-background focus-within:border-ring focus-within:ring-1 focus-within:ring-ring"
          onDragOver={(e) => {
            if (Array.from(e.dataTransfer.items).some((it) => it.kind === 'file')) {
              e.preventDefault()
            }
          }}
          onDrop={attach.onDrop}
        >
          {attach.count > 0 && (
            <div className="flex flex-wrap gap-2 border-b border-border/50 p-2">
              {attach.pendingImages.map((img, i) => (
                <div
                  key={'img-' + i}
                  className="group/thumb relative h-16 w-16 overflow-hidden rounded-md border border-border bg-secondary/30"
                >
                  <img
                    src={imageSrc(img)}
                    alt=""
                    className="h-full w-full cursor-pointer object-cover"
                    onClick={() => onPreviewImage(img)}
                  />
                  <button
                    type="button"
                    onClick={() => attach.removePendingImage(i)}
                    className="absolute right-0.5 top-0.5 hidden h-5 w-5 items-center justify-center rounded-full bg-black/60 text-white hover:bg-black/80 group-hover/thumb:flex"
                    title="移除"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </div>
              ))}
              {attach.pendingFiles.map((f, i) => {
                const Icon = fileIcon(f.name)
                return (
                  <div
                    key={'file-' + i}
                    className="group/thumb relative flex h-16 max-w-[220px] cursor-pointer items-center gap-2 overflow-hidden rounded-md border border-border bg-secondary/30 px-2.5"
                    onClick={() => onPreviewFile(f)}
                    title={f.name}
                  >
                    <Icon className="h-7 w-7 shrink-0 text-info" />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-xs font-medium">{f.name}</div>
                      <div className="text-[10px] text-muted-foreground">
                        {f.sizeBytes ? formatFileSize(f.sizeBytes) : ''}
                        {f.text ? ` · ${f.text.length} 字` : ''}
                        {f.data || f.ref ? ' · PDF' : ''}
                      </div>
                    </div>
                    <button
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation()
                        attach.removePendingFile(i)
                      }}
                      className="absolute right-0.5 top-0.5 hidden h-5 w-5 items-center justify-center rounded-full bg-black/60 text-white hover:bg-black/80 group-hover/thumb:flex"
                      title="移除"
                    >
                      <X className="h-3 w-3" />
                    </button>
                  </div>
                )
              })}
            </div>
          )}
          <textarea
            ref={textareaRef}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onPaste={attach.onPaste}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault()
                void onSend()
              }
            }}
            placeholder="发条消息(Enter 发送 · Shift+Enter 换行 · 支持粘贴/拖入图片或文件)"
            rows={3}
            className="block max-h-[240px] w-full resize-none rounded-t-xl bg-transparent px-3 pt-3 text-sm outline-none"
          />

          <div className="flex items-center justify-between gap-2 border-t border-border/50 px-2 py-1.5">
            <div className="flex items-center gap-1">
              <button
                type="button"
                onClick={() => onOpenPicker()}
                className="flex h-7 items-center gap-2 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              >
                {provider ? (
                  <ProviderAvatar logo={provider.logo} name={provider.name} size={18} />
                ) : (
                  <Sparkles className="h-3.5 w-3.5" />
                )}
                <span className="max-w-[260px] truncate font-medium">
                  {provider?.name ?? '未配置'} · {modelId || '未选模型'}
                </span>
                <ChevronDown className="h-3 w-3" />
              </button>
              <button
                type="button"
                onClick={() => fileInputRef.current?.click()}
                disabled={
                  streaming ||
                  attach.count >= MAX_FILES_PER_MESSAGE
                }
                className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:opacity-50"
                title={
                  `添加附件(最多 ${MAX_FILES_PER_MESSAGE} 个 · 图片≤5MB · PDF≤20MB · docx/xlsx/pptx≤15MB)` +
                  (spec && !spec.capabilities.includes('vision')
                    ? `
当前模型不支持图片输入,只能传文档 / 代码`
                    : '')
                }
              >
                <Paperclip className="h-3.5 w-3.5" />
              </button>
              <input
                ref={fileInputRef}
                type="file"
                accept="image/*,.pdf,.docx,.xlsx,.pptx,.txt,.md,.markdown,.csv,.tsv,.json,.yaml,.yml,.toml,.xml,.html,.css,.log,.go,.mod,.sum,.js,.jsx,.ts,.tsx,.mjs,.cjs,.py,.rb,.php,.rs,.c,.cc,.cpp,.h,.hpp,.java,.kt,.swift,.scala,.cs,.lua,.dart,.sh,.bash,.zsh,.fish,.ps1,.bat,.sql,.proto,.dockerfile,.makefile,.tex"
                multiple
                hidden
                onChange={attach.onPickFiles}
              />

              {canTuneReasoning && (
                <div className="relative">
                  <button
                    type="button"
                    onClick={() => setEffortOpen((v) => !v)}
                    className={cn(
                      'flex h-7 items-center gap-1 rounded-md px-2 text-xs transition-colors',
                      currentEffort !== 'default'
                        ? 'bg-info/10 text-info'
                        : 'text-muted-foreground hover:bg-secondary hover:text-foreground',
                    )}
                    title="思考档位:控制模型在回答前花多少算力推理"
                  >
                    <Brain className="h-3.5 w-3.5" />
                    {EFFORT_LABELS[currentEffort] ?? '默认'}
                  </button>
                  {effortOpen && (
                    <>
                      <div
                        className="fixed inset-0 z-10"
                        onClick={() => setEffortOpen(false)}
                      />
                      <div className="absolute bottom-8 left-0 z-20 min-w-[120px] overflow-hidden rounded-md border border-border bg-card py-1 shadow-lg">
                        {(['default', ...effortOptions] as ReasoningEffort[]).map((e) => (
                          <button
                            key={e}
                            type="button"
                            onClick={() => {
                              setEffortOpen(false)
                              onSetOptions(e, webSearch, toolsOn)
                            }}
                            className={cn(
                              'flex w-full px-3 py-1.5 text-left text-xs transition-colors hover:bg-secondary',
                              e === currentEffort ? 'text-info' : 'text-foreground',
                            )}
                          >
                            {EFFORT_LABELS[e] ?? e}
                          </button>
                        ))}
                      </div>
                    </>
                  )}
                </div>
              )}

              {canWebSearch && (
                <button
                  type="button"
                  onClick={() =>
                    onSetOptions(currentEffort, !webSearch, toolsOn)
                  }
                  className={cn(
                    'flex h-7 items-center gap-1 rounded-md px-2 text-xs transition-colors',
                    webSearch
                      ? 'bg-info/10 text-info'
                      : 'text-muted-foreground hover:bg-secondary hover:text-foreground',
                  )}
                  title="供应商内置联网搜索:由模型服务商自己检索,结果会附引用来源"
                >
                  <Globe className="h-3.5 w-3.5" />
                  联网
                </button>
              )}

              {canUseTools && (
                <ToolsToggle
                  on={toolsOn}
                  onToggle={() => onSetOptions(currentEffort, webSearch, !toolsOn)}
                />
              )}
            </div>

            {streaming ? (
              <Button onClick={onStop} variant="outline" size="sm">
                <Square className="h-3 w-3" />
                停止
              </Button>
            ) : (
              <Button
                onClick={() => void onSend()}
                disabled={
                  !draft.trim() && attach.count === 0
                }
                size="sm"
              >
                <Send className="h-3 w-3" />
                发送
              </Button>
            )}
          </div>
        </div>
      </div>
    </footer>
  )
}

/**
 * 「工具」开关 + 一个能看清背后有什么的浮层。
 *
 * 原来这就是个纯粹的黑盒:打开之后模型手里有哪些工具、哪台 MCP 服务器没连上
 * 因而这一轮用不了,界面上一个字都没有。而没连上的服务器是**静默跳过**的
 * (聊天路径只用已连好的连接,绝不现连),用户根本不知道自己少了东西。
 *
 * 清单是点开时才拉的 —— 常开的话每次渲染都要问一次后端,而这信息只有想看时才有用。
 */
function ToolsToggle({ on, onToggle }: { on: boolean; onToggle: () => void }) {
  const [open, setOpen] = useState(false)
  const [view, setView] = useState<ChatToolsView | null>(null)
  const boxRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    void (async () => {
      const v = (await ListAIChatTools().catch(() => null)) as unknown as ChatToolsView | null
      setView(v ?? { tools: [] })
    })()
    const onDown = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  return (
    <div ref={boxRef} className="relative">
      <div
        className={cn(
          'flex h-7 items-center rounded-md text-xs transition-colors',
          on ? 'bg-info/10 text-info' : 'text-muted-foreground hover:bg-secondary',
        )}
      >
        <button
          type="button"
          onClick={onToggle}
          className="flex h-7 items-center gap-1 rounded-l-md pl-2 pr-1 hover:text-foreground"
          title="允许模型调用工具(内置 + MCP);调用过程会实时显示在回复里"
        >
          <Wrench className="h-3.5 w-3.5" />
          工具
        </button>
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="flex h-7 items-center rounded-r-md pl-0.5 pr-1.5 hover:text-foreground"
          title="看看这一轮会带哪些工具"
        >
          <ChevronDown className={cn('h-3 w-3 transition-transform', open && 'rotate-180')} />
        </button>
      </div>

      {open && (
        <div className="absolute bottom-9 left-0 z-20 max-h-72 w-[300px] overflow-auto rounded-lg border border-border bg-card p-2 shadow-lg">
          {!view ? (
            <div className="px-1 py-2 text-xs text-muted-foreground">读取中...</div>
          ) : (
            <>
              <div className="px-1 pb-1.5 text-[11px] text-muted-foreground">
                {on
                  ? `本轮会声明 ${view.tools.length} 个工具给模型`
                  : `开关关着,这 ${view.tools.length} 个工具不会带给模型`}
              </div>
              <ul className="space-y-0.5">
                {view.tools.map((t) => (
                  <li key={t.name} className="rounded px-1 py-1 hover:bg-secondary/50">
                    <div className="flex items-center gap-1.5">
                      <span className="min-w-0 flex-1 truncate font-mono text-[11px]">
                        {t.name}
                      </span>
                      <span className="shrink-0 rounded bg-secondary px-1 text-[10px] text-muted-foreground">
                        {t.source || '内置'}
                      </span>
                    </div>
                    {t.description && (
                      <div className="mt-0.5 line-clamp-2 text-[10px] leading-snug text-muted-foreground">
                        {t.description}
                      </div>
                    )}
                  </li>
                ))}
              </ul>
              {view.offline && view.offline.length > 0 && (
                <div className="mt-1.5 rounded border border-amber-500/30 bg-amber-500/10 px-2 py-1.5 text-[10px] leading-snug text-amber-600 dark:text-amber-400">
                  {view.offline.join('、')} 还没连上,这一轮不会带它们的工具。
                  到「个人中心 → MCP 服务器」点重连,或稍等片刻再发。
                </div>
              )}
              {view.tools.length === 0 && (
                <div className="px-1 py-2 text-xs text-muted-foreground">
                  没有可用工具。可以到「个人中心 → MCP 服务器」接入。
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  )
}
