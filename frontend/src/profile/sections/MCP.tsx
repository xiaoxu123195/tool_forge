import { useEffect, useState } from 'react'
import {
  AlertCircle,
  CheckCircle2,
  Loader2,
  Plug,
  Plus,
  RefreshCw,
  Trash2,
  Wrench,
} from 'lucide-react'
import {
  DeleteMCPServer,
  ListMCPServers,
  ListMCPStatus,
  ListMCPTools,
  ReconnectMCPServer,
  SaveMCPServer,
  TestMCPServer,
  ToggleMCPServer,
} from '../../../wailsjs/go/main/App'
import { useConfirm } from '@/components/ui/confirm'
import { cn } from '@/lib/utils'
import { MCPServerDialog, type MCPServer } from './mcp/MCPServerDialog'

interface MCPStatus {
  serverId: string
  connected: boolean
  error?: string
  serverInfo?: string
  toolCount: number
}

interface MCPTool {
  serverId: string
  serverName: string
  name: string
  qualifiedName: string
  description: string
}

export function MCPSection() {
  const dialog = useConfirm()
  const [servers, setServers] = useState<MCPServer[]>([])
  const [status, setStatus] = useState<Record<string, MCPStatus>>({})
  const [tools, setTools] = useState<MCPTool[]>([])
  const [editing, setEditing] = useState<MCPServer | null>(null)
  const [busy, setBusy] = useState('')

  const reloadServers = async () => {
    const list = ((await ListMCPServers()) ?? []) as unknown as MCPServer[]
    setServers(list)
  }

  // 连接是懒建的,所以拉工具列表这一步会顺带把启用中的服务器连上 ——
  // 状态必须在它之后读,否则拿到的还是"未连接"
  const reloadLive = async () => {
    const list = ((await ListMCPTools()) ?? []) as unknown as MCPTool[]
    setTools(list)
    const st = ((await ListMCPStatus()) ?? []) as unknown as MCPStatus[]
    setStatus(Object.fromEntries(st.map((s) => [s.serverId, s])))
  }

  useEffect(() => {
    void (async () => {
      await reloadServers()
      await reloadLive()
    })()
  }, [])

  const onSave = async (s: MCPServer) => {
    try {
      await SaveMCPServer(s as never)
    } catch (e) {
      await dialog({ title: '保存失败', message: String(e), confirmLabel: '知道了' })
      return
    }
    setEditing(null)
    await reloadServers()
    await reloadLive()
  }

  const onDelete = async (s: MCPServer) => {
    const ok = await dialog({
      title: '删除服务器',
      message: `确认删除「${s.name}」?它提供的工具会立刻从模型可见的列表里消失。`,
      danger: true,
      confirmLabel: '删除',
    })
    if (!ok) return
    const err = ((await DeleteMCPServer(s.id)) as string) || ''
    if (err) {
      await dialog({ title: '删除失败', message: err, confirmLabel: '知道了' })
      return
    }
    await reloadServers()
    await reloadLive()
  }

  const onToggle = async (s: MCPServer) => {
    setBusy(s.id)
    try {
      const err = ((await ToggleMCPServer(s.id, !s.enabled)) as string) || ''
      if (err) {
        await dialog({ title: '操作失败', message: err, confirmLabel: '知道了' })
        return
      }
      await reloadServers()
      await reloadLive()
    } finally {
      setBusy('')
    }
  }

  const onReconnect = async (s: MCPServer) => {
    setBusy(s.id)
    try {
      const err = ((await ReconnectMCPServer(s.id)) as string) || ''
      await reloadLive()
      if (err) {
        await dialog({ title: '连接失败', message: err, confirmLabel: '知道了' })
      }
    } finally {
      setBusy('')
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-4">
        <p className="text-xs leading-relaxed text-muted-foreground">
          MCP(Model Context Protocol)服务器把外部能力暴露成工具。在这里添加之后,
          它们会和内置工具一起出现在 AI 问答的「工具」开关里。
          <br />
          连接是按需建立的 —— 第一次用到工具时才连,所以刚添加完可能显示未连接,点一下刷新即可。
        </p>
        <button
          type="button"
          onClick={() =>
            setEditing({
              id: '',
              name: '',
              kind: 'stdio',
              enabled: true,
              command: '',
              args: [],
              env: {},
              url: '',
              headers: {},
            })
          }
          className="flex h-8 shrink-0 items-center gap-1.5 rounded-md bg-info px-3 text-xs font-medium text-info-foreground transition-colors hover:bg-info/90"
        >
          <Plus className="h-3.5 w-3.5" />
          添加服务器
        </button>
      </div>

      {servers.length === 0 ? (
        <div className="flex flex-col items-center gap-2 rounded-lg border border-dashed border-border py-10 text-center">
          <Plug className="h-6 w-6 text-muted-foreground" />
          <div className="text-sm font-medium">还没有配置 MCP 服务器</div>
          <p className="max-w-sm text-xs text-muted-foreground">
            常见的例子:文件系统、Git、数据库查询、浏览器自动化。
            <br />
            stdio 方式填启动命令(如 npx -y @modelcontextprotocol/server-filesystem /some/dir),
            HTTP 方式填服务器地址。
          </p>
        </div>
      ) : (
        <ul className="space-y-2">
          {servers.map((s) => {
            const st = status[s.id]
            const own = tools.filter((t) => t.serverId === s.id)
            return (
              <li key={s.id} className="rounded-lg border border-border bg-card p-3">
                <div className="flex items-center gap-2">
                  <StatusDot enabled={s.enabled} status={st} busy={busy === s.id} />
                  <button
                    type="button"
                    onClick={() => setEditing(s)}
                    className="min-w-0 flex-1 text-left"
                  >
                    <div className="truncate text-sm font-medium">{s.name}</div>
                    <div className="truncate font-mono text-[11px] text-muted-foreground">
                      {s.kind === 'stdio'
                        ? [s.command, ...(s.args ?? [])].join(' ')
                        : s.url}
                    </div>
                  </button>
                  <span className="shrink-0 rounded bg-secondary px-1.5 py-0.5 text-[10px] uppercase text-muted-foreground">
                    {s.kind}
                  </span>
                  <button
                    type="button"
                    onClick={() => void onReconnect(s)}
                    disabled={!s.enabled || busy === s.id}
                    title="断开重连"
                    className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:opacity-40"
                  >
                    <RefreshCw className={cn('h-3.5 w-3.5', busy === s.id && 'animate-spin')} />
                  </button>
                  <button
                    type="button"
                    onClick={() => void onToggle(s)}
                    disabled={busy === s.id}
                    className={cn(
                      'h-7 rounded-md border px-2 text-xs transition-colors',
                      s.enabled
                        ? 'border-info/40 bg-info/10 text-info'
                        : 'border-border text-muted-foreground hover:bg-secondary',
                    )}
                  >
                    {s.enabled ? '已启用' : '已停用'}
                  </button>
                  <button
                    type="button"
                    onClick={() => void onDelete(s)}
                    className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </div>

                {st?.error && (
                  <div className="mt-2 flex items-start gap-1.5 rounded border border-destructive/30 bg-destructive/5 p-2 text-[11px] text-destructive">
                    <AlertCircle className="mt-0.5 h-3 w-3 shrink-0" />
                    <span className="whitespace-pre-wrap break-words">{st.error}</span>
                  </div>
                )}

                {own.length > 0 && (
                  <div className="mt-2 flex flex-wrap gap-1">
                    {own.map((t) => (
                      <span
                        key={t.qualifiedName}
                        title={`${t.qualifiedName}\n${t.description}`}
                        className="flex items-center gap-1 rounded border border-border bg-secondary/40 px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground"
                      >
                        <Wrench className="h-2.5 w-2.5" />
                        {t.name}
                      </span>
                    ))}
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      )}

      {editing && (
        <MCPServerDialog
          initial={editing}
          onClose={() => setEditing(null)}
          onSave={(s) => void onSave(s)}
          onTest={(s) => TestMCPServer(s as never) as any}
        />
      )}
    </div>
  )
}

function StatusDot({
  enabled,
  status,
  busy,
}: {
  enabled: boolean
  status?: MCPStatus
  busy: boolean
}) {
  if (busy) {
    return <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-muted-foreground" />
  }
  if (!enabled) {
    return <span className="h-2 w-2 shrink-0 rounded-full bg-muted-foreground/40" title="已停用" />
  }
  if (status?.connected) {
    return (
      <CheckCircle2
        className="h-3.5 w-3.5 shrink-0 text-success"
        aria-label="已连接"
      />
    )
  }
  if (status?.error) {
    return <AlertCircle className="h-3.5 w-3.5 shrink-0 text-destructive" aria-label="连接失败" />
  }
  return <span className="h-2 w-2 shrink-0 rounded-full bg-muted-foreground/40" title="未连接" />
}
