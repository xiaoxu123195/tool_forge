import { useState } from 'react'
import { Plug, Plus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { cn } from '@/lib/utils'
import type { mcp } from '../../../wailsjs/go/models'

/** 临时服务器的初始形状 —— 不保存进配置,只在这一次会话里用 */
export function blankServer(): mcp.Server {
  return {
    id: '',
    name: '临时服务器',
    kind: 'stdio',
    enabled: true,
    command: '',
    args: [],
    env: {},
    url: '',
    headers: {},
    createdAt: 0,
    updatedAt: 0,
  } as mcp.Server
}

/** 一句话说清这条连到哪儿 */
export function targetOf(s: mcp.Server): string {
  if (s.kind === 'http') return s.url || '(没填地址)'
  return [s.command, ...(s.args ?? [])].filter(Boolean).join(' ') || '(没填命令)'
}

export function ServerPicker({
  servers,
  current,
  busy,
  onPick,
  onAdHoc,
}: {
  servers: mcp.Server[]
  current: mcp.Server | null
  busy: boolean
  onPick: (s: mcp.Server) => void
  onAdHoc: (s: mcp.Server) => void
}) {
  const [editing, setEditing] = useState<mcp.Server | null>(null)

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center gap-2 px-1 text-[11px] text-muted-foreground">
        <span className="font-medium">服务器</span>
        <button
          type="button"
          title="连一个没保存过的服务器"
          onClick={() => setEditing(blankServer())}
          className="ml-auto flex items-center gap-0.5 rounded px-1 py-0.5 transition-colors hover:bg-secondary hover:text-foreground"
        >
          <Plus className="h-3 w-3" />
          临时连接
        </button>
      </div>

      {servers.length === 0 ? (
        <p className="px-1 text-[11px] text-muted-foreground/80">
          还没有保存过 MCP 服务器。可以去「设置 → MCP 服务器」加一个，或者点上面的「临时连接」直接试。
        </p>
      ) : (
        <ul className="space-y-1">
          {servers.map((s) => {
            const active = current?.id === s.id && !!s.id
            return (
              <li key={s.id || s.name}>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => onPick(s)}
                  title={targetOf(s)}
                  className={cn(
                    'w-full rounded-md border px-2 py-1.5 text-left transition-colors disabled:opacity-60',
                    active ? 'border-info/50 bg-info/10' : 'border-border bg-card hover:bg-accent/60',
                  )}
                >
                  <div className="flex items-center gap-1.5">
                    <Plug className={cn('h-3 w-3 shrink-0', active ? 'text-info' : 'text-muted-foreground')} />
                    <span className="truncate text-xs font-medium">{s.name}</span>
                    <span className="ml-auto shrink-0 rounded-sm bg-muted px-1 text-[10px] text-muted-foreground">
                      {s.kind}
                    </span>
                  </div>
                  <div className="mt-0.5 truncate font-mono text-[10px] text-muted-foreground">
                    {targetOf(s)}
                  </div>
                </button>
              </li>
            )
          })}
        </ul>
      )}

      {editing && (
        <AdHocDialog
          draft={editing}
          onClose={() => setEditing(null)}
          onConnect={(s) => {
            setEditing(null)
            onAdHoc(s)
          }}
        />
      )}
    </div>
  )
}

function AdHocDialog({
  draft,
  onClose,
  onConnect,
}: {
  draft: mcp.Server
  onClose: () => void
  onConnect: (s: mcp.Server) => void
}) {
  const [s, setS] = useState(draft)
  const [argsText, setArgsText] = useState((draft.args ?? []).join(' '))
  const set = <K extends keyof mcp.Server>(k: K, v: mcp.Server[K]) => setS((p) => ({ ...p, [k]: v }))

  // 命令行按空格切。带空格的路径要用引号,所以这里认一下引号
  const parsedArgs = argsText.match(/"[^"]*"|'[^']*'|\S+/g)?.map((a) => a.replace(/^["']|["']$/g, '')) ?? []
  const ready = s.kind === 'http' ? (s.url ?? '').trim() !== '' : (s.command ?? '').trim() !== ''

  return (
    <Dialog
      open
      onClose={onClose}
      title="临时连接一个 MCP 服务器"
      description="只在这次会话里用，不写进配置列表"
      footer={
        <>
          <Button variant="outline" size="sm" className="ml-auto" onClick={onClose}>
            取消
          </Button>
          <Button size="sm" disabled={!ready} onClick={() => onConnect({ ...s, args: parsedArgs } as mcp.Server)}>
            连接
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <div className="flex gap-1.5">
          {(['stdio', 'http'] as const).map((k) => (
            <button
              key={k}
              type="button"
              onClick={() => set('kind', k)}
              className={cn(
                'rounded-md border px-2.5 py-1 text-xs transition-colors',
                s.kind === k ? 'border-foreground/30 bg-accent font-medium' : 'border-input hover:bg-accent',
              )}
            >
              {k === 'stdio' ? 'stdio（起一个本地进程）' : 'http（连一个地址）'}
            </button>
          ))}
        </div>

        <Row label="名称">
          <input
            value={s.name}
            onChange={(e) => set('name', e.target.value)}
            className="h-8 w-full rounded-md border border-input bg-background px-2 text-xs outline-none focus:ring-1 focus:ring-ring"
          />
        </Row>

        {s.kind === 'stdio' ? (
          <>
            <Row label="命令">
              <input
                value={s.command ?? ''}
                onChange={(e) => set('command', e.target.value)}
                placeholder="npx"
                spellCheck={false}
                className="h-8 w-full rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
              />
            </Row>
            <Row label="参数">
              <input
                value={argsText}
                onChange={(e) => setArgsText(e.target.value)}
                placeholder="-y @modelcontextprotocol/server-filesystem D:\\some\\dir"
                spellCheck={false}
                className="h-8 w-full rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
              />
              {parsedArgs.length > 0 && (
                <p className="mt-0.5 font-mono text-[10px] text-muted-foreground">
                  切成 {parsedArgs.length} 个：{parsedArgs.map((a) => JSON.stringify(a)).join(' ')}
                </p>
              )}
            </Row>
          </>
        ) : (
          <Row label="地址">
            <input
              value={s.url ?? ''}
              onChange={(e) => set('url', e.target.value)}
              placeholder="http://127.0.0.1:11435/mcp"
              spellCheck={false}
              className="h-8 w-full rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
            />
          </Row>
        )}
      </div>
    </Dialog>
  )
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-1 text-[11px] text-muted-foreground">{label}</div>
      {children}
    </div>
  )
}
