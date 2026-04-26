import { useEffect, useMemo, useState } from 'react'
import {
  AlertCircle,
  BookMarked,
  Copy,
  Plus,
  Star,
  Trash2,
} from 'lucide-react'
import { ToolShell } from '@/components/tool/ToolShell'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { meta } from './meta'
import {
  BUILTIN_PRESETS,
  compile,
  findMatches,
  loadSnippets,
  replaceAll,
  saveSnippets,
  type MatchInfo,
  type Snippet,
} from './lib'

type TabKey = 'match' | 'replace' | 'snippets'

const EXAMPLE_TEXT = `示例文本（可随意替换）：
联系: alice@example.com 或 bob.smith+promo@sub.company.co
主页: https://tool-forge.dev/docs/guide?tab=quick
IPv4: 192.168.1.10 10.0.0.255
UUID: 3fa85f64-5717-4562-b3fc-2c963f66afa6
时间: 2026-04-22T09:15:30+08:00`

export default function RegexTool() {
  const [pattern, setPattern] = useState(BUILTIN_PRESETS[0].pattern)
  const [flags, setFlags] = useState('g')
  const [text, setText] = useState(EXAMPLE_TEXT)
  const [replacement, setReplacement] = useState('[$&]')
  const [tab, setTab] = useState<TabKey>('match')
  const [snippets, setSnippets] = useState<Snippet[]>(() => loadSnippets())
  const [snippetName, setSnippetName] = useState('')

  useEffect(() => {
    saveSnippets(snippets)
  }, [snippets])

  const compiled = useMemo(() => compile(pattern, flags), [pattern, flags])
  const matches: MatchInfo[] = useMemo(() => {
    if (!compiled.regex) return []
    return findMatches(compiled.regex, text)
  }, [compiled.regex, text])
  const replaced = useMemo(() => {
    if (!compiled.regex) return ''
    return replaceAll(compiled.regex, text, replacement)
  }, [compiled.regex, text, replacement])

  const clear = () => {
    setPattern('')
    setFlags('g')
    setText('')
    setReplacement('')
  }

  const loadExample = () => {
    setPattern(BUILTIN_PRESETS[0].pattern)
    setFlags('g')
    setText(EXAMPLE_TEXT)
  }

  const toggleFlag = (f: string) => {
    setFlags((cur) => (cur.includes(f) ? cur.replace(f, '') : cur + f))
  }

  const copy = async (s: string) => {
    try {
      await navigator.clipboard.writeText(s)
    } catch {
      // ignore
    }
  }

  const saveAsSnippet = () => {
    if (!pattern) return
    const name = snippetName.trim() || pattern.slice(0, 24)
    const s: Snippet = {
      id: `s-${Date.now()}`,
      name,
      pattern,
      flags,
      createdAt: Date.now(),
    }
    setSnippets((cur) => [s, ...cur])
    setSnippetName('')
  }

  const applySnippet = (s: Snippet) => {
    setPattern(s.pattern)
    setFlags(s.flags)
  }

  const deleteSnippet = (id: string) => {
    setSnippets((cur) => cur.filter((s) => s.id !== id))
  }

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      onClear={clear}
      onLoadExample={loadExample}
    >
      <div className="mx-auto flex h-full max-w-5xl flex-col gap-3">
        {/* 正则输入行 */}
        <div className="flex flex-wrap items-start gap-2">
          <div className="flex min-w-0 flex-1 items-center gap-2 rounded-md border border-border bg-background px-2 py-1.5">
            <span className="shrink-0 text-muted-foreground">/</span>
            <input
              value={pattern}
              onChange={(e) => setPattern(e.target.value)}
              placeholder="输入正则表达式"
              spellCheck={false}
              className="flex-1 border-0 bg-transparent font-mono text-sm outline-none"
            />
            <span className="shrink-0 text-muted-foreground">/</span>
            <input
              value={flags}
              onChange={(e) => setFlags(e.target.value.replace(/[^gimsuy]/g, ''))}
              spellCheck={false}
              className="w-16 border-0 bg-transparent font-mono text-sm outline-none"
              placeholder="flags"
            />
          </div>
          <FlagToggles flags={flags} onToggle={toggleFlag} />
        </div>

        {compiled.error && (
          <div className="flex items-center gap-2 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">
            <AlertCircle className="h-3.5 w-3.5" />
            <span className="font-mono">{compiled.error}</span>
          </div>
        )}

        {/* 主区域：左测试+高亮，右 Tab */}
        <div className="grid flex-1 min-h-0 grid-cols-1 gap-3 lg:grid-cols-2">
          {/* 左：测试文本 */}
          <div className="flex min-h-0 flex-col gap-2">
            <div className="flex items-center justify-between">
              <span className="text-xs text-muted-foreground">测试文本</span>
              <span className="text-[10px] text-muted-foreground">
                {text.length.toLocaleString()} 字符 · {matches.length} 命中
              </span>
            </div>
            <textarea
              value={text}
              onChange={(e) => setText(e.target.value)}
              spellCheck={false}
              className="min-h-[200px] flex-1 resize-none rounded-md border border-border bg-background px-3 py-2 font-mono text-xs outline-none focus:border-primary/50"
            />
            <div className="flex items-center justify-between text-xs text-muted-foreground">
              <span>高亮预览</span>
              <Button
                variant="ghost"
                size="sm"
                onClick={() =>
                  copy(matches.map((m) => m.full).join('\n'))
                }
                disabled={matches.length === 0}
              >
                <Copy className="h-3 w-3" />
                复制全部命中
              </Button>
            </div>
            <HighlightedText
              text={text}
              matches={matches}
              className="min-h-[80px] max-h-48 overflow-auto rounded-md border border-border bg-secondary/30 p-2 font-mono text-xs"
            />
          </div>

          {/* 右：Tab */}
          <div className="flex min-h-0 flex-col gap-2">
            <div className="flex gap-1">
              <TabButton label={`匹配 (${matches.length})`} active={tab === 'match'} onClick={() => setTab('match')} />
              <TabButton label="替换" active={tab === 'replace'} onClick={() => setTab('replace')} />
              <TabButton
                label={
                  <>
                    <BookMarked className="h-3 w-3" />
                    <span>片段</span>
                    <span className="rounded bg-secondary px-1 text-[10px]">
                      {snippets.length + BUILTIN_PRESETS.length}
                    </span>
                  </>
                }
                active={tab === 'snippets'}
                onClick={() => setTab('snippets')}
              />
            </div>
            <div className="flex-1 overflow-auto rounded-md border border-border bg-card">
              {tab === 'match' && <MatchesPanel matches={matches} onCopy={copy} />}
              {tab === 'replace' && (
                <ReplacePanel
                  replacement={replacement}
                  onChange={setReplacement}
                  result={replaced}
                  onCopy={copy}
                />
              )}
              {tab === 'snippets' && (
                <SnippetsPanel
                  userSnippets={snippets}
                  name={snippetName}
                  onName={setSnippetName}
                  onSave={saveAsSnippet}
                  onApply={applySnippet}
                  onDelete={deleteSnippet}
                  canSave={!!pattern && !compiled.error}
                />
              )}
            </div>
          </div>
        </div>
      </div>
    </ToolShell>
  )
}

