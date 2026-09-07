import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { X, Image as ImageIcon, RotateCcw, LayoutGrid } from 'lucide-react'
import type { ProviderType } from '@/tools/ai-chat/types'
import { BUILTIN_LOGOS } from '@/tools/ai-chat/provider-logos'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { ProviderAvatar } from './ProviderAvatar'
import { cn } from '@/lib/utils'

const TYPES: { value: ProviderType; label: string; defaultBaseUrl: string; hint: string }[] = [
  {
    value: 'openai',
    label: 'OpenAI(新版 /responses)',
    defaultBaseUrl: 'https://api.openai.com/v1',
    hint: 'POST {baseUrl}/responses',
  },
  {
    value: 'openai-compatible',
    label: 'OpenAI 兼容(/chat/completions)',
    defaultBaseUrl: 'https://api.openai.com/v1',
    hint: 'POST {baseUrl}/chat/completions · 适用 SiliconFlow / DeepSeek / 中转',
  },
  {
    value: 'gemini',
    label: 'Gemini',
    defaultBaseUrl: 'https://generativelanguage.googleapis.com',
    hint: 'POST {baseUrl}/v1beta/models/{model}:streamGenerateContent',
  },
  {
    value: 'anthropic',
    label: 'Anthropic',
    defaultBaseUrl: 'https://api.anthropic.com',
    hint: 'POST {baseUrl}/v1/messages',
  },
  {
    value: 'xai',
    label: 'xAI Grok',
    defaultBaseUrl: 'https://api.x.ai/v1',
    hint: 'POST {baseUrl}/responses · 支持 web_search / x_search 内置联网',
  },
]

const MAX_LOGO_BYTES = 1024 * 1024 // 1MB

interface Result {
  name: string
  type: ProviderType
  logo: string
  baseUrl: string
}

