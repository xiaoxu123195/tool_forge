import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  AlertTriangle,
  FileText,
  History,
  Loader2,
  Play,
  Plug,
  RefreshCw,
  ShieldAlert,
  Unplug,
  Wrench,
} from 'lucide-react'
import { ToolShell } from '@/components/tool/ToolShell'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import {
  CallMCPToolRaw,
  DisconnectMCPWorkbench,
  GetMCPPrompt,
  InspectMCPServer,
  ListMCPServers,
  ReadMCPResource,
} from '../../../wailsjs/go/main/App'
import type { mcp } from '../../../wailsjs/go/models'
import { meta } from './meta'
import { ServerPicker, targetOf } from './ServerPicker'
import { SchemaForm } from './SchemaForm'
import { ResponsePane } from './ResponsePane'
import { buildArgs, fieldsOf, initialValues, suspiciousSpans, type Field } from './schema'

/**
 * MCP 工作台 —— 连上一个服务器，照着它自己给的 schema 填参数，调一次，看原始报文。
 *
 * 为什么值得单独做一页：MCP 出问题的时候，你在 AI 那头看到的只有"工具调用失败"，
 * 而真正的信息（发出去的参数长什么样、服务器回了什么错误码、content 里到底有什么）
 * 全被夹在中间的那层吞掉了。这一页把那层拆开。
 */

type Kind = 'tools' | 'prompts' | 'resources'

interface HistoryItem {
  server: string
  target: string
  kind: Kind
  name: string
  result: mcp.CallResult
}

const MAX_HISTORY = 50

