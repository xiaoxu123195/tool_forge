import { useMemo, useRef, useState } from 'react'
import { AlertCircle, CheckCircle2, Copy, Download, Upload } from 'lucide-react'
import { ToolShell } from '@/components/tool/ToolShell'
import { CodeEditor } from '@/components/tool/CodeEditor'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { downloadText } from '@/lib/download'
import { useFileDrop } from '@/lib/useFileDrop'
import { meta } from './meta'
import { formatXml, minifyXml, validateXml } from './logic'

const EXAMPLE = `<?xml version="1.0" encoding="UTF-8"?><catalog><book id="1"><title>Go 语言编程</title><year>2024</year></book><book id="2"><title>React 实战</title><year>2023</year></book></catalog>`

export default function XmlEditor() {
  const [input, setInput] = useState('')
  const [opError, setOpError] = useState('')
  const [notice, setNotice] = useState('')
  const fileInputRef = useRef<HTMLInputElement>(null)

  const status = useMemo(() => validateXml(input), [input])

  const loadText = (text: string, oversized: boolean) => {
    setInput(text)
    setOpError('')
    setNotice(oversized ? '文件较大，高亮与校验可能变慢' : '')
  }

  const handleImport = async (file: File) => {
    try {
      const text = await file.text()
      loadText(text, file.size > 5 * 1024 * 1024)
    } catch (e) {
      setOpError(e instanceof Error ? e.message : '读取文件失败')
    }
  }

  const { dragOver, dragHandlers } = useFileDrop({
    accept: ['.xml', '.svg', '.txt'],
    onLoad: (r) => {
      if (r.kind === 'text') loadText(r.text, r.oversized)
    },
    onError: (msg) => setOpError(msg),
  })

  const apply = (fn: (s: string) => string) => {
    try {
      setInput(fn(input))
      setOpError('')
    } catch (e) {
      setOpError(e instanceof Error ? e.message : '操作失败')
    }
  }

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      onClear={() => {
        setInput('')
        setOpError('')
        setNotice('')
      }}
      onLoadExample={() => {
        setInput(EXAMPLE)
        setOpError('')
        setNotice('')
      }}
      actions={
        <div className="flex flex-wrap items-center gap-1.5">
          <input
            ref={fileInputRef}
            type="file"
            accept=".xml,.svg,.txt,application/xml,text/xml,text/plain"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (f) handleImport(f)
              e.target.value = ''
            }}
          />
          <Button
            variant="outline"
            size="sm"
            onClick={() => fileInputRef.current?.click()}
          >
            <Upload className="h-3.5 w-3.5" />
            导入
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => apply((s) => formatXml(s, 2))}
            disabled={!input}
          >
            格式化
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => apply(minifyXml)}
            disabled={!input}
          >
            压缩
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => navigator.clipboard.writeText(input)}
            disabled={!input}
          >
            <Copy className="h-3.5 w-3.5" />
            复制
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() =>
              downloadText(input, 'data.xml', 'application/xml;charset=utf-8')
            }
            disabled={!input}
          >
            <Download className="h-3.5 w-3.5" />
            导出
          </Button>
        </div>
      }
    >
      <div
        {...dragHandlers}
        className={cn(
          'relative flex h-full flex-col gap-3 rounded-lg transition',
          dragOver && 'ring-2 ring-primary ring-offset-2 ring-offset-background'
        )}
      >
        <div className="flex items-center justify-between gap-2 text-xs">
          <StatusBadge status={status} hasInput={!!input} />
          <div className="flex items-center gap-3">
            {notice && <span className="text-amber-600 dark:text-amber-400">{notice}</span>}
            {opError && <span className="text-destructive">{opError}</span>}
          </div>
        </div>
        <CodeEditor
          value={input}
          onChange={setInput}
          language="xml"
          placeholder="粘贴 XML，或将 .xml 文件拖到此处…"
          className="flex-1 overflow-hidden rounded-lg border border-border"
          minHeight="100%"
        />
        {dragOver && (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center rounded-lg bg-primary/10 text-sm font-medium text-primary">
            松开以导入文件
          </div>
        )}
      </div>
    </ToolShell>
  )
}

function StatusBadge({
  status,
  hasInput,
}: {
  status: { valid: boolean; error?: string }
  hasInput: boolean
}) {
  if (!hasInput) {
    return <span className="text-muted-foreground">等待输入…</span>
  }
  if (status.valid) {
    return (
      <span className="flex items-center gap-1.5 text-emerald-600 dark:text-emerald-400">
        <CheckCircle2 className="h-3.5 w-3.5" /> 合法 XML
      </span>
    )
  }
  return (
    <span
      className={cn(
        'flex items-center gap-1.5',
        status.error ? 'text-destructive' : 'text-muted-foreground'
      )}
    >
      <AlertCircle className="h-3.5 w-3.5" />
      {status.error || '格式错误'}
    </span>
  )
}
