import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, ChevronsDownUp, ChevronsUpDown, Copy, Play, Send, Trash2, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { MarkdownPreview } from '@/components/tool/MarkdownPreview'
import { cn } from '@/lib/utils'
import { ReplayLLMProxyRequest } from '../../../wailsjs/go/main/App'
import type { llmproxy } from '../../../wailsjs/go/models'
import { buildCurl, extractDataImages, fmtBytes, fmtTime, foldBase64, methodClass, prettyJSON, statusClass } from './lib'
import { parseConversation, extractResponseText, parseSSE, sseLabel, ssePreview, type ConvBlock, type ConvMsg, type Conversation, type SseEvent as SseEv } from './parse'

interface Props {
  detail: llmproxy.LogDetail
  proxyBase: string
  onClose: () => void
  onDelete: (id: number) => void
  onReplayed: (d: llmproxy.LogDetail) => void
}

type Tab = 'req' | 'resp' | 'raw'

// 折叠总控:nonce 变化时子项同步到 open;子项自身仍可单独开合。
interface CollapseCtl {
  open: boolean
  nonce: number
}

export function LogDetail({ detail, proxyBase, onClose, onDelete, onReplayed }: Props) {
  const e = detail.entry
  const [tab, setTab] = useState<Tab>('resp')
  const [fold, setFold] = useState(true)
  const [md, setMd] = useState(true)
  const [reqRaw, setReqRaw] = useState(false)
  const [respRaw, setRespRaw] = useState(false)
  const [copied, setCopied] = useState(false)
  const [replayOpen, setReplayOpen] = useState(false)
  const [collapse, setCollapse] = useState<CollapseCtl>({ open: true, nonce: 0 })

  const reqConv = useMemo(() => parseConversation(detail.reqBody), [detail.reqBody])
  const answer = useMemo(
    () => (e.stream ? detail.respBody : extractResponseText(detail.respBody)),
    [e.stream, detail.respBody],
  )
  const events = useMemo(() => (e.stream ? parseSSE(detail.respRaw) : []), [e.stream, detail.respRaw])

  useEffect(() => {
    setTab('resp')
    setReplayOpen(false)
    setReqRaw(false)
    setRespRaw(false)
    setCollapse({ open: true, nonce: 0 })
  }, [detail.entry.id])

  const toggleAll = () => setCollapse((c) => ({ open: !c.open, nonce: c.nonce + 1 }))

  const copyCurl = async () => {
    try {
      await navigator.clipboard.writeText(buildCurl(detail, proxyBase))
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      /* ignore */
    }
  }

  const showCollapseAll = (tab === 'req' && !!reqConv && !reqRaw) || tab === 'raw'

  return (
    <>
      <div className="fixed inset-0 z-30 bg-black/30" onClick={onClose} />
      <div className="fixed inset-y-0 right-0 z-40 flex w-full max-w-[720px] flex-col border-l border-border bg-card shadow-xl">
        {/* 头部 */}
        <div className="flex items-center gap-2 border-b border-border px-4 py-2.5">
          <span className={cn('font-mono text-sm font-semibold', methodClass(e.method))}>{e.method}</span>
          <span className={cn('font-mono text-sm font-semibold', statusClass(e.status))}>{e.error ? '错误' : e.status}</span>
          <span className="min-w-0 flex-1 truncate font-mono text-xs" title={`${e.upstream}${e.path}`}>
            {e.upstream}
            {e.path}
          </span>
          <Button variant="ghost" size="sm" className="w-8 px-0" onClick={onClose} title="关闭">
            <X className="h-4 w-4" />
          </Button>
        </div>

        {/* 元信息 + 操作 */}
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-border px-4 py-2 text-[11px] text-muted-foreground">
          <span>{fmtTime(e.ts)}</span>
          <span>{e.durationMs}ms</span>
          {e.ttftMs > 0 && <span title="首字节时间">TTFT {e.ttftMs}ms</span>}
          <span>{fmtBytes(e.reqBytes)} → {fmtBytes(e.respBytes)}</span>
          {e.model && <span>model: {e.model}</span>}
          {e.totalTokens > 0 && <span>tokens: {e.promptTokens}+{e.completionTokens}={e.totalTokens}</span>}
          {e.stream && <span className="text-sky-600 dark:text-sky-400">SSE</span>}
          <div className="ml-auto flex items-center gap-1.5">
            <Button variant="outline" size="sm" onClick={copyCurl}>
              {copied ? <Check className="h-3.5 w-3.5 text-emerald-500" /> : <Copy className="h-3.5 w-3.5" />}
              curl
            </Button>
            <Button variant="outline" size="sm" onClick={() => setReplayOpen((v) => !v)}>
              <Play className="h-3.5 w-3.5" /> 重放
            </Button>
            <Button variant="ghost" size="sm" className="w-8 px-0" onClick={() => onDelete(e.id)} title="删除此条">
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>

        {e.error && (
          <div className="border-b border-border bg-red-500/5 px-4 py-2 text-xs text-red-600 dark:text-red-400">
            {e.error}
          </div>
        )}

        {replayOpen && (
          <ReplayPanel detail={detail} onReplayed={(d) => { setReplayOpen(false); onReplayed(d) }} />
        )}

        {/* tab + 工具条 */}
        <div className="flex items-center gap-1 border-b border-border px-3 py-1.5">
          <TabBtn active={tab === 'req'} onClick={() => setTab('req')}>请求</TabBtn>
          <TabBtn active={tab === 'resp'} onClick={() => setTab('resp')}>响应</TabBtn>
          {e.stream && <TabBtn active={tab === 'raw'} onClick={() => setTab('raw')}>原始 SSE</TabBtn>}

          <div className="ml-auto flex items-center gap-1.5">
            {tab === 'req' && reqConv && (
              <MiniToggle
                options={[{ value: 'chat', label: '对话' }, { value: 'raw', label: '原始' }]}
                value={reqRaw ? 'raw' : 'chat'}
                onChange={(v) => setReqRaw(v === 'raw')}
              />
            )}
            {tab === 'resp' && !e.stream && (
              <MiniToggle
                options={[{ value: 'text', label: '回答' }, { value: 'raw', label: '原始' }]}
                value={respRaw ? 'raw' : 'text'}
                onChange={(v) => setRespRaw(v === 'raw')}
              />
            )}
            <Pill active={md} onClick={() => setMd((v) => !v)} title="按 Markdown 渲染文本">MD</Pill>
            <Pill active={fold} onClick={() => setFold((v) => !v)} title="折叠超长 Base64">B64</Pill>
            {showCollapseAll && (
              <Button variant="ghost" size="sm" className="h-6 w-6 px-0" onClick={toggleAll} title={collapse.open ? '全部折叠' : '全部展开'}>
                {collapse.open ? <ChevronsDownUp className="h-3.5 w-3.5" /> : <ChevronsUpDown className="h-3.5 w-3.5" />}
              </Button>
            )}
          </div>
        </div>

        {/* 内容 */}
        <div className="min-h-0 flex-1 overflow-auto p-4">
          {tab === 'req' && (
            <div className="space-y-3">
              <HeadersView headers={detail.reqHeaders} />
              {detail.reqTruncated && <TruncNote />}
              {reqConv && !reqRaw ? (
                <ConversationView conv={reqConv} fold={fold} md={md} collapse={collapse} />
              ) : (
                <BodyView text={detail.reqBody} fold={fold} />
              )}
            </div>
          )}

          {tab === 'resp' && (
            <div className="space-y-3">
              <HeadersView headers={detail.respHeaders} />
              {detail.respTruncated && <TruncNote />}
              {e.stream ? (
                answer ? (
                  <AnswerView text={answer} md={md} />
                ) : (
                  <Empty hint="未能从 SSE 合并出正文,请切到「原始 SSE」查看" />
                )
              ) : respRaw || !answer ? (
                <BodyView text={detail.respBody} fold={fold} />
              ) : (
                <AnswerView text={answer} md={md} />
              )}
            </div>
          )}

          {tab === 'raw' && <SseView events={events} raw={detail.respRaw} fold={fold} collapse={collapse} />}
        </div>
      </div>
    </>
  )
}

