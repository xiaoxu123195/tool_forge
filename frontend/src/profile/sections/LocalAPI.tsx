import { useCallback, useEffect, useState } from 'react'
import {
  AlertCircle,
  BookOpen,
  Check,
  Copy,
  Eye,
  EyeOff,
  KeyRound,
  Loader2,
  Network,
  Radio,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
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

function bridge(): {
  GetAPIServerConfig: () => Promise<APIConfig>
  UpdateAPIServerConfig: (cfg: APIConfig) => Promise<void>
  GetAPIServerStatus: () => Promise<APIStatus>
  ListAPIServerTools: () => Promise<ToolInfo[]>
  GenerateAPIServerToken: () => Promise<string>
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
  const [copied, setCopied] = useState<'token' | ''>('')
  const [error, setError] = useState('')
  const [dialogTool, setDialogTool] = useState<ToolInfo | null>(null)

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

  const copyText = async (text: string, key: 'token') => {
    await navigator.clipboard.writeText(text)
    setCopied(key)
    setTimeout(() => setCopied(''), 1500)
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
