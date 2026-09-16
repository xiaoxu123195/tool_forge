import { useEffect, useState } from 'react'
import { Check, Copy, Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import type { ocr } from '../../../wailsjs/go/models'

export interface OcrState {
  id: string
  busy: boolean
  result: ocr.Result | null
  error: string
}

/**
 * 认出来的字放在能改的框里,不直接塞回剪贴板:识别不会全对,
 * 一个 0 认成 O 的账号复制出去就是错的,得让人先看一眼
 */
export function OcrDialog({ state, onClose }: { state: OcrState; onClose: () => void }) {
  const [text, setText] = useState('')
  const [copied, setCopied] = useState(false)
  useEffect(() => {
    setText(state.result?.text ?? '')
  }, [state.result])

  const copy = async () => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  const lines = state.result?.lines.length ?? 0
  return (
    <Dialog
      open
      onClose={onClose}
      title="识别图里的文字"
      description={
        state.result
          ? `用的语言包：${state.result.lang} · 认出 ${lines} 行。识别不会全对，可以在下面改好再复制`
          : '走的是系统自带的 OCR，不上传'
      }
      footer={
        <>
          <Button variant="outline" size="sm" className="ml-auto" onClick={onClose}>
            关闭
          </Button>
          <Button size="sm" onClick={() => void copy()} disabled={!text}>
            {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
            {copied ? '已复制' : '复制文字'}
          </Button>
        </>
      }
    >
      {state.busy && (
        <p className="flex items-center gap-2 text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          识别中…
        </p>
      )}
      {state.error && (
        <p className="whitespace-pre-wrap break-words text-destructive">{state.error}</p>
      )}
      {state.result &&
        (lines === 0 ? (
          <p className="text-muted-foreground">这张图里没认出文字。</p>
        ) : (
          <textarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            spellCheck={false}
            rows={Math.min(16, Math.max(6, lines + 1))}
            className="w-full resize-y rounded-md border border-input bg-background px-3 py-2 font-mono text-xs leading-relaxed outline-none focus:ring-1 focus:ring-ring"
          />
        ))}
    </Dialog>
  )
}
