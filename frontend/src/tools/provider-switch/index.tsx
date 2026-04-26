import { useEffect, useMemo, useState } from 'react'
import {
  AlertCircle,
  Check,
  CheckCircle2,
  ChevronDown,
  Copy,
  Edit3,
  Eye,
  EyeOff,
  FolderOpen,
  Loader2,
  Play,
  Plus,
  Radar,
  Trash2,
  X,
} from 'lucide-react'
import {
  ActivateProvider,
  DeleteProvider,
  GetActiveProviderConfig,
  ListProviderPresets,
  ListProviders,
  OpenInExplorer,
  SaveProvider,
  TestProvider,
} from '../../../wailsjs/go/main/App'
import type { providerswitch } from '../../../wailsjs/go/models'
import { ToolShell } from '@/components/tool/ToolShell'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { cn } from '@/lib/utils'
import { meta } from './meta'

type Provider = providerswitch.Provider
type Preset = providerswitch.Preset
type ProviderType = 'claude_code' | 'codex' | 'codex_oauth'

const TYPE_LABELS: Record<ProviderType, string> = {
  claude_code: 'Claude Code',
  codex: 'Codex (API Key)',
  codex_oauth: 'Codex (OAuth)',
}

function newDraft(type: ProviderType): Provider {
  return {
    id: '',
    name: '',
    type,
    baseUrl: '',
    apiKey: '',
    model: '',
    haikuModel: '',
    sonnetModel: '',
    opusModel: '',
    thinkingModel: '',
    oauthAccessToken: '',
    oauthRefreshToken: '',
    oauthIdToken: '',
    oauthAccountId: '',
    isDefault: false,
    isActive: false,
    createdAt: 0,
    updatedAt: 0,
  } as Provider
}

