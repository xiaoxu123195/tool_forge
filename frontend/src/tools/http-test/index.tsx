import { useEffect, useMemo, useRef, useState } from 'react'
import {
  AlertCircle,
  ChevronDown,
  Clock,
  Copy,
  History as HistoryIcon,
  Loader2,
  Plus,
  Send,
  Trash2,
  Wand2,
  X,
} from 'lucide-react'
import {
  ClearHTTPHistory,
  DeleteHTTPHistory,
  ListHTTPHistory,
  SendHTTPRequest,
} from '../../../wailsjs/go/main/App'
import type { httptest } from '../../../wailsjs/go/models'
import { ToolShell } from '@/components/tool/ToolShell'
import { CodeEditor, type EditorLanguage } from '@/components/tool/CodeEditor'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { cn } from '@/lib/utils'

type KV = httptest.KV
type Response = httptest.Response
type HistoryItem = httptest.HistoryItem
type BodyMode = 'none' | 'json' | 'text' | 'form'

const METHODS = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS']

const emptyKV = (): KV => ({ key: '', value: '', disabled: false })

function newHeaderRows(initial: KV[] = []): KV[] {
  return [...initial, emptyKV()]
}

// 末尾保证留一个空行用作"新增"占位
function ensureTrailingEmpty(rows: KV[]): KV[] {
  if (rows.length === 0) return [emptyKV()]
  const last = rows[rows.length - 1]
  if (last.key || last.value) return [...rows, emptyKV()]
  return rows
}

// 从 URL 的 query 部分解析出 KV[](保留原顺序,空 key 跳过)
function parseQueryFromUrl(url: string): KV[] {
  const qIdx = url.indexOf('?')
  if (qIdx < 0) return []
  const queryStr = url.slice(qIdx + 1).split('#')[0]
  if (!queryStr) return []
  const out: KV[] = []
  for (const pair of queryStr.split('&')) {
    if (!pair) continue
    const eqIdx = pair.indexOf('=')
    let k = '', v = ''
    if (eqIdx < 0) {
      k = pair
    } else {
      k = pair.slice(0, eqIdx)
      v = pair.slice(eqIdx + 1)
    }
    try {
      k = decodeURIComponent(k)
      v = decodeURIComponent(v)
    } catch {
      // ignore decode error,保留原文
    }
    out.push({ key: k, value: v, disabled: false })
  }
  return out
}

// 用 params 重写 URL 的 query 部分,保留 path / host / hash
function buildUrlWithQuery(currentUrl: string, params: KV[]): string {
  const hashIdx = currentUrl.indexOf('#')
  const hashPart = hashIdx >= 0 ? currentUrl.slice(hashIdx) : ''
  const noHash = hashIdx >= 0 ? currentUrl.slice(0, hashIdx) : currentUrl
  const qIdx = noHash.indexOf('?')
  const base = qIdx >= 0 ? noHash.slice(0, qIdx) : noHash
  const active = params.filter((p) => p.key && !p.disabled)
  if (active.length === 0) return base + hashPart
  const qs = active
    .map((p) => `${encodeURIComponent(p.key)}=${encodeURIComponent(p.value)}`)
    .join('&')
  return base + '?' + qs + hashPart
}

