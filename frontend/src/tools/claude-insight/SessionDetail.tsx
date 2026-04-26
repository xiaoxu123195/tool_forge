import { useEffect, useMemo, useRef, useState } from 'react'
import {
  AlertCircle,
  ArrowLeft,
  Brain,
  ChevronDown,
  ChevronRight,
  ChevronUp,
  Coins,
  Eye,
  EyeOff,
  Folder,
  Loader2,
  MessageSquare,
  User,
  Wrench,
  X,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { ScrollToTopButton } from '@/components/tool/ScrollToTopButton'
import { MarkdownPreview } from '@/components/tool/MarkdownPreview'
import { cn } from '@/lib/utils'
import { LoadClaudeSession } from '../../../wailsjs/go/main/App'
import type { claudeinsight } from '../../../wailsjs/go/models'
import { formatDateTime, formatTokens } from './lib/format'

type Detail = claudeinsight.SessionDetail
type Message = claudeinsight.Message
type Block = claudeinsight.Block

interface Props {
  filePath: string
  project: string
  onBack: () => void
  /** 从全局搜索跳转过来时要定位的消息 UUID */
  focusUUID?: string
}

export function SessionDetail({ filePath, project, onBack, focusUUID }: Props) {
  const [detail, setDetail] = useState<Detail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [highlightUUID, setHighlightUUID] = useState<string>('')
  const [findOpen, setFindOpen] = useState(false)
  const [globalReplyOnly, setGlobalReplyOnly] = useState(false)
  const bodyRef = useRef<HTMLDivElement>(null)

  // 整个会话里只要有一条 assistant 消息含 thinking / tool_use,就值得显示全局开关
  const hasNoise = useMemo(() => {
    const msgs = detail?.messages ?? []
    return msgs.some(
      (m) =>
        m.role === 'assistant' &&
        m.blocks.some((b) => b.type === 'thinking' || b.type === 'tool_use')
    )
  }, [detail])

  // Ctrl+F / Cmd+F 打开页内搜索。Esc 在 FindBar 内关闭。
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'f') {
        e.preventDefault()
        setFindOpen(true)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    LoadClaudeSession(filePath)
      .then((d) => {
        if (!cancelled) setDetail(d)
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [filePath])

  // 消息加载完成后,若带了 focusUUID 就滚到那条消息并短暂高亮。
  useEffect(() => {
    if (!detail || !focusUUID) return
    // 等 DOM 渲染完(包括 markdown / 代码高亮)
    const id = window.setTimeout(() => {
      const el = document.getElementById(`msg-${focusUUID}`)
      if (el) {
        el.scrollIntoView({ behavior: 'smooth', block: 'center' })
        setHighlightUUID(focusUUID)
        // 2.5 秒后收回高亮
        window.setTimeout(() => setHighlightUUID(''), 2500)
      }
    }, 120)
    return () => window.clearTimeout(id)
  }, [detail, focusUUID])

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-3">
      <DetailHeader
        project={project}
        onBack={onBack}
        showToggle={hasNoise}
        replyOnly={globalReplyOnly}
        onToggleReplyOnly={() => setGlobalReplyOnly((v) => !v)}
      />
      {loading && !detail ? (
        <div className="flex h-40 items-center justify-center gap-3 text-sm text-muted-foreground">
          <Loader2 className="h-5 w-5 animate-spin" />
          正在加载会话...
        </div>
      ) : error ? (
        <div className="flex h-40 flex-col items-center justify-center gap-2 text-center">
          <AlertCircle className="h-8 w-8 text-red-500" />
          <div className="max-w-md text-sm text-muted-foreground">{error}</div>
        </div>
      ) : detail ? (
        <div ref={bodyRef}>
          <MessageList
            messages={detail.messages ?? []}
            highlightUUID={highlightUUID}
            globalReplyOnly={globalReplyOnly}
          />
        </div>
      ) : null}

      {findOpen && bodyRef.current && (
        <FindBar container={bodyRef.current} onClose={() => setFindOpen(false)} />
      )}
      <ScrollToTopButton />
    </div>
  )
}

function DetailHeader({
  project,
  onBack,
  showToggle,
  replyOnly,
  onToggleReplyOnly,
}: {
  project: string
  onBack: () => void
  showToggle: boolean
  replyOnly: boolean
  onToggleReplyOnly: () => void
}) {
  return (
    <div className="flex items-center gap-3">
      <Button variant="outline" size="sm" onClick={onBack}>
        <ArrowLeft className="h-3.5 w-3.5" />
        返回列表
      </Button>
      <div className="flex min-w-0 flex-1 items-center gap-2 text-xs text-muted-foreground">
        <Folder className="h-3.5 w-3.5 shrink-0 text-info" />
        <span className="truncate font-mono" title={project}>
          {project || '—'}
        </span>
      </div>
      {showToggle && (
        <Button
          variant={replyOnly ? 'default' : 'outline'}
          size="sm"
          onClick={onToggleReplyOnly}
          title={replyOnly ? '显示全部内容(思考 + 工具调用)' : '只看回复,隐藏所有思考 / 工具调用'}
        >
          {replyOnly ? <Eye className="h-3.5 w-3.5" /> : <EyeOff className="h-3.5 w-3.5" />}
          {replyOnly ? '显示噪音' : '隐藏噪音'}
        </Button>
      )}
    </div>
  )
}

// Turn 一轮对话:user 消息单独成一轮,或多条连续 assistant 消息合并成一轮。
type Turn =
  | { kind: 'user'; message: Message }
  | {
      kind: 'assistant'
      messages: Message[]
      models: string[]
      startedAt: string
      endedAt: string
      tokens: { input: number; output: number; cacheCreation: number; cacheRead: number }
    }

function groupIntoTurns(messages: Message[]): Turn[] {
  const turns: Turn[] = []
  let buffer: Message[] = []

  const flush = () => {
    if (buffer.length === 0) return
    const models = Array.from(new Set(buffer.map((m) => m.model).filter(Boolean) as string[]))
    const tokens = { input: 0, output: 0, cacheCreation: 0, cacheRead: 0 }
    for (const m of buffer) {
      if (m.tokens) {
        tokens.input += m.tokens.input
        tokens.output += m.tokens.output
        tokens.cacheCreation += m.tokens.cache_creation
        tokens.cacheRead += m.tokens.cache_read
      }
    }
    turns.push({
      kind: 'assistant',
      messages: [...buffer],
      models,
      startedAt: buffer[0].timestamp,
      endedAt: buffer[buffer.length - 1].timestamp,
      tokens,
    })
    buffer = []
  }

  for (const m of messages) {
    if (m.role === 'assistant') {
      buffer.push(m)
    } else {
      flush()
      turns.push({ kind: 'user', message: m })
    }
  }
  flush()
  return turns
}

function MessageList({
  messages,
  highlightUUID,
  globalReplyOnly,
}: {
  messages: Message[]
  highlightUUID: string
  globalReplyOnly: boolean
}) {
  if (messages.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-border bg-card px-6 py-12 text-center text-sm text-muted-foreground">
        此会话没有可展示的消息（可能全是系统事件）
      </div>
    )
  }
  const turns = groupIntoTurns(messages)
  return (
    <div className="space-y-4">
      {turns.map((t, i) =>
        t.kind === 'user' ? (
          <UserCard key={`u-${i}`} message={t.message} highlightUUID={highlightUUID} />
        ) : (
          <AssistantTurn
            key={`a-${i}`}
            turn={t}
            highlightUUID={highlightUUID}
            globalReplyOnly={globalReplyOnly}
          />
        )
      )}
    </div>
  )
}