export default function ProviderSwitch() {
  const confirm = useConfirm()
  const [tab, setTab] = useState<ProviderType>('claude_code')
  const [providers, setProviders] = useState<Provider[]>([])
  const [presets, setPresets] = useState<Preset[]>([])
  const [editing, setEditing] = useState<Provider | null>(null)
  const [showAddModal, setShowAddModal] = useState(false)
  const [activeConfig, setActiveConfig] = useState<Record<string, string>>({})
  const [testing, setTesting] = useState<string | null>(null)
  const [testResults, setTestResults] = useState<Record<string, providerswitch.TestResult>>({})

  const refresh = async () => {
    const list = (await ListProviders()) ?? []
    setProviders(list)
  }
  const refreshActiveConfig = async (t: ProviderType) => {
    const cfg = (await GetActiveProviderConfig(t)) ?? {}
    setActiveConfig(cfg)
  }

  useEffect(() => {
    refresh()
    ListProviderPresets().then((p) => setPresets(p ?? []))
  }, [])
  useEffect(() => {
    refreshActiveConfig(tab)
  }, [tab])

  // 列表过滤 + 排序:default 永远在最上,然后激活态 > 更新时间倒序
  const filtered = useMemo(() => {
    return providers
      .filter((p) => {
        if (tab === 'claude_code') return p.type === 'claude_code'
        return p.type === 'codex' || p.type === 'codex_oauth'
      })
      .sort((a, b) => {
        if (a.isDefault !== b.isDefault) return a.isDefault ? -1 : 1
        if (a.isActive !== b.isActive) return a.isActive ? -1 : 1
        return (b.updatedAt ?? 0) - (a.updatedAt ?? 0)
      })
  }, [providers, tab])

  const onPickPreset = (preset: Preset | null) => {
    setShowAddModal(false)
    const draft = newDraft(tab === 'claude_code' ? 'claude_code' : 'codex')
    if (preset) {
      draft.name = preset.name
      draft.baseUrl = preset.baseUrl
      draft.model = preset.model
    }
    setEditing(draft)
  }

  const onSave = async (p: Provider) => {
    const result = (await SaveProvider(p)) as unknown as [Provider, string]
    const [, err] = result
    if (err) {
      await confirm({ title: '保存失败', message: err, confirmLabel: '知道了' })
      return
    }
    await refresh()
    setEditing(null)
  }

  const onActivate = async (p: Provider) => {
    const r = await ActivateProvider(p.id)
    if (!r.ok) {
      await confirm({ title: '激活失败', message: r.message ?? '未知错误', confirmLabel: '知道了' })
      return
    }
    await refresh()
    await refreshActiveConfig(tab)
  }

  const onDelete = async (p: Provider) => {
    const ok = await confirm({
      title: '删除 Provider',
      message: `确认删除「${p.name}」?该操作不会撤回 ~/.claude 或 ~/.codex 的配置文件。`,
      danger: true,
    })
    if (!ok) return
    await DeleteProvider(p.id)
    await refresh()
  }

  const onCopy = async (p: Provider) => {
    const draft = { ...p, id: '', name: p.name + ' (副本)', isDefault: false, isActive: false }
    setEditing(draft)
  }

  const onTest = async (p: Provider) => {
    setTesting(p.id)
    setTestResults((prev) => {
      const next = { ...prev }
      delete next[p.id]
      return next
    })
    try {
      const r = await TestProvider(p)
      setTestResults((prev) => ({ ...prev, [p.id]: r }))
    } finally {
      setTesting(null)
    }
  }

  return (
    <ToolShell
      title={meta.title}
      description="default = 你 claude /login / codex login 已登录的官方账号;新增条目用 API Key 接入第三方"
      actions={
        <>
          <TabPill label="Claude Code" active={tab === 'claude_code'} onClick={() => setTab('claude_code')} />
          <TabPill label="Codex" active={tab !== 'claude_code'} onClick={() => setTab('codex')} />
          <Button
            size="sm"
            variant="ghost"
            onClick={() => OpenInExplorer(tab === 'claude_code' ? '~/.claude' : '~/.codex')}
            title={tab === 'claude_code' ? '打开 ~/.claude' : '打开 ~/.codex'}
          >
            <FolderOpen className="h-3.5 w-3.5" />
          </Button>
          <Button size="sm" onClick={() => setShowAddModal(true)} className="font-semibold">
            <Plus className="h-3.5 w-3.5" />
            新增
          </Button>
        </>
      }
    >
      <div className="mx-auto max-w-3xl space-y-3">
        {filtered.map((p) => (
          <ProviderCard
            key={p.id}
            provider={p}
            testing={testing === p.id}
            testResult={testResults[p.id]}
            onActivate={() => onActivate(p)}
            onEdit={() => setEditing(p)}
            onCopy={() => onCopy(p)}
            onTest={() => onTest(p)}
            onDelete={() => onDelete(p)}
            onDismissTest={() =>
              setTestResults((prev) => {
                const next = { ...prev }
                delete next[p.id]
                return next
              })
            }
          />
        ))}
        {filtered.length === 0 && (
          <div className="rounded-lg border border-dashed border-border p-12 text-center text-sm text-muted-foreground">
            没有 {tab === 'claude_code' ? 'Claude Code' : 'Codex'} 条目,点右上「新增」添加
          </div>
        )}

        {/* 当前 ~/.claude/settings.json 实际生效内容(给 default 之外的 active 条目做对照) */}
        {Object.keys(activeConfig).length > 0 && (
          <ActiveConfigPanel config={activeConfig} type={tab} />
        )}
      </div>

      {showAddModal && (
        <AddModal
          presets={presets.filter((p) =>
            tab === 'claude_code' ? p.type === 'claude_code' : p.type === 'codex'
          )}
          onPick={onPickPreset}
          onClose={() => setShowAddModal(false)}
        />
      )}

      {editing && (
        <EditModal
          provider={editing}
          onChange={setEditing}
          onSave={() => onSave(editing)}
          onCancel={() => setEditing(null)}
        />
      )}
    </ToolShell>
  )
}

// ─────────────────────────── ProviderCard (cc-switch 风格的横向 card) ───────────────────────────

