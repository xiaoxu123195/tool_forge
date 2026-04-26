import { useRef, useState } from 'react'
import { Moon, Sun, Monitor, Check, Image as ImageIcon, X, Upload } from 'lucide-react'
import { useLayoutStore, type StyleId } from '@/stores/layout'
import { useProfileStore } from '@/stores/profile'
import { cn } from '@/lib/utils'

export function BasicSection() {
  const nickname = useProfileStore((s) => s.nickname)
  const setNickname = useProfileStore((s) => s.setNickname)
  const theme = useLayoutStore((s) => s.theme)
  const setTheme = useLayoutStore((s) => s.setTheme)
  const styleId = useLayoutStore((s) => s.styleId)
  const setStyle = useLayoutStore((s) => s.setStyle)

  return (
    <div className="mx-auto max-w-2xl space-y-8">
      <header>
        <h1 className="text-xl font-semibold">基础信息</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          配置昵称与界面外观，这些信息只存在本地。
        </p>
      </header>

      <Field label="昵称" hint="仅显示在侧边栏与本地，不会被上传">
        <input
          value={nickname}
          onChange={(e) => setNickname(e.target.value)}
          className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring"
          placeholder="给自己起个名字"
        />
      </Field>

      <Field label="明暗模式">
        <div className="flex gap-2">
          <ThemeOption
            active={theme === 'light'}
            onClick={() => setTheme('light')}
            icon={<Sun className="h-4 w-4" />}
            label="浅色"
          />
          <ThemeOption
            active={theme === 'dark'}
            onClick={() => setTheme('dark')}
            icon={<Moon className="h-4 w-4" />}
            label="深色"
          />
          <ThemeOption
            active={theme === 'system'}
            onClick={() => setTheme('system')}
            icon={<Monitor className="h-4 w-4" />}
            label="跟随系统"
          />
        </div>
      </Field>

      <Field label="界面风格" hint="主题会影响首页与个人页的氛围，工具内部保持专注。">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <StylePreviewCard
            id="minimal"
            active={styleId === 'minimal'}
            onSelect={setStyle}
            title="Minimal"
            description="黑白极简，纯粹专注"
          />
          <StylePreviewCard
            id="nebula"
            active={styleId === 'nebula'}
            onSelect={setStyle}
            title="Nebula"
            description="紫色氛围，星云深空"
          />
          <StylePreviewCard
            id="ocean"
            active={styleId === 'ocean'}
            onSelect={setStyle}
            title="Ocean"
            description="海洋蓝调，沉静专注"
          />
          <StylePreviewCard
            id="forest"
            active={styleId === 'forest'}
            onSelect={setStyle}
            title="Forest"
            description="苔绿森系，舒缓低疲"
          />
          <StylePreviewCard
            id="glass"
            active={styleId === 'glass'}
            onSelect={setStyle}
            title="Glass"
            description="毛玻璃 · 自定义壁纸"
          />
        </div>
      </Field>

      {styleId === 'glass' && <GlassWallpaperField />}
    </div>
  )
}

function GlassWallpaperField() {
  const wallpaperLight = useLayoutStore((s) => s.glassWallpaperLight)
  const wallpaperDark = useLayoutStore((s) => s.glassWallpaperDark)
  const setWallpaper = useLayoutStore((s) => s.setGlassWallpaper)

  return (
    <Field label="毛玻璃壁纸" hint="留空使用默认动漫壁纸 · 支持粘贴 URL 或上传本地图片">
      <div className="space-y-4">
        <WallpaperRow
          mode="light"
          label="浅色模式"
          value={wallpaperLight}
          onChange={(v) => setWallpaper('light', v)}
        />
        <WallpaperRow
          mode="dark"
          label="深色模式"
          value={wallpaperDark}
          onChange={(v) => setWallpaper('dark', v)}
        />
      </div>
    </Field>
  )
}

