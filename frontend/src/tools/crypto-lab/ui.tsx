import { useState } from 'react'
import { ArrowRight, Copy, RefreshCcw, Shuffle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import {
  bytesToBase64,
  bytesToHex,
  fromBytes,
  hexToBytes,
  randomBytes,
  toBytes,
  type DataEncoding,
} from './lib/encoding'

// ---- EncodingSelect: 一行小切换按钮 ----
export function EncodingSelect({
  value,
  onChange,
  options = ['utf8', 'hex', 'base64'],
}: {
  value: DataEncoding
  onChange: (v: DataEncoding) => void
  options?: DataEncoding[]
}) {
  return (
    <div className="inline-flex items-center rounded-md border border-border bg-background p-0.5">
      {options.map((o) => (
        <button
          key={o}
          onClick={() => onChange(o)}
          className={cn(
            'rounded-sm px-2 py-0.5 text-[10px] font-medium',
            value === o
              ? 'bg-info text-white'
              : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {o}
        </button>
      ))}
    </div>
  )
}

// ---- CopyableTextArea: 带编码切换与复制 ----
export function DataField({
  label,
  value,
  onChange,
  enc,
  onEnc,
  readOnly,
  rows = 4,
  allowEnc = ['utf8', 'hex', 'base64'],
  placeholder,
  mono,
  right,
}: {
  label: string
  value: string
  onChange?: (v: string) => void
  enc: DataEncoding
  onEnc: (v: DataEncoding) => void
  readOnly?: boolean
  rows?: number
  allowEnc?: DataEncoding[]
  placeholder?: string
  mono?: boolean
  right?: React.ReactNode
}) {
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between">
        <span className="text-xs text-muted-foreground">{label}</span>
        <div className="flex items-center gap-1">
          {right}
          <EncodingSelect value={enc} onChange={onEnc} options={allowEnc} />
          {value && (
            <button
              className="rounded p-1 text-muted-foreground hover:bg-accent"
              onClick={() => void navigator.clipboard.writeText(value)}
              title="复制"
            >
              <Copy className="h-3 w-3" />
            </button>
          )}
        </div>
      </div>
      <textarea
        value={value}
        onChange={(e) => onChange?.(e.target.value)}
        readOnly={readOnly}
        rows={rows}
        spellCheck={false}
        placeholder={placeholder}
        className={cn(
          'resize-y rounded-md border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary/50',
          readOnly && 'cursor-default bg-secondary/40',
          (mono || enc !== 'utf8') && 'font-mono text-xs',
        )}
      />
    </div>
  )
}

// ---- KeyIvInput: 输入 bytes 的字段（以 hex 为主，可切 base64/utf8/random） ----
export function BytesField({
  label,
  value,
  onChange,
  enc,
  onEnc,
  randomSize,
  requiredSize,
  placeholder,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  enc: DataEncoding
  onEnc: (v: DataEncoding) => void
  randomSize?: number // 若指定，提供 "随机" 按钮生成 N 字节，默认按 enc 输出
  requiredSize?: number
  placeholder?: string
}) {
  const [showLen, setShowLen] = useState(true)
  let bytesLen: number | undefined
  try {
    if (value) bytesLen = toBytes(value, enc).length
  } catch {
    // ignore
  }

  const onRandom = () => {
    if (!randomSize) return
    const b = randomBytes(randomSize)
    onChange(fromBytes(b, enc))
  }

  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs text-muted-foreground">{label}</span>
        <div className="flex items-center gap-1">
          {bytesLen != null && showLen && (
            <span
              className={cn(
                'font-mono text-[10px]',
                requiredSize && bytesLen !== requiredSize
                  ? 'text-red-500'
                  : 'text-muted-foreground',
              )}
              title={requiredSize ? `需要 ${requiredSize} 字节` : ''}
              onClick={() => setShowLen(false)}
            >
              {bytesLen} byte
            </span>
          )}
          <EncodingSelect value={enc} onChange={onEnc} />
          {randomSize && (
            <button
              onClick={onRandom}
              className="rounded p-1 text-muted-foreground hover:bg-accent"
              title={`随机生成 ${randomSize} 字节`}
            >
              <Shuffle className="h-3 w-3" />
            </button>
          )}
        </div>
      </div>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        spellCheck={false}
        className="rounded-md border border-border bg-background px-3 py-2 font-mono text-xs outline-none focus:border-primary/50"
      />
    </div>
  )
}

// ---- Section: 操作方向切换 + 执行按钮 + 结果区 ----
export function OpRow({
  direction,
  onChangeDirection,
  labels = ['加密', '解密'],
  onExecute,
  busy,
}: {
  direction: 'enc' | 'dec'
  onChangeDirection: (d: 'enc' | 'dec') => void
  labels?: [string, string]
  onExecute: () => void
  busy?: boolean
}) {
  return (
    <div className="flex items-center gap-2">
      <div className="inline-flex items-center rounded-md border border-border bg-background p-0.5">
        <button
          onClick={() => onChangeDirection('enc')}
          className={cn(
            'rounded-sm px-3 py-1 text-xs font-medium',
            direction === 'enc'
              ? 'bg-info text-white'
              : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {labels[0]}
        </button>
        <button
          onClick={() => onChangeDirection('dec')}
          className={cn(
            'rounded-sm px-3 py-1 text-xs font-medium',
            direction === 'dec'
              ? 'bg-info text-white'
              : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {labels[1]}
        </button>
      </div>
      <Button onClick={onExecute} disabled={busy} size="sm" className="ml-auto">
        {busy ? (
          <RefreshCcw className="h-3.5 w-3.5 animate-spin" />
        ) : (
          <ArrowRight className="h-3.5 w-3.5" />
        )}
        执行
      </Button>
    </div>
  )
}

export function ErrorBanner({ error }: { error: string }) {
  if (!error) return null
  return (
    <div className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">
      <span className="font-mono">{error}</span>
    </div>
  )
}

// 便捷工具：把 bytes 重新用另一种编码展示
export function reencode(value: string, from: DataEncoding, to: DataEncoding): string {
  return fromBytes(toBytes(value, from), to)
}

// 若 hex 或 base64 输入包含空白/不规范，尝试规整成同编码输出（验证用）
export function normalize(value: string, enc: DataEncoding): string {
  try {
    return fromBytes(toBytes(value, enc), enc)
  } catch {
    return value
  }
}

// 把 hex 转 base64 / base64 转 hex 的封装，给 IV 字段快速切换编码用
export { bytesToHex, hexToBytes, bytesToBase64 }