export default function HttpTest() {
  const confirm = useConfirm()
  const [method, setMethod] = useState('GET')
  const [url, setUrl] = useState('')
  const [params, setParams] = useState<KV[]>(newHeaderRows())
  const [headers, setHeaders] = useState<KV[]>(newHeaderRows())
  const [bodyMode, setBodyMode] = useState<BodyMode>('none')
  const [bodyText, setBodyText] = useState('')
  const [bodyForm, setBodyForm] = useState<KV[]>(newHeaderRows())
  const [auth, setAuth] = useState<{ type: 'none' | 'bearer' | 'basic'; token: string; user: string; password: string }>(
    { type: 'none', token: '', user: '', password: '' }
  )
  const [inputTab, setInputTab] = useState<'params' | 'headers' | 'body' | 'auth'>('params')

  const [response, setResponse] = useState<Response | null>(null)
  const [sending, setSending] = useState(false)
  const [respTab, setRespTab] = useState<'body' | 'headers'>('body')
  const [history, setHistory] = useState<HistoryItem[]>([])
  const [splitRatio, setSplitRatio] = useState(0.5)
  const splitRef = useRef<HTMLDivElement>(null)

  const refreshHistory = async () => {
    const list = await ListHTTPHistory()
    setHistory(list ?? [])
  }
  useEffect(() => {
    refreshHistory()
  }, [])

  const onClear = () => {
    setMethod('GET')
    setUrl('')
    setParams(newHeaderRows())
    setHeaders(newHeaderRows())
    setBodyMode('none')
    setBodyText('')
    setBodyForm(newHeaderRows())
    setAuth({ type: 'none', token: '', user: '', password: '' })
    setResponse(null)
  }

  // URL → Params:用户改 URL 时重新解析 query 部分写到 params 表格
  const onUrlChange = (newUrl: string) => {
    setUrl(newUrl)
    const parsed = parseQueryFromUrl(newUrl)
    setParams(ensureTrailingEmpty(parsed))
  }

  // Params → URL:用户改表格时把活跃行写回 URL 的 query 部分
  const onParamsChange = (rows: KV[]) => {
    setParams(rows)
    setUrl((cur) => buildUrlWithQuery(cur, rows))
  }

  const onSend = async () => {
    if (sending) return
    if (!url.trim()) return
    setSending(true)
    try {
      // 把 auth 转成 Authorization header(用户已经手填的 Authorization 优先)
      const finalHeaders = headers.filter((h) => h.key)
      const hasAuthHeader = finalHeaders.some(
        (h) => h.key.toLowerCase() === 'authorization' && !h.disabled
      )
      if (!hasAuthHeader && auth.type !== 'none') {
        if (auth.type === 'bearer' && auth.token) {
          finalHeaders.push({ key: 'Authorization', value: `Bearer ${auth.token}` })
        } else if (auth.type === 'basic' && (auth.user || auth.password)) {
          const encoded = btoa(`${auth.user}:${auth.password}`)
          finalHeaders.push({ key: 'Authorization', value: `Basic ${encoded}` })
        }
      }
      const req = {
        method,
        url: url.trim(),
        headers: finalHeaders,
        bodyMode: bodyMode as string,
        bodyText: bodyText,
        bodyForm: bodyForm.filter((h) => h.key),
        timeoutMs: 0,
      } as unknown as httptest.Request
      const resp = await SendHTTPRequest(req)
      setResponse(resp)
      setRespTab('body')
      refreshHistory()
    } finally {
      setSending(false)
    }
  }

  const loadFromHistory = (item: HistoryItem) => {
    setMethod(item.request.method || 'GET')
    const histUrl = item.request.url || ''
    setUrl(histUrl)
    setParams(ensureTrailingEmpty(parseQueryFromUrl(histUrl)))
    setHeaders(newHeaderRows(item.request.headers || []))
    setBodyMode((item.request.bodyMode || 'none') as BodyMode)
    setBodyText(item.request.bodyText || '')
    setBodyForm(newHeaderRows(item.request.bodyForm || []))
    setAuth({ type: 'none', token: '', user: '', password: '' })
    setResponse(null)
  }

  const onDeleteHistory = async (id: string) => {
    await DeleteHTTPHistory(id)
    refreshHistory()
  }

  const onClearHistory = async () => {
    if (history.length === 0) return
    const ok = await confirm({
      title: '清空历史',
      message: '确认清空全部 HTTP 请求历史?',
      danger: true,
    })
    if (!ok) return
    await ClearHTTPHistory()
    refreshHistory()
  }

  return (
    <ToolShell
      title="HTTP 测试"
      description="发送 HTTP 请求 · 历史记录持久化在 ~/.toolforge/http-history.json"
      onClear={onClear}
    >
      <div className="grid h-full grid-cols-[220px_1fr] gap-3">
        <HistoryPanel
          items={history}
          onPick={loadFromHistory}
          onDelete={onDeleteHistory}
          onClearAll={onClearHistory}
        />
        <div className="flex min-w-0 flex-col gap-3 overflow-hidden">
          <RequestBar
            method={method}
            setMethod={setMethod}
            url={url}
            setUrl={onUrlChange}
            sending={sending}
            onSend={onSend}
          />

          <div ref={splitRef} className="flex min-h-0 flex-1 flex-col">
            <div
              style={{ flex: splitRatio }}
              className="flex min-h-0 flex-col gap-2 overflow-hidden rounded-lg border border-border bg-card"
            >
              <Tabs
                tabs={[
                  { id: 'params', label: 'Params', count: countActive(params) },
                  { id: 'headers', label: 'Headers', count: countActive(headers) },
                  { id: 'body', label: 'Body', dot: bodyMode !== 'none' },
                  { id: 'auth', label: 'Auth', dot: auth.type !== 'none' },
                ]}
                active={inputTab}
                onPick={(id) => setInputTab(id as typeof inputTab)}
              />
              <div className="flex-1 overflow-auto px-3 pb-3">
                {inputTab === 'params' && (
                  <KVEditor rows={params} onChange={onParamsChange} keyPlaceholder="参数名" placeholder="参数值" />
                )}
                {inputTab === 'headers' && (
                  <KVEditor rows={headers} onChange={setHeaders} keyPlaceholder="Header" placeholder="例如 Bearer xxx" />
                )}
                {inputTab === 'body' && (
                  <BodyEditor
                    mode={bodyMode}
                    setMode={setBodyMode}
                    text={bodyText}
                    setText={setBodyText}
                    form={bodyForm}
                    setForm={setBodyForm}
                  />
                )}
                {inputTab === 'auth' && <AuthEditor auth={auth} setAuth={setAuth} />}
              </div>
            </div>

            <VerticalSplitter ratio={splitRatio} onChange={setSplitRatio} containerRef={splitRef} />

            <div style={{ flex: 1 - splitRatio }} className="flex min-h-0 flex-col">
              <ResponsePanel
                response={response}
                sending={sending}
                tab={respTab}
                setTab={setRespTab}
              />
            </div>
          </div>
        </div>
      </div>
    </ToolShell>
  )
}