export default function MCPWorkbench() {
  const [servers, setServers] = useState<mcp.Server[]>([])
  const [current, setCurrent] = useState<mcp.Server | null>(null)
  const [inspect, setInspect] = useState<mcp.InspectResult | null>(null)
  const [connecting, setConnecting] = useState(false)
  const [connectError, setConnectError] = useState('')

  const [kind, setKind] = useState<Kind>('tools')
  const [picked, setPicked] = useState('')
  const [values, setValues] = useState<Record<string, string>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [calling, setCalling] = useState(false)
  const [result, setResult] = useState<mcp.CallResult | null>(null)
  const [history, setHistory] = useState<HistoryItem[]>([])
  const [historyOpen, setHistoryOpen] = useState(false)

  useEffect(() => {
    ListMCPServers()
      .then((list) => setServers(list ?? []))
      .catch(() => setServers([]))
  }, [])

  const connect = useCallback(async (s: mcp.Server) => {
    setCurrent(s)
    setConnecting(true)
    setConnectError('')
    setInspect(null)
    setPicked('')
    setResult(null)
    try {
      const r = await InspectMCPServer(s)
      setInspect(r)
      // 落在有东西的那个页签上 —— 只实现了 tools 的服务器占大多数,
      // 但停在空的 prompts 页会让人以为没连上
      setKind(r.tools.length > 0 ? 'tools' : r.prompts.length > 0 ? 'prompts' : 'resources')
    } catch (e) {
      setConnectError(e instanceof Error ? e.message : String(e))
    } finally {
      setConnecting(false)
    }
  }, [])

  const disconnect = async () => {
    if (current) await DisconnectMCPWorkbench(current).catch(() => {})
    setInspect(null)
    setPicked('')
    setResult(null)
    setConnectError('')
  }

  // 当前选中的那一项,以及它对应的表单字段
  const fields: Field[] = useMemo(() => {
    if (!inspect || !picked) return []
    if (kind === 'tools') {
      return fieldsOf(inspect.tools.find((t) => t.name === picked)?.inputSchema)
    }
    if (kind === 'prompts') {
      const p = inspect.prompts.find((x) => x.name === picked)
      // 提示词的参数是一个扁平数组,不是 JSON Schema。转成同一种形状,共用表单
      return (p?.arguments ?? []).map((a) => ({
        name: a.name,
        kind: 'string' as const,
        required: a.required,
        description: a.description ?? '',
        options: [],
        initial: '',
        raw: a,
      }))
    }
    return []
  }, [inspect, picked, kind])

  useEffect(() => {
    setValues(initialValues(fields))
    setErrors({})
  }, [fields])

  const run = async () => {
    if (!current || !picked) return
    const { args, errors: errs } = buildArgs(fields, values)
    setErrors(errs)
    if (Object.keys(errs).length > 0) return

    setCalling(true)
    try {
      const r =
        kind === 'tools'
          ? await CallMCPToolRaw(current, picked, args)
          : kind === 'prompts'
            ? await GetMCPPrompt(current, picked, args)
            : await ReadMCPResource(current, picked)
      setResult(r)
      setHistory((h) =>
        [{ server: current.name, target: targetOf(current), kind, name: picked, result: r }, ...h].slice(
          0,
          MAX_HISTORY,
        ),
      )
    } catch (e) {
      // 这里只会是"连都连不上"——协议层的失败在 CallResult 里,不抛
      setConnectError(e instanceof Error ? e.message : String(e))
    } finally {
      setCalling(false)
    }
  }

  const counts = {
    tools: inspect?.tools.length ?? 0,
    prompts: inspect?.prompts.length ?? 0,
    resources: inspect?.resources.length ?? 0,
  }

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      actions={
        <div className="flex items-center gap-1.5">
          {inspect && (
            <>
              <span className="mr-1 flex items-center gap-1.5 text-[11px] text-muted-foreground">
                <span className="rounded-sm bg-emerald-200/60 px-1.5 py-0.5 font-medium text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300">
                  已连接
                </span>
                <span className="max-w-[220px] truncate">{inspect.serverInfo || '(服务器没报名字)'}</span>
                {inspect.protocolVersion && (
                  <span className="font-mono opacity-70">{inspect.protocolVersion}</span>
                )}
              </span>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => current && void connect(current)}
                disabled={connecting}
              >
                <RefreshCw className={cn('h-3.5 w-3.5', connecting && 'animate-spin')} />
                重新拉取
              </Button>
              <Button variant="outline" size="sm" onClick={() => void disconnect()}>
                <Unplug className="h-3.5 w-3.5" />
                断开
              </Button>
            </>
          )}
          <Button
            variant={historyOpen ? 'secondary' : 'ghost'}
            size="sm"
            title="调用历史"
            onClick={() => setHistoryOpen((v) => !v)}
          >
            <History className="h-3.5 w-3.5" />
            历史 {history.length > 0 && `(${history.length})`}
          </Button>
        </div>
      }
    >
      <div className="grid h-full min-h-0 grid-cols-[minmax(200px,1fr)_minmax(260px,1.3fr)_minmax(300px,1.6fr)] gap-2">
        {/* 左:服务器 + 条目 */}
        <div className="flex min-h-0 flex-col gap-2 overflow-auto">
          <ServerPicker
            servers={servers}
            current={current}
            busy={connecting}
            onPick={(s) => void connect(s)}
            onAdHoc={(s) => void connect(s)}
          />

          {connecting && (
            <p className="flex items-center gap-1.5 px-1 text-[11px] text-muted-foreground">
              <Loader2 className="h-3 w-3 animate-spin" />
              连接中… stdio 的服务器第一次可能要下依赖，慢一点正常
            </p>
          )}
          {connectError && (
            <p className="whitespace-pre-wrap break-words rounded-md border border-destructive/30 bg-destructive/10 px-2 py-1.5 text-[11px] text-destructive">
              {connectError}
            </p>
          )}
          {inspect?.warnings.map((w) => (
            <p
              key={w}
              className="rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-1.5 text-[11px] text-amber-700 dark:text-amber-400"
            >
              {w}
            </p>
          ))}

          {inspect && (
            <>
              <div className="flex gap-1 px-1">
                {(
                  [
                    ['tools', '工具', Wrench],
                    ['prompts', '提示词', FileText],
                    ['resources', '资源', Plug],
                  ] as const
                ).map(([k, label, Icon]) => (
                  <button
                    key={k}
                    type="button"
                    title={`切到：${label}`}
                    onClick={() => {
                      setKind(k)
                      setPicked('')
                    }}
                    className={cn(
                      'flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] transition-colors',
                      kind === k
                        ? 'bg-secondary font-medium text-foreground'
                        : 'text-muted-foreground hover:bg-secondary/60',
                    )}
                  >
                    <Icon className="h-3 w-3" />
                    {label} {counts[k]}
                  </button>
                ))}
              </div>
              <ItemList
                kind={kind}
                inspect={inspect}
                picked={picked}
                onPick={(n) => {
                  setPicked(n)
                  setResult(null)
                }}
              />
            </>
          )}
        </div>

        {/* 中:参数 */}
        <div className="flex min-h-0 flex-col overflow-hidden rounded-lg border border-border bg-card">
          {!picked ? (
            <div className="flex h-full items-center justify-center px-4 text-center text-[11px] text-muted-foreground">
              {inspect ? '左边挑一个，参数表单会照它的 schema 生成。' : '先连一个 MCP 服务器。'}
            </div>
          ) : (
            <>
              <div className="shrink-0 border-b border-border px-3 py-2">
                <div className="font-mono text-xs font-medium">{picked}</div>
                <Detail kind={kind} inspect={inspect} name={picked} />
              </div>
              <div className="min-h-0 flex-1 overflow-auto p-3">
                {kind === 'resources' ? (
                  <p className="rounded-md border border-dashed border-border px-3 py-4 text-center text-[11px] text-muted-foreground">
                    资源按 URI 读取，不需要参数。
                  </p>
                ) : (
                  <SchemaForm
                    fields={fields}
                    values={values}
                    errors={errors}
                    onChange={(n, v) => setValues((p) => ({ ...p, [n]: v }))}
                  />
                )}
              </div>
              <div className="flex shrink-0 items-center gap-2 border-t border-border bg-muted/30 px-3 py-2">
                <span className="text-[11px] text-muted-foreground">
                  {kind === 'tools' ? 'tools/call' : kind === 'prompts' ? 'prompts/get' : 'resources/read'}
                </span>
                <Button size="sm" className="ml-auto" onClick={() => void run()} disabled={calling}>
                  {calling ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Play className="h-3.5 w-3.5" />
                  )}
                  调用
                </Button>
              </div>
            </>
          )}
        </div>

        {/* 右:结果或历史 */}
        <div className="min-h-0 overflow-hidden rounded-lg border border-border bg-card">
          {historyOpen ? (
            <HistoryPane items={history} onPick={(h) => setResult(h.result)} onClear={() => setHistory([])} />
          ) : (
            <ResponsePane result={result} />
          )}
        </div>
      </div>
    </ToolShell>
  )
}

