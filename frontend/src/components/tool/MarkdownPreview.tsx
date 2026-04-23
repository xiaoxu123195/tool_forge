import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import 'highlight.js/styles/github-dark.css'
import { cn } from '@/lib/utils'

interface Props {
  value: string
  className?: string
}

/**
 * 工具箱内通用的 Markdown 预览。
 *
 * 关键点：所有外观都走 `globals.css` 里 `.markdown-preview` 的真 CSS，
 * 不再靠 Tailwind arbitrary value 或 components override 来区分行内/块级 code
 * —— react-markdown v10 已经不再给 `code` 组件传 `inline` prop，
 *    用 `:not(pre) > code` vs `pre > code` 的 CSS 选择器区分才可靠。
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
                className="mr-1 -mb-0.5 align-middle accent-indigo-500"
                {...props}
              />
            ) : (
              <input {...props} type={type} />
            ),
        }}
      >
        {value}
      </ReactMarkdown>
    </div>
  )
}