// ============ 对话视图 ============

const ROLE_STYLE: Record<string, string> = {
  system: 'bg-amber-500/12 text-amber-700 dark:text-amber-300',
  developer: 'bg-amber-500/12 text-amber-700 dark:text-amber-300',
  user: 'bg-sky-500/12 text-sky-700 dark:text-sky-300',
  assistant: 'bg-emerald-500/12 text-emerald-700 dark:text-emerald-300',
}

function ConversationView({ conv, fold, md, collapse }: { conv: Conversation; fold: boolean; md: boolean; collapse: CollapseCtl }) {
  return (
    <div className="space-y-2.5">
      {conv.params.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {conv.params.map((p) => (
            <span key={p.key} className="rounded bg-secondary px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
              {p.key}: <span className="text-foreground">{p.value}</span>
            </span>
          ))}
        </div>
      )}
      {conv.messages.map((m, i) => (
        <MsgCard key={i} msg={m} fold={fold} md={md} collapse={collapse} defaultCollapsed={m.role === 'system' || m.role === 'developer'} />
      ))}
    </div>
  )
}

function MsgCard({ msg, fold, md, collapse, defaultCollapsed }: { msg: ConvMsg; fold: boolean; md: boolean; collapse: CollapseCtl; defaultCollapsed: boolean }) {
  const chars = msg.blocks.reduce((n, b) => n + (b.text?.length ?? 0), 0)
  const long = chars > 1600
  const [open, setOpen] = useState(!(defaultCollapsed && long))
  useCollapseSync(collapse, setOpen)

  return (
    <div className="rounded-lg border border-border">
      <button onClick={() => setOpen((o) => !o)} className="flex w-full items-center gap-2 px-3 py-1.5 text-left">
        <span className={cn('rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase', ROLE_STYLE[msg.role] ?? 'bg-secondary text-muted-foreground')}>
          {msg.role}
        </span>
        <span className="text-[10px] text-muted-foreground">{chars.toLocaleString()} 字符</span>
        <span className="ml-auto text-[11px] text-primary">{open ? '折叠' : '展开'}</span>
      </button>
      {open && (
        <div className="space-y-2 border-t border-border/60 px-3 py-2">
          {msg.blocks.map((b, i) => (
            <BlockView key={i} block={b} fold={fold} md={md} />
          ))}
        </div>
      )}
    </div>
  )
}

