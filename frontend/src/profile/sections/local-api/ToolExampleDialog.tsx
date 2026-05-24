import { useMemo, useState } from 'react'
import { createPortal } from 'react-dom'
import { AlertCircle, Check, Copy, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { TOOL_EXAMPLES, type ToolExampleSet } from './examples'

export interface ToolForDialog {
  name: string
  title: string
  description: string
  path: string
}

export interface APIConfigForDialog {
  port: number
  auth_enabled: boolean
  token: string
}

export function ToolExampleDialog({
  tool,
  config,
  onClose,
}: {
  tool: ToolForDialog
  config: APIConfigForDialog
  onClose: () => void
}) {
  const set: ToolExampleSet | undefined = TOOL_EXAMPLES[tool.name]
  const [tabIdx, setTabIdx] = useState(0)
  const [copied, setCopied] = useState<'body' | 'curl' | ''>('')

  const scenario = set?.scenarios[tabIdx]
  const url = `http://127.0.0.1:${config.port}${tool.path}`
  const bodyJson = useMemo(() => {
    if (!scenario) return '{}'
    return JSON.stringify(scenario.body, null, 2)
  }, [scenario])

  const curl = useMemo(() => {
    const authHeader = config.auth_enabled && config.token
      ? ` \\\n  -H "Authorization: Bearer ${config.token}"`
      : ''
    return `curl -X POST ${url}${authHeader} \\\n  -H "Content-Type: application/json" \\\n  -d '${bodyJson.replace(/\n/g, '')}'`
  }, [url, bodyJson, config.auth_enabled, config.token])

  const handleCopy = async (text: string, key: 'body' | 'curl') => {
    await navigator.clipboard.writeText(text)
    setCopied(key)
    setTimeout(() => setCopied(''), 1500)
  }

  return createPortal(
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center bg-black/40 p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div className="flex max-h-[85vh] w-[720px] max-w-full flex-col overflow-hidden rounded-xl border border-border bg-card shadow-2xl">
        <header className="flex h-12 shrink-0 items-center justify-between border-b border-border px-4">
          <div className="flex min-w-0 items-baseline gap-2">
            <h3 className="text-sm font-semibold">{tool.title}</h3>
            <span className="truncate font-mono text-[11px] text-muted-foreground">{tool.path}</span>
          </div>
          <button
            onClick={onClose}
            className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            aria-label="关闭"
          >
            <X className="h-4 w-4" />
          </button>
        </header>

        {set ? (
          <div className="min-h-0 flex-1 space-y-4 overflow-auto p-4">
            {/* 场景 tab */}
            <div className="flex flex-wrap gap-1.5">
              {set.scenarios.map((s, i) => (
                <button
                  key={i}
                  type="button"
                  onClick={() => setTabIdx(i)}
                  className={cn(
                    'inline-flex h-7 items-center rounded-md border px-2.5 text-xs transition-colors',
                    i === tabIdx
                      ? 'border-info bg-info/10 font-medium text-info'
                      : 'border-border text-muted-foreground hover:text-foreground',
                  )}
                >
                  {s.label}
                </button>
              ))}
            </div>

            {scenario?.hint && (
              <div className="rounded-md border border-info/30 bg-info/5 px-3 py-2 text-xs text-foreground/80">
                {scenario.hint}
              </div>
            )}

            {/* Request body */}
            <Section title="请求体 (JSON)">
              <CodeBlock
                code={bodyJson}
                onCopy={() => handleCopy(bodyJson, 'body')}
                copied={copied === 'body'}
              />
            </Section>

            {/* curl */}
            <Section title="curl 命令">
              <CodeBlock
                code={curl}
                onCopy={() => handleCopy(curl, 'curl')}
                copied={copied === 'curl'}
              />
            </Section>

            {/* 字段说明 */}
            {set.fields && set.fields.length > 0 && (
              <Section title="字段说明">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="border-b border-border text-muted-foreground">
                      <th className="py-1.5 pr-3 text-left font-medium">字段</th>
                      <th className="py-1.5 pr-3 text-left font-medium">类型</th>
                      <th className="py-1.5 text-left font-medium">说明</th>
                    </tr>
                  </thead>
                  <tbody>
                    {set.fields.map((f) => (
                      <tr key={f.name} className="border-b border-border/40 last:border-b-0">
                        <td className="py-1.5 pr-3 align-top font-mono">
                          {f.name}
                          {f.required && <span className="ml-1 text-destructive">*</span>}
                        </td>
                        <td className="py-1.5 pr-3 align-top font-mono text-muted-foreground">
                          {f.type ?? '-'}
                        </td>
                        <td className="py-1.5 align-top">{f.description}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </Section>
            )}

            {/* 备注 */}
            {set.notes && set.notes.length > 0 && (
              <Section title="备注">
                <ul className="space-y-1 text-xs text-muted-foreground">
                  {set.notes.map((n, i) => (
                    <li key={i} className="leading-relaxed">
                      · {n}
                    </li>
                  ))}
                </ul>
              </Section>
            )}
          </div>
        ) : (
          <div className="flex flex-1 items-center justify-center p-8">
            <div className="flex items-start gap-2 text-sm text-muted-foreground">
              <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
              <span>该工具尚未提供示例数据,等开发者补充</span>
            </div>
          </div>
        )}
      </div>
    </div>,
    document.body,
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-1.5">
      <h4 className="text-xs font-semibold text-muted-foreground">{title}</h4>
      {children}
    </section>
  )
}

function CodeBlock({
  code,
  onCopy,
  copied,
}: {
  code: string
  onCopy: () => void
  copied: boolean
}) {
  return (
    <div className="relative">
      <pre className="overflow-x-auto rounded-md border border-border bg-muted/30 p-3 pr-16 font-mono text-xs leading-relaxed">
        {code}
      </pre>
      <button
        type="button"
        onClick={onCopy}
        className="absolute right-2 top-2 inline-flex h-7 items-center gap-1 rounded-md border border-border bg-background px-2 text-xs hover:bg-accent"
      >
        {copied ? (
          <>
            <Check className="h-3 w-3 text-success" />
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
  )
}
