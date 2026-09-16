import { Dialog } from '@/components/ui/dialog'
import { cn } from '@/lib/utils'
import type { aiconfig } from '../../../wailsjs/go/models'
import { OriginBadge } from './Source'

/**
 * 同名 MCP 在几处各配了一份时,并排比较。
 *
 * 「多处重复」的标签只说了"有好几份",看的人接着要问的是:它们一样吗?哪份是旧的?
 * 分开翻四个文件比不出来 —— 这里把每一份的传输、命令、参数、地址、环境变量键、启停
 * 摆成一张表,值不一样的那一行标出来。只比、不改:改别家的配置要到它自己的文件里去
 */
export function DuplicateDialog({
  name,
  entries,
  onClose,
  onOpen,
}: {
  name: string
  entries: aiconfig.MCPEntry[]
  onClose: () => void
  onOpen: (file: string) => void
}) {
  const rows = [
    { label: '传输', values: entries.map((e) => e.kind || '') },
    { label: '命令', values: entries.map((e) => e.command || '') },
    { label: '参数', values: entries.map((e) => (e.args ?? []).join(' ')) },
    { label: '地址', values: entries.map((e) => e.url || '') },
    { label: '环境变量键', values: entries.map((e) => (e.envKeys ?? []).join('、')) },
    { label: '状态', values: entries.map((e) => (e.enabled ? '已启用' : '已停用')) },
  ].filter((r) => r.values.some((v) => v !== ''))
  const differs = rows.filter((r) => new Set(r.values).size > 1)

  return (
    <Dialog
      open
      onClose={onClose}
      title={`「${name}」在 ${entries.length} 处的配置`}
      description={
        differs.length === 0
          ? '几份完全一致，留哪份都一样'
          : `有差异的字段：${differs.map((r) => r.label).join('、')}`
      }
      width="w-[820px]"
    >
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-[11px]">
          <thead>
            <tr>
              <th className="w-24 border-b border-border px-2 py-1.5 text-left font-medium text-muted-foreground">
                出处
              </th>
              {entries.map((e, i) => (
                <th key={i} className="border-b border-border px-2 py-1.5 text-left align-top font-normal">
                  <OriginBadge origin={e.source.origin} />
                  {e.source.scope && (
                    <span className="ml-1 text-muted-foreground">{e.source.scope}</span>
                  )}
                  <button
                    type="button"
                    onClick={() => onOpen(e.source.file)}
                    title={`打开 ${e.source.file}`}
                    className="mt-1 block max-w-[240px] truncate font-mono text-[10px] text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
                  >
                    {e.source.file}
                  </button>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => {
              const diff = new Set(r.values).size > 1
              return (
                <tr key={r.label} className={cn(diff && 'bg-amber-500/10')}>
                  <td className="border-b border-border/60 px-2 py-1.5 align-top text-muted-foreground">
                    {r.label}
                    {diff && (
                      <span className="ml-1 text-[9px] text-amber-700 dark:text-amber-400">不同</span>
                    )}
                  </td>
                  {r.values.map((v, i) => (
                    <td
                      key={i}
                      className={cn(
                        'break-all border-b border-border/60 px-2 py-1.5 align-top font-mono',
                        diff && 'font-medium',
                      )}
                    >
                      {v || <span className="opacity-40">—</span>}
                    </td>
                  ))}
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      <p className="text-[11px] text-muted-foreground">
        这里只比不改。要留一份删一份，点上面的文件名打开它改。
      </p>
    </Dialog>
  )
}
