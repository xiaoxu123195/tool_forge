import { useState } from 'react'
import { AlertTriangle, FileJson, FolderOpen, Loader2, Search } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { cn } from '@/lib/utils'
import { ParseOpenAPISpec, ParseOpenAPIText, PickLocalFile, SaveAPIPack } from '../../../wailsjs/go/main/App'
import type { apitool } from '../../../wailsjs/go/models'

/**
 * 从 OpenAPI 文档导入接口，包装成 AI 可调用的工具。
 *
 * 三步：给文档 → 勾接口 → 填地址和认证。分步是因为第二步经常要翻很久：
 * 一份真实的文档动辄一两百个接口，而人想导的是其中三五个。
 */

type Step = 'source' | 'pick' | 'config'

const AUTH_KINDS: { value: string; label: string; needName: boolean; hint: string }[] = [
  { value: '', label: '不需要认证', needName: false, hint: '' },
  { value: 'bearer', label: 'Bearer Token', needName: false, hint: '发送 Authorization: Bearer <密钥>' },
  { value: 'header', label: '自定义请求头', needName: true, hint: '发送 <头名>: <密钥>' },
  { value: 'query', label: 'URL 参数', needName: true, hint: '拼成 ?<参数名>=<密钥>' },
  { value: 'basic', label: 'Basic 认证', needName: false, hint: '密钥填成 用户名:密码' },
]

