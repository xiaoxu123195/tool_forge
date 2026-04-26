import { useEffect, useMemo, useState } from 'react'
import { AlertCircle, Eye, FileText, Loader2, Pencil, Save, Settings } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { CodeEditor, type EditorLanguage } from '@/components/tool/CodeEditor'
import { MarkdownPreview } from '@/components/tool/MarkdownPreview'
import { cn } from '@/lib/utils'
import {
  ReadClaudeConfigFile,
  WriteClaudeConfigFile,
} from '../../../wailsjs/go/main/App'
import type { claudeinsight } from '../../../wailsjs/go/models'
import { formatDateTime } from './lib/format'

type ConfigFile = claudeinsight.ConfigFile
type FileKey = 'settings.json' | 'CLAUDE.md'

interface Props {
  reloadToken: number
}

export function Config({ reloadToken }: Props) {
  const [active, setActive] = useState<FileKey>('settings.json')

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-3 h-full overflow-hidden">
      <div className="flex items-center gap-2">
        <FileTabButton
          active={active === 'settings.json'}
          onClick={() => setActive('settings.json')}
          icon={<Settings className="h-3.5 w-3.5" />}
          label="settings.json"
        />
        <FileTabButton
          active={active === 'CLAUDE.md'}
          onClick={() => setActive('CLAUDE.md')}
          icon={<FileText className="h-3.5 w-3.5" />}
          label="CLAUDE.md"
        />
      </div>
      <FileEditor key={`${active}-${reloadToken}`} fileName={active} />
    </div>
  )
}

function FileTabButton({
  active,
  onClick,
  icon,
  label,
}: {
  active: boolean
  onClick: () => void
  icon: React.ReactNode
  label: string
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'inline-flex h-8 items-center gap-1.5 rounded-md px-3 font-mono text-xs font-medium transition-colors',
        active
          ? 'bg-info/15 text-info'
          : 'text-muted-foreground hover:bg-secondary'
      )}
    >
      {icon}
      {label}
    </button>
  )
}

function FileEditor({ fileName }: { fileName: FileKey }) {
  const [meta, setMeta] = useState<ConfigFile | null>(null)
  const [content, setContent] = useState('')
  const [original, setOriginal] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [toast, setToast] = useState('')

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    ReadClaudeConfigFile(fileName)
      .then((r) => {
        if (cancelled) return
        setMeta(r)
        setContent(r.content)
        setOriginal(r.content)
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [fileName])

  const dirty = content !== original

  const save = async () => {
    if (!dirty) return
    try {
      setSaving(true)
      await WriteClaudeConfigFile(fileName, content)
      setOriginal(content)
      setToast('已保存')
      setTimeout(() => setToast(''), 1500)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  const language: EditorLanguage = useMemo(() => {
    if (fileName === 'settings.json') return 'json'
    if (fileName === 'CLAUDE.md') return 'markdown'
    return 'plaintext'
  }, [fileName])

  const isMarkdown = language === 'markdown'
  const [preview, setPreview] = useState(false)
  // 切文件时自动退出预览
  useEffect(() => {
    setPreview(false)
  }, [fileName])

  return (
    <>
      <div className="flex items-center gap-3">
        <div className="flex min-w-0 flex-1 items-center gap-2 text-xs text-muted-foreground">
          <code className="truncate rounded bg-secondary px-1.5 py-0.5 font-mono" title={meta?.path || ''}>
            {meta?.path || fileName}
          </code>
          {!meta?.exists && (
            <span className="shrink-0 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] text-amber-700 dark:text-amber-300">
              尚未创建
            </span>
          )}
          {dirty && <span className="shrink-0 text-[10px] text-amber-600 dark:text-amber-400">●</span>}
        </div>
        {meta?.updated_at && (
          <span className="shrink-0 text-[10px] text-muted-foreground">
            {formatDateTime(meta.updated_at)}
          </span>
        )}
        {isMarkdown && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setPreview((v) => !v)}
            title={preview ? '切回编辑模式' : '预览渲染效果'}
          >
            {preview ? <Pencil className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
            {preview ? '编辑' : '预览'}
          </Button>
        )}
        <Button variant="default" size="sm" onClick={save} disabled={!dirty || saving}>
          <Save className="h-3.5 w-3.5" />
          {saving ? '保存中...' : toast || '保存'}
        </Button>
      </div>

      {loading ? (
        <Loading />
      ) : error ? (
        <div className="flex h-40 flex-col items-center justify-center gap-2 text-center">
          <AlertCircle className="h-8 w-8 text-red-500" />
          <div className="max-w-md text-sm text-muted-foreground">{error}</div>
        </div>
      ) : isMarkdown && preview ? (
        <MarkdownPreview
          value={content}
          className="flex-1 min-h-0 overflow-auto rounded-md border border-border bg-card px-5 py-4"
        />
      ) : (
        <CodeEditor
          value={content}
          onChange={setContent}
          language={language}
          minHeight="100%"
          placeholder={meta?.exists ? '' : `${fileName} 不存在，保存后将创建。`}
          className="flex-1 overflow-hidden rounded-md border border-border"
        />
      )}
    </>
  )
}

function Loading() {
  return (
    <div className="flex h-40 items-center justify-center gap-3 text-sm text-muted-foreground">
      <Loader2 className="h-5 w-5 animate-spin" />
      正在读取...
    </div>
  )
}
