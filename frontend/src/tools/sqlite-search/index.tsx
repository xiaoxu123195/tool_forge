import { useState } from 'react'
import { AlertTriangle, FileSearch, FolderOpen, Search } from 'lucide-react'
import { ToolShell } from '@/components/tool/ToolShell'
import { Button } from '@/components/ui/button'
import { PickDirectory, PickLocalFile, SearchSQLite } from '../../../wailsjs/go/main/App'
import type { sqlitex } from '../../../wailsjs/go/models'
import { HitList } from './HitList'
import { TableViewer } from './TableViewer'
import { splitKeywords } from './types'
import { meta } from './meta'
import { useJump } from '@/lib/jump'

export default function SQLiteSearch() {
  const [root, setRoot] = useState('')
  const [keywords, setKeywords] = useState('')
  const [res, setRes] = useState<sqlitex.SearchResult | null>(null)
  const [running, setRunning] = useState(false)
  const [err, setErr] = useState('')
  // 打开的表:库路径 + 表名
  const [viewing, setViewing] = useState<{ path: string; table: string } | null>(null)

  const list = splitKeywords(keywords)
  const canRun = !running && root.trim().length > 0 && list.length > 0

  // 从移动取证跳过来:输出目录直接填进「搜哪儿」,关键词等人来敲
  useJump('sqlite-search', (j) => {
    setRoot(j.root)
    setRes(null)
    setErr('')
    setViewing(null)
  })

  const pickDir = async () => {
    const p = await PickDirectory('选择取证导出目录', root).catch(() => '')
    if (p) setRoot(p)
  }

  // 单个库也得能选。只给目录选择器的话,想搜一个 .db 就只能手动粘路径 ——
  // 而标签上明明写着可以给文件
  const pickFile = async () => {
    const p = await PickLocalFile('选择 SQLite 数据库文件').catch(() => '')
    if (p) setRoot(p)
  }

  const run = async () => {
    setRunning(true)
    setErr('')
    setRes(null)
    setViewing(null)
    try {
      setRes(await SearchSQLite({ root: root.trim(), keywords: list } as sqlitex.SearchOptions))
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setRunning(false)
    }
  }

  // 命中里给的是相对路径,打开表要绝对路径。
  // 基准用后端给的 base 而不是输入框里的 root —— 选的是单个文件时,
  // 相对路径是相对它所在目录算的,拿 root 去拼会多出一层
  const openTable = (file: string, table: string) => {
    const base = res?.base || root
    const sep = base.includes('\\') ? '\\' : '/'
    const full = base.replace(/[\\/]$/, '') + sep + file.split('/').join(sep)
    setViewing({ path: full, table })
  }

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      onClear={() => {
        setRes(null)
        setErr('')
        setViewing(null)
      }}
    >
      <div className="flex flex-col gap-4">
        <div className="rounded-lg border border-border bg-card p-4">
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            <Field
              label="搜哪儿"
              required
              hint="给目录就把里面所有数据库都搜一遍；也可以只给一个数据库文件"
            >
              <div className="flex gap-2">
                <input
                  value={root}
                  onChange={(e) => setRoot(e.target.value)}
                  placeholder="D:\取证\案件编号\设备导出"
                  spellCheck={false}
                  className="h-9 flex-1 rounded-md border border-input bg-background px-3 font-mono text-sm outline-none focus:ring-1 focus:ring-ring"
                />
                <Button variant="outline" size="sm" onClick={pickDir} type="button">
                  <FolderOpen className="h-3.5 w-3.5" />
                  选目录
                </Button>
                <Button variant="outline" size="sm" onClick={pickFile} type="button">
                  <FileSearch className="h-3.5 w-3.5" />
                  选文件
                </Button>
              </div>
            </Field>

            <Field label="关键词" required hint="一行一个或用逗号分隔，命中任意一个即可">
              <input
                value={keywords}
                onChange={(e) => setKeywords(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && canRun) void run()
                }}
                placeholder="13800138000 或 张三,收款"
                spellCheck={false}
                className="h-9 w-full rounded-md border border-input bg-background px-3 font-mono text-sm outline-none focus:ring-1 focus:ring-ring"
              />
            </Field>
          </div>

          <div className="mt-3 flex items-center gap-3">
            <p className="text-[11px] text-muted-foreground">
              按文件头识别数据库（不看扩展名，安卓和 iOS 上很多库没有扩展名）；读取不会改动原始文件
            </p>
            <Button className="ml-auto" onClick={run} disabled={!canRun}>
              <Search className="h-3.5 w-3.5" />
              {running ? '搜索中…' : '搜索'}
            </Button>
          </div>
        </div>

        {err && (
          <div className="flex items-start gap-2 rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-xs text-destructive">
            <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            <span className="break-all">{err}</span>
          </div>
        )}

        {res && <Summary res={res} />}

        {res && res.hits.length > 0 && !viewing && (
          <HitList hits={res.hits} base={res.base || root} onOpenTable={openTable} />
        )}

        {viewing && (
          <div className="h-[520px]">
            <TableViewer
              path={viewing.path}
              initialTable={viewing.table}
              onClose={() => setViewing(null)}
            />
          </div>
        )}
      </div>
    </ToolShell>
  )
}

function Summary({ res }: { res: sqlitex.SearchResult }) {
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 rounded-lg border border-border bg-muted/30 px-3 py-2 text-xs">
        <span>
          扫了 <strong>{res.files}</strong> 个数据库
        </span>
        <span>
          命中 <strong>{res.hits.length}</strong> 行
        </span>
        <span className="text-muted-foreground">{(res.elapsedMs / 1000).toFixed(1)} 秒</span>
        {res.truncated && (
          <span className="text-amber-600 dark:text-amber-400">
            已到条数上限，可能还有更多
          </span>
        )}
      </div>

      {/* 读不了的库必须摆出来。不说的话,"没搜到"和"根本没打开"长得一模一样,
          而后者是漏证据 */}
      {res.skipped.length > 0 && (
        <details className="rounded-lg border border-amber-500/40 bg-amber-500/5 px-3 py-2 text-xs">
          <summary className="cursor-pointer select-none">
            有 {res.skipped.length} 个数据库没读成 —— 这些没有参与搜索
          </summary>
          <div className="mt-2 space-y-1">
            {res.skipped.map((s, i) => (
              <div key={i} className="break-all font-mono text-[11px] text-muted-foreground">
                {s.file} — {s.reason}
              </div>
            ))}
          </div>
        </details>
      )}

      {res.hits.length === 0 && res.files > 0 && (
        <div className="rounded-lg border border-border p-6 text-center text-sm text-muted-foreground">
          这 {res.files} 个数据库里没有命中
        </div>
      )}
      {res.files === 0 && (
        <div className="rounded-lg border border-border p-6 text-center text-sm text-muted-foreground">
          这个目录里没找到 SQLite 数据库
        </div>
      )}
    </div>
  )
}

function Field({
  label,
  hint,
  required,
  children,
}: {
  label: string
  hint?: string
  required?: boolean
  children: React.ReactNode
}) {
  return (
    <div className="space-y-1.5">
      <label className="flex items-baseline gap-2 text-xs font-medium">
        {label}
        {required && <span className="text-destructive">*</span>}
        {hint && <span className="font-normal text-muted-foreground">· {hint}</span>}
      </label>
      {children}
    </div>
  )
}
