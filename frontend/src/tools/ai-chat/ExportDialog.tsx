import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { Check, Copy, Download, FileText, X } from 'lucide-react'
import {
  ExportAIConversation,
  RenderAIConversationMarkdown,
} from '../../../wailsjs/go/main/App'
import { DEFAULT_EXPORT_OPTIONS, type ExportOptions } from './types'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

const ITEMS: { key: keyof ExportOptions; label: string; hint: string }[] = [
  { key: 'includeSystem', label: '系统提示', hint: '会话的角色设定' },
  { key: 'includeThinking', label: '思考过程', hint: '折叠块,通常比正文还长' },
  { key: 'includeTools', label: '工具调用', hint: '参数与结果,超长会截断' },
  { key: 'includeCitations', label: '引用来源', hint: '联网检索引到的网页' },
  { key: 'includeUsage', label: 'token 用量与耗时', hint: '写在每条回复的副标题上' },
  { key: 'embedImages', label: '内嵌图片', hint: '文件自带图,但体积会大很多' },
]

/**
 * 把会话导出成 Markdown。
 *
 * 预览是实时的:勾选项一变就重新问后端要一份。渲染在 Go 那边做而不是前端拼,
 * 是为了让"预览看到的"和"存进文件的"一定是同一串字符 —— 两边各拼一次,
 * 迟早会分叉成两个不一样的格式。
 */
export function ExportDialog({
  conversationId,
  title,
  onClose,
  onError,
}: {
  conversationId: string
  title: string
  onClose: () => void
  onError: (message: string) => void
}) {
  const [opt, setOpt] = useState<ExportOptions>(DEFAULT_EXPORT_OPTIONS)
  const [preview, setPreview] = useState('')
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)
  const [savedPath, setSavedPath] = useState('')

  useEffect(() => {
    let alive = true
    void (async () => {
      const md = await RenderAIConversationMarkdown(
        conversationId,
        opt as unknown as never,
      ).catch((e) => {
        onError(String(e))
        return ''
      })
      if (alive) setPreview(md as unknown as string)
    })()
    return () => {
      alive = false
    }
  }, [conversationId, opt])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const toggle = (key: keyof ExportOptions) => {
    setOpt((prev) => ({ ...prev, [key]: !prev[key] }))
    setSavedPath('') // 改了勾选,刚才那次保存的结果已经不代表当前设置了
  }

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(preview)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch (e) {
      onError('复制失败: ' + String(e))
    }
  }

  const onSave = async () => {
    setBusy(true)
    try {
      const path = (await ExportAIConversation(
        conversationId,
        opt as unknown as never,
      )) as unknown as string
      // 空路径 = 用户在保存对话框里点了取消。那不是失败,什么都不该弹
      if (path) setSavedPath(path)
    } catch (e) {
      onError(String(e))
    } finally {
      setBusy(false)
    }
  }

  const sizeKB = (new Blob([preview]).size / 1024).toFixed(preview.length > 102400 ? 0 : 1)

  return createPortal(
    <div className="fixed inset-0 z-[70] flex items-center justify-center bg-black/50 p-6">
      <div className="flex h-full max-h-[720px] w-full max-w-4xl flex-col overflow-hidden rounded-lg border border-border bg-card shadow-xl">
        <header className="flex shrink-0 items-center gap-2 border-b border-border px-4 py-3">
          <FileText className="h-4 w-4 text-info" />
          <h3 className="min-w-0 flex-1 truncate text-sm font-semibold">
            导出会话 · {title}
          </h3>
          <button
            type="button"
            onClick={onClose}
            className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </header>

        <div className="flex min-h-0 flex-1">
          <div className="w-56 shrink-0 space-y-1 overflow-auto border-r border-border p-3">
            <div className="mb-2 text-xs font-medium text-muted-foreground">导出内容</div>
            {ITEMS.map((it) => (
              <label
                key={it.key}
                className="flex cursor-pointer items-start gap-2 rounded-md px-2 py-1.5 transition-colors hover:bg-secondary"
              >
                <input
                  type="checkbox"
                  checked={!!opt[it.key]}
                  onChange={() => toggle(it.key)}
                  className="mt-0.5 h-3.5 w-3.5 shrink-0"
                />
                <span className="min-w-0">
                  <span className="block text-xs">{it.label}</span>
                  <span className="block text-[10px] leading-tight text-muted-foreground">
                    {it.hint}
                  </span>
                </span>
              </label>
            ))}
          </div>

          {/* 预览用等宽字体显示 Markdown 源码而不是渲染结果 —— 用户导出后
              拿到的就是这份源码,让他看见真实产物比看见渲染效果更有用 */}
          <div className="min-w-0 flex-1 overflow-auto bg-background p-4">
            <pre className="whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed text-muted-foreground">
              {preview || '正在生成预览…'}
            </pre>
          </div>
        </div>

        <footer className="flex shrink-0 items-center gap-2 border-t border-border px-4 py-3">
          <span className="text-xs text-muted-foreground">约 {sizeKB} KB</span>
          {savedPath && (
            <span className="min-w-0 flex-1 truncate text-xs text-success" title={savedPath}>
              已保存到 {savedPath}
            </span>
          )}
          <div className={cn('flex items-center gap-2', !savedPath && 'ml-auto')}>
            <Button variant="secondary" size="sm" onClick={() => void onCopy()}>
              {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              {copied ? '已复制' : '复制'}
            </Button>
            <Button size="sm" onClick={() => void onSave()} disabled={busy || !preview}>
              <Download className="h-3.5 w-3.5" />
              {busy ? '保存中…' : '保存为文件'}
            </Button>
          </div>
        </footer>
      </div>
    </div>,
    document.body,
  )
}
