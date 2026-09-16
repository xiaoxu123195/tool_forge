import { useCallback, useEffect, useMemo, useState } from 'react'
import { AlertTriangle, Blocks, Plug, RefreshCw, Sparkles } from 'lucide-react'
import { ScanAIConfig, ToggleMCPServer } from '../../../wailsjs/go/main/App'
import type { aiconfig } from '../../../wailsjs/go/models'
import { ToolShell } from '@/components/tool/ToolShell'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { cn } from '@/lib/utils'
import { meta } from './meta'
import { ORIGINS, originMeta } from './origins'
import { OriginBadge, SourceLine } from './Source'
import { SourceViewer } from './SourceViewer'
import { DuplicateDialog } from './DuplicateDialog'

type Kind = 'all' | 'mcp' | 'skills' | 'plugins'

/**
 * 本机 AI 配置总览。
 *
 * 一台开发机上往往同时装着好几家 AI 工具,每家把 MCP、skills、插件落在自己的地方:
 * Claude Code 在 ~/.claude 和 ~/.claude.json(全局一段、每个项目一段),Codex 在
 * config.toml,Gemini 在 settings.json,Cline 跟着 VS Code 走,Continue 和 Trae 的
 * skills 全是指向 ~/.agents/skills 这个共享池的软链……
 *
 * 人要回答"我到底配了哪些 MCP / 这个 skill 是哪来的",得挨个去翻。翻漏一处就以为
 * 没配,实际配在了另一处。真机上扫出来立刻能看到这种局面的代价:同一个 context7
 * 在两家各配了一份、acemcp 全局和项目级各一份、有个插件启用了却根本没装。
 *
 * 所以这一页做两件事:扫全,并且每一条都标明来源和出处文件。
 */