function FlagToggles({
  flags,
  onToggle,
}: {
  flags: string
  onToggle: (f: string) => void
}) {
  const list: Array<{ f: string; label: string; title: string }> = [
    { f: 'g', label: 'g', title: 'global' },
    { f: 'i', label: 'i', title: 'ignoreCase' },
    { f: 'm', label: 'm', title: 'multiline' },
    { f: 's', label: 's', title: 'dotAll' },
    { f: 'u', label: 'u', title: 'unicode' },
    { f: 'y', label: 'y', title: 'sticky' },
  ]
  return (
    <div className="flex items-center gap-1">
      {list.map((x) => (
        <button
          key={x.f}
          onClick={() => onToggle(x.f)}
          title={x.title}
          className={cn(
            'inline-flex h-8 w-8 items-center justify-center rounded-md border font-mono text-xs',
            flags.includes(x.f)
              ? 'border-info/50 bg-info/15 text-info'
              : 'border-border bg-background text-muted-foreground hover:bg-accent',
          )}
        >
          {x.label}
        </button>
      ))}
    </div>
  )
}

function HighlightedText({
  text,
  matches,
  className,
}: {
  text: string
  matches: MatchInfo[]
  className?: string
}) {
  if (!text) return <div className={cn(className, 'text-muted-foreground')}>（空）</div>
  if (matches.length === 0)
    return (
      <pre className={cn(className, 'whitespace-pre-wrap')}>
        <span className="text-muted-foreground">{text}</span>
      </pre>
    )
  const parts: React.ReactNode[] = []
  let cursor = 0
  matches.forEach((m, i) => {
    if (m.index > cursor) {
      parts.push(
        <span key={`t-${i}`} className="text-muted-foreground">
          {text.slice(cursor, m.index)}
        </span>,
      )
    }
    parts.push(
      <mark
        key={`m-${i}`}
        className="rounded bg-amber-500/40 px-0.5 text-amber-950 dark:text-amber-100"
        title={`#${i + 1} · ${m.length} 字符`}
      >
        {m.full || '∅'}
      </mark>,
    )
    cursor = m.index + m.length
  })
  if (cursor < text.length) {
    parts.push(
      <span key="tail" className="text-muted-foreground">
        {text.slice(cursor)}
      </span>,
    )
  }
  return <pre className={cn(className, 'whitespace-pre-wrap')}>{parts}</pre>
}

