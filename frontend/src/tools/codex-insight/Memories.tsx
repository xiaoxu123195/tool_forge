import { useEffect, useMemo, useState } from 'react'
import {
  AlertCircle,
  ArrowLeft,
  Brain,
  Eye,
  File,
  FilePlus,
  Folder,
  Loader2,
  Pencil,
  Save,
  Trash2,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { CodeEditor, type EditorLanguage } from '@/components/tool/CodeEditor'
import { MarkdownPreview } from '@/components/tool/MarkdownPreview'
import { cn } from '@/lib/utils'
import {
  DeleteCodexMemory,
  ListCodexMemories,
  ReadCodexMemory,
  WriteCodexMemory,
} from '../../../wailsjs/go/main/App'
import type { codexinsight } from '../../../wailsjs/go/models'
import { formatDateTime, formatRelative } from './lib/format'
import { ConfirmDialog, PromptDialog } from './dialogs'

type MemoryFile = codexinsight.MemoryFile
type FileContent = codexinsight.MemoryFileContent

interface Props {
  reloadToken: number
}

type View =
  | { kind: 'list' }
  | { kind: 'file'; path: string }

export function Memories({ reloadToken }: Props) {
  const [view, setView] = useState<View>({ kind: 'list' })

  if (view.kind === 'file') {
    return (
      <FileEditor
        path={view.path}
        onBack={() => setView({ kind: 'list' })}
      />
    )
  }
  return <MemoryList reloadToken={reloadToken} onOpen={(p) => setView({ kind: 'file', path: p })} />
}

function MemoryList({
  reloadToken,
  onOpen,
}: {
  reloadToken: number
  onOpen: (path: string) => void
}) {
  const [files, setFiles] = useState<MemoryFile[] | null>(null)
  const [memoryDir, setMemoryDir] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [reloadNonce, setReloadNonce] = useState(0)
  const [showCreate, setShowCreate] = useState(false)
  const [toDelete, setToDelete] = useState<string | null>(null)

  const load = () => setReloadNonce((n) => n + 1)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    ListCodexMemories()
      .then((r) => {
        if (cancelled) return
        setFiles(r.files ?? [])
        setMemoryDir(r.memory_dir)
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
  }, [reloadToken, reloadNonce])

  const doCreate = async (rel: string) => {
    setShowCreate(false)
    try {
      setBusy(true)
      await WriteCodexMemory(rel, '')
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const doDelete = async () => {
    const path = toDelete
    setToDelete(null)
    if (!path) return
    try {
      setBusy(true)
      await DeleteCodexMemory(path)
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (loading && !files) {
    return <Loading text="正在读取 ~/.codex/memories ..." />
  }
  if (error) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
        <AlertCircle className="h-8 w-8 text-red-500" />
        <div className="max-w-md text-sm text-muted-foreground">{error}</div>
      </div>
    )
  }

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-3">
      <div className="flex items-center gap-3">
        <div className="min-w-0 flex-1 text-xs text-muted-foreground">
          <code className="rounded bg-secondary px-1.5 py-0.5 font-mono" title={memoryDir}>
            {memoryDir}
          </code>
        </div>
        <Button variant="default" size="sm" onClick={() => setShowCreate(true)} disabled={busy}>
          <FilePlus className="h-3.5 w-3.5" />
          新建文件
        </Button>
      </div>

      {files && files.length === 0 ? (
        <div className="flex h-40 flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-border bg-card text-center">
          <Brain className="h-8 w-8 text-indigo-500" />
          <p className="text-sm text-muted-foreground">还没有 memory 文件</p>
        </div>
      ) : (
        <ul className="space-y-1.5">
          {files?.map((f) => (
            <MemoryRow
              key={f.path}
              file={f}
              onOpen={() => !f.is_dir && onOpen(f.path)}
              onDelete={() => setToDelete(f.path)}
            />
          ))}
        </ul>
      )}

      <PromptDialog
        open={showCreate}
        title="新建 Memory 文件"
        label="相对路径"
        placeholder="例如 notes/project-a.md"
        confirmLabel="创建"
        onConfirm={doCreate}
        onCancel={() => setShowCreate(false)}
      />
      <ConfirmDialog
        open={toDelete !== null}
        title="删除"
        message={`将删除 ${toDelete}，此操作不可撤销。`}
        confirmLabel="删除"
        destructive
        onConfirm={doDelete}
        onCancel={() => setToDelete(null)}
      />
    </div>
  )
}

function MemoryRow({
  file,
  onOpen,
  onDelete,
}: {
  file: MemoryFile
  onOpen: () => void
  onDelete: () => void
}) {
  return (
    <li className="group flex items-center gap-2 rounded-md border border-border bg-card px-3 py-2 transition-colors hover:border-indigo-500/40 hover:bg-indigo-500/5">
      <button
        onClick={onOpen}
        disabled={file.is_dir}
        className="flex min-w-0 flex-1 items-center gap-2 text-left disabled:cursor-default"
      >
        {file.is_dir ? (
          <Folder className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <File className="h-3.5 w-3.5 shrink-0 text-indigo-500" />
        )}
        <span className="min-w-0 flex-1 truncate font-mono text-xs">{file.path}</span>
        <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
          {formatSize(file.size)}
        </span>
        <span className="shrink-0 text-[10px] text-muted-foreground">
          {formatRelative(file.updated_at)}
        </span>
      </button>
      <button
        onClick={onDelete}
        title="删除"
        className="hidden h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-red-500/10 hover:text-red-600 group-hover:inline-flex"
      >
        <Trash2 className="h-3.5 w-3.5" />
      </button>
    </li>
  )
}

function FileEditor({ path, onBack }: { path: string; onBack: () => void }) {
  const [content, setContent] = useState('')
  const [original, setOriginal] = useState('')
  const [meta, setMeta] = useState<FileContent | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [toast, setToast] = useState('')
  const [confirmLeave, setConfirmLeave] = useState(false)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    ReadCodexMemory(path)
      .then((r) => {
        if (cancelled) return
        setContent(r.content)
        setOriginal(r.content)
        setMeta(r)
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
  }, [path])

  const dirty = content !== original

  const save = async () => {
    if (!dirty) return
    try {
      setSaving(true)
      await WriteCodexMemory(path, content)
      setOriginal(content)
      setToast('已保存')
      setTimeout(() => setToast(''), 1500)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  const goBack = () => {
    if (dirty) {
      setConfirmLeave(true)
      return
    }
    onBack()
  }

  const language: EditorLanguage = useMemo(() => {
    const ext = path.toLowerCase().split('.').pop() ?? ''
    if (ext === 'md' || ext === 'markdown') return 'markdown'
    if (ext === 'ts' || ext === 'tsx') return 'typescript'
    if (ext === 'js' || ext === 'mjs' || ext === 'jsx') return 'javascript'
    if (ext === 'json') return 'json'
    if (ext === 'xml') return 'xml'
    if (ext === 'yaml' || ext === 'yml') return 'yaml'
    if (ext === 'ini' || ext === 'toml') return 'ini'
    return 'plaintext'
  }, [path])

  const isMarkdown = language === 'markdown'
  const [preview, setPreview] = useState(false)
  useEffect(() => {
    setPreview(false)
  }, [path])

  return (
    <div className="flex h-full flex-col gap-3 overflow-hidden">
      <div className="flex items-center gap-3">
        <Button variant="outline" size="sm" onClick={goBack}>
          <ArrowLeft className="h-3.5 w-3.5" />
          返回
        </Button>
        <div className="flex min-w-0 flex-1 items-center gap-2 text-xs">
          <Brain className="h-3.5 w-3.5 shrink-0 text-indigo-500" />
          <span className="min-w-0 flex-1 truncate font-mono">{path}</span>
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
        <Loading text="正在读取..." />
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
          className="flex-1 overflow-hidden rounded-md border border-border"
        />
      )}

      <ConfirmDialog
        open={confirmLeave}
        title="放弃修改？"
        message="当前文件有未保存的改动。"
        confirmLabel="放弃并离开"
        cancelLabel="留在这里"
        destructive
        onConfirm={() => {
          setConfirmLeave(false)
          onBack()
        }}
        onCancel={() => setConfirmLeave(false)}
      />
    </div>
  )
}

function Loading({ text }: { text: string }) {
  return (
    <div className="flex h-40 items-center justify-center gap-3 text-sm text-muted-foreground">
      <Loader2 className="h-5 w-5 animate-spin" />
      {text}
    </div>
  )
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

// 防止 cn 未使用
void cn