function ProviderCard({
  provider,
  testing,
  testResult,
  onActivate,
  onEdit,
  onCopy,
  onTest,
  onDelete,
  onDismissTest,
}: {
  provider: Provider
  testing: boolean
  testResult?: providerswitch.TestResult
  onActivate: () => void
  onEdit: () => void
  onCopy: () => void
  onTest: () => void
  onDelete: () => void
  onDismissTest: () => void
}) {
  const desc =
    provider.isDefault
      ? '未配置 Base URL · 走 CLI 自带的 OAuth 登录'
      : provider.baseUrl

  return (
    <div
      className={cn(
        'group/card overflow-hidden rounded-xl border transition-colors',
        provider.isActive
          ? 'border-sky-400/60 bg-sky-50/60 dark:border-sky-500/40 dark:bg-sky-500/5'
          : 'border-border bg-card hover:border-border'
      )}
    >
      <div className="flex items-center gap-3 px-4 py-3.5">
        <Avatar provider={provider} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="text-[14px] font-semibold leading-tight">{provider.name}</span>
            {provider.isDefault && (
              <span className="rounded bg-secondary px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                default
              </span>
            )}
          </div>
          <div
            className={cn(
              'mt-0.5 truncate text-[12px] leading-tight',
              provider.isDefault
                ? 'text-muted-foreground'
                : 'font-mono text-sky-600 dark:text-sky-400'
            )}
          >
            {desc}
          </div>
        </div>
        <div className="flex items-center gap-1">
          {provider.isActive ? (
            <span className="inline-flex items-center gap-1 rounded-md bg-secondary px-2.5 py-1 text-xs font-medium text-muted-foreground">
              <Check className="h-3.5 w-3.5" />
              使用中
            </span>
          ) : (
            <button
              onClick={onActivate}
              className="inline-flex items-center gap-1 rounded-md bg-sky-500 px-2.5 py-1 text-xs font-semibold text-white transition-colors hover:bg-sky-600"
            >
              <Play className="h-3.5 w-3.5" />
              启用
            </button>
          )}
          {/* hover 才显出来的次级操作 */}
          <div className="ml-1 flex items-center gap-0.5 opacity-0 transition-opacity group-hover/card:opacity-100">
            {!provider.isDefault && (
              <IconBtn title="测试连通性" onClick={onTest}>
                {testing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Radar className="h-3.5 w-3.5" />}
              </IconBtn>
            )}
            <IconBtn title="编辑" onClick={onEdit}>
              <Edit3 className="h-3.5 w-3.5" />
            </IconBtn>
            {!provider.isDefault && (
              <>
                <IconBtn title="复制为新条目" onClick={onCopy}>
                  <Copy className="h-3.5 w-3.5" />
                </IconBtn>
                <IconBtn title="删除" onClick={onDelete} danger>
                  <Trash2 className="h-3.5 w-3.5" />
                </IconBtn>
              </>
            )}
          </div>
        </div>
      </div>
      {testResult && (
        <div
          className={cn(
            'flex items-center gap-2 border-t px-4 py-2 text-xs',
            testResult.ok
              ? 'border-emerald-500/30 bg-emerald-500/5 text-emerald-700 dark:text-emerald-300'
              : 'border-red-500/30 bg-red-500/5 text-red-700 dark:text-red-300'
          )}
        >
          {testResult.ok ? <CheckCircle2 className="h-3.5 w-3.5" /> : <AlertCircle className="h-3.5 w-3.5" />}
          <span className="min-w-0 flex-1 truncate">
            {testResult.ok
              ? `连通正常 · ${testResult.statusCode} · ${testResult.durationMs}ms`
              : `${testResult.message}${testResult.durationMs ? ` · ${testResult.durationMs}ms` : ''}`}
          </span>
          <button onClick={onDismissTest} className="text-current/60 hover:text-current">
            <X className="h-3 w-3" />
          </button>
        </div>
      )}
    </div>
  )
}

