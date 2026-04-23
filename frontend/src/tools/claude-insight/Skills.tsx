import { useEffect, useMemo, useState } from 'react'
import {
  AlertCircle,
  ArrowLeft,
  Eye,
  File,
  FilePlus,
  FolderPlus,
  Loader2,
  Pencil,
  Plus,
  Save,
  Sparkles,
  Trash2,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { CodeEditor, type EditorLanguage } from '@/components/tool/CodeEditor'
import { MarkdownPreview } from '@/components/tool/MarkdownPreview'
import { cn } from '@/lib/utils'
import {
  CreateClaudeSkill,
  DeleteClaudeSkill,
  DeleteClaudeSkillFile,
  ListClaudeSkillFiles,
  ListClaudeSkills,
  ReadClaudeSkillFile,
  WriteClaudeSkillFile,
} from '../../../wailsjs/go/main/App'
import type { claudeinsight } from '../../../wailsjs/go/models'
import { formatDateTime, formatRelative } from './lib/format'

type Summary = claudeinsight.SkillSummary
type SkillFile = claudeinsight.SkillFile
type FileContent = claudeinsight.SkillFileContent

interface SkillsProps {
  reloadToken: number
}

type View =
  | { kind: 'list' }
  | { kind: 'skill'; skill: string }
  | { kind: 'file'; skill: string; path: string }

export function Skills({ reloadToken }: SkillsProps) {
  const [view, setView] = useState<View>({ kind: 'list' })

  // 进入一个 skill 或一个 file。提供给子组件用的导航函数。
  const openSkill = (name: string) => setView({ kind: 'skill', skill: name })
  const openFile = (skill: string, path: string) => setView({ kind: 'file', skill, path })
  const backToList = () => setView({ kind: 'list' })
  const backToSkill = (skill: string) => setView({ kind: 'skill', skill })

  if (view.kind === 'file') {
    return (
      <FileEditor
        skill={view.skill}
        path={view.path}
        onBack={() => backToSkill(view.skill)}
      />
    )
  }
  if (view.kind === 'skill') {
    return (
      <SkillDetail
        skill={view.skill}
        onBack={backToList}
        onOpenFile={(path) => openFile(view.skill, path)}
        reloadToken={reloadToken}
      />
    )
  }
  return <SkillsList onOpen={openSkill} reloadToken={reloadToken} />
}

// ---------- 列表页 ----------

function SkillsList({
  onOpen,
  reloadToken,
}: {
  onOpen: (name: string) => void
  reloadToken: number
}) {
  const [items, setItems] = useState<Summary[] | null>(null)
  const [skillDir, setSkillDir] = useState('')
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
    ListClaudeSkills()
      .then((r) => {
        if (!cancelled) {
          setItems(r.items ?? [])
          setSkillDir(r.skill_dir)
        }
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

  const doCreate = async (name: string) => {
    setShowCreate(false)
    try {
      setBusy(true)
      await CreateClaudeSkill(name)
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const doDelete = async () => {
    const name = toDelete
    setToDelete(null)
    if (!name) return
    try {
      setBusy(true)
      await DeleteClaudeSkill(name)
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (loading && !items) {
    return <Loading text="正在扫描 ~/.claude/skills ..." />
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
    <div className="mx-auto flex max-w-5xl flex-col gap-4">
      <div className="flex items-center gap-3">
        <div className="min-w-0 flex-1 text-xs text-muted-foreground">
          <code className="rounded bg-secondary px-1.5 py-0.5 font-mono" title={skillDir}>
            {skillDir}
          </code>
        </div>
        <Button variant="default" size="sm" onClick={() => setShowCreate(true)} disabled={busy}>
          <Plus className="h-3.5 w-3.5" />
          新建 Skill
        </Button>
      </div>

      {items && items.length === 0 ? (
        <EmptySkills onCreate={() => setShowCreate(true)} />
      ) : (
        <ul className="grid grid-cols-1 gap-2 md:grid-cols-2">
          {items?.map((s) => (
            <SkillCard
              key={s.name}
              item={s}
              onOpen={() => onOpen(s.name)}
              onDelete={() => setToDelete(s.name)}
            />
          ))}
        </ul>
      )}

      <PromptDialog
        open={showCreate}
        title="新建 Skill"
        label="名称"
        placeholder="例如 code-reviewer"
        confirmLabel="创建"
        validate={(v) =>
          /^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(v)
            ? null
            : '只能包含字母、数字、下划线、连字符和点,首字符为字母数字'
        }
        onConfirm={doCreate}
        onCancel={() => setShowCreate(false)}
      />
      <ConfirmDialog
        open={toDelete !== null}
        title={`删除 Skill "${toDelete}"`}
        message={`将永久删除 ${toDelete} 及其所有文件，此操作不可撤销。`}
        confirmLabel="删除"
        destructive
        onConfirm={doDelete}
        onCancel={() => setToDelete(null)}
      />
    </div>
  )
}

function EmptySkills({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="flex h-64 flex-col items-center justify-center gap-3 rounded-lg border border-dashed border-border bg-card text-center">
      <Sparkles className="h-8 w-8 text-indigo-500" />
      <div className="space-y-1">
        <h2 className="text-sm font-medium">还没有 skill</h2>
        <p className="max-w-md text-xs text-muted-foreground">
          skill 是一段可复用的指令或工作流,Claude Code 会根据它调整行为。
        </p>
      </div>
      <Button size="sm" onClick={onCreate}>
        <Plus className="h-3.5 w-3.5" />
        新建第一个 Skill
      </Button>
    </div>
  )
}

function SkillCard({
  item,
  onOpen,
  onDelete,
}: {
  item: Summary
  onOpen: () => void
  onDelete: () => void
}) {
  return (
    <li className="group relative rounded-lg border border-border bg-card transition-colors hover:border-indigo-500/40 hover:bg-indigo-500/5">
      <button onClick={onOpen} className="flex w-full flex-col items-start gap-1.5 p-3 text-left">
        <div className="flex w-full items-center gap-2">
          <Sparkles className="h-3.5 w-3.5 shrink-0 text-indigo-500" />
          <span className="min-w-0 flex-1 truncate text-sm font-medium">{item.name}</span>
          <span className="shrink-0 rounded bg-secondary px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
            {item.file_count} 个文件
          </span>
        </div>
        <div className="line-clamp-2 text-xs text-muted-foreground">
          {item.description || (
            <span className="italic">{item.has_skill_md ? '（无描述）' : '缺少 SKILL.md'}</span>
          )}
        </div>
        {item.updated_at && (
          <div className="text-[10px] text-muted-foreground/70">
            更新于 {formatRelative(item.updated_at)}
          </div>
        )}
      </button>
      <button
        onClick={(e) => {
          e.stopPropagation()
          onDelete()
        }}
        title="删除"
        className="absolute right-2 top-2 hidden h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-red-500/10 hover:text-red-600 group-hover:inline-flex"
      >
        <Trash2 className="h-3.5 w-3.5" />
      </button>
    </li>
  )
}

// ---------- 单个 Skill 详情(文件列表) ----------

function SkillDetail({
  skill,
  onBack,
  onOpenFile,
  reloadToken,
}: {
  skill: string
  onBack: () => void
  onOpenFile: (path: string) => void
  reloadToken: number
}) {
  const [files, setFiles] = useState<SkillFile[] | null>(null)
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
    ListClaudeSkillFiles(skill)
      .then((r) => {
        if (!cancelled) setFiles(r.files ?? [])
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
  }, [skill, reloadToken, reloadNonce])

  const doCreateFile = async (rel: string) => {
    setShowCreate(false)
    try {
      setBusy(true)
      await WriteClaudeSkillFile(skill, rel, '')
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const doDeleteFile = async () => {
    const path = toDelete
    setToDelete(null)
    if (!path) return
    try {
      setBusy(true)
      await DeleteClaudeSkillFile(skill, path)
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (loading && !files) {
    return <Loading text="正在读取..." />
  }
  if (error) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
        <AlertCircle className="h-8 w-8 text-red-500" />
        <div className="max-w-md text-sm text-muted-foreground">{error}</div>
        <Button variant="outline" size="sm" onClick={onBack}>
          <ArrowLeft className="h-3.5 w-3.5" />
          返回列表
        </Button>
      </div>
    )
  }

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-3">
      <div className="flex items-center gap-3">
        <Button variant="outline" size="sm" onClick={onBack}>
          <ArrowLeft className="h-3.5 w-3.5" />
          返回列表
        </Button>
        <div className="flex min-w-0 flex-1 items-center gap-2 text-xs">
          <Sparkles className="h-3.5 w-3.5 shrink-0 text-indigo-500" />
          <span className="truncate font-mono text-muted-foreground">{skill}</span>
        </div>
        <Button variant="default" size="sm" onClick={() => setShowCreate(true)} disabled={busy}>
          <FilePlus className="h-3.5 w-3.5" />
          新建文件
        </Button>
      </div>

      {files && files.length === 0 ? (
        <div className="flex h-40 items-center justify-center rounded-lg border border-dashed border-border bg-card text-sm text-muted-foreground">
          这个 skill 下还没有文件
        </div>
      ) : (
        <ul className="space-y-1.5">
          {files?.map((f) => (
            <FileRow
              key={f.path}
              file={f}
              onOpen={() => !f.is_dir && onOpenFile(f.path)}
              onDelete={() => setToDelete(f.path)}
            />
          ))}
        </ul>
      )}

      <PromptDialog
        open={showCreate}
        title="新建文件"
        label="相对路径"
        placeholder="例如 helpers/util.md"
        confirmLabel="创建"
        onConfirm={doCreateFile}
        onCancel={() => setShowCreate(false)}
      />
      <ConfirmDialog
        open={toDelete !== null}
        title="删除文件"
        message={`将删除 ${toDelete}，此操作不可撤销。`}
        confirmLabel="删除"
        destructive
        onConfirm={doDeleteFile}
        onCancel={() => setToDelete(null)}
      />
    </div>
  )
}

function FileRow({
  file,
  onOpen,
  onDelete,
}: {
  file: SkillFile
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
          <FolderPlus className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
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

// ---------- 文件编辑器 ----------

function FileEditor({
  skill,
  path,
  onBack,
}: {
  skill: string
  path: string
  onBack: () => void
}) {
  const [content, setContent] = useState<string>('')
  const [original, setOriginal] = useState<string>('')
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
    ReadClaudeSkillFile(skill, path)
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
  }, [skill, path])

  const dirty = content !== original

  const save = async () => {
    if (!dirty) return
    try {
      setSaving(true)
      await WriteClaudeSkillFile(skill, path, content)
      setOriginal(content)
      setToast('已保存')
      setTimeout(() => setToast(''), 1500)
    } catch (e) {
      alert(e instanceof Error ? e.message : String(e))
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
    if (ext === 'js' || ext === 'jsx' || ext === 'mjs') return 'javascript'
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
          <span className="shrink-0 font-mono text-muted-foreground">{skill}</span>
          <span className="shrink-0 text-muted-foreground">/</span>
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
        message="当前文件有未保存的改动。如果现在离开，改动将丢失。"
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

// ---------- 共享 ----------

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

// ---------- 自定义对话框(替代 window.confirm / window.prompt 的原生外观) ----------

function DialogShell({
  open,
  onClose,
  children,
}: {
  open: boolean
  onClose: () => void
  children: React.ReactNode
}) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])
  if (!open) return null
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
    >
      <div
        role="dialog"
        className="w-full max-w-sm overflow-hidden rounded-lg border border-border bg-card shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        {children}
      </div>
    </div>
  )
}

export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel = '确定',
  cancelLabel = '取消',
  destructive,
  onConfirm,
  onCancel,
}: {
  open: boolean
  title: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  destructive?: boolean
  onConfirm: () => void
  onCancel: () => void
}) {
  return (
    <DialogShell open={open} onClose={onCancel}>
      <div className="space-y-3 p-5">
        <h3 className="text-sm font-semibold">{title}</h3>
        <p className="whitespace-pre-wrap text-sm text-muted-foreground">{message}</p>
      </div>
      <div className="flex justify-end gap-2 border-t border-border bg-secondary/30 px-4 py-3">
        <Button variant="ghost" size="sm" onClick={onCancel}>
          {cancelLabel}
        </Button>
        <Button
          size="sm"
          variant={destructive ? 'destructive' : 'default'}
          onClick={onConfirm}
          autoFocus
        >
          {confirmLabel}
        </Button>
      </div>
    </DialogShell>
  )
}

export function PromptDialog({
  open,
  title,
  label,
  defaultValue = '',
  placeholder,
  confirmLabel = '确定',
  cancelLabel = '取消',
  validate,
  onConfirm,
  onCancel,
}: {
  open: boolean
  title: string
  label: string
  defaultValue?: string
  placeholder?: string
  confirmLabel?: string
  cancelLabel?: string
  validate?: (v: string) => string | null
  onConfirm: (value: string) => void
  onCancel: () => void
}) {
  const [value, setValue] = useState(defaultValue)
  const [err, setErr] = useState<string | null>(null)

  // 每次打开时重置输入框
  useEffect(() => {
    if (open) {
      setValue(defaultValue)
      setErr(null)
    }
  }, [open, defaultValue])

  const submit = () => {
    const v = value.trim()
    if (!v) {
      setErr('不能为空')
      return
    }
    if (validate) {
      const msg = validate(v)
      if (msg) {
        setErr(msg)
        return
      }
    }
    onConfirm(v)
  }

  return (
    <DialogShell open={open} onClose={onCancel}>
      <div className="space-y-3 p-5">
        <h3 className="text-sm font-semibold">{title}</h3>
        <label className="block space-y-1.5">
          <span className="text-xs text-muted-foreground">{label}</span>
          <input
            autoFocus
            value={value}
            onChange={(e) => {
              setValue(e.target.value)
              if (err) setErr(null)
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                submit()
              }
            }}
            placeholder={placeholder}
            className={cn(
              'h-9 w-full rounded-md border bg-background px-2 text-sm outline-none focus:border-foreground/30',
              err ? 'border-red-500/60' : 'border-border'
            )}
          />
          {err && <span className="text-xs text-red-600 dark:text-red-400">{err}</span>}
        </label>
      </div>
      <div className="flex justify-end gap-2 border-t border-border bg-secondary/30 px-4 py-3">
        <Button variant="ghost" size="sm" onClick={onCancel}>
          {cancelLabel}
        </Button>
        <Button size="sm" onClick={submit}>
          {confirmLabel}
        </Button>
      </div>
    </DialogShell>
  )
}