function TabButton({
  label,
  active,
  onClick,
}: {
  label: React.ReactNode
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'inline-flex h-8 items-center gap-1 rounded-md px-3 text-xs font-medium transition-colors',
        active
          ? 'bg-info/15 text-info'
          : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground',
      )}
    >
      {label}
    </button>
  )
}

function MatchesPanel({
  matches,
  onCopy,
}: {
  matches: MatchInfo[]
  onCopy: (s: string) => void
}) {
  if (matches.length === 0) {
    return (
      <div className="flex h-40 items-center justify-center text-sm text-muted-foreground">
        暂无匹配
      </div>
    )
  }
  return (
    <ul className="divide-y divide-border text-xs">
      {matches.map((m, i) => (
        <li key={i} className="p-2">
          <div className="flex items-start gap-2">
            <span className="shrink-0 rounded bg-info/15 px-1.5 py-0.5 font-mono text-[10px] text-info">
              #{i + 1}
            </span>
            <code className="min-w-0 flex-1 break-all rounded bg-secondary px-1.5 py-0.5 font-mono">
              {m.full}
            </code>
            <span className="shrink-0 text-[10px] text-muted-foreground">
              [{m.index}..{m.index + m.length})
            </span>
            <button
              onClick={() => onCopy(m.full)}
              className="shrink-0 rounded p-1 text-muted-foreground hover:bg-accent"
              title="复制"
            >
              <Copy className="h-3 w-3" />
            </button>
          </div>
          {m.groups.length > 0 && (
            <ul className="mt-1 ml-8 space-y-0.5">
              {m.groups.map((g, gi) => (
                <li key={gi} className="flex items-start gap-2">
                  <span className="shrink-0 rounded bg-secondary px-1 py-0.5 font-mono text-[10px] text-muted-foreground">
                    {g.name ? `<${g.name}>` : `$${gi + 1}`}
                  </span>
                  <code className="min-w-0 flex-1 break-all font-mono">
                    {g.value ?? <span className="italic text-muted-foreground">undefined</span>}
                  </code>
                </li>
              ))}
            </ul>
          )}
        </li>
      ))}
    </ul>
  )
}

