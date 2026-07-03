import { Search } from 'lucide-react'
import type { llmproxy } from '../../../wailsjs/go/models'
import { cn } from '@/lib/utils'
import { fmtBytes, fmtTime, methodClass, statusClass } from './lib'

interface Props {
  page: llmproxy.LogPage
  query: llmproxy.LogQuery
  setQuery: (patch: Partial<llmproxy.LogQuery>) => void
  upstreams: string[]
  selectedId: number | null
  onSelect: (id: number) => void
}

export function LogList({ page, query, setQuery, upstreams, selectedId, onSelect }: Props) {
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-1.5">
        <div className="relative flex-1 min-w-[180px]">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <input
            value={query.search}
            onChange={(e) => setQuery({ search: e.target.value })}
            placeholder="搜索路径 / model…"
            className="h-8 w-full rounded-md border border-input bg-background pl-8 pr-2.5 text-xs outline-none focus:border-ring"
          />
        </div>
        <select value={query.upstream} onChange={(e) => setQuery({ upstream: e.target.value })} className={selCls}>
          <option value="">全部上游</option>
          {upstreams.map((u) => (
            <option key={u} value={u}>{u}</option>
          ))}
        </select>
        <select value={query.method} onChange={(e) => setQuery({ method: e.target.value })} className={selCls}>
          <option value="">全部方法</option>
          <option value="GET">GET</option>
          <option value="POST">POST</option>
          <option value="DELETE">DELETE</option>
        </select>
        <select value={query.status} onChange={(e) => setQuery({ status: e.target.value })} className={selCls}>
          <option value="">全部状态</option>
          <option value="2xx">2xx</option>
          <option value="4xx">4xx</option>
          <option value="5xx">5xx</option>
          <option value="error">错误</option>
        </select>
      </div>

      <div className="overflow-hidden rounded-lg border border-border">
        <table className="w-full text-xs">
          <thead className="bg-secondary/40 text-muted-foreground">
            <tr>
              <Th>时间</Th>
              <Th>上游</Th>
              <Th>方法</Th>
              <Th className="w-full">路径</Th>
              <Th>状态</Th>
              <Th>耗时</Th>
              <Th>tokens</Th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border/50">
            {page.items.map((e) => (
              <tr
                key={e.id}
                onClick={() => onSelect(e.id)}
                className={cn(
                  'cursor-pointer transition-colors hover:bg-accent/60',
                  selectedId === e.id && 'bg-accent'
                )}
              >
                <Td className="whitespace-nowrap text-muted-foreground">{fmtTime(e.ts)}</Td>
                <Td className="whitespace-nowrap">{e.upstream}</Td>
                <Td className={cn('whitespace-nowrap font-medium', methodClass(e.method))}>{e.method}</Td>
                <Td className="max-w-0 truncate font-mono" title={e.path}>
                  {e.path}
                  {e.stream && <span className="ml-1 rounded bg-sky-500/15 px-1 text-[10px] text-sky-600 dark:text-sky-400">SSE</span>}
                  {e.tag === 'replay' && <span className="ml-1 rounded bg-violet-500/15 px-1 text-[10px] text-violet-600 dark:text-violet-400">重放</span>}
                </Td>
                <Td className={cn('whitespace-nowrap font-mono font-medium', statusClass(e.status))}>
                  {e.error ? '错误' : e.status ? e.status : <span className="text-muted-foreground">进行中…</span>}
                </Td>
                <Td className="whitespace-nowrap text-muted-foreground" title={e.ttftMs ? `首字节 ${e.ttftMs}ms` : ''}>
                  {e.durationMs}ms
                  {e.ttftMs > 0 && <span className="ml-1 text-[10px] opacity-70">· 首{e.ttftMs}</span>}
                </Td>
                <Td className="whitespace-nowrap text-muted-foreground" title={`${fmtBytes(e.reqBytes)} → ${fmtBytes(e.respBytes)}`}>
                  {e.totalTokens ? e.totalTokens : '-'}
                </Td>
              </tr>
            ))}
            {page.items.length === 0 && (
              <tr>
                <td colSpan={7} className="p-10 text-center text-sm text-muted-foreground">
                  还没有请求记录。启动代理后,把客户端 base_url 指到代理地址即可。
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {page.total > page.items.length && (
        <div className="text-center text-[11px] text-muted-foreground">
          共 {page.total} 条,显示最近 {page.items.length} 条
        </div>
      )}
    </div>
  )
}

const selCls = 'h-8 rounded-md border border-input bg-background px-2 text-xs outline-none focus:border-ring'

function Th({ children, className }: { children: React.ReactNode; className?: string }) {
  return <th className={cn('px-2.5 py-1.5 text-left font-medium', className)}>{children}</th>
}
function Td({ children, className, title }: { children: React.ReactNode; className?: string; title?: string }) {
  return (
    <td className={cn('px-2.5 py-1.5', className)} title={title}>
      {children}
    </td>
  )
}
