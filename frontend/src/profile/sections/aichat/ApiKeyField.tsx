import { useEffect, useMemo, useState } from 'react'
import { Eye, EyeOff, FlaskConical, KeyRound } from 'lucide-react'
import { ListAIProviderKeys, SaveAIProviderKeys } from '../../../../wailsjs/go/main/App'
import type { APIKeyEntry, Provider } from '@/tools/ai-chat/types'
import { useConfirm } from '@/components/ui/confirm'
import { ApiKeyDialog } from './ApiKeyDialog'

/**
 * 供应商详情里的「API 密钥」一行。
 *
 * 输入框里是**当前启用的那些密钥**,多把用逗号连起来 —— 只配一把的人看到的就是
 * 一个普通密钥框,和以前没区别;配了多把的人在这里改动仍然直观。要单独启停、
 * 加备注、逐把检测,再点旁边的钥匙进管理弹窗,不把这些堆到主界面上。
 */
export function ApiKeyField({
  provider,
  onChanged,
  onTest,
}: {
  provider: Provider
  onChanged: () => void
  onTest: () => void
}) {
  const dialog = useConfirm()
  const [keys, setKeys] = useState<APIKeyEntry[]>([])
  const [draft, setDraft] = useState('')
  const [show, setShow] = useState(false)
  const [managing, setManaging] = useState(false)
  const [saving, setSaving] = useState(false)

  const enabledText = useMemo(
    () => keys.filter((k) => !k.disabled).map((k) => k.key).join(','),
    [keys],
  )

  const load = async () => {
    const list = await ListAIProviderKeys(provider.id)
    const next = Array.isArray(list) ? list : []
    setKeys(next)
    setDraft(next.filter((k) => !k.disabled).map((k) => k.key).join(','))
  }

  useEffect(() => {
    setShow(false)
    setManaging(false)
    void load()
  }, [provider.id])

  /**
   * 把输入框里的文本写回密钥池。
   *
   * 不能简单地"清空重建":那样每把密钥的 ID 都会变,备注、启用状态、检测结果全跟着丢。
   * 所以按密钥值去认领已有条目,认不到才新建;停用中的条目原样保留 ——
   * 它们本来就不在这个输入框里,不该因为一次编辑被删掉。
   */
  const commit = async () => {
    const parts = splitKeys(draft)
    if (parts.join(',') === enabledText) return
    const claimed = new Set<string>()
    const next: APIKeyEntry[] = parts.map((key) => {
      const hit = keys.find((k) => k.key === key && !claimed.has(k.id))
      if (hit) {
        claimed.add(hit.id)
        return { ...hit, disabled: false }
      }
      return { id: '', key }
    })
    const kept = keys.filter((k) => k.disabled && !claimed.has(k.id))
    setSaving(true)
    try {
      const saved = await SaveAIProviderKeys(provider.id, [...next, ...kept] as never)
      setKeys(Array.isArray(saved) ? saved : [])
      onChanged()
    } catch (e) {
      await dialog({ title: '保存失败', message: String(e), confirmLabel: '知道了' })
      await load()
    } finally {
      setSaving(false)
    }
  }

  const enabledCount = keys.filter((k) => !k.disabled).length

  return (
    <div className="space-y-1.5">
      <label className="text-xs font-medium text-muted-foreground">API 密钥</label>
      <div className="flex gap-2">
        <div className="relative flex-1">
          <input
            type={show ? 'text' : 'password'}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={() => void commit()}
            onKeyDown={(e) => {
              if (e.key === 'Enter') e.currentTarget.blur()
              if (e.key === 'Escape') setDraft(enabledText)
            }}
            placeholder="sk-...(多把用逗号分隔)"
            className="h-9 w-full rounded-md border border-input bg-background pl-3 pr-9 font-mono text-sm outline-none focus:ring-1 focus:ring-ring"
          />
          <button
            type="button"
            onClick={() => setShow((v) => !v)}
            title={show ? '隐藏' : '显示'}
            className="absolute right-1 top-1/2 flex h-7 w-7 -translate-y-1/2 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          >
            {show ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
          </button>
        </div>
        <button
          type="button"
          onClick={() => setManaging(true)}
          title="API 密钥管理"
          className="relative flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-input text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
        >
          <KeyRound className="h-4 w-4" />
          {keys.length > 1 && (
            <span className="absolute -right-1 -top-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-info px-1 text-[9px] font-medium text-info-foreground">
              {keys.length}
            </span>
          )}
        </button>
        <button
          type="button"
          onClick={onTest}
          className="flex h-9 shrink-0 items-center gap-1.5 rounded-md border border-input px-3 text-xs text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
        >
          <FlaskConical className="h-3.5 w-3.5" />
          检测
        </button>
      </div>
      <p className="text-[11px] text-muted-foreground">
        {keys.length > 1
          ? `共 ${keys.length} 把 · 启用 ${enabledCount} 把,轮着用;某把被限流或欠费时会自动换下一把重发`
          : '可以填多把(逗号分隔),会轮着用并在失效时自动换把重发'}
        {saving && ' · 保存中...'}
      </p>

      {managing && (
        <ApiKeyDialog
          provider={provider}
          keys={keys}
          onClose={() => setManaging(false)}
          onSave={async (next) => {
            try {
              const saved = await SaveAIProviderKeys(provider.id, next as never)
              const list = Array.isArray(saved) ? saved : []
              setKeys(list)
              setDraft(list.filter((k) => !k.disabled).map((k) => k.key).join(','))
              onChanged()
              return true
            } catch (e) {
              await dialog({ title: '保存失败', message: String(e), confirmLabel: '知道了' })
              return false
            }
          }}
        />
      )}
    </div>
  )
}

/** 和后端 splitKeyString 保持一致的分隔规则:逗号 / 分号 / 任意空白 */
export function splitKeys(text: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const part of text.split(/[,;\s]+/)) {
    const t = part.trim()
    if (!t || seen.has(t)) continue
    seen.add(t)
    out.push(t)
  }
  return out
}

export function maskKey(k: string): string {
  const t = k.trim()
  if (t.length === 0) return ''
  if (t.length <= 8) return '*'.repeat(t.length)
  return `${t.slice(0, 4)}****${t.slice(-4)}`
}