export function ImportDialog({
  editing,
  onClose,
  onSaved,
}: {
  /** 非空表示在改一个已有的包 */
  editing: apitool.Pack | null
  onClose: () => void
  onSaved: (p: apitool.Pack) => void
}) {
  const [step, setStep] = useState<Step>(editing ? 'config' : 'source')
  const [source, setSource] = useState(editing?.source ?? '')
  const [pasted, setPasted] = useState('')
  const [parsed, setParsed] = useState<apitool.ParseResult | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const [picked, setPicked] = useState<Set<string>>(new Set(editing?.ops.map((o) => o.id) ?? []))
  const [filter, setFilter] = useState('')

  const [name, setName] = useState(editing?.name ?? '')
  const [baseURL, setBaseURL] = useState(editing?.baseUrl ?? '')
  const [authKind, setAuthKind] = useState(editing?.auth.kind ?? '')
  const [authName, setAuthName] = useState(editing?.auth.name ?? '')
  const [secret, setSecret] = useState('')

  const ops = parsed?.ops ?? editing?.ops ?? []
  const shown = ops.filter((o) => {
    if (!filter.trim()) return true
    const q = filter.toLowerCase()
    return (
      o.id.toLowerCase().includes(q) ||
      o.path.toLowerCase().includes(q) ||
      (o.summary ?? '').toLowerCase().includes(q) ||
      (o.tags ?? []).some((t) => t.toLowerCase().includes(q))
    )
  })

  const parse = async (fn: () => Promise<apitool.ParseResult>) => {
    setBusy(true)
    setError('')
    try {
      const r = await fn()
      setParsed(r)
      setPicked(new Set(r.ops.map((o) => o.id)))
      if (!name) setName(r.title || '接口包')
      setBaseURL(r.baseUrl || '')
      setStep('pick')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const save = async () => {
    setBusy(true)
    setError('')
    try {
      const pack = {
        ...(editing ?? {}),
        id: editing?.id ?? '',
        name: name.trim(),
        baseUrl: baseURL.trim(),
        source: source || editing?.source || '',
        specTitle: parsed?.title ?? editing?.specTitle ?? '',
        auth: { kind: authKind, name: authName.trim(), hasSecret: editing?.auth.hasSecret ?? false },
        ops: ops.filter((o) => picked.has(o.id)),
        createdAt: editing?.createdAt ?? 0,
        updatedAt: 0,
      } as apitool.Pack
      const saved = await SaveAPIPack(pack, secret)
      onSaved(saved)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const authMeta = AUTH_KINDS.find((a) => a.value === authKind) ?? AUTH_KINDS[0]
  const canSave = name.trim() !== '' && baseURL.trim() !== '' && picked.size > 0

  return (
    <Dialog
      open
      onClose={onClose}
      title={editing ? `修改接口包：${editing.name}` : '从 OpenAPI 文档导入接口'}
      description="选中的接口会变成工具，挂在本地 API 上；还要在「本地 API」页里勾选才会对外暴露"
      width="w-[720px]"
      footer={
        <>
          {step === 'pick' && (
            <span className="text-[11px] text-muted-foreground">
              选中 {picked.size} / {ops.length}
            </span>
          )}
          {step !== 'source' && !editing && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setStep(step === 'config' ? 'pick' : 'source')}
            >
              上一步
            </Button>
          )}
          <Button variant="outline" size="sm" className={cn(step === 'pick' ? '' : 'ml-auto')} onClick={onClose}>
            取消
          </Button>
          {step === 'pick' && (
            <Button size="sm" disabled={picked.size === 0} onClick={() => setStep('config')}>
              下一步
            </Button>
          )}
          {step === 'config' && (
            <Button size="sm" disabled={!canSave || busy} onClick={() => void save()}>
              {busy && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              保存并生成工具
            </Button>
          )}
        </>
      }
    >
      {error && (
        <p className="whitespace-pre-wrap break-words rounded-md border border-destructive/30 bg-destructive/10 px-2 py-1.5 text-[11px] text-destructive">
          {error}
        </p>
      )}

      {step === 'source' && (
        <div className="space-y-3">
          <div>
            <div className="mb-1 text-[11px] text-muted-foreground">文档地址或本地文件</div>
            <div className="flex gap-1.5">
              <input
                value={source}
                onChange={(e) => setSource(e.target.value)}
                placeholder="https://example.com/openapi.json"
                spellCheck={false}
                className="h-8 flex-1 rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
              />
              <Button
                variant="outline"
                size="sm"
                onClick={async () => {
                  const p = await PickLocalFile('选择 OpenAPI 文档').catch(() => '')
                  if (p) setSource(p)
                }}
              >
                <FolderOpen className="h-3.5 w-3.5" />
                选文件
              </Button>
              <Button
                size="sm"
                disabled={!source.trim() || busy}
                onClick={() => void parse(() => ParseOpenAPISpec(source.trim()))}
              >
                {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Search className="h-3.5 w-3.5" />}
                解析
              </Button>
            </div>
          </div>

          <div>
            <div className="mb-1 text-[11px] text-muted-foreground">或者直接粘贴内容（JSON / YAML 都行）</div>
            <textarea
              value={pasted}
              onChange={(e) => setPasted(e.target.value)}
              rows={7}
              spellCheck={false}
              placeholder={'openapi: 3.0.0\ninfo:\n  title: ...'}
              className="w-full resize-y rounded-md border border-input bg-background px-2 py-1.5 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
            />
            <Button
              size="sm"
              variant="secondary"
              className="mt-1.5"
              disabled={!pasted.trim() || busy}
              onClick={() => void parse(() => ParseOpenAPIText(pasted))}
            >
              <FileJson className="h-3.5 w-3.5" />
              解析粘贴的内容
            </Button>
          </div>
        </div>
      )}

      {step === 'pick' && (
        <div className="space-y-2">
          {parsed && (
            <p className="text-[11px] text-muted-foreground">
              {parsed.title} {parsed.version} · {parsed.specVersion} · 共 {ops.length} 个接口
            </p>
          )}
          {/* 跳过了什么必须说:跳过的接口在列表里是看不见的,不说就成了"怎么少了几个" */}
          {parsed?.warnings.map((w) => (
            <p
              key={w}
              className="flex items-start gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-1 text-[11px] text-amber-700 dark:text-amber-400"
            >
              <AlertTriangle className="mt-0.5 h-3 w-3 shrink-0" />
              {w}
            </p>
          ))}

          <div className="flex items-center gap-2">
            <input
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="按路径、名称或 tag 过滤"
              spellCheck={false}
              className="h-7 flex-1 rounded-md border border-input bg-background px-2 text-xs outline-none focus:ring-1 focus:ring-ring"
            />
            <button
              type="button"
              onClick={() => setPicked(new Set(shown.map((o) => o.id)))}
              className="rounded px-1.5 py-0.5 text-[11px] text-muted-foreground hover:bg-secondary hover:text-foreground"
            >
              全选
            </button>
            <button
              type="button"
              onClick={() => setPicked(new Set())}
              className="rounded px-1.5 py-0.5 text-[11px] text-muted-foreground hover:bg-secondary hover:text-foreground"
            >
              全不选
            </button>
          </div>

          <ul className="max-h-[320px] space-y-1 overflow-auto">
            {shown.map((o) => (
              <li key={o.id}>
                <label
                  className={cn(
                    'flex cursor-pointer items-start gap-2 rounded-md border px-2 py-1.5 transition-colors',
                    picked.has(o.id) ? 'border-info/40 bg-info/5' : 'border-border hover:bg-accent/40',
                  )}
                >
                  <input
                    type="checkbox"
                    checked={picked.has(o.id)}
                    onChange={(e) =>
                      setPicked((p) => {
                        const next = new Set(p)
                        if (e.target.checked) next.add(o.id)
                        else next.delete(o.id)
                        return next
                      })
                    }
                    className="mt-0.5 h-3.5 w-3.5 shrink-0"
                  />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                      <span className={cn('shrink-0 rounded-sm px-1 text-[9px] font-medium', methodTone(o.method))}>
                        {o.method}
                      </span>
                      <span className="truncate font-mono text-[11px]">{o.path}</span>
                      {o.deprecated && (
                        <span className="shrink-0 rounded-sm bg-amber-500/15 px-1 text-[9px] text-amber-700 dark:text-amber-400">
                          已废弃
                        </span>
                      )}
                    </div>
                    {o.summary && (
                      <div className="mt-0.5 truncate text-[10px] text-muted-foreground">{o.summary}</div>
                    )}
                  </div>
                  <span className="shrink-0 text-[10px] text-muted-foreground">{o.params.length} 参数</span>
                </label>
              </li>
            ))}
          </ul>
        </div>
      )}

      {step === 'config' && (
        <div className="space-y-3">
          <Field label="包名" hint="会作为工具名前缀，只保留字母数字和短横线">
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="订单服务"
              className="h-8 w-full rounded-md border border-input bg-background px-2 text-xs outline-none focus:ring-1 focus:ring-ring"
            />
          </Field>

          <Field label="请求地址" hint="文档里的地址只是默认值，内网服务通常要改">
            <input
              value={baseURL}
              onChange={(e) => setBaseURL(e.target.value)}
              placeholder="https://api.example.com/v1"
              spellCheck={false}
              className="h-8 w-full rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
            />
          </Field>

          <Field label="认证方式" hint={authMeta.hint}>
            <div className="flex gap-1.5">
              <select
                value={authKind}
                onChange={(e) => setAuthKind(e.target.value)}
                className="h-8 rounded-md border border-input bg-background px-2 text-xs outline-none focus:ring-1 focus:ring-ring"
              >
                {AUTH_KINDS.map((a) => (
                  <option key={a.value} value={a.value}>
                    {a.label}
                  </option>
                ))}
              </select>
              {authMeta.needName && (
                <input
                  value={authName}
                  onChange={(e) => setAuthName(e.target.value)}
                  placeholder={authKind === 'query' ? 'api_key' : 'X-API-Key'}
                  spellCheck={false}
                  className="h-8 w-40 rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
                />
              )}
            </div>
          </Field>

          {authKind !== '' && (
            <Field
              label="密钥"
              hint={
                editing?.auth.hasSecret
                  ? '已经存过一个。留空表示不改动它'
                  : '存进系统凭据库，不会写进配置文件；发请求那一刻才读出来'
              }
            >
              <input
                type="password"
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
                placeholder={editing?.auth.hasSecret ? '••••••（留空不改）' : ''}
                spellCheck={false}
                className="h-8 w-full rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
              />
            </Field>
          )}

          <p className="rounded-md border border-border bg-muted/40 px-2 py-1.5 text-[11px] text-muted-foreground">
            会生成 {picked.size} 个工具，名字形如{' '}
            <span className="font-mono">api-{slug(name) || '包名'}-接口名</span>。
            生成后要去「设置 → 本地 API」里勾选，才会对 Claude Code / Codex 暴露。
          </p>
        </div>
      )}
    </Dialog>
  )
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-1 flex items-baseline gap-2">
        <span className="text-[11px] font-medium">{label}</span>
        {hint && <span className="text-[10px] text-muted-foreground">{hint}</span>}
      </div>
      {children}
    </div>
  )
}

function methodTone(m: string): string {
  switch (m) {
    case 'GET':
      return 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
    case 'POST':
      return 'bg-info/15 text-info'
    case 'DELETE':
      return 'bg-destructive/15 text-destructive'
    default:
      return 'bg-amber-500/15 text-amber-700 dark:text-amber-400'
  }
}

/** 和后端 sanitize 一致的简化版，只为在界面上预览工具名 */
export function slug(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, '-')
    .replace(/^-+|-+$/g, '')
}