function countActive(rows: KV[]): number {
  return rows.filter((r) => r.key && !r.disabled).length
}

// ─────────────────────── 子组件 ───────────────────────

function RequestBar({
  method,
  setMethod,
  url,
  setUrl,
  sending,
  onSend,
}: {
  method: string
  setMethod: (m: string) => void
  url: string
  setUrl: (u: string) => void
  sending: boolean
  onSend: () => void
}) {
  return (
    <div className="flex items-center gap-2">
      <div className="relative">
        <select
          value={method}
          onChange={(e) => setMethod(e.target.value)}
          className={cn(
            'h-9 appearance-none rounded-md border border-input bg-background pl-3 pr-8 text-sm font-semibold outline-none focus:border-ring',
            methodColor(method)
          )}
        >
          {METHODS.map((m) => (
            <option key={m} value={m} className="font-semibold text-foreground">
              {m}
            </option>
          ))}
        </select>
        <ChevronDown className="pointer-events-none absolute right-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
      </div>
      <input
        value={url}
        onChange={(e) => setUrl(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !sending) onSend()
        }}
        placeholder="https://api.example.com/users"
        spellCheck={false}
        className="h-9 flex-1 rounded-md border border-input bg-background px-3 font-mono text-sm outline-none focus:border-ring"
      />
      <Button
        onClick={onSend}
        disabled={sending || !url.trim()}
        className="px-6 font-semibold shadow-sm shadow-primary/20"
      >
        {sending ? (
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
        ) : (
          <Send className="h-3.5 w-3.5" />
        )}
        发送
      </Button>
    </div>
  )
}

