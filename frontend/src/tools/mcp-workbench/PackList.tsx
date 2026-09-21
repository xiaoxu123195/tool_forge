import { FileCode2, Pencil, Plus, Trash2 } from 'lucide-react'
import { useConfirm } from '@/components/ui/confirm'
import type { apitool } from '../../../wailsjs/go/models'
import { slug } from './ImportDialog'

/**
 * 已导入的接口包。
 *
 * 放在 MCP 工作台里而不是单开一页：导进来的东西最终是通过 MCP 被调用的，
 * 而这一页本来就是「看 MCP 有什么、试一下」的地方。
 */
export function PackList({
  packs,
  onImport,
  onEdit,
  onDelete,
}: {
  packs: apitool.Pack[]
  onImport: () => void
  onEdit: (p: apitool.Pack) => void
  onDelete: (p: apitool.Pack) => void
}) {
  const confirm = useConfirm()

  const remove = async (p: apitool.Pack) => {
    const ok = await confirm({
      title: `删掉接口包「${p.name}」？`,
      message: (
        <div className="space-y-1">
          <p>
            它生成的 {p.ops.length} 个工具会一起撤销，已经配置的密钥也会从系统凭据库里删掉。
          </p>
          <p className="text-muted-foreground">
            正在用这些工具的 Claude Code / Codex 会在下一次拉工具列表时看到它们消失。
          </p>
        </div>
      ),
      confirmLabel: '删掉',
      danger: true,
    })
    if (ok) onDelete(p)
  }

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center gap-2 px-1 text-[11px] text-muted-foreground">
        <span className="font-medium">接口包</span>
        <span className="opacity-70">OpenAPI → 工具</span>
        <button
          type="button"
          title="从 OpenAPI 文档导入接口"
          onClick={onImport}
          className="ml-auto flex items-center gap-0.5 rounded px-1 py-0.5 transition-colors hover:bg-secondary hover:text-foreground"
        >
          <Plus className="h-3 w-3" />
          导入
        </button>
      </div>

      {packs.length === 0 ? (
        <p className="px-1 text-[11px] text-muted-foreground/80">
          还没有导入过。有 OpenAPI 文档的服务，勾几个接口就能变成 AI 可调用的工具。
        </p>
      ) : (
        <ul className="space-y-1">
          {packs.map((p) => (
            <li key={p.id} className="group/pack rounded-md border border-border bg-card px-2 py-1.5">
              <div className="flex items-center gap-1.5">
                <FileCode2 className="h-3 w-3 shrink-0 text-muted-foreground" />
                <span className="truncate text-xs font-medium">{p.name}</span>
                <span className="ml-auto shrink-0 rounded-sm bg-muted px-1 text-[10px] text-muted-foreground">
                  {p.ops.length} 个
                </span>
                <button
                  type="button"
                  title="修改这个接口包"
                  onClick={() => onEdit(p)}
                  className="shrink-0 rounded p-0.5 text-muted-foreground opacity-0 transition-opacity hover:bg-secondary hover:text-foreground focus-visible:opacity-100 group-hover/pack:opacity-100"
                >
                  <Pencil className="h-3 w-3" />
                </button>
                <button
                  type="button"
                  title={`删掉接口包「${p.name}」`}
                  onClick={() => void remove(p)}
                  className="shrink-0 rounded p-0.5 text-muted-foreground opacity-0 transition-opacity hover:bg-destructive/10 hover:text-destructive focus-visible:opacity-100 group-hover/pack:opacity-100"
                >
                  <Trash2 className="h-3 w-3" />
                </button>
              </div>
              <div className="mt-0.5 truncate font-mono text-[10px] text-muted-foreground" title={p.baseUrl}>
                {p.baseUrl}
              </div>
              <div className="mt-0.5 flex items-center gap-1.5 text-[10px] text-muted-foreground">
                <span className="font-mono opacity-70">api-{slug(p.name)}-*</span>
                {p.auth.kind !== '' && (
                  <span
                    className={
                      p.auth.hasSecret
                        ? 'rounded-sm bg-emerald-500/15 px-1 text-emerald-700 dark:text-emerald-400'
                        : 'rounded-sm bg-amber-500/15 px-1 text-amber-700 dark:text-amber-400'
                    }
                    title={p.auth.hasSecret ? '密钥已存进系统凭据库' : '配了认证方式但还没填密钥，请求会不带认证发出去'}
                  >
                    {p.auth.hasSecret ? '已配密钥' : '缺密钥'}
                  </span>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
