import { useEffect, useRef, useState } from 'react'
import {
  Bot,
  Brain,
  Check,
  ChevronDown,
  Copy,
  Eraser,
  Globe,
  Link2,
  Pencil,
  RotateCcw,
  Trash2,
  User,
  Wrench,
  X,
} from 'lucide-react'
import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime'
import { formatFileSize, imageSrc } from './file-parsers'
import { MarkdownPreview } from '@/components/tool/MarkdownPreview'
import {
  thinkingText,
  type Citation,
  type FileBlock,
  type ImageBlock,
  type Message,
  type ToolCall,
} from './types'
import { fileIcon } from './chat-utils'
import { cn } from '@/lib/utils'

// 单条消息的渲染:正文 / 思考块 / 引用来源 / 附件 / 悬浮操作条

export function ClearDivider({ onDelete }: { onDelete?: () => void }) {
  return (
    <li className="group/clear flex items-center gap-3 py-1 text-[11px] text-muted-foreground">
      <div className="h-px flex-1 bg-border" />
      <Eraser className="h-3 w-3" />
      <span>上下文已清除 · 之后的问答不再带上之前的对话</span>
      {onDelete && (
        <button
          type="button"
          onClick={onDelete}
          title="移除分隔线"
          className="hidden h-4 w-4 items-center justify-center rounded text-muted-foreground hover:bg-secondary hover:text-foreground group-hover/clear:flex"
        >
          <X className="h-3 w-3" />
        </button>
      )}
      <div className="h-px flex-1 bg-border" />
    </li>
  )
}