function methodColor(m: string): string {
  switch (m) {
    case 'GET':
      return 'text-emerald-600 dark:text-emerald-400'
    case 'POST':
      return 'text-amber-600 dark:text-amber-400'
    case 'PUT':
      return 'text-sky-600 dark:text-sky-400'
    case 'PATCH':
      return 'text-cyan-600 dark:text-cyan-400'
    case 'DELETE':
      return 'text-rose-600 dark:text-rose-400'
    default:
      return 'text-muted-foreground'
  }
}

function Tabs({
  tabs,
  active,
  onPick,
}: {
  tabs: { id: string; label: string; count?: number; dot?: boolean }[]
  active: string
  onPick: (id: string) => void
}) {
  return (
    <div className="flex items-center gap-1 border-b border-border/60 px-2 pt-1">
      {tabs.map((t) => (
        <button
          key={t.id}
          onClick={() => onPick(t.id)}
          className={cn(
            'group/tab relative inline-flex h-8 items-center gap-1.5 rounded-t-md px-3 text-xs font-medium transition-colors',
            active === t.id
              ? 'bg-secondary/50 text-foreground'
              : 'text-muted-foreground hover:text-foreground'
          )}
        >
          <span>{t.label}</span>
          {t.count !== undefined && t.count > 0 && (
            <span
              className={cn(
                'inline-flex h-4 min-w-[16px] items-center justify-center rounded-full px-1 font-mono text-[10px] font-semibold',
                active === t.id
                  ? 'bg-primary/15 text-primary'
                  : 'bg-secondary text-muted-foreground'
              )}
            >
              {t.count}
            </span>
          )}
          {t.dot && (
            <span className="ml-0.5 h-1.5 w-1.5 rounded-full bg-primary/80" />
          )}
        </button>
      ))}
    </div>
  )
}

function KVEditor({
  rows,
  onChange,
  placeholder,
  keyPlaceholder = '参数名',
}: {
  rows: KV[]
  onChange: (rows: KV[]) => void
  placeholder?: string
  keyPlaceholder?: string
}) {
  const update = (i: number, patch: Partial<KV>) => {
    const next = rows.slice()
    next[i] = { ...next[i], ...patch }
    // 末尾自动加新行
    if (i === rows.length - 1 && (patch.key || patch.value)) {
      next.push(emptyKV())
    }
    onChange(next)
  }
  const remove = (i: number) => {
    const next = rows.slice()
    next.splice(i, 1)
    if (next.length === 0) next.push(emptyKV())
    onChange(next)
  }
  return (
    <div className="overflow-hidden rounded-md border border-border/60">
      {/* 表头 */}
      <div className="flex items-center gap-2 border-b border-border/60 bg-secondary/30 px-2 py-1.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
        <span className="w-4 shrink-0" />
        <span className="w-40 shrink-0">参数名</span>
        <span className="flex-1">参数值</span>
        <span className="w-7 shrink-0" />
      </div>
      {rows.map((row, i) => {
        const hasContent = row.key !== '' || row.value !== ''
        return (
          <div
            key={i}
            className={cn(
              'flex items-center gap-2 border-b border-border/40 px-2 py-1 last:border-b-0',
              row.disabled && 'opacity-50'
            )}
          >
            <input
              type="checkbox"
              checked={!row.disabled && hasContent}
              disabled={!hasContent}
              onChange={(e) => update(i, { disabled: !e.target.checked })}
              className="h-3.5 w-3.5 shrink-0 cursor-pointer accent-info"
              title={row.disabled ? '已禁用,点击启用' : '点击禁用此项'}
            />
            <input
              value={row.key}
              onChange={(e) => update(i, { key: e.target.value })}
              placeholder={keyPlaceholder}
              spellCheck={false}
              className="h-7 w-40 shrink-0 rounded border-0 bg-transparent px-2 font-mono text-xs outline-none focus:bg-background focus:ring-1 focus:ring-ring"
            />
            <input
              value={row.value}
              onChange={(e) => update(i, { value: e.target.value })}
              placeholder={placeholder ?? '参数值'}
              spellCheck={false}
              className="h-7 flex-1 rounded border-0 bg-transparent px-2 font-mono text-xs outline-none focus:bg-background focus:ring-1 focus:ring-ring"
            />
            <button
              type="button"
              onClick={() => remove(i)}
              className="flex h-7 w-7 shrink-0 items-center justify-center rounded text-muted-foreground/60 hover:bg-secondary hover:text-foreground"
              title="删除"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        )
      })}
    </div>
  )
}

