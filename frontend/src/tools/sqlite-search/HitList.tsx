import { useState } from 'react'
import { Check, ChevronRight, Copy, Database, FolderOpen, Table2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { OpenInExplorer } from '../../../wailsjs/go/main/App'
import { cellText, groupByFile, highlight, hitIndex, type Hit } from './types'

interface Props {
  hits: Hit[]
  /** 命中里的 file 是相对路径,拼绝对路径要靠它 */
  base: string
  onOpenTable: (file: string, table: string) => void
}

export function HitList({ hits, base, onOpenTable }: Props) {
  const groups = groupByFile(hits)
  return (
    <div className="space-y-3">
      {groups.map((g) => (
        <FileGroup key={g.file} file={g.file} base={base} hits={g.hits} onOpenTable={onOpenTable} />
      ))}
    </div>
  )
}

/** 相对命中路径拼成本机绝对路径;分隔符跟着 base 走,Windows 上别混出正反斜杠 */
export function absPath(base: string, file: string): string {
  const sep = base.includes('\\') ? '\\' : '/'
  return base.replace(/[\\/]$/, '') + sep + file.split('/').join(sep)
}

function dirOf(p: string): string {
  const i = Math.max(p.lastIndexOf('\\'), p.lastIndexOf('/'))
  return i > 0 ? p.slice(0, i) : p
}

function FileGroup({
  file,
  base,
  hits,
  onOpenTable,
}: {
  file: string
  base: string
  hits: Hit[]
  onOpenTable: (file: string, table: string) => void
}) {
  const [open, setOpen] = useState(true)
  const [copied, setCopied] = useState(false)
  const full = absPath(base, file)
  const copyPath = async () => {
    await navigator.clipboard.writeText(full)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }
  return (
    <div className="overflow-hidden rounded-lg border border-border">
      {/* 折叠开关和两个动作是并排的兄弟,不是套在一个 button 里 —— button 里不能再放 button */}
      <div className="flex items-center gap-2 bg-muted/40 px-3 py-2 text-xs">
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="flex min-w-0 flex-1 items-center gap-2 text-left transition-colors hover:text-foreground"
        >
          <ChevronRight
            className={cn('h-3.5 w-3.5 shrink-0 transition-transform', open && 'rotate-90')}
          />
          <Database className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          {/* 路径从右往左看更有用:末尾的库名才是人要认的那部分 */}
          <span className="truncate font-mono" dir="rtl" title={full}>
            {file}
          </span>
        </button>
        <span className="shrink-0 text-muted-foreground">{hits.length} 行</span>
        <button
          type="button"
          title={copied ? '已复制' : '复制这个库的完整路径'}
          onClick={() => void copyPath()}
          className="shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
        >
          {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
        </button>
        <button
          type="button"
          title="在文件管理器里打开它所在的文件夹"
          onClick={() => void OpenInExplorer(dirOf(full))}
          className="shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
        >
          <FolderOpen className="h-3.5 w-3.5" />
        </button>
      </div>
      {open && (
        <div className="divide-y divide-border">
          {hits.map((h, i) => (
            <HitRow key={i} hit={h} onOpenTable={onOpenTable} />
          ))}
        </div>
      )}
    </div>
  )
}

function HitRow({
  hit,
  onOpenTable,
}: {
  hit: Hit
  onOpenTable: (file: string, table: string) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const idx = hitIndex(hit)
  const matched = idx >= 0 ? hit.row[idx] : undefined

  return (
    <div className="px-3 py-2 text-xs">
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={() => onOpenTable(hit.file, hit.table)}
          className="flex items-center gap-1 rounded px-1 py-0.5 font-mono text-[11px] text-foreground transition-colors hover:bg-secondary"
          title="打开这张表"
        >
          <Table2 className="h-3 w-3" />
          {hit.table}
        </button>
        {hit.column && (
          <span className="font-mono text-[11px] text-muted-foreground">· {hit.column}</span>
        )}
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="ml-auto text-[11px] text-muted-foreground transition-colors hover:text-foreground"
        >
          {expanded ? '收起整行' : `展开整行（${hit.columns.length} 列）`}
        </button>
      </div>

      {/* 默认只展示命中的那一格 —— 一行几十列全铺出来根本看不清哪里命中了 */}
      {!expanded && matched && (
        <div className="mt-1 break-all rounded bg-muted/40 px-2 py-1 font-mono leading-relaxed">
          {highlight(cellText(matched), hit.keyword).map((p, i) => (
            <span key={i} className={p.hit ? 'rounded bg-amber-400/40 font-medium' : undefined}>
              {p.s}
            </span>
          ))}
        </div>
      )}

      {expanded && (
        <div className="mt-1 overflow-x-auto">
          <table className="w-full border-collapse text-[11px]">
            <tbody>
              {hit.columns.map((col, i) => {
                const c = hit.row[i]
                return (
                  <tr key={col} className={cn('align-top', i === idx && 'bg-amber-400/10')}>
                    <td className="whitespace-nowrap py-0.5 pr-3 font-mono text-muted-foreground">
                      {col}
                    </td>
                    <td className="break-all py-0.5 font-mono">
                      {c?.null ? (
                        <span className="italic text-muted-foreground">NULL</span>
                      ) : (
                        <>
                          {highlight(cellText(c), i === idx ? hit.keyword : '').map((p, j) => (
                            <span
                              key={j}
                              className={p.hit ? 'rounded bg-amber-400/40 font-medium' : undefined}
                            >
                              {p.s}
                            </span>
                          ))}
                          {c?.blob && (
                            <span className="ml-1 text-[10px] text-muted-foreground">
                              (BLOB {c.size} 字节)
                            </span>
                          )}
                        </>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