function WallpaperRow({
  mode,
  label,
  value,
  onChange,
}: {
  mode: 'light' | 'dark'
  label: string
  value: string
  onChange: (v: string) => void
}) {
  const [draft, setDraft] = useState(value)
  const fileRef = useRef<HTMLInputElement>(null)
  const [error, setError] = useState('')

  // 同步外部变化(切回/重置)
  if (draft !== value && document.activeElement?.tagName !== 'INPUT') {
    setDraft(value)
  }

  const onPickFile = async (file: File) => {
    setError('')
    if (!file.type.startsWith('image/')) {
      setError('只能选图片文件')
      return
    }
    if (file.size > 4 * 1024 * 1024) {
      setError('图片不能超过 4 MB(localStorage 容量限制)')
      return
    }
    const reader = new FileReader()
    reader.onload = () => {
      const dataUrl = reader.result as string
      setDraft(dataUrl)
      onChange(dataUrl)
    }
    reader.onerror = () => setError('读取失败')
    reader.readAsDataURL(file)
  }

  const isDataUrl = value.startsWith('data:')
  const displayValue = isDataUrl ? '(本地图片)' : value

  return (
    <div className="space-y-1.5">
      <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
        {mode === 'light' ? <Sun className="h-3 w-3" /> : <Moon className="h-3 w-3" />}
        {label}
      </div>
      <div className="flex items-center gap-2">
        <div className="relative h-10 w-16 shrink-0 overflow-hidden rounded border border-border bg-secondary">
          {value ? (
            <img src={value} alt="" className="h-full w-full object-cover" />
          ) : (
            <div className="flex h-full w-full items-center justify-center text-muted-foreground">
              <ImageIcon className="h-4 w-4" />
            </div>
          )}
        </div>
        <input
          value={isDataUrl ? '(本地图片,粘贴 URL 可替换)' : draft}
          onChange={(e) => {
            if (isDataUrl) return
            setDraft(e.target.value)
          }}
          onBlur={() => {
            if (isDataUrl) return
            if (draft !== value) onChange(draft.trim())
          }}
          placeholder="https://... 或留空用默认"
          readOnly={isDataUrl}
          className="h-9 flex-1 rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring"
        />
        <button
          onClick={() => fileRef.current?.click()}
          title="上传本地图片"
          className="inline-flex h-9 items-center gap-1.5 rounded-md border border-input bg-background px-3 text-sm transition-colors hover:bg-accent"
        >
          <Upload className="h-3.5 w-3.5" />
          上传
        </button>
        {value && (
          <button
            onClick={() => {
              setDraft('')
              onChange('')
              setError('')
              if (fileRef.current) fileRef.current.value = ''
            }}
            title="重置为默认壁纸"
            className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-input bg-background text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        )}
        <input
          ref={fileRef}
          type="file"
          accept="image/*"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0]
            if (f) onPickFile(f)
          }}
        />
      </div>
      {displayValue && !isDataUrl && (
        <div className="truncate pl-[72px] text-[11px] text-muted-foreground">
          {displayValue}
        </div>
      )}
      {error && (
        <div className="pl-[72px] text-[11px] text-destructive">{error}</div>
      )}
    </div>
  )
}

function Field({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-2">
      <div>
        <div className="text-sm font-medium">{label}</div>
        {hint && <div className="text-xs text-muted-foreground">{hint}</div>}
      </div>
      {children}
    </div>
  )
}

function ThemeOption({
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
        'flex h-9 items-center gap-2 rounded-md border border-input bg-background px-3 text-sm transition-colors hover:bg-accent',
        active && 'border-foreground/30 bg-accent font-medium'
      )}
    >
      {icon}
      {label}
    </button>
  )
}

function StylePreviewCard({
  id,
  active,
  onSelect,
  title,
  description,
}: {
  id: StyleId
  active: boolean
  onSelect: (id: StyleId) => void
  title: string
  description: string
}) {
  return (
    <button
      onClick={() => onSelect(id)}
      className={cn(
        'group relative flex flex-col gap-3 rounded-lg border p-3 text-left transition-all',
        active
          ? 'border-primary ring-2 ring-primary/20'
          : 'border-border hover:border-foreground/30'
      )}
    >
      <StylePreview styleId={id} />
      <div className="flex items-center justify-between">
        <div>
          <div className="text-sm font-medium">{title}</div>
          <div className="text-xs text-muted-foreground">{description}</div>
        </div>
        {active && (
          <div className="flex h-5 w-5 items-center justify-center rounded-full bg-primary text-primary-foreground">
            <Check className="h-3 w-3" />
          </div>
        )}
      </div>
    </button>
  )
}