function BodyEditor({
  mode,
  setMode,
  text,
  setText,
  form,
  setForm,
}: {
  mode: BodyMode
  setMode: (m: BodyMode) => void
  text: string
  setText: (t: string) => void
  form: KV[]
  setForm: (rows: KV[]) => void
}) {
  const onPretty = () => {
    if (mode !== 'json') return
    try {
      const parsed = JSON.parse(text)
      setText(JSON.stringify(parsed, null, 2))
    } catch {
      // ignore parse errors
    }
  }
  const labelMap: Record<BodyMode, string> = {
    none: 'none',
    json: 'JSON',
    text: 'Text',
    form: 'x-www-form-urlencoded',
  }
  const ctMap: Record<BodyMode, string> = {
    none: '',
    json: 'application/json',
    text: 'text/plain',
    form: 'application/x-www-form-urlencoded',
  }
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-1">
        {(['none', 'json', 'text', 'form'] as BodyMode[]).map((m) => (
          <button
            key={m}
            onClick={() => setMode(m)}
            className={cn(
              'rounded-full px-3 py-1 text-xs font-medium transition-all',
              mode === m
                ? 'bg-primary text-primary-foreground shadow-sm'
                : 'text-muted-foreground hover:bg-secondary hover:text-foreground'
            )}
          >
            {labelMap[m]}
          </button>
        ))}
        {ctMap[mode] && (
          <span className="ml-2 font-mono text-[10px] text-muted-foreground">
            {ctMap[mode]}
          </span>
        )}
        {mode === 'json' && (
          <Button size="sm" variant="ghost" onClick={onPretty} className="ml-auto">
            <Wand2 className="h-3.5 w-3.5" />
            格式化
          </Button>
        )}
        {mode === 'form' && (
          <Button size="sm" variant="ghost" onClick={() => setForm(newHeaderRows())} className="ml-auto">
            <Plus className="h-3.5 w-3.5" />
            重置
          </Button>
        )}
      </div>
      {mode === 'none' && (
        <div className="rounded border border-dashed border-border p-4 text-center text-xs text-muted-foreground">
          此请求没有 body
        </div>
      )}
      {(mode === 'json' || mode === 'text') && (
        <CodeEditor
          value={text}
          onChange={setText}
          language={mode === 'json' ? 'json' : 'plaintext'}
          placeholder={mode === 'json' ? '{ "key": "value" }' : '请求体内容'}
          minHeight="180px"
        />
      )}
      {mode === 'form' && (
        <KVEditor rows={form} onChange={setForm} placeholder="表单 value" />
      )}
    </div>
  )
}

function VerticalSplitter({
  ratio,
  onChange,
  containerRef,
}: {
  ratio: number
  onChange: (r: number) => void
  containerRef: React.RefObject<HTMLDivElement>
}) {
  const dragRef = useRef<{ startY: number; startRatio: number; height: number } | null>(null)
  const onMouseDown = (e: React.MouseEvent) => {
    e.preventDefault()
    const c = containerRef.current
    if (!c) return
    const rect = c.getBoundingClientRect()
    dragRef.current = { startY: e.clientY, startRatio: ratio, height: rect.height }
    const onMove = (e2: MouseEvent) => {
      const d = dragRef.current
      if (!d || d.height <= 0) return
      const dy = e2.clientY - d.startY
      const r = d.startRatio + dy / d.height
      onChange(Math.max(0.15, Math.min(0.85, r)))
    }
    const onUp = () => {
      dragRef.current = null
      document.removeEventListener('mousemove', onMove)
      document.removeEventListener('mouseup', onUp)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }
    document.addEventListener('mousemove', onMove)
    document.addEventListener('mouseup', onUp)
    document.body.style.cursor = 'row-resize'
    document.body.style.userSelect = 'none'
  }
  return (
    <div
      onMouseDown={onMouseDown}
      className="group/spl flex h-2.5 shrink-0 cursor-row-resize items-center justify-center"
      title="拖拽调整请求 / 响应区域比例"
    >
      <div className="h-0.5 w-12 rounded-full bg-border transition-colors group-hover/spl:bg-primary/60" />
    </div>
  )
}

