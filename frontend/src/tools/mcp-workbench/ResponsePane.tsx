import { useState } from 'react'
import { AlertTriangle, Check, CheckCircle2, Copy, XCircle } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { mcp } from '../../../wailsjs/go/models'

/**
 * 一次调用的结果。
 *
 * 三个页签是有讲究的:「文本」是模型会看到的东西,「原始响应」是服务器真正回的,
 * 「请求」是我们真正发出去的。调试时最常见的困惑是这三者对不上 ——
 * 比如工具明明返回了结构化数据,但 content 里只有一句摘要。
 */
export function ResponsePane({ result }: { result: mcp.CallResult | null }) {
  const [tab, setTab] = useState<'text' | 'response' | 'request'>('text')
  const [copied, setCopied] = useState('')

  if (!result) {
    return (
      <div className="flex h-full items-center justify-center px-4 text-center text-[11px] text-muted-foreground">
        左边选一个工具，填好参数点「调用」，结果显示在这里。
      </div>
    )
  }

  const failed = !!result.error
  const body =
    tab === 'text' ? result.text || '（没有可提取的文本）' : tab === 'response' ? result.response : result.request

  const copy = async () => {
    await navigator.clipboard.writeText(body)
    setCopied(tab)
    setTimeout(() => setCopied(''), 1500)
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div
        className={cn(
          'flex shrink-0 flex-wrap items-center gap-2 border-b border-border px-3 py-1.5 text-[11px]',
          failed
            ? 'bg-destructive/10 text-destructive'
            : result.isError
              ? 'bg-amber-500/10 text-amber-700 dark:text-amber-400'
              : 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
        )}
      >
        {failed ? (
          <>
            <XCircle className="h-3.5 w-3.5" />
            <span className="font-medium">调用失败</span>
            {result.rpcCode !== 0 && (
              <span className="rounded-sm bg-destructive/15 px-1 font-mono">
                JSON-RPC {result.rpcCode}
              </span>
            )}
          </>
        ) : result.isError ? (
          <>
            <AlertTriangle className="h-3.5 w-3.5" />
            {/* 协议成功、工具自己说失败,这两者要分开:前者是配置/连接问题,
                后者是参数或业务问题,查的方向完全不同 */}
            <span className="font-medium">工具报告失败（协议层是成功的）</span>
          </>
        ) : (
          <>
            <CheckCircle2 className="h-3.5 w-3.5" />
            <span className="font-medium">成功</span>
          </>
        )}
        <span className="font-mono opacity-70">{result.method}</span>
        <span className="ml-auto tabular-nums opacity-70">{result.durationMs} ms</span>
      </div>

      {failed && (
        <p className="shrink-0 whitespace-pre-wrap break-words border-b border-border px-3 py-2 text-[11px] text-destructive">
          {result.error}
        </p>
      )}

      <div className="flex shrink-0 items-center gap-1 border-b border-border px-2 py-1">
        {(
          [
            ['text', '文本'],
            ['response', '原始响应'],
            ['request', '请求'],
          ] as const
        ).map(([k, label]) => (
          <button
            key={k}
            type="button"
            title={`查看：${label}`}
            onClick={() => setTab(k)}
            className={cn(
              'rounded px-2 py-0.5 text-[11px] transition-colors',
              tab === k ? 'bg-secondary font-medium text-foreground' : 'text-muted-foreground hover:bg-secondary/60',
            )}
          >
            {label}
          </button>
        ))}
        <button
          type="button"
          title="复制当前内容"
          onClick={() => void copy()}
          className="ml-auto rounded p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
        >
          {copied === tab ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
        </button>
      </div>

      <pre className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-words bg-background p-3 font-mono text-[11px] leading-relaxed">
        {body || '（空）'}
      </pre>
    </div>
  )
}