function BlockView({ block, fold, md }: { block: ConvBlock; fold: boolean; md: boolean }) {
  if (block.kind === 'image') {
    const isData = block.text?.startsWith('data:')
    return (
      <div className="flex items-center gap-2">
        {isData ? (
          <img src={block.text} alt="" className="h-16 w-16 rounded border border-border object-cover" />
        ) : (
          <span className="rounded bg-secondary px-2 py-1 text-[11px] text-muted-foreground">🖼 {block.label ?? 'image'}</span>
        )}
        {block.text && !isData && (
          <span className="break-all font-mono text-[10px] text-muted-foreground">{block.text.slice(0, 120)}</span>
        )}
      </div>
    )
  }
  if (block.kind === 'json') {
    return (
      <div className="space-y-1">
        {block.label && <div className="text-[10px] uppercase tracking-wide text-muted-foreground">{block.label}</div>}
        <pre className="overflow-auto whitespace-pre-wrap break-all rounded-md border border-border bg-secondary/20 p-2 font-mono text-[11px]">
          {fold ? foldBase64(block.text ?? '') : block.text}
        </pre>
      </div>
    )
  }
  const text = fold ? foldBase64(block.text ?? '') : (block.text ?? '')
  if (md) return <MarkdownPreview value={text} className="text-[12px]" />
  return <div className="whitespace-pre-wrap break-words text-[12px] leading-relaxed">{text}</div>
}

function AnswerView({ text, md }: { text: string; md: boolean }) {
  return (
    <div className="rounded-lg border border-border bg-secondary/20 p-3">
      {md ? (
        <MarkdownPreview value={text} className="text-[12.5px]" />
      ) : (
        <div className="whitespace-pre-wrap break-words text-[12px] leading-relaxed">{text}</div>
      )}
    </div>
  )
}