export default function AIConfigTool() {
  const [snap, setSnap] = useState<aiconfig.Snapshot | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  // 选中的来源;空 = 全部
  const [origin, setOrigin] = useState('')
  const [kind, setKind] = useState<Kind>('all')
  const [viewing, setViewing] = useState('')
  // 正在并排比较的那个名字;空 = 没开
  const [comparing, setComparing] = useState('')
  // 正在启停的那条(工具箱自己的 MCP 的 id)
  const [busy, setBusy] = useState('')
  const dialog = useConfirm()

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      setSnap((await ScanAIConfig()) as unknown as aiconfig.Snapshot)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  // 只有工具箱自己的 MCP 能在这页启停 —— 复用设置页那条路,改完重扫,
  // 让来源墙上的计数和列表状态一起刷新
  const toggle = async (m: aiconfig.MCPEntry) => {
    if (!m.toggleable || !m.id) return
    setBusy(m.id)
    try {
      const err = ((await ToggleMCPServer(m.id, !m.enabled)) as string) || ''
      if (err) {
        await dialog({ title: '切换失败', message: err, confirmLabel: '知道了' })
        return
      }
      await load()
    } finally {
      setBusy('')
    }
  }

  const mcp = useMemo(
    () => (snap?.mcp ?? []).filter((m) => !origin || m.source.origin === origin),
    [snap, origin],
  )
  const skills = useMemo(
    () => (snap?.skills ?? []).filter((s) => !origin || s.source.origin === origin),
    [snap, origin],
  )
  const plugins = useMemo(
    () => (snap?.plugins ?? []).filter((p) => !origin || p.source.origin === origin),
    [snap, origin],
  )

  // 同名跨来源出现 = 在好几家各配了一份。按名字归组,不看来源;归到一起才能并排比
  const dupeGroups = useMemo(() => {
    const g = new Map<string, aiconfig.MCPEntry[]>()
    for (const m of snap?.mcp ?? []) g.set(m.name, [...(g.get(m.name) ?? []), m])
    for (const [k, v] of g) if (v.length < 2) g.delete(k)
    return g
  }, [snap])

  const originInfo = useMemo(() => {
    const m = new Map<string, aiconfig.OriginInfo>()
    for (const o of snap?.origins ?? []) m.set(o.origin, o)
    return m
  }, [snap])

  // 墙上的卡片:名单里的按固定顺序排,后端按形状发现的名单外来源追在后面。
  // 只画名单会让「装了但不在名单里」的那几家凭空消失 —— 而它们的 skills 在对话里是看得见的
  const wall = useMemo(() => {
    const extra = (snap?.origins ?? [])
      .filter((o) => !o.known)
      .map((o) => originMeta(o.origin))
    return [...ORIGINS, ...extra]
  }, [snap])

  const total = {
    mcp: snap?.mcp?.length ?? 0,
    skills: snap?.skills?.length ?? 0,
    plugins: snap?.plugins?.length ?? 0,
  }
  const selectedMeta = origin ? originMeta(origin) : null

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      actions={
        <Button variant="ghost" size="sm" onClick={() => void load()} disabled={loading}>
          <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />
          重新扫描
        </Button>
      }
    >
      <div className="mx-auto max-w-6xl space-y-6">
        {error && (
          <div className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive">
            {error}
          </div>
        )}

        {/* 解析失败的文件必须摆出来:它在列表上的表现是「这一处什么都没有」,
            和「这一处本来就是空的」一模一样,而前者是要处理的 */}
        {snap && snap.problems?.length > 0 && (
          <div className="space-y-1 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-[11px] text-amber-700 dark:text-amber-400">
            <div className="flex items-center gap-1.5 font-medium">
              <AlertTriangle className="h-3.5 w-3.5" />
              有 {snap.problems.length} 个文件没读成,下面列出的内容是不全的
            </div>
            {snap.problems.map((p) => (
              <div key={p.file} className="font-mono">
                {p.file} —— {p.detail}
              </div>
            ))}
          </div>
        )}

        {/* ---- 来源墙 ---- */}
        <section>
          <SectionTitle
            title="来源"
            hint={origin ? '再点一次回到全部' : '点一家只看它的;没装的也列着,免得以为漏扫了'}
          />
          <div className="grid grid-cols-2 gap-2.5 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
            {wall.map((o) => {
              const info = originInfo.get(o.id)
              const present = info?.present ?? false
              const active = origin === o.id
              const Icon = o.icon
              const count = (info?.mcp ?? 0) + (info?.skills ?? 0) + (info?.plugins ?? 0)
              return (
                <button
                  key={o.id}
                  onClick={() => setOrigin(active ? '' : o.id)}
                  disabled={!present}
                  title={present ? o.blurb : `${o.name}:本机没有它的配置目录`}
                  className={cn(
                    'group relative flex flex-col overflow-hidden rounded-lg border bg-card p-3 text-left transition-all duration-200 card-soft',
                    present
                      ? 'cursor-pointer hover:-translate-y-0.5 hover:border-foreground/30 hover:shadow-lg hover:shadow-foreground/5'
                      : 'cursor-default opacity-45',
                    active ? 'border-foreground/40 ring-1 ring-foreground/20' : 'border-border/60',
                  )}
                >
                  {/* 顶部一道色线:八家各一色,扫一眼就分得开 */}
                  <span className={cn('absolute inset-x-0 top-0 h-0.5', o.bar, !present && 'opacity-40')} />
                  <div className="flex items-center gap-2.5">
                    <div
                      className={cn(
                        'flex h-8 w-8 shrink-0 items-center justify-center rounded-md transition-transform',
                        o.tone,
                        present && 'group-hover:scale-110',
                      )}
                    >
                      <Icon className="h-4 w-4" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-medium">{o.name}</div>
                      <div className="text-[10px] text-muted-foreground">
                        {!present ? '未安装' : count === 0 ? '装了,没配东西' : `${count} 项`}
                      </div>
                    </div>
                  </div>
                  {present && count > 0 && (
                    <div className="mt-2.5 flex gap-1 text-[10px] text-muted-foreground">
                      <Stat n={info?.mcp ?? 0} label="MCP" />
                      <Stat n={info?.skills ?? 0} label="skills" />
                      <Stat n={info?.plugins ?? 0} label="插件" />
                    </div>
                  )}
                </button>
              )
            })}
          </div>
        </section>

        {/* ---- 类型筛选 ---- */}
        <div className="flex items-center gap-1 border-b border-border">
          <KindTab title="筛选：全部" active={kind === 'all'} onClick={() => setKind('all')}>
            全部
          </KindTab>
          <KindTab title="筛选：MCP 服务器" active={kind === 'mcp'} onClick={() => setKind('mcp')}>
            <Plug className="h-3.5 w-3.5" />
            MCP 服务器
            <Count n={origin ? mcp.length : total.mcp} />
          </KindTab>
          <KindTab title="筛选：Skills" active={kind === 'skills'} onClick={() => setKind('skills')}>
            <Sparkles className="h-3.5 w-3.5" />
            Skills
            <Count n={origin ? skills.length : total.skills} />
          </KindTab>
          <KindTab title="筛选：插件" active={kind === 'plugins'} onClick={() => setKind('plugins')}>
            <Blocks className="h-3.5 w-3.5" />
            插件
            <Count n={origin ? plugins.length : total.plugins} />
          </KindTab>
          {selectedMeta && (
            <span className="ml-auto flex items-center gap-1.5 pb-1 text-[11px] text-muted-foreground">
              只看 <OriginBadge origin={selectedMeta.id} />
              <button onClick={() => setOrigin('')} className="underline-offset-2 hover:underline">
                清除
              </button>
            </span>
          )}
        </div>

        {loading && !snap ? (
          <Hint>扫描中…</Hint>
        ) : (
          <div className="space-y-8">
            {(kind === 'all' || kind === 'mcp') && (
              <section>
                {kind === 'all' && (
                  <SectionTitle title="MCP 服务器" count={mcp.length} icon={<Plug className="h-4 w-4" />} />
                )}
                <MCPList
                  items={mcp}
                  dupes={dupeGroups}
                  onOpen={setViewing}
                  onCompare={setComparing}
                  onToggle={toggle}
                  busy={busy}
                />
              </section>
            )}
            {(kind === 'all' || kind === 'skills') && (
              <section>
                {kind === 'all' && (
                  <SectionTitle title="Skills" count={skills.length} icon={<Sparkles className="h-4 w-4" />} />
                )}
                <SkillList items={skills} onOpen={setViewing} />
              </section>
            )}
            {(kind === 'all' || kind === 'plugins') && (
              <section>
                {kind === 'all' && (
                  <SectionTitle title="插件" count={plugins.length} icon={<Blocks className="h-4 w-4" />} />
                )}
                <PluginList items={plugins} onOpen={setViewing} />
              </section>
            )}
          </div>
        )}
      </div>

      <SourceViewer path={viewing} onClose={() => setViewing('')} onSaved={() => void load()} />
      {comparing && dupeGroups.has(comparing) && (
        <DuplicateDialog
          name={comparing}
          entries={dupeGroups.get(comparing) ?? []}
          onClose={() => setComparing('')}
          onOpen={(f) => {
            setComparing('')
            setViewing(f)
          }}
        />
      )}
    </ToolShell>
  )
}

