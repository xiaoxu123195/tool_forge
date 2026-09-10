import { useEffect, useMemo, useRef, useState } from 'react'
import { AlertCircle, CheckCircle2, Copy, Upload } from 'lucide-react'
import { ToolShell } from '@/components/tool/ToolShell'
import { CodeEditor } from '@/components/tool/CodeEditor'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { useNativeFileDrop } from '@/lib/useNativeFileDrop'
import {
  ParsePlistEncoded,
  ParsePlistFile,
  ParsePlistText,
  PickLocalFile,
} from '../../../wailsjs/go/main/App'
import type { plist } from '../../../wailsjs/go/models'
import { meta } from './meta'

// 解析全部在 Go 里做。这个页面以前在浏览器里自带一整套 bplist/XML/NSKeyedArchive
// 解析器,和后端那份是两套实现 —— 同一个文件两边解出不一样的结果时谁也说不清
// 该信哪个。现在页面只负责显示,解析和 MCP 工具走同一条代码路径。

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

/** 编辑器里打字后等多久才发一次解析请求。解析在后端,不该每敲一个键就过一次桥 */
const PARSE_DEBOUNCE_MS = 250

export default function PlistTool() {
  // xmlText 是编辑器里的内容,也是唯一的输入面。res 是后端给的三视图结果
  const [xmlText, setXmlText] = useState('')
  const [res, setRes] = useState<plist.DesktopResult | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [view, setView] = useState<ViewMode>('xml')
  const [importMode, setImportMode] = useState<'base64' | 'hex' | null>(null)
  const [importText, setImportText] = useState('')

  // 编辑器里打的字要重新解析,但从文件/粘贴导入时 xmlText 是后端刚给回来的,
  // 再解析一次纯属浪费(而且会把 format 从 binary 冲成 xml)
  const skipNextParse = useRef(false)

  useEffect(() => {
    if (skipNextParse.current) {
      skipNextParse.current = false
      return
    }
    if (!xmlText.trim()) {
      setRes(null)
      setError('')
      return
    }
    let cancelled = false
    const t = setTimeout(() => {
      ParsePlistText(xmlText)
        .then((r) => {
          if (cancelled) return
          setRes(r)
          setError('')
        })
        .catch((e) => {
          if (cancelled) return
          setRes(null)
          setError(String(e))
        })
    }, PARSE_DEBOUNCE_MS)
    return () => {
      cancelled = true
      clearTimeout(t)
    }
  }, [xmlText])

  /** 从后端结果重置整个页面状态 */
  const applyResult = (r: plist.DesktopResult) => {
    skipNextParse.current = true
    setXmlText(r.xml)
    setRes(r)
    setError('')
    setView('xml')
  }

  const runImport = async (fn: () => Promise<plist.DesktopResult>) => {
    setBusy(true)
    try {
      applyResult(await fn())
    } catch (e) {
      setRes(null)
      setError(String(e))
    } finally {
      setBusy(false)
    }
  }

  const openPath = (path: string) => runImport(() => ParsePlistFile(path))

  // 原生拖放给的是绝对路径(HTML5 拖放给不了),后端直接读文件,
  // 几十 MB 的 plist 不用先在 JS 里读一遍再 base64 过桥
  useNativeFileDrop((paths) => {
    if (paths.length > 0) openPath(paths[0])
  })

  const pickFile = async () => {
    const p = await PickLocalFile('选择 plist 文件')
    if (p) openPath(p)
  }

  const importFromEncoded = () =>
    runImport(async () => {
      const r = await ParsePlistEncoded(importText, importMode ?? '')
      setImportMode(null)
      setImportText('')
      return r
    })

  const displayText = useMemo(() => {
    if (view === 'xml') return xmlText
    if (!res) return ''
    try {
      return JSON.stringify(view === 'parsed' ? res.parsed : res.raw, null, 2)
    } catch (e) {
      return `/* 序列化失败: ${e instanceof Error ? e.message : String(e)} */`
    }
  }, [res, view, xmlText])

  const editorReadOnly = view !== 'xml'
  const hasContent = !!res || !!error || !!xmlText

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      onClear={() => {
        skipNextParse.current = true
        setXmlText('')
        setRes(null)
        setError('')
        setImportMode(null)
        setImportText('')
        setView('xml')
      }}
      onLoadExample={() => {
        setXmlText(EXAMPLE)
        setView('xml')
      }}
      actions={
        <div className="flex flex-wrap items-center gap-1.5">
          <Button variant="outline" size="sm" onClick={pickFile} disabled={busy}>
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
        style={{ ['--wails-drop-target' as never]: 'drop' }}
        className="relative flex h-full flex-col gap-3 rounded-lg"
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
                  : "62 70 6c 69 73 74 30 30 d4 ...  或 SQLite 的 X'62706c...'"
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
              <Button
                size="sm"
                onClick={importFromEncoded}
                disabled={!importText.trim() || busy}
              >
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
            <StatusBadge res={res} error={error} hasContent={hasContent} />
          </div>
        </div>

        {/* notes 是后端在解析时发现的事:循环引用、越界 UID、反向生成 XML 失败。
            这些不是错误(结果照样能看),但不说一声的话人会以为数据本来就长这样 */}
        {res && (res.notes?.length || res.xmlError) && !error && (
          <div className="space-y-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-400">
            {res.xmlError && <div>{res.xmlError}</div>}
            {res.notes?.map((n) => (
              <div key={n}>{n}</div>
            ))}
          </div>
        )}

        {error && (
          <div
            className="whitespace-pre-wrap break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive"
            title={error}
          >
            {error}
          </div>
        )}

        <CodeEditor
          value={displayText}
          onChange={editorReadOnly ? undefined : setXmlText}
          readOnly={editorReadOnly}
          language={view === 'xml' ? 'xml' : 'json'}
          placeholder="粘贴 XML plist，或将 .plist / .bplist 文件拖到此处…"
          className="flex-1 overflow-hidden rounded-lg border border-border"
          minHeight="100%"
        />
      </div>
    </ToolShell>
  )
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

const FORMAT_LABELS: Record<string, string> = {
  binary: '二进制 Plist',
  xml: 'XML Plist',
  openstep: 'OpenStep Plist',
  gnustep: 'GNUStep Plist',
}

function StatusBadge({
  res,
  error,
  hasContent,
}: {
  res: plist.DesktopResult | null
  error: string
  hasContent: boolean
}) {
  if (!hasContent) {
    return <span className="text-muted-foreground">等待输入…</span>
  }
  if (error) {
    return (
      <span className="flex items-center gap-1.5 text-destructive">
        <AlertCircle className="h-3.5 w-3.5" />
        解析失败
      </span>
    )
  }
  if (res) {
    return (
      <span className="flex items-center gap-1.5 text-emerald-600 dark:text-emerald-400">
        <CheckCircle2 className="h-3.5 w-3.5" />
        {FORMAT_LABELS[res.format] ?? res.format}
        {res.nsKeyed && ' · NSKeyedArchive'}
      </span>
    )
  }
  return null
}
