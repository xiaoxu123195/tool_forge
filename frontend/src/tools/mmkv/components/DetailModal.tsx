import { useEffect, useState } from 'react'
import { Check, Copy, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { mmkv } from '../../../../wailsjs/go/models'
import { HEX_TYPE, bgOf, displayOf, labelOf, optionsOf } from '../valueTypes'

interface DetailContext {
  key: string
  index: number
  total: number
  value: mmkv.Value
  type: string
  /** 回后端取这个值的完整十六进制。表格里带的那份是截断过的 */
  fullHex: () => Promise<string>
}

interface Props {
  ctx: DetailContext
  onClose: () => void
}

export function DetailModal({ ctx, onClose }: Props) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const label = ctx.total > 1 ? `历史 ${ctx.index === 0 ? '最新' : ctx.index}` : '当前'
  // 后端一次就把所有解得通的读法都算好了,索性全列出来 ——
  // 以前要一个个点着循环才能看到别的类型长什么样
  const others = optionsOf(ctx.value).filter((t) => t !== ctx.type && t !== HEX_TYPE)

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-background/60 p-4 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex max-h-[85vh] w-full max-w-2xl flex-col rounded-lg border border-border bg-card shadow-xl"
      >
        <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-semibold">{ctx.key}</div>
            <div className="mt-0.5 flex items-center gap-2 text-[11px] text-muted-foreground">
              <span>{label}</span>
              <span>·</span>
              <span>{ctx.value.size} bytes</span>
              <span>·</span>
              <span
                className={cn(
                  'rounded-sm px-1.5 py-0.5 font-medium text-orange-600 dark:text-orange-400',
                  bgOf(ctx.type)
                )}
              >
                {labelOf(ctx.type)}
              </span>
            </div>
          </div>
          <button
            onClick={onClose}
            className="shrink-0 rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex-1 space-y-4 overflow-auto p-4">
          {/* 主展示：当前类型的完整解码值 */}
          <Section
            title={`${labelOf(ctx.type)} 解码`}
            text={displayOf(ctx.value, ctx.type)}
            className={bgOf(ctx.type)}
          />

          {/* 其它读法：MMKV 的值不带类型标记,同一串字节读成什么全看写入方,
              所以别的解释也一并摆出来,由人判断哪个才是原意 */}
          {others.length > 0 && (
            <section>
              <div className="mb-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                其它可能的读法
              </div>
              <div className="space-y-1.5">
                {others.map((t) => (
                  <div
                    key={t}
                    className={cn(
                      'flex items-start gap-2 rounded-md border border-border px-3 py-2 font-mono text-[12px]',
                      bgOf(t)
                    )}
                  >
                    <span className="shrink-0 text-[11px] text-orange-600 dark:text-orange-400">
                      ({labelOf(t)})
                    </span>
                    <span className="min-w-0 flex-1 break-all">
                      {displayOf(ctx.value, t)}
                    </span>
                    <CopyButton getText={() => displayOf(ctx.value, t)} />
                  </div>
                ))}
              </div>
            </section>
          )}

          {/* 辅展示：原始字节 hex */}
          <HexSection ctx={ctx} />
        </div>
      </div>
    </div>
  )
}

/**
 * 原始字节。表格里带过来的那份是截断过的 —— 一个文件几千个值,每个都带完整
 * 十六进制的话传过桥的数据能比文件本身还大。所以这里默认显示截断的那份,
 * 想要完整的按一下,再回后端读一次。
 */
function HexSection({ ctx }: { ctx: DetailContext }) {
  const [full, setFull] = useState('')
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  // 后端在截断处会加一句"还有 N 字节",没有这句就说明本来就是完整的
  const truncated = ctx.value.hex.includes('还有')

  const loadFull = async () => {
    setLoading(true)
    setErr('')
    try {
      setFull(await ctx.fullHex())
    } catch (e) {
      setErr(String(e))
    } finally {
      setLoading(false)
    }
  }

  return (
    <section>
      <div className="mb-1.5 flex items-center justify-between gap-2">
        <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
          原始字节（hex）
        </div>
        <div className="flex items-center gap-1">
          {truncated && !full && (
            <button
              onClick={loadFull}
              disabled={loading}
              className="rounded px-2 py-1 text-[11px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            >
              {loading ? '读取中…' : `读取完整 ${ctx.value.size} 字节`}
            </button>
          )}
          <CopyButton getText={() => full || ctx.value.hex} />
        </div>
      </div>
      {err && (
        <div className="mb-1.5 rounded-md border border-destructive/30 bg-destructive/10 px-2 py-1 text-[11px] text-destructive">
          {err}
        </div>
      )}
      <div className="whitespace-pre-wrap break-all rounded-md border border-border bg-muted/30 p-3 font-mono text-[12.5px] leading-relaxed text-muted-foreground">
        {full || ctx.value.hex}
      </div>
    </section>
  )
}

function Section({
  title,
  text,
  className,
}: {
  title: string
  text: string
  className?: string
}) {
  return (
    <section>
      <div className="mb-1.5 flex items-center justify-between">
        <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
          {title}
        </div>
        {!!text && <CopyButton getText={() => text} />}
      </div>
      <div
        className={cn(
          'whitespace-pre-wrap break-all rounded-md border border-border p-3 font-mono text-[12.5px] leading-relaxed',
          className
        )}
      >
        {text}
      </div>
    </section>
  )
}

function CopyButton({
  getText,
  className,
}: {
  getText: () => string
  className?: string
}) {
  const [copied, setCopied] = useState(false)
  const doCopy = async (e: React.MouseEvent) => {
    e.stopPropagation()
    try {
      await navigator.clipboard.writeText(getText())
      setCopied(true)
      setTimeout(() => setCopied(false), 1200)
    } catch {
      /* ignore */
    }
  }
  return (
    <button
      onClick={doCopy}
      title="复制"
      className={cn(
        'inline-flex shrink-0 items-center gap-1 rounded px-2 py-1 text-[11px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground',
        className
      )}
    >
      {copied ? (
        <>
          <Check className="h-3 w-3 text-emerald-500" />
          已复制
        </>
      ) : (
        <>
          <Copy className="h-3 w-3" />
          复制
        </>
      )}
    </button>
  )
}

export type { DetailContext }