// ---------------------------------------------------------------- 列表

function MCPList({
  items,
  dupes,
  onOpen,
  onCompare,
  onToggle,
  busy,
}: {
  items: aiconfig.MCPEntry[]
  dupes: Map<string, aiconfig.MCPEntry[]>
  onOpen: (p: string) => void
  onCompare: (name: string) => void
  onToggle: (m: aiconfig.MCPEntry) => void
  busy: string
}) {
  if (items.length === 0) return <Empty>没有 MCP 服务器</Empty>
  return (
    <div className="grid grid-cols-1 gap-2.5 lg:grid-cols-2">
      {items.map((m, i) => (
        <Card key={`${m.source.file}-${m.source.scope}-${m.name}-${i}`} origin={m.source.origin}>
          <div className="flex items-center gap-2">
            <OriginBadge origin={m.source.origin} />
            <span className="truncate font-medium">{m.name}</span>
            <Tag>{m.kind}</Tag>
            {dupes.has(m.name) && (
              <button
                type="button"
                onClick={() => onCompare(m.name)}
                title="这个名字在多处各配了一份 —— 点开并排比较"
                className="shrink-0"
              >
                <Tag tone="warn">多处重复 · 对比</Tag>
              </button>
            )}
            {m.toggleable ? (
              <button
                type="button"
                onClick={() => onToggle(m)}
                disabled={busy === m.id}
                title={m.enabled ? '点击停用' : '点击启用'}
                className={cn(
                  'ml-auto h-6 shrink-0 rounded-md border px-2 text-[10px] transition-colors disabled:opacity-50',
                  m.enabled
                    ? 'border-info/40 bg-info/10 text-info'
                    : 'border-border text-muted-foreground hover:bg-secondary',
                )}
              >
                {busy === m.id ? '…' : m.enabled ? '已启用' : '已停用'}
              </button>
            ) : (
              !m.enabled && <Tag>已停用</Tag>
            )}
          </div>
          <div className="mt-1.5 truncate font-mono text-[11px] text-muted-foreground" title={m.url || m.command}>
            {m.url || [m.command, ...(m.args ?? [])].join(' ')}
          </div>
          {/* 环境变量只列键名:里面常年躺着 API key */}
          {m.envKeys && m.envKeys.length > 0 && (
            <div className="mt-1 truncate text-[10px] text-muted-foreground" title={m.envKeys.join('\n')}>
              env：{m.envKeys.join('、')}
              <span className="ml-1 opacity-60">(只列键名)</span>
            </div>
          )}
          <SourceLine source={m.source} onOpen={onOpen} />
        </Card>
      ))}
    </div>
  )
}

