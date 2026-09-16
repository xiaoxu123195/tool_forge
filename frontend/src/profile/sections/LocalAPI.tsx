import { useCallback, useEffect, useState } from 'react'
import {
  AlertCircle,
  BookOpen,
  Check,
  Copy,
  Eye,
  EyeOff,
  FileDown,
  KeyRound,
  Loader2,
  Network,
  Radio,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { cn } from '@/lib/utils'
import { ToolExampleDialog } from './local-api/ToolExampleDialog'
import { TOOL_EXAMPLES } from './local-api/examples'

/**
 * 本地 API server 配置页。
 *
 * RPC 通过 window.go.main.App.* 调用,避开 wails 生成 binding 的导入(开发期还未重新
 * 生成时也能编译)。调用签名由 backend/apiserver 决定。
 */

interface APIConfig {
  enabled: boolean
  port: number
  auth_enabled: boolean
  token: string
  enabled_tools: Record<string, boolean>
}

interface APIStatus {
  running: boolean
  addr: string
  error?: string
}

interface ToolInfo {
  name: string
  title: string
  description: string
  path: string
  enabled: boolean
}

type MCPTarget = 'claude' | 'codex'

/** 后端试算 / 写入的结果,形状见 aiconfig.MCPInstallResult */
interface MCPInstallResult {
  target: MCPTarget
  file: string
  action: 'create' | 'add' | 'replace' | 'unchanged'
  block: string
  oldBlock?: string
  backup?: string
  applied: boolean
}

const MCP_TARGETS: Record<MCPTarget, { label: string; file: string; restart: string }> = {
  claude: { label: 'Claude Code', file: '~/.claude.json', restart: '下次启动 Claude Code 时生效' },
  codex: { label: 'Codex', file: '~/.codex/config.toml', restart: '下次启动 Codex 时生效' },
}

function bridge(): {
  GetAPIServerConfig: () => Promise<APIConfig>
  UpdateAPIServerConfig: (cfg: APIConfig) => Promise<void>
  GetAPIServerStatus: () => Promise<APIStatus>
  ListAPIServerTools: () => Promise<ToolInfo[]>
  GenerateAPIServerToken: () => Promise<string>
  InstallLocalAPIMCP: (target: MCPTarget, apply: boolean) => Promise<MCPInstallResult>
} {
  const App = (window as any).go?.main?.App
  if (!App) {
    throw new Error('Wails RPC 未就绪')
  }
  return App
}

const DEFAULT_CONFIG: APIConfig = {
  enabled: false,
  port: 11435,
  auth_enabled: false,
  token: '',
  enabled_tools: {},
}

export function LocalAPISection() {
  const [cfg, setCfg] = useState<APIConfig>(DEFAULT_CONFIG)
  const [status, setStatus] = useState<APIStatus>({ running: false, addr: '' })
  const [tools, setTools] = useState<ToolInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [showToken, setShowToken] = useState(false)
  const [copied, setCopied] = useState('')
  const [error, setError] = useState('')
  const [dialogTool, setDialogTool] = useState<ToolInfo | null>(null)
  const confirm = useConfirm()
  const [installing, setInstalling] = useState<MCPTarget | ''>('')
  const [installNote, setInstallNote] = useState<{
    target: MCPTarget
    ok: boolean
    text: string
  } | null>(null)

  const refresh = useCallback(async () => {
    try {
      const api = bridge()
      const [c, s, t] = await Promise.all([
        api.GetAPIServerConfig(),
        api.GetAPIServerStatus(),
        api.ListAPIServerTools(),
      ])
      setCfg({ ...DEFAULT_CONFIG, ...c, enabled_tools: c.enabled_tools ?? {} })
      setStatus(s)
      setTools(t ?? [])
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const save = async (next: APIConfig) => {
    setSaving(true)
    try {
      await bridge().UpdateAPIServerConfig(next)
      setCfg(next)
      // server 状态需要重新拉
      const s = await bridge().GetAPIServerStatus()
      setStatus(s)
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  const handleToggleEnabled = (v: boolean) => void save({ ...cfg, enabled: v })

  const handlePortApply = (port: number) => {
    if (!Number.isFinite(port) || port < 1 || port > 65535) {
      setError('端口必须在 1-65535')
      return
    }
    void save({ ...cfg, port })
  }

  const handleAuthToggle = (v: boolean) => {
    // 开启鉴权但没 token,自动生成一个
    if (v && !cfg.token) {
      void (async () => {
        const newToken = await bridge().GenerateAPIServerToken()
        await save({ ...cfg, auth_enabled: true, token: newToken })
      })()
      return
    }
    void save({ ...cfg, auth_enabled: v })
  }

  const handleGenerateToken = async () => {
    const t = await bridge().GenerateAPIServerToken()
    await save({ ...cfg, token: t, auth_enabled: cfg.auth_enabled || true })
    setShowToken(true)
  }

  const handleToolToggle = (name: string, v: boolean) => {
    const next: APIConfig = {
      ...cfg,
      enabled_tools: { ...cfg.enabled_tools, [name]: v },
    }
    void save(next)
  }

  const copyText = async (text: string, key: string) => {
    await navigator.clipboard.writeText(text)
    setCopied(key)
    setTimeout(() => setCopied(''), 1500)
  }

  // 一键写进 Claude Code / Codex 的配置。先试算拿回将写入的那段,让用户看过再落盘;
  // 已经是这份就直接说明,不弹框也不备份
  const installMCP = async (target: MCPTarget) => {
    const meta = MCP_TARGETS[target]
    setInstalling(target)
    setInstallNote(null)
    try {
      const plan = await bridge().InstallLocalAPIMCP(target, false)
      if (plan.action === 'unchanged') {
        setInstallNote({ target, ok: true, text: `${plan.file} 里已经是这份配置了，不用再写。` })
        return
      }
      const ok = await confirm({
        title: `写入 ${meta.label} 的配置`,
        message: <InstallPreview plan={plan} />,
        confirmLabel: plan.action === 'replace' ? '替换并写入' : '写入',
        danger: plan.action === 'replace',
      })
      if (!ok) return
      const done = await bridge().InstallLocalAPIMCP(target, true)
      setInstallNote({
        target,
        ok: true,
        text: `已写入 ${done.file}${done.backup ? `，原文件备份在 ${done.backup}` : ''}。${meta.restart}。`,
      })
    } catch (e) {
      setInstallNote({ target, ok: false, text: e instanceof Error ? e.message : String(e) })
    } finally {
      setInstalling('')
    }
  }

  if (loading) {
    return (
      <div className="mx-auto max-w-3xl">
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          加载配置中...
        </div>
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-3xl space-y-5">
      <header>
        <h1 className="flex items-center gap-2 text-xl font-semibold">
          <Network className="h-5 w-5" />
          本地 API
        </h1>
        <p className="mt-1 text-sm text-muted-foreground">
          把 Tool Forge 的工具暴露成本地 HTTP 接口,让外部脚本 / AI Agent 调用。仅监听 127.0.0.1,默认不接受其它机器访问。
        </p>
      </header>

      {error && (
        <div className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
          <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
          <span className="flex-1">{error}</span>
          <button onClick={() => void refresh()} className="text-xs underline">
            重试
          </button>
        </div>
      )}

      {/* 状态 + 总开关 */}
      <div className="flex items-center justify-between rounded-lg border border-border bg-card p-4">
        <div className="flex items-center gap-3">
          <div
            className={cn(
              'flex h-9 w-9 items-center justify-center rounded-lg',
              status.running ? 'bg-success/15 text-success' : 'bg-muted text-muted-foreground',
            )}
          >
            <Network className="h-4 w-4" />
          </div>
          <div>
            <div className="text-sm font-medium">
              {status.running ? '运行中' : '未启动'}
            </div>
            <div className="text-xs text-muted-foreground">
              {status.running
                ? `监听 ${status.addr}`
                : status.error
                  ? `上次错误: ${status.error}`
                  : '启用后将监听本地端口'}
            </div>
          </div>
        </div>
        <Toggle
          checked={cfg.enabled}
          onChange={handleToggleEnabled}
          disabled={saving}
          label={cfg.enabled ? '已启用' : '已关闭'}
        />
      </div>

      {/* 监听端口 */}
      <Card title="监听端口" description="默认 11435。改完点应用,server 会立即重启监听新端口。">
        <PortRow
          value={cfg.port}
          onApply={handlePortApply}
          disabled={saving}
        />
      </Card>

      {/* 鉴权 */}
      <Card
        title={
          <span className="flex items-center gap-2">
            <ShieldCheck className="h-4 w-4" />
            Token 鉴权
          </span>
        }
        description="开启后所有 API 请求必须带 Authorization: Bearer <token>。单机使用可不开,多账户共享或局域网用务必开。"
      >
        <div className="space-y-3">
          <Toggle
            checked={cfg.auth_enabled}
            onChange={handleAuthToggle}
            disabled={saving}
            label={cfg.auth_enabled ? '已启用' : '已关闭'}
          />
          <div className="flex items-center gap-2">
            <input
              type={showToken ? 'text' : 'password'}
              value={cfg.token}
              readOnly
              placeholder="还没有 token,点右侧生成"
              className="h-9 flex-1 rounded-md border border-input bg-background px-3 font-mono text-xs outline-none"
            />
            <Button
              variant="outline"
              size="sm"
              onClick={() => setShowToken((v) => !v)}
              title={showToken ? '隐藏' : '显示'}
            >
              {showToken ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => copyText(cfg.token, 'token')}
              disabled={!cfg.token}
            >
              {copied === 'token' ? (
                <>
                  <Check className="h-3.5 w-3.5 text-success" />
                  已复制
                </>
              ) : (
                <>
                  <Copy className="h-3.5 w-3.5" />
                  复制
                </>
              )}
            </Button>
            <Button size="sm" onClick={handleGenerateToken} disabled={saving}>
              <KeyRound className="h-3.5 w-3.5" />
              {cfg.token ? '重新生成' : '生成'}
            </Button>
          </div>
        </div>
      </Card>

      {/* MCP 端点 —— 让 Claude Code / Codex 直接调这些工具 */}
      <Card
        title="MCP 端点"
        description="把下面这段配进 Claude Code 或 Codex，勾选的工具就能被它们直接调用；也可以点「写入」让工具箱直接改它们的配置文件 —— 只动 tool-forge 这一段，改前留一份 .bak。开了 Token 鉴权的话配置里会带上 Authorization 头。"
      >
        <div className="space-y-4">
          {(Object.keys(MCP_TARGETS) as MCPTarget[]).map((t) => {
            const meta = MCP_TARGETS[t]
            const snippet = t === 'claude' ? claudeMcpSnippet(cfg) : codexMcpSnippet(cfg)
            return (
              <div key={t}>
                <div className="mb-1.5 flex flex-wrap items-center gap-2">
                  <span className="text-xs font-medium">{meta.label}</span>
                  <span className="font-mono text-[11px] text-muted-foreground">{meta.file}</span>
                  <div className="ml-auto flex items-center gap-1.5">
                    <Button
                      size="sm"
                      variant="ghost"
                      title={`复制 ${meta.label} 的配置`}
                      onClick={() => copyText(snippet, `mcp-${t}`)}
                    >
                      {copied === `mcp-${t}` ? (
                        <>
                          <Check className="h-3.5 w-3.5" />
                          已复制
                        </>
                      ) : (
                        <>
                          <Copy className="h-3.5 w-3.5" />
                          复制
                        </>
                      )}
                    </Button>
                    <Button
                      size="sm"
                      variant="secondary"
                      title={`写入 ${meta.label}`}
                      disabled={installing !== ''}
                      onClick={() => void installMCP(t)}
                    >
                      {installing === t ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <FileDown className="h-3.5 w-3.5" />
                      )}
                      写入 {meta.label}
                    </Button>
                  </div>
                </div>
                <pre className="overflow-x-auto rounded-md border border-border bg-background p-3 font-mono text-[11px] leading-relaxed">
                  {snippet}
                </pre>
                {installNote?.target === t && (
                  <p
                    className={cn(
                      'mt-1.5 break-all text-[11px]',
                      installNote.ok ? 'text-success' : 'text-destructive',
                    )}
                  >
                    {installNote.text}
                  </p>
                )}
              </div>
            )
          })}
        </div>
        <div className="mt-3 font-mono text-[11px] text-muted-foreground">
          端点 http://127.0.0.1:{cfg.port}/mcp
        </div>
        {!cfg.enabled && (
          <p className="mt-2 text-[11px] text-amber-600 dark:text-amber-400">
            本地 API 服务还没启用，上面的地址现在连不上 —— 先把最上面那个开关打开。
          </p>
        )}
      </Card>

      {/* 工具列表 */}
      <Card
        title="已注册的工具"
        description="勾选要对外暴露的工具,未勾选的工具调用会返回 403。"
      >
        {tools.length === 0 ? (
          <div className="text-xs text-muted-foreground">暂无可暴露的工具</div>
        ) : (
          <ul className="space-y-1">
            {tools.map((t) => {
              const set = TOOL_EXAMPLES[t.name]
              const hasExamples = !!set
              return (
                <li
                  key={t.name}
                  className="flex items-start gap-3 rounded-md border border-border bg-background px-3 py-2.5"
                >
                  <input
                    type="checkbox"
                    checked={!!cfg.enabled_tools[t.name]}
                    onChange={(e) => handleToolToggle(t.name, e.target.checked)}
                    disabled={saving}
                    className="mt-0.5 h-4 w-4 accent-info"
                  />
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2 text-sm">
                      <span className="font-medium">{t.title}</span>
                      <span className="font-mono text-[11px] text-muted-foreground">{t.path}</span>
                      {set?.streaming && (
                        <span
                          className="inline-flex items-center gap-0.5 rounded border border-info/40 bg-info/5 px-1 text-[10px] text-info"
                          title="返回 SSE 流式响应"
                        >
                          <Radio className="h-2.5 w-2.5" />
                          SSE
                        </span>
                      )}
                      {set?.sensitive && (
                        <span
                          className="inline-flex items-center gap-0.5 rounded border border-amber-500/40 bg-amber-500/5 px-1 text-[10px] text-amber-700 dark:text-amber-300"
                          title="敏感操作,建议开 Token 鉴权"
                        >
                          <ShieldAlert className="h-2.5 w-2.5" />
                          敏感
                        </span>
                      )}
                    </div>
                    <div className="text-xs text-muted-foreground">{t.description}</div>
                  </div>
                  {hasExamples && (
                    <button
                      type="button"
                      onClick={() => setDialogTool(t)}
                      className="inline-flex h-7 shrink-0 items-center gap-1 rounded-md border border-border bg-background px-2 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                      title="查看示例与参数说明"
                    >
                      <BookOpen className="h-3 w-3" />
                      示例
                    </button>
                  )}
                </li>
              )
            })}
          </ul>
        )}
      </Card>

      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => void refresh()}
          className="h-7 px-2"
        >
          <RefreshCw className="h-3 w-3" />
          刷新状态
        </Button>
        {!status.running && cfg.enabled && (
          <span className="text-amber-500">
            server 已启用但未在监听,可能端口被占用
          </span>
        )}
        <span className="ml-auto">点工具旁的"示例"查看调用方法</span>
      </div>

      {dialogTool && (
        <ToolExampleDialog
          tool={dialogTool}
          config={{
            port: cfg.port,
            auth_enabled: cfg.auth_enabled,
            token: cfg.token,
          }}
          onClose={() => setDialogTool(null)}
        />
      )}
    </div>
  )
}

/** 确认框里的内容:写到哪个文件、做什么、写什么;换掉的话原来那段也摆出来 */
function InstallPreview({ plan }: { plan: MCPInstallResult }) {
  const what: Record<MCPInstallResult['action'], string> = {
    create: '文件还不存在，会新建它并写入下面这段。',
    add: '在文件里追加下面这段，其它内容一个字节不动。',
    replace: '把文件里已有的 tool-forge 那段换成下面这段，其它内容一个字节不动。',
    unchanged: '已经是这份配置了。',
  }
  return (
    <div className="space-y-2">
      <p className="break-all font-mono text-xs">{plan.file}</p>
      <p>
        {what[plan.action] ?? plan.action}
        {plan.action !== 'create' && ' 写之前会留一份带时间戳的 .bak。'}
      </p>
      <pre className="overflow-x-auto rounded-md border border-border bg-background p-2 font-mono text-[11px] leading-relaxed">
        {plan.block}
      </pre>
      {plan.action === 'replace' && plan.oldBlock && (
        <>
          <p className="text-muted-foreground">原来那段（会被换掉）：</p>
          <pre className="overflow-x-auto rounded-md border border-border bg-muted/40 p-2 font-mono text-[11px] leading-relaxed text-muted-foreground">
            {plan.oldBlock}
          </pre>
        </>
      )}
    </div>
  )
}

function Card({
  title,
  description,
  children,
}: {
  title: React.ReactNode
  description?: string
  children: React.ReactNode
}) {
  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <h2 className="text-sm font-semibold">{title}</h2>
      {description && <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>}
      <div className="mt-3">{children}</div>
    </div>
  )
}

function Toggle({
  checked,
  onChange,
  disabled,
  label,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  disabled?: boolean
  label?: string
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cn(
        'inline-flex h-6 w-11 items-center rounded-full transition-colors',
        checked ? 'bg-info' : 'bg-muted',
        disabled && 'opacity-60',
      )}
    >
      <span
        className={cn(
          'inline-block h-5 w-5 transform rounded-full bg-background shadow transition-transform',
          checked ? 'translate-x-[22px]' : 'translate-x-0.5',
        )}
      />
      {label && <span className="sr-only">{label}</span>}
    </button>
  )
}

function PortRow({
  value,
  onApply,
  disabled,
}: {
  value: number
  onApply: (n: number) => void
  disabled?: boolean
}) {
  const [local, setLocal] = useState<string>(String(value))
  useEffect(() => setLocal(String(value)), [value])
  const changed = String(value) !== local.trim()
  return (
    <div className="flex items-center gap-2">
      <input
        type="number"
        min={1}
        max={65535}
        value={local}
        onChange={(e) => setLocal(e.target.value)}
        disabled={disabled}
        className="h-9 w-32 rounded-md border border-input bg-background px-3 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
      />
      <Button
        size="sm"
        onClick={() => onApply(Number(local))}
        disabled={!changed || disabled}
      >
        应用
      </Button>
      <span className="text-xs text-muted-foreground">监听 127.0.0.1:{value}</span>
    </div>
  )
}

/**
 * 生成一段能直接粘进 Claude Code 的 MCP 配置。
 *
 * 给的是 http 传输而不是 stdio:这个应用本来就常驻,stdio 还要再包一层启动器。
 * 开了鉴权就把 Authorization 头一起写进去 —— 少了它客户端会连上但一个工具都列不出来,
 * 而那种"连上了却是空的"最难猜是哪儿不对。
 */
function claudeMcpSnippet(cfg: APIConfig): string {
  const server: Record<string, unknown> = {
    type: 'http',
    url: `http://127.0.0.1:${cfg.port}/mcp`,
  }
  if (cfg.auth_enabled && cfg.token) {
    server.headers = { Authorization: `Bearer ${cfg.token}` }
  }
  return JSON.stringify({ mcpServers: { 'tool-forge': server } }, null, 2)
}

/**
 * Codex 那份是 TOML。鉴权走 http_headers:Codex 不接受把 token 直接写成 bearer_token,
 * 只认环境变量名(bearer_token_env_var)或者静态头;静态头不用再去设环境变量,
 * 和上面那段 JSON 的用法对得上。真写入时后端生成的就是同一段。
 */
function codexMcpSnippet(cfg: APIConfig): string {
  const lines = ['[mcp_servers.tool-forge]', `url = "http://127.0.0.1:${cfg.port}/mcp"`]
  if (cfg.auth_enabled && cfg.token) {
    lines.push(`http_headers = { Authorization = "Bearer ${cfg.token}" }`)
  }
  return lines.join('\n')
}