function ItemList({
  kind,
  inspect,
  picked,
  onPick,
}: {
  kind: Kind
  inspect: mcp.InspectResult
  picked: string
  onPick: (name: string) => void
}) {
  const rows: { name: string; sub: string; warn: string[] }[] =
    kind === 'tools'
      ? inspect.tools.map((t) => ({
          name: t.name,
          sub: t.description || '',
          warn: suspiciousSpans(t.description || ''),
        }))
      : kind === 'prompts'
        ? inspect.prompts.map((p) => ({
            name: p.name,
            sub: p.description || '',
            warn: suspiciousSpans(p.description || ''),
          }))
        : inspect.resources.map((r) => ({
            name: r.uri,
            sub: [r.name, r.mimeType].filter(Boolean).join(' · '),
            warn: [],
          }))

  if (rows.length === 0) {
    return (
      <p className="px-1 text-[11px] text-muted-foreground/80">这个服务器没有提供这一类。</p>
    )
  }
  return (
    <ul className="space-y-1">
      {rows.map((r) => (
        <li key={r.name}>
          <button
            type="button"
            onClick={() => onPick(r.name)}
            title={r.name}
            className={cn(
              'w-full rounded-md border px-2 py-1.5 text-left transition-colors',
              picked === r.name ? 'border-info/50 bg-info/10' : 'border-border bg-card hover:bg-accent/60',
            )}
          >
            <div className="flex items-center gap-1.5">
              <span className="truncate font-mono text-xs">{r.name}</span>
              {r.warn.length > 0 && (
                <span
                  title={`描述里有可疑内容：${r.warn.join('、')}。工具描述会直接进模型上下文，值得看一眼`}
                  className="ml-auto shrink-0 text-amber-600 dark:text-amber-400"
                >
                  <ShieldAlert className="h-3.5 w-3.5" />
                </span>
              )}
            </div>
            {r.sub && (
              <div className="mt-0.5 line-clamp-2 text-[10px] leading-snug text-muted-foreground">
                {r.sub}
              </div>
            )}
          </button>
        </li>
      ))}
    </ul>
  )
}