function SkillList({ items, onOpen }: { items: aiconfig.SkillEntry[]; onOpen: (p: string) => void }) {
  if (items.length === 0) return <Empty>没有 skills</Empty>
  return (
    <div className="grid grid-cols-1 gap-2.5 lg:grid-cols-2 xl:grid-cols-3">
      {items.map((s, i) => (
        <Card key={`${s.dir}-${i}`} origin={s.source.origin}>
          <div className="flex items-center gap-2">
            <OriginBadge origin={s.source.origin} />
            <span className="truncate font-medium">{s.name}</span>
            {!s.hasSkillMd && (
              <Tag tone="warn" title="没有 SKILL.md,多半不会被加载">
                缺 SKILL.md
              </Tag>
            )}
          </div>
          {s.description ? (
            <p className="mt-1.5 line-clamp-2 text-[11px] leading-relaxed text-muted-foreground">
              {s.description}
            </p>
          ) : (
            <p className="mt-1.5 text-[11px] italic text-muted-foreground/60">没有描述</p>
          )}
          <div className="mt-1.5 text-[10px] text-muted-foreground">
            {s.fileCount} 个文件{s.updatedAt && ` · ${s.updatedAt}`}
          </div>
          <SourceLine
            source={{ ...s.source, file: s.hasSkillMd ? `${s.dir}\\SKILL.md` : s.dir }}
            linkTarget={s.linkTarget}
            onOpen={onOpen}
            openable={s.hasSkillMd}
          />
        </Card>
      ))}
    </div>
  )
}

