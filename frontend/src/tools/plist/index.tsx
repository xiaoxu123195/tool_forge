import { useMemo, useRef, useState } from 'react'
import { AlertCircle, CheckCircle2, Copy, Upload } from 'lucide-react'
import { ToolShell } from '@/components/tool/ToolShell'
import { CodeEditor } from '@/components/tool/CodeEditor'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { useFileDrop } from '@/lib/useFileDrop'
import { meta } from './meta'
import {
  isNSKeyedArchive,
  parseAny,
  parseXmlPlist,
  toJson,
  toXmlPlist,
  unwrapNSKeyedArchive,
  type PlistValue,
} from './logic'

type ViewMode = 'parsed' | 'raw' | 'xml'

const EXAMPLE = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key>
  <string>com.example.toolforge</string>
  <key>CFBundleVersion</key>
  <string>1.0.0</string>
  <key>Enabled</key>
  <true/>
  <key>Retry</key>
  <integer>3</integer>
  <key>Tags</key>
  <array>
    <string>dev</string>
    <string>utils</string>
  </array>
</dict>
</plist>`

interface ParseState {
  xmlText: string
  parsed: PlistValue | null
  error: string
  source: 'xml' | 'binary' | 'empty'
  isArchive: boolean
  notice: string
}

const EMPTY: ParseState = {
  xmlText: '',
  parsed: null,
  error: '',
  source: 'empty',
  isArchive: false,
  notice: '',
}

export default function PlistTool() {
  const [state, setState] = useState<ParseState>(EMPTY)
  // 默认进 XML 视图：空状态就是可编辑输入区；加载后也让用户直观看到完整内容
  const [view, setView] = useState<ViewMode>('xml')
  const [importMode, setImportMode] = useState<'base64' | 'hex' | null>(null)
  const [importText, setImportText] = useState('')
  const fileInputRef = useRef<HTMLInputElement>(null)

  const displayText = useMemo(() => {
    if (view === 'xml') return state.xmlText
    if (!state.parsed) return ''
    const value =
      view === 'parsed' ? unwrapNSKeyedArchive(state.parsed as PlistValue) : state.parsed
    try {
      return toJson(value as PlistValue, 2)
    } catch (e) {
      return `/* 序列化失败: ${e instanceof Error ? e.message : String(e)} */`
    }
  }, [state, view])

  const editorLanguage = view === 'xml' ? 'xml' : 'json'
  const editorReadOnly = view !== 'xml'

  const handleXmlTextChange = (txt: string) => {
    setState((s) => parseFromXmlText(txt, s.notice))
  }

  const applyParsed = (
    parsed: PlistValue,
    source: 'xml' | 'binary',
    xmlText: string,
    notice = ''
  ) => {
    setState({
      xmlText,
      parsed,
      error: '',
      source,
      isArchive: isNSKeyedArchive(parsed),
      notice,
    })
    setView('xml')
  }

  const handleBytes = (buf: ArrayBuffer | Uint8Array, oversized: boolean) => {
    try {
      const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf)
      const { value, source } = parseAny(bytes)
      const xmlText = source === 'xml' ? new TextDecoder('utf-8').decode(bytes) : toXmlPlist(value)
      applyParsed(value, source, xmlText, oversized ? '文件较大，高亮与渲染可能变慢' : '')
    } catch (e) {
      setState({ ...EMPTY, error: e instanceof Error ? e.message : '解析失败' })
    }
  }

  const { dragOver, dragHandlers } = useFileDrop({
    accept: ['.plist', '.bplist', '.xml', '.txt'],
    binary: true,
    onLoad: (r) => {
      if (r.kind === 'binary') handleBytes(r.buffer, r.oversized)
    },
    onError: (msg) => setState({ ...EMPTY, error: msg }),
  })

  const importFromEncoded = () => {
    try {
      const { value, source } = parseAny(importText)
      const xmlText = source === 'xml' ? importText : toXmlPlist(value)
      applyParsed(value, source, xmlText)
      setImportMode(null)
      setImportText('')
    } catch (e) {
      setState({ ...EMPTY, error: e instanceof Error ? e.message : '解码失败' })
    }
  }

  const hasContent = !!state.parsed || !!state.error || !!state.xmlText

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      onClear={() => {
        setState(EMPTY)
        setImportMode(null)
        setImportText('')
        setView('xml')
      }}
      onLoadExample={() => {
        setState(parseFromXmlText(EXAMPLE))
        setView('xml')
      }}
      actions={
        <div className="flex flex-wrap items-center gap-1.5">
          <input
            ref={fileInputRef}
            type="file"
            accept=".plist,.bplist,.xml,.txt"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (f) {
                f.arrayBuffer().then((buf) =>
                  handleBytes(buf, f.size > 5 * 1024 * 1024)
                )
              }
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
            variant={importMode === 'base64' ? 'default' : 'outline'}
            size="sm"
            onClick={() => {
              setImportMode(importMode === 'base64' ? null : 'base64')
              setImportText('')
            }}
          >
            base64
          </Button>
          <Button
            variant={importMode === 'hex' ? 'default' : 'outline'}
            size="sm"
            onClick={() => {
              setImportMode(importMode === 'hex' ? null : 'hex')
              setImportText('')
            }}
          >
            hex
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => navigator.clipboard.writeText(displayText)}
            disabled={!displayText}
          >
            <Copy className="h-3.5 w-3.5" />
            复制
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
        {importMode && (
          <div className="space-y-2 rounded-md border border-border bg-card p-3">
            <div className="text-xs font-medium text-muted-foreground">
              从 {importMode} 粘贴二进制 Plist 数据：
            </div>
            <textarea
              value={importText}
              onChange={(e) => setImportText(e.target.value)}
              placeholder={
                importMode === 'base64'
                  ? 'YnBsaXN0MDDUAQIDBAUGBwhXJGFyY2hpdmVy...'
                  : '62 70 6c 69 73 74 30 30 d4 ...'
              }
              spellCheck={false}
              rows={3}
              className="w-full rounded-md border border-input bg-background px-2 py-1.5 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
            />
            <div className="flex justify-end gap-1.5">
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setImportMode(null)
                  setImportText('')
                }}
              >
                取消
              </Button>
              <Button size="sm" onClick={importFromEncoded} disabled={!importText.trim()}>
                解析
              </Button>
            </div>
          </div>
        )}

        <div className="flex items-center justify-between gap-3 text-xs">
          <div className="flex shrink-0 items-center gap-2">
            <ViewTab current={view} value="xml" onClick={setView}>
              XML 原文
            </ViewTab>
            <ViewTab current={view} value="parsed" onClick={setView}>
              解析结果
            </ViewTab>
            <ViewTab current={view} value="raw" onClick={setView}>
              原始结构
            </ViewTab>
          </div>
          <div className="flex min-w-0 shrink-0 items-center gap-3">
            <StatusBadge state={state} hasContent={hasContent} />
            {state.notice && !state.error && (
              <span className="truncate text-amber-600 dark:text-amber-400">
                {state.notice}
              </span>
            )}
          </div>
        </div>

        {state.error && (
          <div
            className="whitespace-pre-wrap break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive"
            title={state.error}
          >
            {state.error}
          </div>
        )}

        <CodeEditor
          value={displayText}
          onChange={editorReadOnly ? undefined : handleXmlTextChange}
          readOnly={editorReadOnly}
          language={editorLanguage}
          placeholder="粘贴 XML plist，或将 .plist / .bplist 文件拖到此处…"
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

function parseFromXmlText(xmlText: string, inheritNotice = ''): ParseState {
  if (!xmlText.trim()) return EMPTY
  try {
    const parsed = parseXmlPlist(xmlText)
    return {
      xmlText,
      parsed,
      error: '',
      source: 'xml',
      isArchive: isNSKeyedArchive(parsed),
      notice: inheritNotice,
    }
  } catch (e) {
    return {
      xmlText,
      parsed: null,
      error: e instanceof Error ? e.message : '解析失败',
      source: 'xml',
      isArchive: false,
      notice: '',
    }
  }
}

function ViewTab({
  current,
  value,
  onClick,
  children,
}: {
  current: ViewMode
  value: ViewMode
  onClick: (v: ViewMode) => void
  children: React.ReactNode
}) {
  const active = current === value
  return (
    <button
      type="button"
      onClick={() => onClick(value)}
      className={cn(
        'rounded-md px-2.5 py-1 text-xs transition',
        active
          ? 'bg-primary text-primary-foreground'
          : 'text-muted-foreground hover:bg-muted hover:text-foreground'
      )}
    >
      {children}
    </button>
  )
}

function StatusBadge({
  state,
  hasContent,
}: {
  state: ParseState
  hasContent: boolean
}) {
  if (!hasContent) {
    return <span className="text-muted-foreground">等待输入…</span>
  }
  if (state.error) {
    return (
      <span className="flex items-center gap-1.5 text-destructive">
        <AlertCircle className="h-3.5 w-3.5" />
        解析失败
      </span>
    )
  }
  if (state.parsed) {
    return (
      <span className="flex items-center gap-1.5 text-emerald-600 dark:text-emerald-400">
        <CheckCircle2 className="h-3.5 w-3.5" />
        {state.source === 'binary' ? '二进制 Plist' : 'XML Plist'}
        {state.isArchive && ' · NSKeyedArchive'}
      </span>
    )
  }
  return null
}