// ============ 原始 SSE 视图 ============

function SseView({ events, raw, fold, collapse }: { events: SseEv[]; raw: string; fold: boolean; collapse: CollapseCtl }) {
  if (!raw) return <Empty />
  if (events.length === 0) return <BodyView text={raw} fold={fold} />
  return (
    <div className="space-y-1">
      <div className="px-0.5 text-[11px] text-muted-foreground">{events.length} 个事件</div>
      {events.map((ev, i) => (
        <SseEventRow key={i} ev={ev} index={i} fold={fold} collapse={collapse} />
      ))}
    </div>
  )
}

function SseEventRow({ ev, index, fold, collapse }: { ev: SseEv; index: number; fold: boolean; collapse: CollapseCtl }) {
  const [open, setOpen] = useState(false)
  useCollapseSync(collapse, setOpen)
  const done = ev.data === '[DONE]'
  const label = sseLabel(ev)
  const preview = ssePreview(ev.data)

  return (
    <div className="rounded border border-border/70">
      <button onClick={() => setOpen((o) => !o)} className="flex w-full items-center gap-2 px-2 py-1 text-left">
        <span className="w-7 shrink-0 text-right text-[10px] tabular-nums text-muted-foreground">#{index}</span>
        <span className="shrink-0 rounded bg-secondary px-1.5 py-0.5 text-[10px] font-medium">{label}</span>
        {done ? (
          <span className="text-[10px] text-emerald-600 dark:text-emerald-400">stream done</span>
        ) : (
          !open && preview && <span className="min-w-0 flex-1 truncate font-mono text-[10px] text-muted-foreground">{preview}</span>
        )}
      </button>
      {open && !done && (
        <pre className="overflow-auto whitespace-pre-wrap break-all border-t border-border/60 bg-secondary/20 p-2 font-mono text-[11px]">
          {fold ? foldBase64(prettyJSON(ev.data)) : prettyJSON(ev.data)}
        </pre>
      )}
    </div>
  )
}

// ============ 通用小组件 ============

// useCollapseSync 让子项在“全部折叠/展开”触发(nonce 变化)时同步 open;首挂载不干扰各自默认值。
function useCollapseSync(collapse: CollapseCtl, setOpen: (b: boolean) => void) {
  const first = useRef(true)
  useEffect(() => {
    if (first.current) {
      first.current = false
      return
    }
    setOpen(collapse.open)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [collapse.nonce])
}

function HeadersView({ headers }: { headers: Record<string, string> }) {
  const entries = Object.entries(headers || {})
  if (entries.length === 0) return null
  return (
    <details className="rounded-md border border-border">
      <summary className="cursor-pointer select-none px-3 py-1.5 text-xs font-medium text-muted-foreground">
        Headers ({entries.length})
      </summary>
      <div className="space-y-0.5 border-t border-border/60 px-3 py-2 font-mono text-[11px]">
        {entries.map(([k, v]) => (
          <div key={k} className="break-all">
            <span className="text-muted-foreground">{k}:</span> {v}
          </div>
        ))}
      </div>
    </details>
  )
}

function BodyView({ text, fold }: { text: string; fold: boolean }) {
  if (!text) return <Empty />
  const images = fold ? extractDataImages(text) : []
  const shown = fold ? foldBase64(prettyJSON(text)) : prettyJSON(text)
  return (
    <div className="space-y-2">
      {images.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {images.map((src, i) => (
            <img key={i} src={src} alt="" className="h-16 w-16 rounded border border-border object-cover" />
          ))}
        </div>
      )}
      <pre className="overflow-auto whitespace-pre-wrap break-all rounded-md border border-border bg-secondary/20 p-3 font-mono text-[11px] leading-relaxed">
        {shown}
      </pre>
    </div>
  )
}

function Empty({ hint }: { hint?: string }) {
  return <div className="text-xs text-muted-foreground">{hint ?? '(空)'}</div>
}