type AuthState = { type: 'none' | 'bearer' | 'basic'; token: string; user: string; password: string }

function AuthEditor({
  auth,
  setAuth,
}: {
  auth: AuthState
  setAuth: (a: AuthState) => void
}) {
  const set = (patch: Partial<AuthState>) => setAuth({ ...auth, ...patch })
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-1">
        {(['none', 'bearer', 'basic'] as const).map((t) => (
          <button
            key={t}
            onClick={() => set({ type: t })}
            className={cn(
              'rounded-full px-3 py-1 text-xs font-medium transition-all',
              auth.type === t
                ? 'bg-primary text-primary-foreground shadow-sm'
                : 'text-muted-foreground hover:bg-secondary hover:text-foreground'
            )}
          >
            {t === 'none' ? 'No Auth' : t === 'bearer' ? 'Bearer Token' : 'Basic Auth'}
          </button>
        ))}
      </div>
      {auth.type === 'bearer' && (
        <div className="space-y-1.5">
          <div className="text-[11px] text-muted-foreground">Token</div>
          <input
            value={auth.token}
            onChange={(e) => set({ token: e.target.value })}
            placeholder="Bearer 后面的 token,发送时自动注入到 Authorization header"
            spellCheck={false}
            className="h-8 w-full rounded border border-input bg-background px-2 font-mono text-xs outline-none focus:border-ring"
          />
        </div>
      )}
      {auth.type === 'basic' && (
        <div className="grid grid-cols-2 gap-3">
          <div className="space-y-1.5">
            <div className="text-[11px] text-muted-foreground">Username</div>
            <input
              value={auth.user}
              onChange={(e) => set({ user: e.target.value })}
              spellCheck={false}
              className="h-8 w-full rounded border border-input bg-background px-2 font-mono text-xs outline-none focus:border-ring"
            />
          </div>
          <div className="space-y-1.5">
            <div className="text-[11px] text-muted-foreground">Password</div>
            <input
              type="password"
              value={auth.password}
              onChange={(e) => set({ password: e.target.value })}
              className="h-8 w-full rounded border border-input bg-background px-2 font-mono text-xs outline-none focus:border-ring"
            />
          </div>
        </div>
      )}
      {auth.type === 'none' && (
        <div className="rounded border border-dashed border-border p-4 text-center text-xs text-muted-foreground">
          未启用鉴权。如果需要,可以在 Headers tab 里手动加 Authorization。
        </div>
      )}
    </div>
  )
}

