import { useState } from 'react'
import { createPortal } from 'react-dom'
import { Loader2, X } from 'lucide-react'
import { cn } from '@/lib/utils'

export interface MCPServer {
  id: string
  name: string
  kind: 'stdio' | 'http'
  enabled: boolean
  command?: string
  args?: string[]
  env?: Record<string, string>
  url?: string
  headers?: Record<string, string>
}

interface TestResult {
  ok: boolean
  message?: string
  serverInfo?: string
  tools?: string[]
  durationMs: number
}

/**
 * 新增 / 编辑一个 MCP 服务器。
 *
 * 命令行和环境变量都用多行文本输入而不是键值对表格:MCP 服务器的启动命令通常是从
 * README 里整行复制过来的,拆成表格反而要用户自己手动分词。
 */
export function MCPServerDialog({
  initial,
  onClose,
  onSave,
  onTest,
}: {
  initial: MCPServer
  onClose: () => void
  onSave: (s: MCPServer) => void
  onTest: (s: MCPServer) => Promise<TestResult>
}) {
  const [name, setName] = useState(initial.name)
  const [kind, setKind] = useState<'stdio' | 'http'>(initial.kind)
  const [commandLine, setCommandLine] = useState(
    [initial.command ?? '', ...(initial.args ?? [])].join(' ').trim(),
  )
  const [envText, setEnvText] = useState(kvToText(initial.env))
  const [url, setUrl] = useState(initial.url ?? '')
  const [headersText, setHeadersText] = useState(kvToText(initial.headers))
  const [testing, setTesting] = useState(false)
  const [result, setResult] = useState<TestResult | null>(null)

  const build = (): MCPServer => {
    const parts = splitCommand(commandLine)
    return {
      ...initial,
      name: name.trim(),
      kind,
      command: parts[0] ?? '',
      args: parts.slice(1),
      env: textToKV(envText),
      url: url.trim(),
      headers: textToKV(headersText),
    }
  }

  const valid =
    name.trim() !== '' && (kind === 'stdio' ? commandLine.trim() !== '' : url.trim() !== '')

  const runTest = async () => {
    setTesting(true)
    setResult(null)
    try {
      setResult(await onTest(build()))
    } catch (e) {
      setResult({ ok: false, message: String(e), durationMs: 0 })
    } finally {
      setTesting(false)
    }
  }

  return createPortal(
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center bg-black/40 p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div className="flex max-h-[85vh] w-[640px] max-w-full flex-col overflow-hidden rounded-xl border border-border bg-card shadow-2xl">
        <header className="flex h-12 shrink-0 items-center justify-between border-b border-border px-4">
          <h3 className="text-sm font-semibold">
            {initial.id ? '编辑 MCP 服务器' : '添加 MCP 服务器'}
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
            <label className="text-xs font-medium">名称</label>
            <input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="filesystem"
              className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring"
            />
            <p className="text-[11px] text-muted-foreground">
              会作为工具名前缀,避免不同服务器的同名工具打架。非字母数字的字符会被替换成下划线。
            </p>
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium">连接方式</label>
            <div className="flex gap-2">
              {(['stdio', 'http'] as const).map((k) => (
                <button
                  key={k}
                  type="button"
                  onClick={() => setKind(k)}
                  className={cn(
                    'h-7 rounded-md border px-3 text-xs transition-colors',
                    kind === k
                      ? 'border-info/50 bg-info/10 text-info'
                      : 'border-input text-muted-foreground hover:bg-secondary',
                  )}
                >
                  {k === 'stdio' ? '本地进程 (stdio)' : '远程 HTTP'}
                </button>
              ))}
            </div>
          </div>

          {kind === 'stdio' ? (
            <>
              <div className="space-y-1.5">
                <label className="text-xs font-medium">启动命令</label>
                <textarea
                  value={commandLine}
                  onChange={(e) => setCommandLine(e.target.value)}
                  rows={2}
                  placeholder="npx -y @modelcontextprotocol/server-filesystem D:\\my_project"
                  className="w-full resize-none rounded-md border border-input bg-background px-3 py-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
                />
                <p className="text-[11px] text-muted-foreground">
                  整行从服务器的 README 里复制过来即可。含空格的参数用双引号括起来。
                </p>
              </div>
              <KVField
                label="环境变量"
                hint="一行一条,形如 API_KEY=xxx。会与当前进程的环境合并。"
                value={envText}
                onChange={setEnvText}
              />
            </>
          ) : (
            <>
              <div className="space-y-1.5">
                <label className="text-xs font-medium">服务器地址</label>
                <input
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  placeholder="https://example.com/mcp"
                  className="h-9 w-full rounded-md border border-input bg-background px-3 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
                />
                <p className="text-[11px] text-muted-foreground">
                  仅支持 Streamable HTTP(单端点 POST);旧的 HTTP+SSE 双端点传输已废弃。
                </p>
              </div>
              <KVField
                label="请求头"
                hint="一行一条,形如 Authorization=Bearer xxx"
                value={headersText}
                onChange={setHeadersText}
              />
            </>
          )}

          {result && (
            <div
              className={cn(
                'space-y-1 rounded-md border p-3 text-xs',
                result.ok
                  ? 'border-success/30 bg-success/5'
                  : 'border-destructive/30 bg-destructive/5',
              )}
            >
              <div className={cn('font-medium', result.ok ? 'text-success' : 'text-destructive')}>
                {result.ok ? '连接成功' : '连接失败'}
                <span className="ml-2 font-normal text-muted-foreground">
                  {result.durationMs} ms
                </span>
              </div>
              {result.serverInfo && (
                <div className="text-muted-foreground">服务器:{result.serverInfo}</div>
              )}
              {result.tools && result.tools.length > 0 && (
                <div className="text-muted-foreground">
                  提供 {result.tools.length} 个工具:{result.tools.join('、')}
                </div>
              )}
              {result.message && (
                <div className="whitespace-pre-wrap break-words text-muted-foreground">
                  {result.message}
                </div>
              )}
            </div>
          )}
        </div>

        <footer className="flex h-12 shrink-0 items-center justify-between border-t border-border bg-secondary/30 px-3 text-xs">
          <button
            type="button"
            onClick={() => void runTest()}
            disabled={!valid || testing}
            className="flex h-7 items-center gap-1.5 rounded-md border border-border px-3 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:opacity-50"
          >
            {testing && <Loader2 className="h-3 w-3 animate-spin" />}
            测试连接
          </button>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={onClose}
              className="h-7 rounded-md px-3 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => onSave(build())}
              disabled={!valid}
              className="h-7 rounded-md bg-info px-3 font-medium text-info-foreground transition-colors hover:bg-info/90 disabled:opacity-50"
            >
              保存
            </button>
          </div>
        </footer>
      </div>
    </div>,
    document.body,
  )
}

