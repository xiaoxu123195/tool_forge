import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import { Check, Copy } from 'lucide-react'
import { cn } from '@/lib/utils'

interface Props {
  value: string
  className?: string
}

/**
 * 工具箱内通用的 Markdown 预览。
 *
 * 关键点:外观由 `globals.css` 里 `.markdown-preview` 控制(浅/深主题双套);
 * react-markdown v10 不再给 `code` 组件传 `inline` prop,用
 * `:not(pre) > code` vs `pre > code` 的 CSS 选择器区分才可靠。
 *
 * 块级代码(`<pre>`)被 `CodeBlock` 组件包装,加上语言徽标 + 复制按钮。
 */
export function MarkdownPreview({ value, className }: Props) {
  if (!value.trim()) {
    return (
      <div
        className={cn(
          'flex h-full items-center justify-center text-sm italic text-muted-foreground',
          className,
        )}
      >
        （空 Markdown）
      </div>
    )
  }

  return (
    <div
      className={cn(
        'markdown-preview',
        'prose prose-sm dark:prose-invert max-w-none',
        className,
      )}
    >
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
        components={{
          a: ({ children, ...props }) => (
            <a {...props} target="_blank" rel="noreferrer">
              {children}
            </a>
          ),
          input: ({ type, checked, ...props }) =>
            type === 'checkbox' ? (
              <input
                type="checkbox"
                checked={checked}
                readOnly
                className="mr-1 -mb-0.5 align-middle accent-info"
                {...props}
              />
            ) : (
              <input {...props} type={type} />
            ),
          pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
        }}
      >
        {value}
      </ReactMarkdown>
    </div>
  )
}

/** 块级代码:语言徽标 + 复制按钮 + 原 <pre> 内容 */
function CodeBlock({ children }: { children: React.ReactNode }) {
  const [copied, setCopied] = useState(false)
  // react-markdown 把 fenced code 渲成 <pre><code className="language-xxx hljs">...</code></pre>,
  // 这里 children 通常就是单一的 code 元素;从中提取语言 + 文本
  const codeElement = (children as any)?.props ? (children as any) : null
  const className: string = codeElement?.props?.className ?? ''
  const langMatch = className.match(/language-([\w-]+)/)
  const lang = langMatch ? langMatch[1] : ''
  const codeText = extractText(codeElement?.props?.children ?? '')

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(codeText)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // ignored
    }
  }

  return (
    <div className="group/code relative my-2">
      {(lang || codeText) && (
        <div className="absolute right-2 top-2 z-10 flex items-center gap-2 text-[10px] text-muted-foreground opacity-0 transition-opacity group-hover/code:opacity-100">
          {lang && (
            <span className="rounded bg-secondary/70 px-1.5 py-0.5 font-mono uppercase tracking-wider">
              {lang}
            </span>
          )}
          <button
            type="button"
            onClick={onCopy}
            className="flex h-6 items-center gap-1 rounded bg-secondary/70 px-1.5 transition-colors hover:bg-secondary hover:text-foreground"
            title="复制代码"
          >
            {copied ? (
              <>
                <Check className="h-3 w-3" />
                已复制
              </>
            ) : (
              <>
                <Copy className="h-3 w-3" />
                复制
              </>
            )}
          </button>
        </div>
      )}
      <pre>{children}</pre>
    </div>
  )
}

function extractText(node: React.ReactNode): string {
  if (node == null || node === false) return ''
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(extractText).join('')
  if (typeof node === 'object' && 'props' in (node as any)) {
    return extractText((node as any).props.children)
  }
  return ''
}