function PluginList({ items, onOpen }: { items: aiconfig.PluginEntry[]; onOpen: (p: string) => void }) {
  if (items.length === 0) return <Empty>没有插件</Empty>
  return (
    <div className="grid grid-cols-1 gap-2.5 lg:grid-cols-2">
      {items.map((p) => (
        <Card key={p.name} origin={p.source.origin}>
          <div className="flex items-center gap-2">
            <OriginBadge origin={p.source.origin} />
            <span className="truncate font-medium">{p.name}</span>
            {p.version && <span className="text-[10px] text-muted-foreground">v{p.version}</span>}
            {/* 「装了」和「启用了」是两个文件各自记的,实测会对不上。
                对不上时插件是静默不起作用的,不点破没人会发现 */}
            {p.enabled && !p.installed ? (
              <Tag tone="warn">启用了但没装</Tag>
            ) : !p.enabled && p.installed ? (
              <Tag>装了但没启用</Tag>
            ) : (
              <Tag tone="ok">已启用</Tag>
            )}
          </div>
          <div className="mt-1.5 text-[10px] text-muted-foreground">
            {p.skillCount > 0 ? `自带 ${p.skillCount} 个 skill` : '不带 skill'}
          </div>
          {p.installPath && (
            <div className="mt-1 truncate font-mono text-[10px] text-muted-foreground" title={p.installPath}>
              {p.installPath}
            </div>
          )}
          <SourceLine source={p.source} onOpen={onOpen} />
        </Card>
      ))}
    </div>
  )
}

// ---------------------------------------------------------------- 小件

/** 卡片左边一道来源色的竖线,列表里靠它和 badge 一起分来源 */
function Card({ origin, children }: { origin: string; children: React.ReactNode }) {
  const m = originMeta(origin)
  return (
    <div className="relative overflow-hidden rounded-lg border border-border/60 bg-card p-3 pl-4 card-soft">
      <span className={cn('absolute inset-y-0 left-0 w-0.5', m.bar)} />
      {children}
    </div>
  )
}

function Tag({
  children,
  tone,
  title,
}: {
  children: React.ReactNode
  tone?: 'warn' | 'ok'
  title?: string
}) {
  return (
    <span
      title={title}
      className={cn(
        'shrink-0 rounded px-1.5 py-0.5 text-[10px]',
        tone === 'warn'
          ? 'bg-amber-500/15 text-amber-700 dark:text-amber-400'
          : tone === 'ok'
            ? 'bg-success/15 text-success'
            : 'bg-muted text-muted-foreground',
      )}
    >
      {children}
    </span>
  )
}

function Stat({ n, label }: { n: number; label: string }) {
  return (
    <span className={cn('rounded bg-muted px-1.5 py-px', n === 0 && 'opacity-40')}>
      {n} {label}
    </span>
  )
}

function Count({ n }: { n: number }) {
  return <span className="rounded-full bg-muted px-1.5 text-[10px] tabular-nums">{n}</span>
}

function KindTab({
  active,
  onClick,
  title,
  children,
}: {
  active: boolean
  onClick: () => void
  /** 精确的标签名。文字里还拼着计数,textContent 是「插件2」,按文字找不到它 */
  title: string
  children: React.ReactNode
}) {
  return (
    <button
      onClick={onClick}
      title={title}
      className={cn(
        'relative flex h-9 items-center gap-1.5 px-3 text-sm transition-colors',
        active ? 'font-medium text-foreground' : 'text-muted-foreground hover:text-foreground',
      )}
    >
      {children}
      {active && <span className="absolute inset-x-2 bottom-0 h-0.5 rounded-full bg-info" />}
    </button>
  )
}

function SectionTitle({
  title,
  count,
  hint,
  icon,
}: {
  title: string
  count?: number
  hint?: string
  icon?: React.ReactNode
}) {
  return (
    <div className="mb-2.5 flex items-baseline gap-2">
      <h2 className="flex items-center gap-1.5 text-sm font-semibold">
        {icon}
        {title}
      </h2>
      {count !== undefined && <span className="text-xs text-muted-foreground">{count}</span>}
      {hint && <span className="text-[11px] text-muted-foreground">· {hint}</span>}
    </div>
  )
}

function Empty({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-dashed border-border p-6 text-center text-xs text-muted-foreground">
      {children}
    </div>
  )
}

function Hint({ children }: { children: React.ReactNode }) {
  return <div className="py-16 text-center text-sm text-muted-foreground">{children}</div>
}