function StylePreview({ styleId }: { styleId: StyleId }) {
  if (styleId === 'nebula') {
    return (
      <div className="relative h-20 overflow-hidden rounded-md bg-[hsl(240_20%_5%)]">
        <div
          className="absolute inset-0"
          style={{
            background:
              'radial-gradient(circle at 15% 20%, hsl(258 90% 50% / 0.35), transparent 55%), radial-gradient(circle at 85% 80%, hsl(280 90% 45% / 0.25), transparent 55%)',
          }}
        />
        <div className="relative flex h-full items-center gap-2 p-3">
          <div className="h-6 w-6 rounded-md bg-violet-500/30 ring-1 ring-violet-400/40" />
          <div className="h-6 w-6 rounded-md bg-rose-500/30 ring-1 ring-rose-400/40" />
          <div className="h-6 w-6 rounded-md bg-emerald-500/30 ring-1 ring-emerald-400/40" />
          <div className="ml-auto h-1.5 w-8 rounded-full bg-gradient-to-r from-violet-400 to-fuchsia-400" />
        </div>
      </div>
    )
  }
  if (styleId === 'ocean') {
    return (
      <div className="relative h-20 overflow-hidden rounded-md bg-[hsl(215_35%_6%)]">
        <div
          className="absolute inset-0"
          style={{
            background:
              'radial-gradient(circle at 15% 20%, hsl(195 90% 45% / 0.35), transparent 55%), radial-gradient(circle at 85% 80%, hsl(175 80% 40% / 0.22), transparent 55%)',
          }}
        />
        <div className="relative flex h-full items-center gap-2 p-3">
          <div className="h-6 w-6 rounded-md bg-cyan-500/30 ring-1 ring-cyan-400/40" />
          <div className="h-6 w-6 rounded-md bg-sky-500/30 ring-1 ring-sky-400/40" />
          <div className="h-6 w-6 rounded-md bg-teal-500/30 ring-1 ring-teal-400/40" />
          <div className="ml-auto h-1.5 w-8 rounded-full bg-gradient-to-r from-cyan-400 to-sky-400" />
        </div>
      </div>
    )
  }
  if (styleId === 'glass') {
    return (
      <div
        className="relative h-20 overflow-hidden rounded-md"
        style={{
          backgroundImage:
            "url('https://cdn.jsdelivr.net/gh/buwant888/ck/test/%E3%80%90%E5%93%B2%E9%A3%8E%E5%A3%81%E7%BA%B8%E3%80%91eva-%E5%8A%A8%E6%BC%AB%E5%A3%81%E7%BA%B8.png')",
          backgroundSize: 'cover',
          backgroundPosition: 'center',
        }}
      >
        <div
          className="absolute left-3 top-3 h-14 w-[55%] rounded-md"
          style={{
            background: 'hsl(0 0% 100% / 0.55)',
            backdropFilter: 'blur(14px) saturate(180%)',
            WebkitBackdropFilter: 'blur(14px) saturate(180%)',
            boxShadow:
              'inset 0 1px 0 hsl(0 0% 100% / 0.85), 0 4px 12px -4px hsl(240 30% 20% / 0.18)',
            border: '1px solid hsl(0 0% 100% / 0.6)',
          }}
        />
        <div
          className="absolute right-3 top-3 h-14 w-[28%] rounded-md"
          style={{
            background: 'hsl(0 0% 100% / 0.45)',
            backdropFilter: 'blur(14px) saturate(180%)',
            WebkitBackdropFilter: 'blur(14px) saturate(180%)',
            boxShadow:
              'inset 0 1px 0 hsl(0 0% 100% / 0.85), 0 4px 12px -4px hsl(240 30% 20% / 0.18)',
            border: '1px solid hsl(0 0% 100% / 0.6)',
          }}
        />
      </div>
    )
  }
  if (styleId === 'forest') {
    return (
      <div className="relative h-20 overflow-hidden rounded-md bg-[hsl(160_30%_5%)]">
        <div
          className="absolute inset-0"
          style={{
            background:
              'radial-gradient(circle at 15% 20%, hsl(158 70% 40% / 0.32), transparent 55%), radial-gradient(circle at 85% 80%, hsl(135 70% 35% / 0.22), transparent 55%)',
          }}
        />
        <div className="relative flex h-full items-center gap-2 p-3">
          <div className="h-6 w-6 rounded-md bg-emerald-500/30 ring-1 ring-emerald-400/40" />
          <div className="h-6 w-6 rounded-md bg-green-500/30 ring-1 ring-green-400/40" />
          <div className="h-6 w-6 rounded-md bg-lime-500/30 ring-1 ring-lime-400/40" />
          <div className="ml-auto h-1.5 w-8 rounded-full bg-gradient-to-r from-emerald-400 to-green-400" />
        </div>
      </div>
    )
  }
  return (
    <div className="relative h-20 overflow-hidden rounded-md bg-white ring-1 ring-zinc-200 dark:bg-zinc-900 dark:ring-zinc-800">
      <div className="flex h-full items-center gap-2 p-3">
        <div className="h-6 w-6 rounded-md bg-zinc-100 dark:bg-zinc-800" />
        <div className="h-6 w-6 rounded-md bg-zinc-100 dark:bg-zinc-800" />
        <div className="h-6 w-6 rounded-md bg-zinc-100 dark:bg-zinc-800" />
        <div className="ml-auto h-1.5 w-8 rounded-full bg-zinc-900 dark:bg-zinc-100" />
      </div>
    </div>
  )
}
