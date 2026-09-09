import { useEffect, useRef, useState } from 'react'
import {
  AlertTriangle,
  Bot,
  Brain,
  Check,
  CornerDownRight,
  ChevronDown,
  Copy,
  Eraser,
  Globe,
  Link2,
  Pencil,
  RotateCcw,
  Trash2,
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
} from './types'
import { fileIcon } from './chat-utils'
import { ChatActivity } from './ChatActivity'
import { cn } from '@/lib/utils'

// 单条消息的渲染:正文 / 思考块 / 活动面板 / 引用来源 / 附件 / 悬浮操作条
//
// 两种角色的排版刻意不对称:用户的话靠右、装在气泡里,长度通常是一两行;
// 模型的回答通铺整行、不加卡片边框 —— 它常常是几屏的 Markdown,再套一层框
// 既浪费横向空间,也让代码块和表格显得局促。

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
  onContinue,
  onEditResend,
  onDelete,
  onPreviewImage,
  onPreviewFile,
}: {
  message: Message
  fallbackModel: string
  streaming?: boolean
  onRegenerate?: () => void
  onContinue?: () => void
  onEditResend?: (newContent: string) => void
  onDelete?: () => void
  onPreviewImage?: (img: ImageBlock) => void
  onPreviewFile?: (f: FileBlock) => void
}) {
  const [copied, setCopied] = useState(false)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(message.content)
  const isUser = message.role === 'user'

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

  const attachments = (
    <>
      {message.images && message.images.length > 0 && (
        <div className={cn('flex flex-wrap gap-2', isUser && 'justify-end')}>
          {message.images.map((img, i) => (
            <button
              key={i}
              type="button"
              onClick={() => onPreviewImage?.(img)}
              className="overflow-hidden rounded-lg border border-border bg-secondary/30 transition-transform hover:scale-[1.02]"
            >
              <img src={imageSrc(img)} alt="" className="block max-h-64 max-w-xs object-contain" />
            </button>
          ))}
        </div>
      )}
      {message.files && message.files.length > 0 && (
        <div className={cn('flex flex-wrap gap-2', isUser && 'justify-end')}>
          {message.files.map((f, i) => {
            const Icon = fileIcon(f.name)
            return (
              <button
                key={i}
                type="button"
                onClick={() => onPreviewFile?.(f)}
                className="flex max-w-[280px] items-center gap-2 rounded-lg border border-border bg-secondary/30 px-3 py-2 text-left transition-colors hover:bg-secondary"
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
    </>
  )

  if (isUser) {
    return (
      <li className="group/msg flex flex-col items-end gap-1.5">
        {attachments}
        {editing ? (
          <div className="w-full rounded-2xl border border-info/50 bg-info/5">
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
              rows={Math.min(12, Math.max(2, draft.split('\n').length))}
              className="block w-full resize-none rounded-t-2xl bg-transparent px-3.5 py-2.5 text-sm outline-none"
            />
            <div className="flex items-center justify-end gap-2 border-t border-info/20 px-2 py-1.5 text-[11px]">
              <span className="mr-auto text-muted-foreground">Ctrl/⌘+Enter 重发 · Esc 取消</span>
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
          message.content && (
            <div className="max-w-[88%] whitespace-pre-wrap break-words rounded-2xl bg-info/10 px-3.5 py-2.5 text-sm leading-relaxed">
              {message.content}
            </div>
          )
        )}
        {!editing && (
          <MessageActions
            copied={copied}
            onCopy={() => void onCopy()}
            onEdit={onEditResend ? startEdit : undefined}
            onDelete={onDelete}
            align="end"
          />
        )}
      </li>
    )
  }

  const hasBody =
    !!message.content ||
    !!thinkingText(message) ||
    !!message.images?.length ||
    !!message.files?.length

  return (
    <li className="group/msg space-y-1.5">
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Bot className="h-3.5 w-3.5 text-success" />
        <span className="min-w-0 truncate font-medium">
          {message.model || fallbackModel || '助手'}
        </span>
      </div>

      {thinkingText(message) && (
        <ThinkingBlock content={thinkingText(message)} streaming={!!streaming && !message.content} />
      )}

      <ChatActivity
        searches={message.searches}
        toolCalls={message.toolCalls}
        streaming={streaming}
      />

      {attachments}

      {message.content ? (
        <div className="min-w-0 text-sm leading-relaxed">
          <MarkdownPreview value={message.content} className="markdown-preview text-sm" />
          {streaming && <StreamingCaret />}
        </div>
      ) : !hasBody ? (
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <span className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-muted-foreground" />
          {streaming ? pendingLabel(message) : '(没有返回内容,请检查后端日志或换个模型再试)'}
        </div>
      ) : null}

      {message.citations && message.citations.length > 0 && (
        <CitationList list={message.citations} />
      )}

      {message.truncated && !streaming && (
        <div className="flex items-center gap-2 text-[11px] text-amber-600 dark:text-amber-400">
          <AlertTriangle className="h-3 w-3 shrink-0" />
          这条回复没写完
          {onContinue && (
            <button
              type="button"
              onClick={onContinue}
              className="flex items-center gap-1 rounded border border-amber-500/40 px-1.5 py-0.5 transition-colors hover:bg-amber-500/10"
            >
              <CornerDownRight className="h-3 w-3" />
              继续写
            </button>
          )}
        </div>
      )}

      {!streaming && <UsageLine message={message} />}

      {message.content && !streaming && (
        <MessageActions
          copied={copied}
          onCopy={() => void onCopy()}
          onRegenerate={onRegenerate}
          onDelete={onDelete}
          align="start"
        />
      )}
    </li>
  )
}

/**
 * 这条回复花了多少 token、用了多久。
 *
 * 「AI 用量」页记的是流水总账 —— 发现某天忽然变贵时,从总账里翻不出是哪条对话、
 * 哪次重新生成干的。挂在消息上才能一眼对上。默认淡显,不跟正文抢注意力。
 */
function UsageLine({ message }: { message: Message }) {
  const u = message.usage
  if (!u || (!u.inputTokens && !u.outputTokens)) return null
  const parts = [`↑ ${fmtTokens(u.inputTokens)}`, `↓ ${fmtTokens(u.outputTokens)}`]
  if (u.reasoningTokens) parts.push(`思考 ${fmtTokens(u.reasoningTokens)}`)
  if (u.cachedTokens) parts.push(`缓存 ${fmtTokens(u.cachedTokens)}`)
  if (message.durationMs) parts.push(fmtDuration(message.durationMs))
  return (
    <div className="text-[10px] text-muted-foreground/70" title="输入 / 输出 token 与耗时">
      {parts.join(' · ')}
    </div>
  )
}

function fmtTokens(n: number): string {
  if (n < 1000) return String(n)
  return (n / 1000).toFixed(n < 10000 ? 1 : 0) + 'k'
}

function fmtDuration(ms: number): string {
  if (ms < 1000) return ms + ' ms'
  const s = ms / 1000
  if (s < 60) return s.toFixed(1) + ' s'
  return Math.floor(s / 60) + ' 分 ' + Math.round(s % 60) + ' 秒'
}

/**
 * 流式输出时跟在正文末尾的光标。
 *
 * 纯装饰,但少了它很难分辨"模型正在慢慢写"和"它已经写完了、只是答得短"——
 * 前者该继续等,后者可以直接追问。
 */
function StreamingCaret() {
  return (
    <span className="ml-0.5 inline-block h-4 w-[2px] translate-y-0.5 animate-pulse rounded-sm bg-foreground/70 align-middle" />
  )
}

function MessageActions({
  copied,
  onCopy,
  onEdit,
  onRegenerate,
  onDelete,
  align,
}: {
  copied: boolean
  onCopy: () => void
  onEdit?: () => void
  onRegenerate?: () => void
  onDelete?: () => void
  align: 'start' | 'end'
}) {
  return (
    <div
      className={cn(
        'flex items-center gap-3 opacity-0 transition-opacity group-hover/msg:opacity-100',
        align === 'end' ? 'justify-end' : 'justify-start',
      )}
    >
      <ActionBtn onClick={onCopy} icon={copied ? Check : Copy} label={copied ? '已复制' : '复制'} />
      {onEdit && <ActionBtn onClick={onEdit} icon={Pencil} label="编辑" title="编辑并重发" />}
      {onRegenerate && (
        <ActionBtn onClick={onRegenerate} icon={RotateCcw} label="重新生成" title="重新生成" />
      )}
      {onDelete && (
        <ActionBtn onClick={onDelete} icon={Trash2} label="删除" title="删除这条消息" danger />
      )}
    </div>
  )
}

function ActionBtn({
  onClick,
  icon: Icon,
  label,
  title,
  danger,
}: {
  onClick: () => void
  icon: typeof Copy
  label: string
  title?: string
  danger?: boolean
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title ?? label}
      className={cn(
        'flex items-center gap-1 rounded text-[11px] text-muted-foreground transition-colors',
        danger ? 'hover:text-destructive' : 'hover:text-foreground',
      )}
    >
      <Icon className="h-3 w-3" />
      {label}
    </button>
  )
}

/**
 * 正文还没开始时,占位行上显示什么。
 *
 * 一律写"正在思考"会误导:模型开着联网 / 工具时,这段等待里它可能正在搜网页或跑工具,
 * 而这两件事都可能要好几秒 —— 说清楚在等什么,用户才知道是在干活还是卡住了。
 */
function pendingLabel(m: Message): string {
  if ((m.searches ?? []).some((s) => s.status === 'running')) return '正在联网搜索...'
  if ((m.toolCalls ?? []).some((c) => c.status === 'running')) return '正在调用工具...'
  if ((m.toolCalls ?? []).length > 0 || (m.searches ?? []).length > 0) return '正在整理回复...'
  return '正在思考...'
}

/** 联网搜索引用到的来源;默认折叠,点开是可跳转的链接列表 */
export function CitationList({ list }: { list: Citation[] }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="overflow-hidden rounded-lg border border-border bg-secondary/20">
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
    <div className="overflow-hidden rounded-lg border border-border bg-secondary/30">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-secondary/50"
      >
        <Brain className={cn('h-3.5 w-3.5', streaming && 'animate-pulse')} />
        <span className="font-medium">{streaming ? '正在思考...' : '思考过程'}</span>
        <span className="text-[10px] opacity-60">({content.length} 字)</span>
        <ChevronDown
          className={cn('ml-auto h-3.5 w-3.5 transition-transform', open ? 'rotate-180' : '')}
        />
      </button>
      {open && (
        <div className="max-h-80 overflow-auto border-t border-border/50 px-3 py-2 text-xs leading-relaxed text-muted-foreground">
          <div className="whitespace-pre-wrap break-words">{content}</div>
        </div>
      )}
    </div>
  )
}