function ResponsePanel({
  response,
  sending,
  tab,
  setTab,
}: {
  response: Response | null
  sending: boolean
  tab: 'body' | 'headers'
  setTab: (t: 'body' | 'headers') => void
}) {
  if (!response && !sending) {
    return (
      <div className="rounded-lg border border-dashed border-border bg-card/40 p-8 text-center text-sm text-muted-foreground">
        点击右上"发送",这里会显示响应。
      </div>
    )
  }
  if (sending && !response) {
    return (
      <div className="flex items-center gap-2 rounded-lg border border-border bg-card p-6 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        正在发送...
      </div>
    )
  }
  if (!response) return null

  return (
    <div className="flex min-h-0 flex-1 flex-col rounded-lg border border-border bg-card">
      <div className="flex flex-wrap items-center gap-2 border-b border-border/60 px-3 py-2 text-xs">
        {response.error ? (
          <span className="inline-flex items-center gap-1 rounded bg-red-500/15 px-2 py-0.5 font-mono text-[11px] text-red-600 dark:text-red-400">
            <AlertCircle className="h-3 w-3" />
            请求失败
          </span>
        ) : (
          <>
            <StatusBadge code={response.statusCode} />
            <span className="text-muted-foreground">{response.statusText}</span>
          </>
        )}
        {!response.error && (
          <>
            <span className="inline-flex items-center gap-1 text-muted-foreground">
              <Clock className="h-3 w-3" />
              {response.durationMs} ms
            </span>
            <span className="text-muted-foreground">{formatSize(response.sizeBytes)}</span>
            {response.contentType && (
              <span className="truncate font-mono text-[10px] text-muted-foreground">
                {response.contentType}
              </span>
            )}
          </>
        )}
      </div>
      {response.error ? (
        <div className="p-4 font-mono text-xs text-red-600 dark:text-red-400">
          {response.error}
        </div>
      ) : (
        <>
          <Tabs
            tabs={[
              { id: 'body', label: 'Body' },
              { id: 'headers', label: `Headers (${(response.headers ?? []).length})` },
            ]}
            active={tab}
            onPick={(id) => setTab(id as 'body' | 'headers')}
          />
          <div className="min-h-0 flex-1 overflow-auto p-3">
            {tab === 'body' ? (
              <ResponseBody response={response} />
            ) : (
              <ResponseHeaders headers={response.headers ?? []} />
            )}
          </div>
        </>
      )}
    </div>
  )
}

function ResponseBody({ response }: { response: Response }) {
  const [pretty, setPretty] = useState(true)
  const lang = guessLang(response.contentType)
  const text = useMemo(() => {
    if (!pretty || lang !== 'json') return response.bodyText
    try {
      const obj = JSON.parse(response.bodyText)
      return JSON.stringify(obj, null, 2)
    } catch {
      return response.bodyText
    }
  }, [response.bodyText, pretty, lang])

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(text)
    } catch {
      // ignore
    }
  }
  if (response.isBinary) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        {response.bodyText}
      </div>
    )
  }
  return (
    <div className="flex h-full min-h-[160px] flex-col gap-2">
      <div className="flex items-center gap-2">
        {lang === 'json' && (
          <button
            onClick={() => setPretty((v) => !v)}
            className="rounded px-2 py-0.5 text-[11px] text-muted-foreground hover:bg-secondary"
          >
            {pretty ? '原始' : '美化'}
          </button>
        )}
        <Button size="sm" variant="ghost" onClick={onCopy} className="ml-auto">
          <Copy className="h-3.5 w-3.5" />
          复制
        </Button>
      </div>
      <CodeEditor value={text} readOnly language={lang} minHeight="100%" className="flex-1" />
    </div>
  )
}

function ResponseHeaders({ headers }: { headers: KV[] }) {
  if (headers.length === 0) {
    return <div className="text-xs italic text-muted-foreground">(无)</div>
  }
  return (
    <div className="space-y-1 font-mono text-xs">
      {headers.map((h, i) => (
        <div key={i} className="flex gap-3">
          <span className="w-44 shrink-0 text-muted-foreground">{h.key}</span>
          <span className="break-all">{h.value}</span>
        </div>
      ))}
    </div>
  )
}

