import { useState } from 'react'
import { AlertCircle, Download, FileQuestion } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import type { devicefs } from '../../../wailsjs/go/models'
import { defaultTypeOf, displayOf } from '../mmkv/valueTypes'

/**
 * 预览面板。按后端给的 kind 决定画什么。
 *
 * 解析全在 Go 里 —— plist 和 MMKV 用的就是工具页那两个解析器,
 * 三个入口(工具页 / MCP / 这里)是同一份代码。
 */
interface Props {
  preview: devicefs.Preview | null
  loading: boolean
  error: string
  onExport: () => void
  exporting: boolean
  /** 导出成功后的落地路径 */
  exportedTo: string
}

export function PreviewPane({
  preview,
  loading,
  error,
  onExport,
  exporting,
  exportedTo,
}: Props) {
  if (loading) {
    return <Centered>正在从设备上取…</Centered>
  }
  if (error) {
    return (
      <div className="m-3 flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
        <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
        <span className="whitespace-pre-wrap break-words">{error}</span>
      </div>
    )
  }
  if (!preview) {
    return (
      <Centered>
        <FileQuestion className="mb-2 h-8 w-8 opacity-50" />
        选中一个文件看内容
      </Centered>
    )
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-start justify-between gap-3 border-b border-border px-3 py-2">
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium" title={preview.path}>
            {preview.name}
          </div>
          <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-muted-foreground">
            <KindBadge kind={preview.kind} />
            <span>{fmtSize(preview.size)}</span>
            {/* why 是"凭什么判成这个类型"。判错时得能一眼看出错在哪一步,
                而不是对着一个空面板猜 */}
            <span className="truncate" title={preview.why}>
              · {preview.why}
            </span>
          </div>
        </div>
        <Button variant="outline" size="sm" onClick={onExport} disabled={exporting}>
          <Download className="h-3.5 w-3.5" />
          {exporting ? '导出中…' : '导出'}
        </Button>
      </div>

      {/* 用行内提示而不是 window.alert:这是个桌面 app,弹一个浏览器原生对话框
          既不像自己的界面,还会把整个窗口卡住 */}
      {exportedTo && (
        <div className="border-b border-emerald-500/30 bg-emerald-500/10 px-3 py-1.5 text-[11px] text-emerald-700 dark:text-emerald-400">
          已导出到 {exportedTo}
        </div>
      )}
      {preview.truncated && (
        <Banner>只从设备上取了开头一段。要完整内容请用「导出」</Banner>
      )}
      {preview.note && <Banner>{preview.note}</Banner>}

      <div className="min-h-0 flex-1 overflow-auto p-3">
        <Body preview={preview} />
      </div>
    </div>
  )
}

function Body({ preview }: { preview: devicefs.Preview }) {
  if (preview.plist) {
    return <Mono>{safeJson(preview.plist.parsed)}</Mono>
  }
  if (preview.mmkv) {
    return <MmkvTable res={preview.mmkv} />
  }
  if (preview.imageB64) {
    return (
      <img
        src={preview.imageB64}
        alt={preview.name}
        className="max-w-full rounded-md border border-border"
      />
    )
  }
  if (preview.text) {
    return <Mono>{preview.text}</Mono>
  }
  if (preview.hex) {
    return (
      <div className="space-y-2">
        {preview.info?.magicHex && (
          <div className="text-[11px] text-muted-foreground">
            魔数 {preview.info.magicHex}
            {preview.info.mimeType && ` · ${preview.info.mimeType}`}
          </div>
        )}
        <div className="break-all rounded-md border border-border bg-muted/30 p-3 font-mono text-[11px] leading-relaxed text-muted-foreground">
          {preview.hex}
        </div>
      </div>
    )
  }
  return <Centered>没有可显示的内容</Centered>
}

/** MMKV 的键值表。和 MMKV 工具页共用类型判断,只是这里不做点击循环 */
function MmkvTable({ res }: { res: NonNullable<devicefs.Preview['mmkv']> }) {
  const [q, setQ] = useState('')
  const rows = res.entries.filter(
    (e) => !q.trim() || e.key.toLowerCase().includes(q.trim().toLowerCase())
  )
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2 text-[11px] text-muted-foreground">
        <span>
          {res.entries.length} 个 key
          {res.removedCount > 0 && ` · ${res.removedCount} 个删除标记`}
        </span>
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="过滤 key"
          spellCheck={false}
          className="h-6 w-40 rounded-md border border-input bg-background px-2 text-[11px] outline-none focus:ring-1 focus:ring-ring"
        />
      </div>
      <table className="w-full table-fixed border-separate border-spacing-0 text-[12px]">
        <colgroup>
          <col style={{ width: '38%' }} />
          <col />
        </colgroup>
        <tbody>
          {rows.map((e) => {
            const v = e.values[0]
            const t = defaultTypeOf(v)
            return (
              <tr key={e.key} className="align-top">
                <td className="border-b border-border px-2 py-1 font-mono">
                  <div className="truncate" title={e.key}>
                    {e.key}
                  </div>
                  {e.values.length > 1 && (
                    <div className="text-[10px] text-muted-foreground">
                      {e.values.length} 个历史值
                    </div>
                  )}
                </td>
                <td className="min-w-0 border-b border-border px-2 py-1 font-mono">
                  <span className="mr-1.5 text-[10px] text-orange-600 dark:text-orange-400">
                    ({t})
                  </span>
                  <span className="break-all">{displayOf(v, t)}</span>
                </td>
              </tr>
            )
          })}
          {rows.length === 0 && (
            <tr>
              <td colSpan={2} className="px-2 py-6 text-center text-muted-foreground">
                没有匹配的 key
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}

const KIND_LABELS: Record<string, string> = {
  plist: 'plist',
  mmkv: 'MMKV',
  sqlite: 'SQLite',
  image: '图片',
  text: '文本',
  binary: '二进制',
}

const KIND_BG: Record<string, string> = {
  plist: 'bg-sky-200/60 dark:bg-sky-900/40',
  mmkv: 'bg-emerald-200/60 dark:bg-emerald-900/40',
  sqlite: 'bg-amber-200/60 dark:bg-amber-900/40',
  image: 'bg-violet-200/60 dark:bg-violet-900/40',
  text: 'bg-slate-200/60 dark:bg-slate-800/60',
  binary: 'bg-muted',
}

function KindBadge({ kind }: { kind: string }) {
  return (
    <span
      className={cn(
        'rounded-sm px-1.5 py-0.5 font-medium text-foreground/80',
        KIND_BG[kind] ?? KIND_BG.binary
      )}
    >
      {KIND_LABELS[kind] ?? kind}
    </span>
  )
}

function Banner({ children }: { children: React.ReactNode }) {
  return (
    <div className="border-b border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-[11px] text-amber-700 dark:text-amber-400">
      {children}
    </div>
  )
}

function Mono({ children }: { children: React.ReactNode }) {
  return (
    <pre className="whitespace-pre-wrap break-all font-mono text-[12px] leading-relaxed">
      {children}
    </pre>
  )
}

function Centered({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-full flex-col items-center justify-center text-xs text-muted-foreground">
      {children}
    </div>
  )
}

function safeJson(v: unknown): string {
  try {
    return JSON.stringify(v, null, 2)
  } catch (e) {
    return `/* 序列化失败: ${e instanceof Error ? e.message : String(e)} */`
  }
}

export function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}