function UserCard({ message, highlightUUID }: { message: Message; highlightUUID: string }) {
  const isHighlighted = highlightUUID && message.uuid === highlightUUID
  return (
    <div
      id={message.uuid ? `msg-${message.uuid}` : undefined}
      className={cn(
        'rounded-lg border border-info/30 bg-info/5 transition-shadow',
        isHighlighted && 'ring-2 ring-info/70 ring-offset-2 ring-offset-background'
      )}
    >
      <div className="flex items-center gap-2 border-b border-border/60 px-4 py-2 text-xs text-muted-foreground">
        <User className="h-3.5 w-3.5 text-info" />
        <span className="font-medium">你</span>
        <span className="ml-auto">{formatDateTime(message.timestamp)}</span>
      </div>
      <div className="space-y-3 p-4">
        {message.blocks.map((b, i) => (
          <BlockView key={i} block={b} />
        ))}
      </div>
    </div>
  )
}

function AssistantTurn({
  turn,
  highlightUUID,
  globalReplyOnly,
}: {
  turn: Extract<Turn, { kind: 'assistant' }>
  highlightUUID: string
  globalReplyOnly: boolean
}) {
  // 只看回复:隐藏 thinking / tool_use / image 以外的非文本块,聚焦 Claude 的纯文字回复
  const [replyOnly, setReplyOnly] = useState(globalReplyOnly)
  // 全局开关切换时,把每个回合同步到全局值(用户之后仍可对单个回合再次微调)
  useEffect(() => {
    setReplyOnly(globalReplyOnly)
  }, [globalReplyOnly])
  const turnHighlighted = highlightUUID && turn.messages.some((m) => m.uuid === highlightUUID)

  // 是否值得显示 toggle:只有在回合含 thinking 或 tool_use 时才提供
  const noisyCount = turn.messages.reduce(
    (sum, m) =>
      sum + m.blocks.filter((b) => b.type === 'thinking' || b.type === 'tool_use').length,
    0
  )
  const toggleable = noisyCount > 0

  const filterBlocks = (blocks: Block[]): Block[] => {
    if (!replyOnly) return blocks
    return blocks.filter((b) => b.type === 'text' || b.type === 'image')
  }
  const tokenTotal =
    turn.tokens.input + turn.tokens.output + turn.tokens.cacheCreation + turn.tokens.cacheRead
  return (
    <div
      className={cn(
        'rounded-lg border border-border bg-card transition-shadow',
        turnHighlighted && 'ring-2 ring-info/70 ring-offset-2 ring-offset-background'
      )}
    >
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 border-b border-border/60 px-4 py-2 text-xs text-muted-foreground">
        <MessageSquare className="h-3.5 w-3.5 text-emerald-500" />
        <span className="font-medium">Claude</span>
        {turn.models.map((m) => (
          <span key={m} className="rounded bg-secondary px-1.5 py-0.5 font-mono text-[10px]">
            {m}
          </span>
        ))}
        {turn.messages.length > 1 && (
          <span className="rounded bg-secondary/60 px-1.5 py-0.5 text-[10px]">
            {turn.messages.length} 条消息
          </span>
        )}
        {tokenTotal > 0 && (
          <span
            className="inline-flex items-center gap-1 rounded bg-secondary/60 px-1.5 py-0.5 font-mono text-[10px]"
            title={`输入 ${turn.tokens.input.toLocaleString()} · 输出 ${turn.tokens.output.toLocaleString()} · cache 写 ${turn.tokens.cacheCreation.toLocaleString()} · cache 读 ${turn.tokens.cacheRead.toLocaleString()}`}
          >
            <Coins className="h-3 w-3" />
            {formatTokens(tokenTotal)}
          </span>
        )}
        <span className="ml-auto flex items-center gap-1">
          {toggleable && (
            <button
              onClick={() => setReplyOnly((v) => !v)}
              className={cn(
                'inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] transition-colors',
                replyOnly
                  ? 'bg-info/15 text-info'
                  : 'hover:bg-secondary'
              )}
              title={replyOnly ? '显示全部（思考 + 工具调用）' : `只看回复（隐藏 ${noisyCount} 个思考/工具块）`}
            >
              {replyOnly ? <Eye className="h-3 w-3" /> : <EyeOff className="h-3 w-3" />}
              {replyOnly ? '只看回复' : '隐藏噪音'}
            </button>
          )}
          <span>{formatDateTime(turn.endedAt)}</span>
        </span>
      </div>
      <div className="divide-y divide-border/40">
        {turn.messages.map((m, i) => {
          const visible = filterBlocks(m.blocks)
          if (visible.length === 0) return null
          return (
            <div
              key={m.uuid || i}
              id={m.uuid ? `msg-${m.uuid}` : undefined}
              className="space-y-3 p-4"
            >
              {visible.map((b, j) => (
                <BlockView key={j} block={b} />
              ))}
            </div>
          )
        })}
      </div>
    </div>
  )
}