export function MessageItem({
  message,
  fallbackModel,
  streaming,
  onRegenerate,
  onEditResend,
  onDelete,
  onPreviewImage,
  onPreviewFile,
}: {
  message: Message
  fallbackModel: string
  streaming?: boolean
  onRegenerate?: () => void
  onEditResend?: (newContent: string) => void
  onDelete?: () => void
  onPreviewImage?: (img: ImageBlock) => void
  onPreviewFile?: (f: FileBlock) => void
}) {
  const [copied, setCopied] = useState(false)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(message.content)
  const isUser = message.role === 'user'
  const label = isUser ? '你' : message.model || fallbackModel || '助手'

  const onCopy = async () => {
    await navigator.clipboard.writeText(message.content)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  const startEdit = () => {
    setDraft(message.content)
    setEditing(true)
  }
  const cancelEdit = () => {
    setEditing(false)
    setDraft(message.content)
  }
  const submitEdit = () => {
    const next = draft.trim()
    if (!next || !onEditResend) return
    if (next === message.content.trim()) {
      setEditing(false)
      return
    }
    onEditResend(next)
    setEditing(false)
  }

  return (
    <li className="group/msg flex gap-3">
      <div
        className={cn(
          'flex h-8 w-8 shrink-0 items-center justify-center rounded-full',
          isUser ? 'bg-info/15 text-info' : 'bg-success/15 text-success',
        )}
      >
        {isUser ? <User className="h-4 w-4" /> : <Bot className="h-4 w-4" />}
      </div>
      <div className="min-w-0 flex-1 space-y-1">
        <div className="text-xs font-medium text-muted-foreground" title={label}>
          {label}
        </div>

        {!isUser && thinkingText(message) && (
          <ThinkingBlock
            content={thinkingText(message)}
            streaming={!!streaming && !message.content}
          />
        )}

        {!isUser && message.toolCalls && message.toolCalls.length > 0 && (
          <ToolCallList list={message.toolCalls} />
        )}

        {message.images && message.images.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {message.images.map((img, i) => (
              <button
                key={i}
                type="button"
                onClick={() => onPreviewImage?.(img)}
                className="overflow-hidden rounded-md border border-border bg-secondary/30 transition-transform hover:scale-[1.02]"
              >
                <img
                  src={imageSrc(img)}
                  alt=""
                  className="block max-h-64 max-w-xs object-contain"
                />
              </button>
            ))}
          </div>
        )}

        {message.files && message.files.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {message.files.map((f, i) => {
              const Icon = fileIcon(f.name)
              return (
                <button
                  key={i}
                  type="button"
                  onClick={() => onPreviewFile?.(f)}
                  className="flex max-w-[280px] items-center gap-2 rounded-md border border-border bg-secondary/30 px-3 py-2 text-left transition-colors hover:bg-secondary"
                  title={f.name}
                >
                  <Icon className="h-7 w-7 shrink-0 text-info" />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-xs font-medium">{f.name}</div>
                    <div className="text-[10px] text-muted-foreground">
                      {f.sizeBytes ? formatFileSize(f.sizeBytes) : ''}
                      {f.text ? ` · ${f.text.length} 字` : ''}
                      {f.data ? ' · PDF' : ''}
                    </div>
                  </div>
                </button>
              )
            })}
          </div>
        )}

        <div className="min-w-0 text-sm leading-relaxed">
          {isUser ? (
            editing ? (
              <div className="rounded-lg border border-info/50 bg-info/5">
                <textarea
                  autoFocus
                  value={draft}
                  onChange={(e) => setDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                      e.preventDefault()
                      submitEdit()
                    } else if (e.key === 'Escape') {
                      e.preventDefault()
                      cancelEdit()
                    }
                  }}
                  rows={Math.min(8, Math.max(2, draft.split('\n').length))}
                  className="block w-full resize-none rounded-t-lg bg-transparent px-3 py-2 text-sm outline-none"
                />
                <div className="flex items-center justify-end gap-2 border-t border-info/20 px-2 py-1.5 text-[11px]">
                  <span className="mr-auto text-muted-foreground">
                    Ctrl/⌘+Enter 重发 · Esc 取消
                  </span>
                  <button
                    type="button"
                    onClick={cancelEdit}
                    className="rounded-md px-2 py-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
                  >
                    取消
                  </button>
                  <button
                    type="button"
                    onClick={submitEdit}
                    disabled={!draft.trim()}
                    className="rounded-md bg-info px-2.5 py-1 font-medium text-info-foreground transition-colors hover:bg-info/90 disabled:opacity-50"
                  >
                    重新发送
                  </button>
                </div>
              </div>
            ) : (
              <div className="whitespace-pre-wrap break-words rounded-lg bg-info/5 px-3 py-2">
                {message.content}
              </div>
            )
          ) : message.content ? (
            <div className="rounded-lg border border-border bg-card px-3 py-2">
              <MarkdownPreview value={message.content} className="markdown-preview text-sm" />
            </div>
          ) : !thinkingText(message) &&
            (!message.images || message.images.length === 0) &&
            (!message.files || message.files.length === 0) ? (
            <div className="flex items-center gap-2 rounded-lg border border-border bg-card px-3 py-2 text-xs text-muted-foreground">
              <span className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-muted-foreground" />
              {streaming ? '正在思考...' : '(没有返回内容,请检查后端日志或换个模型再试)'}
            </div>
          ) : null}
        </div>
        {!isUser && message.citations && message.citations.length > 0 && (
          <CitationList list={message.citations} />
        )}
        {message.content && !streaming && !editing && (
          <div className="flex items-center gap-3 opacity-0 transition-opacity group-hover/msg:opacity-100">
            <button
              type="button"
              onClick={onCopy}
              className="flex items-center gap-1 rounded text-[11px] text-muted-foreground hover:text-foreground"
            >
              {copied ? (
                <>
                  <Check className="h-3 w-3" />
                  已复制
                </>
              ) : (
                <>
                  <Copy className="h-3 w-3" />
                  复制
                </>
              )}
            </button>
            {onEditResend && (
              <button
                type="button"
                onClick={startEdit}
                className="flex items-center gap-1 rounded text-[11px] text-muted-foreground hover:text-foreground"
                title="编辑并重发"
              >
                <Pencil className="h-3 w-3" />
                编辑
              </button>
            )}
            {onRegenerate && (
              <button
                type="button"
                onClick={onRegenerate}
                className="flex items-center gap-1 rounded text-[11px] text-muted-foreground hover:text-foreground"
                title="重新生成"
              >
                <RotateCcw className="h-3 w-3" />
                重新生成
              </button>
            )}
            {onDelete && (
              <button
                type="button"
                onClick={onDelete}
                className="ml-auto flex items-center gap-1 rounded text-[11px] text-muted-foreground hover:text-destructive"
                title="删除这条消息"
              >
                <Trash2 className="h-3 w-3" />
                删除
              </button>
            )}
          </div>
        )}
      </div>
    </li>
  )
}