function HistoryPanel({
  items,
  onPick,
  onDelete,
  onClearAll,
}: {
  items: HistoryItem[]
  onPick: (item: HistoryItem) => void
  onDelete: (id: string) => void
  onClearAll: () => void
}) {
  return (
    <div className="flex h-full min-h-0 flex-col rounded-lg border border-border bg-card">
      <div className="flex items-center justify-between gap-2 border-b border-border/60 px-3 py-2">
        <div className="inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
          <HistoryIcon className="h-3.5 w-3.5" />
          历史 ({items.length})
        </div>
        {items.length > 0 && (
          <button
            onClick={onClearAll}
            className="text-[11px] text-muted-foreground hover:text-foreground"
            title="清空所有历史"
          >
            <Trash2 className="h-3 w-3" />
          </button>
        )}
      </div>
      <div className="flex-1 overflow-y-auto p-1">
        {items.length === 0 ? (
          <div className="px-3 py-6 text-center text-[11px] text-muted-foreground">
            尚无历史记录
          </div>
        ) : (
          items.map((it) => <HistoryRow key={it.id} item={it} onPick={onPick} onDelete={onDelete} />)
        )}
      </div>
    </div>
  )
}

function HistoryRow({
  item,
  onPick,
  onDelete,
}: {
  item: HistoryItem
  onPick: (item: HistoryItem) => void
  onDelete: (id: string) => void
}) {
  const tooltip = `${item.request.method} ${item.request.url}\n${
    item.error ? '失败: ' + item.error : `${item.statusCode} · ${item.durationMs}ms · ${formatSize(item.sizeBytes)}`
  }`
  const ageStr = relativeTime(item.savedAt)
  return (
    <div
      onClick={() => onPick(item)}
      className="group/hist cursor-pointer rounded px-2 py-1.5 hover:bg-accent"
      title={tooltip}
    >
      <div className="flex items-center gap-1.5">
        <span className={cn('shrink-0 font-mono text-[10px] font-semibold', methodColor(item.request.method))}>
          {item.request.method}
        </span>
        <span className="min-w-0 flex-1 truncate font-mono text-[11px]">{item.request.url}</span>
        <button
          onClick={(e) => {
            e.stopPropagation()
            onDelete(item.id)
          }}
          className="opacity-0 transition-opacity group-hover/hist:opacity-60 hover:!opacity-100"
        >
          <X className="h-3 w-3" />
        </button>
      </div>
      <div className="ml-10 flex items-center gap-1.5 text-[10px] text-muted-foreground">
        {item.error ? (
          <span className="text-red-500/80">失败</span>
        ) : (
          <span className={statusColor(item.statusCode)}>{item.statusCode || '—'}</span>
        )}
        <span>·</span>
        <span>{ageStr}</span>
      </div>
    </div>
  )
}

function StatusBadge({ code }: { code: number }) {
  const c =
    code >= 200 && code < 300
      ? 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-300'
      : code >= 300 && code < 400
      ? 'bg-sky-500/15 text-sky-600 dark:text-sky-300'
      : code >= 400 && code < 500
      ? 'bg-amber-500/15 text-amber-600 dark:text-amber-300'
      : code >= 500
      ? 'bg-red-500/15 text-red-600 dark:text-red-400'
      : 'bg-secondary text-muted-foreground'
  return <span className={cn('rounded px-2 py-0.5 font-mono text-[11px] font-semibold', c)}>{code || '—'}</span>
}

function statusColor(code: number): string {
  if (code >= 200 && code < 300) return 'text-emerald-500/80'
  if (code >= 300 && code < 400) return 'text-sky-500/80'
  if (code >= 400 && code < 500) return 'text-amber-500/80'
  if (code >= 500) return 'text-red-500/80'
  return ''
}

function formatSize(n: number): string {
  if (n <= 0) return '0 B'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(2)} MB`
}

function relativeTime(ts: number): string {
  const diff = (Date.now() - ts) / 1000
  if (diff < 60) return '刚刚'
  if (diff < 3600) return `${Math.floor(diff / 60)}分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)}小时前`
  return new Date(ts).toLocaleDateString()
}

function guessLang(contentType: string): EditorLanguage {
  const ct = contentType.toLowerCase()
  if (ct.includes('json')) return 'json'
  if (ct.includes('xml') || ct.includes('html')) return 'xml'
  if (ct.includes('yaml')) return 'yaml'
  if (ct.includes('javascript')) return 'javascript'
  return 'plaintext'
}