function BlockView({ block }: { block: Block }) {
  switch (block.type) {
    case 'text':
      return <TextBlock text={block.text ?? ''} />
    case 'thinking':
      return <ThinkingBlock text={block.text ?? ''} />
    case 'image':
      return <ImageBlock src={block.text ?? ''} />
    case 'tool_use':
      return (
        <ToolUseBlock
          name={block.name ?? ''}
          input={block.input ?? ''}
          output={block.output ?? ''}
          isError={!!block.is_error}
        />
      )
    case 'tool_result':
      // 未配对的孤立 tool_result(罕见);按兼容处理保留渲染
      return <ToolResultBlock output={block.output ?? ''} isError={!!block.is_error} />
    default:
      return null
  }
}

function TextBlock({ text }: { text: string }) {
  return <MarkdownPreview value={text} />
}

function ThinkingBlock({ text }: { text: string }) {
  const [open, setOpen] = useState(false)
  return (
    <Collapsible
      open={open}
      onToggle={() => setOpen((v) => !v)}
      icon={<Brain className="h-3.5 w-3.5" />}
      label={`思考（${text.length} 字）`}
      accent="text-muted-foreground"
    >
      <pre className="whitespace-pre-wrap font-mono text-[12px] text-muted-foreground">{text}</pre>
    </Collapsible>
  )
}