function Avatar({ provider }: { provider: Provider }) {
  // 与 cc-switch 一致:都用低饱和的圆形字母,默认 D
  const letter = (provider.name.trim()[0] || '?').toUpperCase()
  return (
    <div
      className={cn(
        'flex h-10 w-10 shrink-0 items-center justify-center rounded-full border border-border bg-secondary/60 text-sm font-semibold text-muted-foreground'
      )}
    >
      {provider.isDefault ? 'D' : letter}
    </div>
  )
}

function IconBtn({
  children,
  onClick,
  title,
  danger,
}: {
  children: React.ReactNode
  onClick: () => void
  title?: string
  danger?: boolean
}) {
  return (
    <button
      onClick={onClick}
      title={title}
      className={cn(
        'flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground',
        danger && 'hover:bg-red-500/10 hover:text-red-600'
      )}
    >
      {children}
    </button>
  )
}

// ─────────────────────────── EditModal ───────────────────────────

function EditModal({
  provider,
  onChange,
  onSave,
  onCancel,
}: {
  provider: Provider
  onChange: (p: Provider) => void
  onSave: () => void
  onCancel: () => void
}) {
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [showSecret, setShowSecret] = useState(false)
  const isCodex = provider.type === 'codex' || provider.type === 'codex_oauth'
  const isNew = !provider.id
  const set = (patch: Partial<Provider>) => onChange({ ...provider, ...patch })

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 animate-in fade-in" onClick={onCancel}>
      <div
        className="w-[600px] max-w-[92vw] max-h-[90vh] overflow-y-auto rounded-xl border border-border bg-card p-5 shadow-2xl animate-in zoom-in-95"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h3 className="text-base font-semibold">
            {provider.isDefault ? '编辑 default 条目' : isNew ? '新增 Provider' : '编辑 Provider'}
          </h3>
          <button onClick={onCancel} className="text-muted-foreground hover:text-foreground">
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="space-y-3">
          {provider.isDefault && (
            <div className="rounded-md border border-sky-500/30 bg-sky-500/5 p-3 text-[11px] leading-relaxed text-sky-700 dark:text-sky-300">
              <div className="font-semibold">这是 default 条目,代表你已经登录的 {provider.type === 'claude_code' ? 'Claude' : 'ChatGPT'} 官方账号</div>
              激活后会清掉本工具写过的{' '}
              {provider.type === 'claude_code' ? 'ANTHROPIC_*' : 'OPENAI_API_KEY / model_provider'} 字段,让 CLI 回到自己的 OAuth 登录。
              只允许改名,无法删除。
            </div>
          )}
          <Field label="名称">
            <Input value={provider.name} onChange={(v) => set({ name: v })} placeholder="default / GLM 智谱(自用)" />
          </Field>
          {!provider.isDefault && (
            <>
              {isCodex && (
                <Field label="模式">
                  <div className="flex items-center gap-1">
                    {(['codex', 'codex_oauth'] as const).map((t) => (
                      <button
                        key={t}
                        onClick={() => set({ type: t })}
                        className={cn(
                          'rounded-md px-3 py-1 text-xs font-medium transition-colors',
                          provider.type === t
                            ? 'bg-primary/15 text-primary'
                            : 'text-muted-foreground hover:bg-accent'
                        )}
                      >
                        {t === 'codex' ? 'API Key' : 'ChatGPT OAuth'}
                      </button>
                    ))}
                  </div>
                </Field>
              )}
              <Field label="Base URL" hint="不要带 /v1/messages 或 /chat/completions 后缀">
                <Input
                  value={provider.baseUrl}
                  onChange={(v) => set({ baseUrl: v })}
                  placeholder={
                    provider.type === 'claude_code'
                      ? 'https://api.anthropic.com'
                      : 'https://api.openai.com/v1'
                  }
                  mono
                />
              </Field>
              {provider.type === 'codex_oauth' ? (
                <>
                  <Field label="Access Token">
                    <SecretInput
                      value={provider.oauthAccessToken ?? ''}
                      onChange={(v) => set({ oauthAccessToken: v })}
                      show={showSecret}
                      setShow={setShowSecret}
                      placeholder="ChatGPT OAuth access_token"
                    />
                  </Field>
                  <Field label="Refresh Token">
                    <SecretInput
                      value={provider.oauthRefreshToken ?? ''}
                      onChange={(v) => set({ oauthRefreshToken: v })}
                      show={showSecret}
                      setShow={setShowSecret}
                      placeholder="refresh_token"
                    />
                  </Field>
                  <Field label="ID Token">
                    <Input
                      value={provider.oauthIdToken ?? ''}
                      onChange={(v) => set({ oauthIdToken: v })}
                      placeholder="id_token"
                      mono
                    />
                  </Field>
                  <Field label="Account ID">
                    <Input
                      value={provider.oauthAccountId ?? ''}
                      onChange={(v) => set({ oauthAccountId: v })}
                      placeholder="account_id"
                      mono
                    />
                  </Field>
                </>
              ) : (
                <Field label="API Key">
                  <SecretInput
                    value={provider.apiKey ?? ''}
                    onChange={(v) => set({ apiKey: v })}
                    show={showSecret}
                    setShow={setShowSecret}
                    placeholder={
                      provider.type === 'claude_code'
                        ? 'sk-ant-... 或第三方 token'
                        : 'sk-... 或第三方 token'
                    }
                  />
                </Field>
              )}
              <Field
                label="模型"
                hint={
                  provider.type === 'claude_code'
                    ? '会写到 ANTHROPIC_MODEL,4 个细分模型若不填会自动落到这里'
                    : '会写到 config.toml model'
                }
              >
                <Input
                  value={provider.model ?? ''}
                  onChange={(v) => set({ model: v })}
                  placeholder={provider.type === 'claude_code' ? 'claude-sonnet-4-5' : 'gpt-5'}
                  mono
                />
              </Field>
              {provider.type === 'claude_code' && (
                <>
                  <button
                    onClick={() => setShowAdvanced((v) => !v)}
                    className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                  >
                    <ChevronDown className={cn('h-3.5 w-3.5 transition-transform', !showAdvanced && '-rotate-90')} />
                    高级 · 细分模型(Haiku / Sonnet / Opus / Thinking)
                  </button>
                  {showAdvanced && (
                    <div className="grid grid-cols-2 gap-3 pl-5">
                      <Field label="Haiku">
                        <Input value={provider.haikuModel ?? ''} onChange={(v) => set({ haikuModel: v })} placeholder="留空 = 主模型" mono />
                      </Field>
                      <Field label="Sonnet">
                        <Input value={provider.sonnetModel ?? ''} onChange={(v) => set({ sonnetModel: v })} placeholder="留空 = 主模型" mono />
                      </Field>
                      <Field label="Opus">
                        <Input value={provider.opusModel ?? ''} onChange={(v) => set({ opusModel: v })} placeholder="留空 = 主模型" mono />
                      </Field>
                      <Field label="Thinking / Small Fast">
                        <Input value={provider.thinkingModel ?? ''} onChange={(v) => set({ thinkingModel: v })} placeholder="留空 = 主模型" mono />
                      </Field>
                    </div>
                  )}
                </>
              )}
            </>
          )}
        </div>
        <div className="mt-5 flex items-center justify-end gap-2">
          <Button variant="ghost" onClick={onCancel}>
            取消
          </Button>
          <Button onClick={onSave} disabled={!provider.name.trim()} className="font-semibold">
            <Check className="h-3.5 w-3.5" />
            {isNew ? '保存' : '保存修改'}
          </Button>
        </div>
      </div>
    </div>
  )
}