function Detail({
  kind,
  inspect,
  name,
}: {
  kind: Kind
  inspect: mcp.InspectResult | null
  name: string
}) {
  if (!inspect) return null
  const desc =
    kind === 'tools'
      ? inspect.tools.find((t) => t.name === name)?.description
      : kind === 'prompts'
        ? inspect.prompts.find((p) => p.name === name)?.description
        : inspect.resources.find((r) => r.uri === name)?.description
  const warn = suspiciousSpans(desc || '')
  return (
    <>
      {desc && <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">{desc}</p>}
      {warn.length > 0 && (
        <p className="mt-1 flex items-start gap-1 text-[11px] text-amber-700 dark:text-amber-400">
          <AlertTriangle className="mt-0.5 h-3 w-3 shrink-0" />
          <span>
            描述里有可疑内容（{warn.join('、')}）。工具描述会原样进模型上下文，
            恶意服务器可以借它给模型下指令。
          </span>
        </p>
      )}
    </>
  )
}

function HistoryPane({
  items,
  onPick,
  onClear,
}: {
  items: HistoryItem[]
  onPick: (h: HistoryItem) => void
  onClear: () => void
}) {
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b border-border px-3 py-1.5 text-[11px]">
        <span className="font-medium">调用历史</span>
        <span className="text-muted-foreground">本次会话内，最多 {MAX_HISTORY} 条</span>
        <button
          type="button"
          onClick={onClear}
          disabled={items.length === 0}
          className="ml-auto rounded px-1.5 py-0.5 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:opacity-40"
        >
          清空
        </button>
      </div>
      {items.length === 0 ? (
        <div className="flex h-full items-center justify-center px-4 text-center text-[11px] text-muted-foreground">
          还没有调用过。
        </div>
      ) : (
        <ul className="min-h-0 flex-1 overflow-auto text-[11px]">
          {items.map((h, i) => (
            <li key={i} className="border-b border-border/40 last:border-0">
              <button
                type="button"
                onClick={() => onPick(h)}
                title={`${h.target}\n点击查看这次的报文`}
                className="flex w-full items-center gap-2 px-3 py-1.5 text-left transition-colors hover:bg-accent/60"
              >
                <span
                  className={cn(
                    'w-8 shrink-0 rounded-sm px-1 text-center text-[9px] font-medium',
                    h.result.error
                      ? 'bg-destructive/15 text-destructive'
                      : h.result.isError
                        ? 'bg-amber-500/15 text-amber-700 dark:text-amber-400'
                        : 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400',
                  )}
                >
                  {h.result.error ? '失败' : h.result.isError ? '异常' : 'OK'}
                </span>
                <span className="min-w-0 flex-1 truncate font-mono">{h.name}</span>
                <span className="shrink-0 truncate text-muted-foreground">{h.server}</span>
                <span className="shrink-0 tabular-nums text-muted-foreground">
                  {h.result.durationMs} ms
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