function ToolUseBlock({
  name,
  input,
  output,
  isError,
}: {
  name: string
  input: string
  output: string
  isError: boolean
}) {
  const [open, setOpen] = useState(false)
  const hasOutput = output.trim() !== ''
  const status = !hasOutput ? 'pending' : isError ? 'error' : 'ok'
  const statusText = status === 'pending' ? '等待返回' : status === 'error' ? '报错' : '已返回'
  const accent =
    status === 'error'
      ? 'text-red-600 dark:text-red-400'
      : status === 'pending'
      ? 'text-muted-foreground'
      : 'text-amber-600 dark:text-amber-400'
  const outputPreview =
    output.length > 80 ? output.slice(0, 80).replace(/\s+/g, ' ') + '…' : output.replace(/\s+/g, ' ')

  return (
    <Collapsible
      open={open}
      onToggle={() => setOpen((v) => !v)}
      icon={<Wrench className="h-3.5 w-3.5" />}
      label={
        <>
          <span className="font-mono text-foreground">{name || '—'}</span>
          <span
            className={cn(
              'ml-1 rounded px-1.5 py-0.5 text-[10px]',
              status === 'error'
                ? 'bg-red-500/15 text-red-600 dark:text-red-400'
                : status === 'pending'
                ? 'bg-secondary'
                : 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400'
            )}
          >
            {statusText}
          </span>
          {!open && hasOutput && (
            <span className="ml-2 truncate font-mono text-[11px] text-muted-foreground">
              {outputPreview}
            </span>
          )}
        </>
      }
      accent={accent}
    >
      <div className="space-y-2">
        <div>
          <div className="mb-1 text-[10px] uppercase tracking-wide text-muted-foreground">输入</div>
          {input ? (
            <pre className="overflow-x-auto rounded-md bg-secondary/50 p-2 font-mono text-[12px]">
              {input}
            </pre>
          ) : (
            <span className="text-xs italic text-muted-foreground">（无输入参数）</span>
          )}
        </div>
        {(hasOutput || status !== 'pending') && (
          <div>
            <div
              className={cn(
                'mb-1 text-[10px] uppercase tracking-wide',
                status === 'error' ? 'text-red-600 dark:text-red-400' : 'text-muted-foreground'
              )}
            >
              {status === 'error' ? '错误输出' : '返回'}
            </div>
            <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-md bg-secondary/50 p-2 font-mono text-[12px]">
              {hasOutput ? output : '（空）'}
            </pre>
          </div>
        )}
      </div>
    </Collapsible>
  )
}

function ImageBlock({ src }: { src: string }) {
  const [broken, setBroken] = useState(false)
  if (!src) return null
  if (broken) {
    return (
      <div className="rounded border border-dashed border-border bg-card px-3 py-2 text-xs text-muted-foreground">
        图片加载失败
      </div>
    )
  }
  return (
    <a href={src} target="_blank" rel="noreferrer" className="inline-block">
      <img
        src={src}
        alt=""
        onError={() => setBroken(true)}
        className="max-h-96 rounded-md border border-border"
      />
    </a>
  )
}

function ToolResultBlock({ output, isError }: { output: string; isError: boolean }) {
  const [open, setOpen] = useState(false)
  const text = output.trim()
  const preview = text.length > 120 ? text.slice(0, 120) + '…' : text
  return (
    <Collapsible
      open={open}
      onToggle={() => setOpen((v) => !v)}
      icon={<Wrench className="h-3.5 w-3.5" />}
      label={
        <>
          工具{isError ? '报错' : '返回'}
          <span className="ml-2 truncate font-mono text-[11px] text-muted-foreground">{preview}</span>
        </>
      }
      accent={isError ? 'text-red-600 dark:text-red-400' : 'text-emerald-600 dark:text-emerald-400'}
    >
      <pre className="whitespace-pre-wrap break-words font-mono text-[12px]">{output || '（空）'}</pre>
    </Collapsible>
  )
}

