import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { Bug, RefreshCw, Trash2, X } from 'lucide-react'
import {
  ClearAIRequestTraces,
  GetAIRequestTrace,
  ListAIRequestTraces,
} from '../../../wailsjs/go/main/App'
import type { RequestTrace, TraceSummary } from './types'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

/**
 * 最近几次发往上游的请求留档。
 *
 * 接自建中转时,「请求失败」四个字提供的信息量约等于零 —— 分不清是我们发的参数
 * 它不认,还是它回的格式我们没解出来。这个面板把两边都摊开:
 * 左边是最近的请求列表,右边分「请求」和「响应」两页。
 *
 * 只记内存,关掉应用就没了;密钥在后端就已经打过码,这里拿到的本来就是脱敏后的。
 */
export function TraceDialog({
  conversationId,
  onClose,
}: {
  /** 当前会话;用于「只看当前会话」的过滤 */
  conversationId: string
  onClose: () => void
}) {
  const [list, setList] = useState<TraceSummary[]>([])
  const [onlyThis, setOnlyThis] = useState(true)
  const [pickedId, setPickedId] = useState('')
  const [detail, setDetail] = useState<RequestTrace | null>(null)
  const [tab, setTab] = useState<'request' | 'response'>('request')
  const [loadErr, setLoadErr] = useState('')

  const reload = async () => {
    const l = ((await ListAIRequestTraces().catch(() => [])) ??
      []) as unknown as TraceSummary[]
    setList(l)
    // 没选过就自动选最新那条 —— 打开面板多半就是为了看刚出问题的这次
    setPickedId((cur) => (cur && l.some((t) => t.id === cur) ? cur : (l[0]?.id ?? '')))
  }

  useEffect(() => {
    void reload()
  }, [])

  useEffect(() => {
    if (!pickedId) {
      setDetail(null)
      return
    }
    let alive = true
    void (async () => {
      try {
        const t = (await GetAIRequestTrace(pickedId)) as unknown as RequestTrace
        if (alive) {
          setDetail(t)
          setLoadErr('')
        }
      } catch (e) {
        if (alive) {
          setDetail(null)
          setLoadErr(String(e))
        }
      }
    })()
    return () => {
      alive = false
    }
  }, [pickedId])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const shown = onlyThis ? list.filter((t) => t.convId === conversationId) : list
  // 被过滤掉的条数要显式说出来。检测请求没有会话 id,勾着"只看当前会话"时
  // 它们全都不见 —— 而"检测失败想看看发了什么"恰恰是打开这个面板的主要理由之一
  const hidden = list.length - shown.length

  return createPortal(
    <div className="fixed inset-0 z-[70] flex items-center justify-center bg-black/50 p-6">
      <div className="flex h-full max-h-[760px] w-full max-w-5xl flex-col overflow-hidden rounded-lg border border-border bg-card shadow-xl">
        <header className="flex shrink-0 items-center gap-2 border-b border-border px-4 py-3">
          <Bug className="h-4 w-4 text-info" />
          <h3 className="text-sm font-semibold">请求留档</h3>
          <span className="text-[11px] text-muted-foreground">
            只在内存里 · 保留最近 40 次请求(合计不超过 8MB) · 密钥已打码
          </span>
          <div className="ml-auto flex items-center gap-2">
            <label className="flex cursor-pointer items-center gap-1 text-xs text-muted-foreground">
              <input
                type="checkbox"
                checked={onlyThis}
                onChange={(e) => setOnlyThis(e.target.checked)}
                className="h-3.5 w-3.5"
              />
              只看当前会话
            </label>
            <button
              type="button"
              onClick={() => void reload()}
              title="刷新"
              className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            >
              <RefreshCw className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              onClick={() => {
                void ClearAIRequestTraces().then(() => reload())
              }}
              title="清空"
              className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-destructive"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              onClick={onClose}
              className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        </header>

        <div className="flex min-h-0 flex-1">
          <ul className="w-64 shrink-0 overflow-auto border-r border-border p-2">
            {shown.length === 0 ? (
              <li className="px-2 py-6 text-center text-xs text-muted-foreground">
                {onlyThis ? '这个会话还没有发过请求' : '还没有记录 —— 问一句就有了'}
              </li>
            ) : (
              shown.map((t) => (
                <li key={t.id}>
                  <button
                    type="button"
                    onClick={() => setPickedId(t.id)}
                    className={cn(
                      'mb-1 w-full rounded-md px-2 py-1.5 text-left transition-colors',
                      pickedId === t.id ? 'bg-info/15' : 'hover:bg-secondary',
                    )}
                  >
                    <div className="flex items-center gap-1.5 text-xs">
                      <StatusDot t={t} />
                      {t.kind === 'test' && (
                        <span className="shrink-0 rounded bg-secondary px-1 text-[10px] text-muted-foreground">
                          检测
                        </span>
                      )}
                      <span className="min-w-0 flex-1 truncate font-mono">{t.model}</span>
                      {t.durationMs ? (
                        <span className="shrink-0 text-[10px] text-muted-foreground">
                          {(t.durationMs / 1000).toFixed(1)}s
                        </span>
                      ) : null}
                    </div>
                    <div className="truncate text-[10px] text-muted-foreground">
                      {new Date(t.ts).toLocaleTimeString()} · {t.providerName} · {t.frameCount} 帧
                    </div>
                  </button>
                </li>
              ))
            )}
            {hidden > 0 && (
              <li className="px-2 py-2 text-center text-[11px] text-muted-foreground">
                另有 {hidden} 条来自检测或其他会话,取消上面的勾选可以看到
              </li>
            )}
          </ul>

          <div className="flex min-w-0 flex-1 flex-col">
            {!detail ? (
              <div className="flex flex-1 items-center justify-center px-6 text-center text-xs text-muted-foreground">
                {loadErr || '左边选一条'}
              </div>
            ) : (
              <>
                <div className="shrink-0 space-y-1 border-b border-border px-4 py-2">
                  <div className="break-all font-mono text-[11px]">
                    <span className="text-muted-foreground">{detail.method} </span>
                    {detail.url}
                  </div>
                  <div className="flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
                    <span>{detail.endpoint}</span>
                    {detail.status ? (
                      <span
                        className={cn(
                          detail.status >= 200 && detail.status < 300
                            ? 'text-success'
                            : 'text-destructive',
                        )}
                      >
                        HTTP {detail.status}
                      </span>
                    ) : null}
                    {!detail.done && <span className="text-warning">进行中</span>}
                  </div>
                  {detail.error && (
                    <div className="whitespace-pre-wrap break-words rounded border border-destructive/40 bg-destructive/10 px-2 py-1 text-[11px] text-destructive">
                      {detail.error}
                    </div>
                  )}
                </div>

                <div className="flex shrink-0 gap-1 border-b border-border px-4 py-1.5">
                  {(
                    [
                      ['request', '请求'],
                      ['response', `响应 (${detail.frames?.length ?? 0} 帧)`],
                    ] as const
                  ).map(([k, label]) => (
                    <button
                      key={k}
                      type="button"
                      onClick={() => setTab(k)}
                      className={cn(
                        'rounded px-2 py-0.5 text-xs transition-colors',
                        tab === k
                          ? 'bg-info/15 text-info'
                          : 'text-muted-foreground hover:bg-secondary',
                      )}
                    >
                      {label}
                    </button>
                  ))}
                </div>

                <div className="min-h-0 flex-1 overflow-auto bg-background p-3">
                  {tab === 'request' ? (
                    <>
                      <Section title="请求头">
                        {(detail.headers ?? []).join('\n') || '(无)'}
                      </Section>
                      <Section title={'请求体' + (detail.bodyTruncated ? ' · 已截断' : '')}>
                        {prettyJSON(detail.body ?? '')}
                      </Section>
                    </>
                  ) : (
                    <Section
                      title={'原始 SSE' + (detail.framesTruncated ? ' · 已截断' : '')}
                    >
                      {(detail.frames ?? []).join('\n') || '(没有收到任何帧)'}
                    </Section>
                  )}
                </div>
              </>
            )}
          </div>
        </div>

        <footer className="flex shrink-0 items-center justify-end gap-2 border-t border-border px-4 py-2">
          <Button variant="secondary" size="sm" onClick={onClose}>
            关闭
          </Button>
        </footer>
      </div>
    </div>,
    document.body,
  )
}

function StatusDot({ t }: { t: TraceSummary }) {
  const cls = !t.done
    ? 'bg-warning'
    : t.error
      ? 'bg-destructive'
      : t.status && (t.status < 200 || t.status >= 300)
        ? 'bg-destructive'
        : 'bg-success'
  return <span className={cn('h-1.5 w-1.5 shrink-0 rounded-full', cls)} />
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="mb-3">
      <div className="mb-1 text-[11px] font-medium text-muted-foreground">{title}</div>
      <pre className="whitespace-pre-wrap break-all rounded border border-border bg-card p-2 font-mono text-[11px] leading-relaxed">
        {children}
      </pre>
    </div>
  )
}

/**
 * 请求体格式化。
 *
 * 解不动就原样返回 —— 被截断的请求体本来就是残缺 JSON,
 * 而那恰恰是最需要看的一种(附件太大把后半截切了,前面的参数还在)。
 */
function prettyJSON(raw: string): string {
  if (!raw.trim()) return '(空)'
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}