function ReplacePanel({
  replacement,
  onChange,
  result,
  onCopy,
}: {
  replacement: string
  onChange: (v: string) => void
  result: string
  onCopy: (s: string) => void
}) {
  return (
    <div className="flex h-full flex-col gap-2 p-2">
      <div>
        <div className="mb-1 flex items-center justify-between text-[11px] text-muted-foreground">
          <span>替换模板</span>
          <span className="font-mono text-[10px]">
            支持 $&, $1-$9, $&lt;name&gt;
          </span>
        </div>
        <input
          value={replacement}
          onChange={(e) => onChange(e.target.value)}
          placeholder="例如 [$1]"
          spellCheck={false}
          className="w-full rounded-md border border-border bg-background px-2 py-1.5 font-mono text-xs outline-none focus:border-primary/50"
        />
      </div>
      <div className="flex items-center justify-between text-[11px] text-muted-foreground">
        <span>替换后结果</span>
        <Button variant="ghost" size="sm" onClick={() => onCopy(result)} disabled={!result}>
          <Copy className="h-3 w-3" />
          复制结果
        </Button>
      </div>
      <pre className="flex-1 overflow-auto whitespace-pre-wrap rounded-md border border-border bg-secondary/30 p-2 font-mono text-xs">
        {result || <span className="text-muted-foreground">（空）</span>}
      </pre>
    </div>
  )
}

function SnippetsPanel({
  userSnippets,
  name,
  onName,
  onSave,
  onApply,
  onDelete,
  canSave,
}: {
  userSnippets: Snippet[]
  name: string
  onName: (v: string) => void
  onSave: () => void
  onApply: (s: Snippet) => void
  onDelete: (id: string) => void
  canSave: boolean
}) {
  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-2 border-b border-border p-2">
        <input
          value={name}
          onChange={(e) => onName(e.target.value)}
          placeholder="片段名称（留空则取模式前 24 字符）"
          className="flex-1 rounded-md border border-border bg-background px-2 py-1 text-xs outline-none focus:border-primary/50"
        />
        <Button size="sm" onClick={onSave} disabled={!canSave}>
          <Plus className="h-3 w-3" />
          保存当前
        </Button>
      </div>
      <div className="flex-1 overflow-auto">
        {userSnippets.length > 0 && (
          <>
            <SectionTitle>我的片段</SectionTitle>
            <ul className="divide-y divide-border">
              {userSnippets.map((s) => (
                <SnippetRow key={s.id} s={s} onApply={onApply} onDelete={onDelete} />
              ))}
            </ul>
          </>
        )}
        <SectionTitle>
          <Star className="h-3 w-3" />
          内置预设
        </SectionTitle>
        <ul className="divide-y divide-border">
          {BUILTIN_PRESETS.map((s) => (
            <SnippetRow key={s.id} s={s} onApply={onApply} />
          ))}
        </ul>
      </div>
    </div>
  )
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-1.5 bg-secondary/40 px-2 py-1 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
      {children}
    </div>
  )
}

function SnippetRow({
  s,
  onApply,
  onDelete,
}: {
  s: Snippet
  onApply: (s: Snippet) => void
  onDelete?: (id: string) => void
}) {
  return (
    <li className="flex items-center gap-2 p-2 text-xs">
      <button
        onClick={() => onApply(s)}
        className="flex min-w-0 flex-1 flex-col text-left hover:opacity-80"
      >
        <span className="truncate font-medium">{s.name}</span>
        <code className="truncate font-mono text-[10px] text-muted-foreground">
          /{s.pattern}/{s.flags}
        </code>
      </button>
      {onDelete && (
        <button
          onClick={() => onDelete(s.id)}
          className="shrink-0 rounded p-1 text-muted-foreground hover:bg-accent hover:text-red-500"
          title="删除"
        >
          <Trash2 className="h-3 w-3" />
        </button>
      )}
    </li>
  )
}