function Collapsible({
  open,
  onToggle,
  icon,
  label,
  accent,
  children,
}: {
  open: boolean
  onToggle: () => void
  icon: React.ReactNode
  label: React.ReactNode
  accent: string
  children: React.ReactNode
}) {
  return (
    <div className="rounded-md border border-border/60 bg-background/50">
      <button
        onClick={onToggle}
        className="flex w-full items-center gap-2 px-2 py-1.5 text-left text-xs"
      >
        {open ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
        <span className={cn('inline-flex items-center gap-1.5 font-medium', accent)}>
          {icon}
          {label}
        </span>
      </button>
      {open && <div className="border-t border-border/60 p-2">{children}</div>}
    </div>
  )
}

// ---------- 页内搜索(Ctrl+F) ----------

type Match = { node: Text; start: number; end: number }

function FindBar({ container, onClose }: { container: HTMLElement; onClose: () => void }) {
  const [query, setQuery] = useState('')
  const [matches, setMatches] = useState<Match[]>([])
  const [current, setCurrent] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)

  // 挂载时聚焦输入框
  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  // 监听 Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  // query 变化时重算匹配
  useEffect(() => {
    const q = query.trim()
    if (!q) {
      setMatches([])
      setCurrent(0)
      return
    }
    const found = findAllMatches(container, q)
    setMatches(found)
    setCurrent(0)
  }, [query, container])

  // current 变化时 scroll 到对应 match
  useEffect(() => {
    if (matches.length === 0) return
    const m = matches[current]
    if (!m) return
    const parent = m.node.parentElement
    if (parent) {
      parent.scrollIntoView({ behavior: 'smooth', block: 'center' })
    }
    // 用 selection 临时选中,借用浏览器自带高亮
    try {
      const sel = window.getSelection()
      if (sel) {
        sel.removeAllRanges()
        const range = document.createRange()
        range.setStart(m.node, m.start)
        range.setEnd(m.node, m.end)
        sel.addRange(range)
      }
    } catch {
      // ignore
    }
  }, [current, matches])

  const goPrev = () => {
    if (matches.length === 0) return
    setCurrent((c) => (c - 1 + matches.length) % matches.length)
  }
  const goNext = () => {
    if (matches.length === 0) return
    setCurrent((c) => (c + 1) % matches.length)
  }

  return (
    <div className="pointer-events-none fixed inset-x-0 top-14 z-40 flex justify-center">
      <div
        className="pointer-events-auto flex items-center gap-2 rounded-md border border-border bg-card/95 px-2 py-1.5 shadow-lg backdrop-blur"
        style={{ minWidth: 340 }}
      >
        <input
          ref={inputRef}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              if (e.shiftKey) goPrev()
              else goNext()
            }
          }}
          placeholder="在本会话中查找..."
          className="h-7 w-48 flex-1 rounded border border-border bg-background px-2 text-xs outline-none focus:border-foreground/30"
        />
        <span className="shrink-0 font-mono text-[11px] text-muted-foreground" style={{ minWidth: 48 }}>
          {matches.length === 0 ? (query ? '无' : '') : `${current + 1}/${matches.length}`}
        </span>
        <button
          onClick={goPrev}
          disabled={matches.length === 0}
          className="inline-flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-secondary disabled:opacity-40"
          title="上一个 (Shift+Enter)"
        >
          <ChevronUp className="h-3.5 w-3.5" />
        </button>
        <button
          onClick={goNext}
          disabled={matches.length === 0}
          className="inline-flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-secondary disabled:opacity-40"
          title="下一个 (Enter)"
        >
          <ChevronDown className="h-3.5 w-3.5" />
        </button>
        <button
          onClick={onClose}
          className="inline-flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-secondary"
          title="关闭 (Esc)"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </div>
    </div>
  )
}

/**
 * findAllMatches 遍历 container 下所有可见文本节点,找出 query 的全部命中。
 * 大小写无关,不跨文本节点。
 */
function findAllMatches(container: HTMLElement, query: string): Match[] {
  const q = query.toLowerCase()
  if (!q) return []
  const out: Match[] = []
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      // 跳过隐藏元素
      const parent = node.parentElement
      if (!parent) return NodeFilter.FILTER_REJECT
      if (parent.closest('[aria-hidden="true"]')) return NodeFilter.FILTER_REJECT
      if (!node.nodeValue || node.nodeValue.trim() === '') return NodeFilter.FILTER_REJECT
      return NodeFilter.FILTER_ACCEPT
    },
  })
  let node = walker.nextNode() as Text | null
  while (node) {
    const lower = node.nodeValue!.toLowerCase()
    let from = 0
    while (true) {
      const idx = lower.indexOf(q, from)
      if (idx < 0) break
      out.push({ node, start: idx, end: idx + q.length })
      from = idx + q.length
    }
    node = walker.nextNode() as Text | null
  }
  return out
}