function KVField({
  label,
  hint,
  value,
  onChange,
}: {
  label: string
  hint: string
  value: string
  onChange: (v: string) => void
}) {
  return (
    <div className="space-y-1.5">
      <label className="text-xs font-medium">{label}</label>
      <textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        rows={3}
        className="w-full resize-none rounded-md border border-input bg-background px-3 py-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
      />
      <p className="text-[11px] text-muted-foreground">{hint}</p>
    </div>
  )
}

/** 按空格切命令行,双引号内的空格不切 —— Windows 路径里空格很常见 */
function splitCommand(line: string): string[] {
  const out: string[] = []
  let cur = ''
  let quoted = false
  for (const ch of line.trim()) {
    if (ch === '"') {
      quoted = !quoted
      continue
    }
    if (ch === ' ' && !quoted) {
      if (cur) out.push(cur)
      cur = ''
      continue
    }
    cur += ch
  }
  if (cur) out.push(cur)
  return out
}

function kvToText(kv: Record<string, string> | undefined): string {
  if (!kv) return ''
  return Object.entries(kv)
    .map(([k, v]) => `${k}=${v}`)
    .join('\n')
}

/** 只在第一个 = 处切,值里可以再含 =(base64、URL 都可能) */
function textToKV(text: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of text.split('\n')) {
    const t = line.trim()
    if (!t) continue
    const i = t.indexOf('=')
    if (i <= 0) continue
    out[t.slice(0, i).trim()] = t.slice(i + 1).trim()
  }
  return out
}
