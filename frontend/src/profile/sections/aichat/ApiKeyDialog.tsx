import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Check, Copy, Minus, Pencil, Plus, X } from 'lucide-react'
import type { APIKeyEntry, Provider } from '@/tools/ai-chat/types'
import { useConfirm } from '@/components/ui/confirm'
import { cn } from '@/lib/utils'
import { maskKey, splitKeys } from './ApiKeyField'

/**
 * 一个供应商下多把 API 密钥的管理弹窗。
 *
 * 主界面那个输入框已经能改"启用中的密钥"了,这里补的是它表达不了的东西:
 * 单独停用某一把(留着但先不用)、给密钥加备注、复制、逐条删。
 */
export function ApiKeyDialog({
  provider,
  keys,
  onClose,
  onSave,
}: {
  provider: Provider
  keys: APIKeyEntry[]
  onClose: () => void
  /** 返回 true 表示写盘成功 */
  onSave: (next: APIKeyEntry[]) => Promise<boolean>
}) {
  const dialog = useConfirm()
  const [editingId, setEditingId] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !editingId && !adding) onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, editingId, adding])

  const commit = async (next: APIKeyEntry[]) => {
    setBusy(true)
    try {
      const ok = await onSave(next)
      if (ok) {
        setEditingId(null)
        setAdding(false)
      }
    } finally {
      setBusy(false)
    }
  }

  const onDelete = async (entry: APIKeyEntry) => {
    const ok = await dialog({
      title: '删除密钥',
      message: `确认删除${entry.label ? `「${entry.label}」` : ''} ${maskKey(entry.key)}?`,
      danger: true,
      confirmLabel: '删除',
    })
    if (!ok) return
    await commit(keys.filter((k) => k.id !== entry.id))
  }

  const enabled = keys.filter((k) => !k.disabled).length

  return createPortal(
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center bg-black/40 p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget && !editingId && !adding) onClose()
      }}
    >
      <div className="flex max-h-[80vh] w-[560px] max-w-full flex-col overflow-hidden rounded-xl border border-border bg-card shadow-2xl">
        <header className="flex shrink-0 items-start justify-between border-b border-border px-4 py-3">
          <div>
            <h3 className="text-sm font-semibold">API 密钥管理</h3>
            <p className="mt-0.5 text-[11px] text-muted-foreground">
              管理「{provider.name}」的多把 API Key
            </p>
          </div>
          <button
            onClick={onClose}
            className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </header>

        <div className="min-h-0 flex-1 space-y-1.5 overflow-auto p-4">
          {keys.length === 0 && !adding && (
            <div className="rounded-md border border-dashed border-border p-6 text-center text-xs text-muted-foreground">
              还没有密钥。点下面「添加」填入,可以一次粘贴多把。
            </div>
          )}
          {keys.map((k) =>
            editingId === k.id ? (
              <KeyEditor
                key={k.id}
                initial={k}
                existing={keys}
                onCancel={() => setEditingId(null)}
                onSave={(e) => void commit(keys.map((x) => (x.id === e.id ? e : x)))}
              />
            ) : (
              <KeyRow
                key={k.id}
                entry={k}
                busy={busy}
                onEdit={() => {
                  setEditingId(k.id)
                  setAdding(false)
                }}
                onToggle={() =>
                  void commit(keys.map((x) => (x.id === k.id ? { ...x, disabled: !x.disabled } : x)))
                }
                onDelete={() => void onDelete(k)}
              />
            ),
          )}
          {adding && (
            <KeyEditor
              initial={{ id: '', key: '', label: '' }}
              existing={keys}
              allowBulk
              onCancel={() => setAdding(false)}
              onSave={(e) => void commit([...keys, e])}
              onBulk={(text) => {
                const parts = splitKeys(text).filter((t) => !keys.some((k) => k.key === t))
                if (parts.length === 0) {
                  setAdding(false)
                  return
                }
                void commit([...keys, ...parts.map((key) => ({ id: '', key }))])
              }}
            />
          )}
        </div>

        <footer className="flex shrink-0 items-center justify-between border-t border-border px-4 py-2.5">
          <span className="text-[11px] text-muted-foreground">
            {enabled} / {keys.length} 已启用
          </span>
          <button
            type="button"
            onClick={() => {
              setAdding(true)
              setEditingId(null)
            }}
            disabled={adding}
            className="flex h-7 items-center gap-1.5 rounded-md border border-border px-2.5 text-xs text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:opacity-50"
          >
            <Plus className="h-3.5 w-3.5" />
            添加
          </button>
        </footer>
      </div>
    </div>,
    document.body,
  )
}