function TruncNote() {
  return (
    <div className="rounded bg-amber-500/10 px-2 py-1 text-[11px] text-amber-600 dark:text-amber-400">
      内容超过上限,已截断(可在设置里调大单条 body 上限)
    </div>
  )
}

function Pill({ active, onClick, title, children }: { active: boolean; onClick: () => void; title?: string; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      title={title}
      className={cn(
        'rounded-md border px-1.5 py-0.5 text-[10px] font-medium transition-colors',
        active ? 'border-primary bg-primary/10 text-primary' : 'border-border text-muted-foreground hover:text-foreground',
      )}
    >
      {children}
    </button>
  )
}

function MiniToggle<T extends string>({
  options,
  value,
  onChange,
}: {
  options: { value: T; label: string }[]
  value: T
  onChange: (v: T) => void
}) {
  return (
    <div className="inline-flex items-center rounded-md border border-border bg-background p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          onClick={() => onChange(o.value)}
          className={cn(
            'rounded-sm px-2 py-0.5 text-[10px] font-medium',
            value === o.value ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

function ReplayPanel({ detail, onReplayed }: { detail: llmproxy.LogDetail; onReplayed: (d: llmproxy.LogDetail) => void }) {
  const e = detail.entry
  const [body, setBody] = useState(detail.reqBody)
  const [auth, setAuth] = useState('')
  const [extraHeaders, setExtraHeaders] = useState(() =>
    Object.entries(detail.reqHeaders || {})
      .filter(([k]) => !['authorization', 'host', 'content-length'].includes(k.toLowerCase()))
      .map(([k, v]) => `${k}: ${v}`)
      .join('\n')
  )
  const [sending, setSending] = useState(false)
  const [err, setErr] = useState('')

  const send = async () => {
    setSending(true)
    setErr('')
    try {
      const headers: Record<string, string> = {}
      for (const line of extraHeaders.split('\n')) {
        const idx = line.indexOf(':')
        if (idx > 0) headers[line.slice(0, idx).trim()] = line.slice(idx + 1).trim()
      }
      if (auth.trim()) headers['Authorization'] = auth.trim()
      const d = await ReplayLLMProxyRequest({
        upstream: e.upstream,
        method: e.method,
        path: e.path,
        headers,
        body,
      })
      if (d) onReplayed(d)
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : String(ex))
    } finally {
      setSending(false)
    }
  }

  return (
    <div className="space-y-2 border-b border-border bg-secondary/20 p-3">
      <div className="text-xs font-medium">重放(密钥未保存,需填 Authorization)</div>
      <input
        type="password"
        value={auth}
        onChange={(e) => setAuth(e.target.value)}
        placeholder="Authorization,如 Bearer sk-..."
        autoComplete="off"
        spellCheck={false}
        className="h-8 w-full rounded-md border border-input bg-background px-2.5 font-mono text-xs outline-none focus:border-ring"
      />
      <textarea
        value={extraHeaders}
        onChange={(e) => setExtraHeaders(e.target.value)}
        rows={2}
        spellCheck={false}
        placeholder="其它头,每行 key: value"
        className="w-full rounded-md border border-input bg-background px-2.5 py-1.5 font-mono text-[11px] outline-none focus:border-ring"
      />
      <textarea
        value={body}
        onChange={(e) => setBody(e.target.value)}
        rows={6}
        spellCheck={false}
        placeholder="请求体"
        className="w-full rounded-md border border-input bg-background px-2.5 py-1.5 font-mono text-[11px] outline-none focus:border-ring"
      />
      {err && <div className="text-[11px] text-red-600 dark:text-red-400">{err}</div>}
      <div className="flex justify-end">
        <Button size="sm" onClick={send} disabled={sending}>
          <Send className="h-3.5 w-3.5" /> {sending ? '发送中…' : '发送'}
        </Button>
      </div>
    </div>
  )
}

function TabBtn({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'rounded-md px-2.5 py-1 text-xs font-medium transition-colors',
        active ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:bg-secondary'
      )}
    >
      {children}
    </button>
  )
}