export function AddProviderDialog({
  onCancel,
  onConfirm,
}: {
  onCancel: () => void
  onConfirm: (r: Result) => void | Promise<void>
}) {
  const dialog = useConfirm()
  const [name, setName] = useState('')
  const [type, setType] = useState<ProviderType>('openai')
  const [logo, setLogo] = useState('')
  const [picker, setPicker] = useState<'menu' | 'builtin' | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)
  const popRef = useRef<HTMLDivElement>(null)

  const def = TYPES.find((t) => t.value === type) ?? TYPES[0]

  useEffect(() => {
    if (!picker) return
    const close = (e: MouseEvent) => {
      if (popRef.current && !popRef.current.contains(e.target as Node)) {
        setPicker(null)
      }
    }
    window.addEventListener('mousedown', close)
    return () => window.removeEventListener('mousedown', close)
  }, [picker])

  const onPickFile = () => {
    setPicker(null)
    fileRef.current?.click()
  }

  const onFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0]
    e.target.value = ''
    if (!f) return
    if (f.size > MAX_LOGO_BYTES) {
      await dialog({
        title: '图片过大',
        message: `请挑一张小于 ${Math.round(MAX_LOGO_BYTES / 1024)}KB 的图`,
        confirmLabel: '知道了',
      })
      return
    }
    const reader = new FileReader()
    reader.onload = () => setLogo(String(reader.result || ''))
    reader.readAsDataURL(f)
  }

  const submit = async () => {
    if (!name.trim()) {
      await dialog({ title: '提示', message: '请填写供应商名称', confirmLabel: '知道了' })
      return
    }
    setSubmitting(true)
    try {
      await onConfirm({
        name: name.trim(),
        type,
        logo,
        baseUrl: def.defaultBaseUrl,
      })
    } finally {
      setSubmitting(false)
    }
  }

  return createPortal(
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center bg-black/40 p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onCancel()
      }}
    >
      <div className="w-[400px] max-w-full overflow-hidden rounded-xl border border-border bg-card shadow-2xl">
        <header className="flex h-12 shrink-0 items-center justify-between border-b border-border px-4">
          <h3 className="text-sm font-semibold">添加供应商</h3>
          <button
            onClick={onCancel}
            className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </header>

        <div className="space-y-5 p-5">
          {/* Avatar with dropdown */}
          <div className="flex justify-center">
            <div className="relative" ref={popRef}>
              <button
                type="button"
                onClick={() => setPicker(picker ? null : 'menu')}
                className="rounded-full transition-opacity hover:opacity-80"
                title="点击修改头像"
              >
                <ProviderAvatar logo={logo} name={name} size={64} />
              </button>

              {picker === 'menu' && (
                <div className="absolute left-1/2 top-full z-10 mt-2 w-44 -translate-x-1/2 overflow-hidden rounded-lg border border-border bg-card text-sm shadow-xl">
                  <MenuRow icon={<ImageIcon className="h-3.5 w-3.5" />} onClick={onPickFile}>
                    上传图片
                  </MenuRow>
                  <MenuRow
                    icon={<LayoutGrid className="h-3.5 w-3.5" />}
                    onClick={() => setPicker('builtin')}
                  >
                    内置头像
                  </MenuRow>
                  <MenuRow
                    icon={<RotateCcw className="h-3.5 w-3.5" />}
                    onClick={() => {
                      setLogo('')
                      setPicker(null)
                    }}
                  >
                    重置头像
                  </MenuRow>
                </div>
              )}

              {picker === 'builtin' && (
                <div className="absolute left-1/2 top-full z-10 mt-2 w-72 -translate-x-1/2 rounded-lg border border-border bg-card p-3 shadow-xl">
                  <div className="mb-2 text-[11px] font-medium text-muted-foreground">
                    选一个内置头像
                  </div>
                  <div className="grid grid-cols-5 gap-2">
                    {BUILTIN_LOGOS.map((b) => (
                      <button
                        key={b.id}
                        type="button"
                        onClick={() => {
                          setLogo(b.id)
                          setPicker(null)
                        }}
                        title={b.name}
                        className={cn(
                          'rounded-md border p-1 transition-colors hover:border-info hover:bg-info/10',
                          logo === b.id ? 'border-info bg-info/10' : 'border-border',
                        )}
                      >
                        <ProviderAvatar logo={b.id} name={b.name} size={36} />
                      </button>
                    ))}
                  </div>
                </div>
              )}

              <input
                ref={fileRef}
                type="file"
                accept="image/png,image/jpeg,image/svg+xml,image/webp"
                className="hidden"
                onChange={onFile}
              />
            </div>
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">供应商名称</label>
            <input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="例如:OpenAI / 我的中转"
              maxLength={32}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.nativeEvent.isComposing) void submit()
              }}
              className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring"
            />
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">供应商类型</label>
            <select
              value={type}
              onChange={(e) => setType(e.target.value as ProviderType)}
              className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring"
            >
              {TYPES.map((t) => (
                <option key={t.value} value={t.value}>
                  {t.label}
                </option>
              ))}
            </select>
            <p className="text-[11px] text-muted-foreground">{def.hint.replace('{baseUrl}', def.defaultBaseUrl)}</p>
          </div>
        </div>

        <footer className="flex h-12 items-center justify-end gap-2 border-t border-border bg-secondary/30 px-4">
          <Button variant="ghost" size="sm" onClick={onCancel}>
            取消
          </Button>
          <Button onClick={submit} disabled={submitting || !name.trim()} size="sm">
            {submitting ? '添加中...' : '添加'}
          </Button>
        </footer>
      </div>
    </div>,
    document.body,
  )
}

function MenuRow({
  icon,
  onClick,
  children,
}: {
  icon: React.ReactNode
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full items-center gap-2 px-3 py-2 text-left text-xs transition-colors hover:bg-secondary"
    >
      <span className="text-muted-foreground">{icon}</span>
      {children}
    </button>
  )
}