// ─────────────────────────── 其余组件 ───────────────────────────

function ActiveConfigPanel({ config, type }: { config: Record<string, string>; type: ProviderType }) {
  return (
    <div className="rounded-lg border border-border bg-card">
      <div className="border-b border-border/60 px-3 py-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
        当前 {type === 'claude_code' ? '~/.claude/settings.json env' : '~/.codex/config.toml'} 实际内容
      </div>
      <div className="divide-y divide-border/40 font-mono text-xs">
        {Object.entries(config).map(([k, v]) => (
          <div key={k} className="grid grid-cols-[200px_1fr] gap-3 px-3 py-1.5">
            <span className="text-muted-foreground">{k}</span>
            <span className="break-all">{maskSecret(k, v)}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function maskSecret(key: string, value: string): string {
  const k = key.toLowerCase()
  if (k.includes('token') || k.includes('key')) {
    if (value.length <= 12) return '•'.repeat(value.length)
    return value.slice(0, 6) + '•'.repeat(Math.max(8, value.length - 12)) + value.slice(-4)
  }
  return value
}

function AddModal({
  presets,
  onPick,
  onClose,
}: {
  presets: Preset[]
  onPick: (p: Preset | null) => void
  onClose: () => void
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 animate-in fade-in" onClick={onClose}>
      <div
        className="w-[480px] max-w-[90vw] rounded-lg border border-border bg-card p-4 shadow-2xl animate-in zoom-in-95"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-semibold">选一个预设(也可以从空白开始)</h3>
          <button onClick={onClose} className="text-muted-foreground hover:text-foreground">
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="grid max-h-[60vh] grid-cols-2 gap-2 overflow-y-auto">
          {presets.map((p) => (
            <button
              key={p.name}
              onClick={() => onPick(p)}
              className="flex flex-col gap-1 rounded-md border border-border bg-background p-2.5 text-left transition-colors hover:border-primary/40 hover:bg-primary/5"
            >
              <span className="text-xs font-semibold">{p.name}</span>
              <span className="truncate font-mono text-[10px] text-muted-foreground">{p.baseUrl}</span>
              {p.hint && <span className="text-[10px] text-muted-foreground/80">{p.hint}</span>}
            </button>
          ))}
        </div>
        <div className="mt-3 border-t border-border pt-3">
          <button
            onClick={() => onPick(null)}
            className="w-full rounded-md border border-dashed border-border py-2 text-xs text-muted-foreground hover:border-primary/40 hover:text-foreground"
          >
            从空白开始
          </button>
        </div>
      </div>
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
    <div className="space-y-1">
      <div className="flex items-center justify-between">
        <label className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{label}</label>
        {hint && <span className="text-[10px] text-muted-foreground/70">{hint}</span>}
      </div>
      {children}
    </div>
  )
}

function Input({
  value,
  onChange,
  placeholder,
  mono,
}: {
  value: string
  onChange: (v: string) => void
  placeholder?: string
  mono?: boolean
}) {
  return (
    <input
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      spellCheck={false}
      className={cn(
        'h-8 w-full rounded-md border border-input bg-background px-2.5 text-xs outline-none focus:border-ring',
        mono && 'font-mono'
      )}
    />
  )
}

function SecretInput({
  value,
  onChange,
  show,
  setShow,
  placeholder,
}: {
  value: string
  onChange: (v: string) => void
  show: boolean
  setShow: (v: boolean) => void
  placeholder?: string
}) {
  return (
    <div className="relative">
      <input
        value={value}
        type={show ? 'text' : 'password'}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        spellCheck={false}
        autoComplete="off"
        className="h-8 w-full rounded-md border border-input bg-background px-2.5 pr-9 font-mono text-xs outline-none focus:border-ring"
      />
      <button
        type="button"
        onClick={() => setShow(!show)}
        className="absolute right-1 top-1/2 -translate-y-1/2 rounded p-1 text-muted-foreground hover:bg-secondary hover:text-foreground"
        title={show ? '隐藏' : '显示'}
      >
        {show ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
      </button>
    </div>
  )
}

function TabPill({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'inline-flex h-8 items-center gap-1.5 rounded-md px-3 text-xs font-medium transition-colors',
        active
          ? 'bg-info/15 text-info'
          : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
      )}
    >
      {label}
    </button>
  )
}