/**
 * 模型调用了哪些本地工具。默认折叠,点开能看到入参和返回值。
 *
 * 展开的价值在于排查:模型答得不对时,先看它到底调了什么、传了什么参数、拿回了什么,
 * 比对着最终回复猜要快得多。
 */
function ToolCallList({ list }: { list: ToolCall[] }) {
  const [open, setOpen] = useState(false)
  const failed = list.filter((c) => c.error).length
  return (
    <div className="rounded-lg border border-border bg-secondary/20">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-secondary/40"
      >
        <Wrench className="h-3.5 w-3.5" />
        <span className="font-medium">调用了工具</span>
        <span className="text-[10px] opacity-60">
          ({list.map((c) => c.name).join(', ')})
        </span>
        {failed > 0 && (
          <span className="rounded bg-destructive/15 px-1.5 text-[10px] text-destructive">
            {failed} 个失败
          </span>
        )}
        <ChevronDown
          className={cn('ml-auto h-3.5 w-3.5 transition-transform', open ? 'rotate-180' : '')}
        />
      </button>
      {open && (
        <div className="space-y-2 border-t border-border/50 px-3 py-2">
          {list.map((c, i) => (
            <div key={c.id + i} className="space-y-1 text-xs">
              <div className="font-mono font-medium">{c.name}</div>
              {c.arguments && c.arguments !== '{}' && (
                <div className="whitespace-pre-wrap break-all font-mono text-[11px] text-muted-foreground">
                  入参 {c.arguments}
                </div>
              )}
              <div
                className={cn(
                  'whitespace-pre-wrap break-words font-mono text-[11px]',
                  c.error ? 'text-destructive' : 'text-muted-foreground',
                )}
              >
                {c.error ? '失败:' + c.error : (c.result ?? '(执行中...)')}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

/** 联网搜索引用到的来源;默认折叠,点开是可跳转的链接列表 */
export function CitationList({ list }: { list: Citation[] }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="rounded-lg border border-border bg-secondary/20">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-secondary/40"
      >
        <Globe className="h-3.5 w-3.5" />
        <span className="font-medium">引用来源</span>
        <span className="text-[10px] opacity-60">({list.length})</span>
        <ChevronDown
          className={cn('ml-auto h-3.5 w-3.5 transition-transform', open ? 'rotate-180' : '')}
        />
      </button>
      {open && (
        <ol className="space-y-1.5 border-t border-border/50 px-3 py-2">
          {list.map((c, i) => (
            <li key={c.url + i} className="flex gap-2 text-xs">
              <span className="shrink-0 text-muted-foreground">{i + 1}.</span>
              <button
                type="button"
                onClick={() => BrowserOpenURL(c.url)}
                className="flex min-w-0 items-start gap-1 text-left text-info hover:underline"
                title={c.url}
              >
                <Link2 className="mt-0.5 h-3 w-3 shrink-0" />
                <span className="min-w-0 break-all">{c.title || c.url}</span>
              </button>
            </li>
          ))}
        </ol>
      )}
    </div>
  )
}

/** 折叠思考块:streaming 时默认展开,完成后默认折叠 */
export function ThinkingBlock({
  content,
  streaming,
}: {
  content: string
  streaming: boolean
}) {
  const [open, setOpen] = useState(streaming)
  const wasStreamingRef = useRef(streaming)

  // 流结束的瞬间自动折叠
  useEffect(() => {
    if (wasStreamingRef.current && !streaming) setOpen(false)
    if (!wasStreamingRef.current && streaming) setOpen(true)
    wasStreamingRef.current = streaming
  }, [streaming])

  return (
    <div className="rounded-lg border border-border bg-secondary/30">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-secondary/50"
      >
        <Brain className={cn('h-3.5 w-3.5', streaming && 'animate-pulse')} />
        <span className="font-medium">{streaming ? '正在思考...' : '思考过程'}</span>
        <span className="text-[10px] opacity-60">({content.length} 字)</span>
        <ChevronDown
          className={cn(
            'ml-auto h-3.5 w-3.5 transition-transform',
            open ? 'rotate-180' : '',
          )}
        />
      </button>
      {open && (
        <div className="border-t border-border/50 px-3 py-2 text-xs leading-relaxed text-muted-foreground">
          <div className="whitespace-pre-wrap break-words font-mono">{content}</div>
        </div>
      )}
    </div>
  )
}
