import { useEffect, useRef, useState } from 'react'
import { Check, ChevronDown, Globe, Loader2, Wrench, X } from 'lucide-react'
import type { SearchQuery, ToolCall } from './types'
import { cn } from '@/lib/utils'

/**
 * 模型回答之前干了什么:搜了哪些词、调了哪些工具。
 *
 * 存在的理由是等待期间的那几秒到十几秒。模型开着联网或工具时,正文迟迟不出现是常态,
 * 而界面上如果只有一个"正在思考",用户分不清它是在干活还是卡死了。把动作实时摊开,
 * 等待就从"不知道在等什么"变成"它在搜 X"。
 *
 * 答完之后这个面板还有第二个用途:模型答得不对时,先看它搜了什么、工具返回了什么,
 * 比对着最终回复猜要快得多 —— 所以完成后不是隐藏,而是折叠成一行摘要。
 */
export function ChatActivity({
  searches,
  toolCalls,
  streaming,
}: {
  searches?: SearchQuery[]
  toolCalls?: ToolCall[]
  streaming?: boolean
}) {
  const items = [...(searches ?? []), ...(toolCalls ?? [])]
  // 初值跟着 streaming 走:这个组件是随占位消息一起挂载的,那时流已经在跑了 ——
  // 只靠下面的 effect 等"开始"这个跳变的话,它永远等不到,整轮都是折叠的
  const [open, setOpen] = useState(!!streaming)
  const wasStreamingRef = useRef(streaming)

  // 生成中默认展开(用户正等着看进展),流一结束自动折叠(这时正文才是主角)。
  // 用户手动改过的状态不覆盖 —— 只在"流的开始 / 结束"这两个瞬间动它。
  useEffect(() => {
    if (!wasStreamingRef.current && streaming) setOpen(true)
    if (wasStreamingRef.current && !streaming) setOpen(false)
    wasStreamingRef.current = streaming
  }, [streaming])

  if (items.length === 0) return null

  const running = firstRunning(searches, toolCalls)
  const failed = (toolCalls ?? []).filter((c) => c.error).length

  return (
    <div className="overflow-hidden rounded-lg border border-border bg-secondary/20">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-secondary/40"
      >
        {running ? (
          <>
            <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-info" />
            <span className="min-w-0 truncate text-info">{running}</span>
          </>
        ) : (
          <>
            <Wrench className="h-3.5 w-3.5 shrink-0" />
            <span className="min-w-0 truncate font-medium">{summary(searches, toolCalls)}</span>
          </>
        )}
        {failed > 0 && (
          <span className="shrink-0 rounded bg-destructive/15 px-1.5 text-[10px] text-destructive">
            {failed} 个失败
          </span>
        )}
        <ChevronDown
          className={cn('ml-auto h-3.5 w-3.5 shrink-0 transition-transform', open && 'rotate-180')}
        />
      </button>

      {open && (
        <ul className="space-y-2 border-t border-border/50 px-3 py-2">
          {(searches ?? []).map((s, i) => (
            <SearchRow key={'s' + i + s.query} search={s} />
          ))}
          {(toolCalls ?? []).map((c, i) => (
            <ToolRow key={'t' + i + c.id} call={c} />
          ))}
        </ul>
      )}
    </div>
  )
}

function SearchRow({ search }: { search: SearchQuery }) {
  const done = search.status !== 'running'
  return (
    <li className="flex items-start gap-2 text-xs">
      <Globe
        className={cn('mt-0.5 h-3 w-3 shrink-0', done ? 'text-muted-foreground' : 'text-info')}
      />
      <span className="min-w-0 flex-1 break-words">{search.query}</span>
      <span className="shrink-0 text-[10px] text-muted-foreground">
        {done ? (search.results ? `${search.results} 条结果` : '已完成') : '搜索中'}
      </span>
    </li>
  )
}

function ToolRow({ call }: { call: ToolCall }) {
  // 只认 status。用"没有 result 也没有 error"来判断的话,返回空串的工具
  // (MCP 里不少)会永远停在转圈状态 —— 空输出是合法结果,不是"还没跑完"
  const running = call.status === 'running'
  // MCP 工具名带服务器前缀(如 filesystem_read_file),名字本身就够长了,
  // 参数和返回值默认收起来,需要排查时再点开
  const [open, setOpen] = useState(false)
  const hasDetail = !!(call.arguments && call.arguments !== '{}') || !!call.result || !!call.error

  return (
    <li className="text-xs">
      <button
        type="button"
        onClick={() => hasDetail && setOpen((v) => !v)}
        disabled={!hasDetail}
        className="flex w-full items-center gap-2 text-left"
      >
        {running ? (
          <Loader2 className="h-3 w-3 shrink-0 animate-spin text-info" />
        ) : call.error ? (
          <X className="h-3 w-3 shrink-0 text-destructive" />
        ) : (
          <Check className="h-3 w-3 shrink-0 text-success" />
        )}
        <span className="min-w-0 flex-1 truncate font-mono">{call.name}</span>
        {running && <span className="shrink-0 text-[10px] text-muted-foreground">执行中</span>}
        {hasDetail && (
          <ChevronDown
            className={cn(
              'h-3 w-3 shrink-0 text-muted-foreground transition-transform',
              open && 'rotate-180',
            )}
          />
        )}
      </button>
      {open && (
        <div className="mt-1 space-y-1 border-l border-border pl-3">
          {call.arguments && call.arguments !== '{}' && (
            <div className="whitespace-pre-wrap break-all font-mono text-[11px] text-muted-foreground">
              入参 {call.arguments}
            </div>
          )}
          {(call.result || call.error) && (
            <div
              className={cn(
                'max-h-64 overflow-auto whitespace-pre-wrap break-words font-mono text-[11px]',
                call.error ? 'text-destructive' : 'text-muted-foreground',
              )}
            >
              {call.error ? '失败:' + call.error : call.result}
            </div>
          )}
        </div>
      )}
    </li>
  )
}

/** 当前正在进行的那一步,用作折叠状态下的标题;都做完了返回空 */
function firstRunning(searches?: SearchQuery[], toolCalls?: ToolCall[]): string {
  const s = (searches ?? []).find((x) => x.status === 'running')
  if (s) return `正在搜索「${s.query}」`
  const c = (toolCalls ?? []).find((x) => x.status === 'running')
  if (c) return `正在调用 ${c.name}`
  return ''
}

function summary(searches?: SearchQuery[], toolCalls?: ToolCall[]): string {
  const parts: string[] = []
  if (searches?.length) parts.push(`搜索 ${searches.length} 次`)
  if (toolCalls?.length) parts.push(`调用 ${toolCalls.length} 个工具`)
  return parts.join(' · ')
}