function KeyRow({
  entry,
  busy,
  onEdit,
  onToggle,
  onDelete,
}: {
  entry: APIKeyEntry
  busy: boolean
  onEdit: () => void
  onToggle: () => void
  onDelete: () => void
}) {
  const [copied, setCopied] = useState(false)
  return (
    <div
      className={cn(
        'flex items-center gap-2 rounded-lg border border-border bg-background px-3 py-2.5',
        entry.disabled && 'opacity-55',
      )}
    >
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{entry.label || 'API Key'}</div>
        <div className="truncate font-mono text-[11px] text-muted-foreground">
          {maskKey(entry.key)}
        </div>
      </div>
      <IconBtn
        title="复制"
        onClick={() => {
          void navigator.clipboard.writeText(entry.key)
          setCopied(true)
          setTimeout(() => setCopied(false), 1500)
        }}
      >
        {copied ? <Check className="h-3.5 w-3.5 text-success" /> : <Copy className="h-3.5 w-3.5" />}
      </IconBtn>
      <IconBtn title="编辑" onClick={onEdit} disabled={busy}>
        <Pencil className="h-3.5 w-3.5" />
      </IconBtn>
      <IconBtn title="删除" onClick={onDelete} danger disabled={busy}>
        <Minus className="h-3.5 w-3.5" />
      </IconBtn>
      <Switch checked={!entry.disabled} onChange={onToggle} disabled={busy} />
    </div>
  )
}

function KeyEditor({
  initial,
  existing,
  allowBulk,
  onCancel,
  onSave,
  onBulk,
}: {
  initial: APIKeyEntry
  existing: APIKeyEntry[]
  allowBulk?: boolean
  onCancel: () => void
  onSave: (e: APIKeyEntry) => void
  onBulk?: (text: string) => void
}) {
  const [key, setKey] = useState(initial.key)
  const [label, setLabel] = useState(initial.label ?? '')
  const ref = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    ref.current?.focus()
  }, [])

  const parts = splitKeys(key)
  const bulk = !!allowBulk && parts.length > 1
  const dup =
    !bulk && parts.length === 1 && existing.some((e) => e.id !== initial.id && e.key === parts[0])
  const valid = parts.length > 0 && !dup

  const submit = () => {
    if (!valid) return
    if (bulk && onBulk) {
      onBulk(key)
      return
    }
    onSave({ ...initial, key: parts[0], label: label.trim() })
  }

  return (
    <div className="space-y-2 rounded-lg border border-info/40 bg-info/5 p-3">
      <textarea
        ref={ref}
        value={key}
        onChange={(e) => setKey(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
            e.preventDefault()
            submit()
          } else if (e.key === 'Escape') {
            e.preventDefault()
            onCancel()
          }
        }}
        rows={allowBulk ? 3 : 1}
        placeholder={allowBulk ? 'sk-...(可一次粘贴多把,每行一个或逗号分隔)' : 'sk-...'}
        className="w-full resize-none rounded-md border border-input bg-background px-2.5 py-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
      />
      {!bulk && (
        <input
          value={label}
          onChange={(e) => setLabel(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              submit()
            } else if (e.key === 'Escape') {
              e.preventDefault()
              onCancel()
            }
          }}
          placeholder="备注(可选),如 个人号 / 公司额度"
          className="h-8 w-full rounded-md border border-input bg-background px-2.5 text-xs outline-none focus:ring-1 focus:ring-ring"
        />
      )}
      <div className="flex items-center gap-2 text-[11px]">
        <span className="mr-auto text-muted-foreground">
          {dup ? '这把密钥已经在列表里了' : bulk ? `将添加 ${parts.length} 把密钥` : 'Esc 取消'}
        </span>
        <button
          type="button"
          onClick={onCancel}
          className="h-7 rounded-md px-2.5 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
        >
          取消
        </button>
        <button
          type="button"
          onClick={submit}
          disabled={!valid}
          className="h-7 rounded-md bg-info px-3 font-medium text-info-foreground transition-colors hover:bg-info/90 disabled:opacity-50"
        >
          保存
        </button>
      </div>
    </div>
  )
}

function IconBtn({
  title,
  onClick,
  children,
  danger,
  disabled,
}: {
  title: string
  onClick: () => void
  children: React.ReactNode
  danger?: boolean
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors disabled:opacity-40',
        danger
          ? 'hover:bg-destructive/10 hover:text-destructive'
          : 'hover:bg-secondary hover:text-foreground',
      )}
    >
      {children}
    </button>
  )
}

function Switch({
  checked,
  onChange,
  disabled,
}: {
  checked: boolean
  onChange: () => void
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={onChange}
      disabled={disabled}
      className={cn(
        'relative inline-flex h-5 w-9 shrink-0 items-center rounded-full p-0.5 transition-colors disabled:opacity-50',
        checked ? 'bg-success' : 'bg-secondary',
      )}
    >
      <span
        className={cn(
          'inline-block h-4 w-4 rounded-full bg-white shadow-sm transition-transform',
          checked ? 'translate-x-4' : 'translate-x-0',
        )}
      />
    </button>
  )
}
