import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { cn } from '@/lib/utils'
import type { Platform } from './types'

export interface PresetRow {
  label: string
  path: string
}

const PATH_PLACEHOLDER: Record<Platform, string> = {
  android: '/data/data/包名/',
  ios: '/private/var/mobile/Library/…',
}

/**
 * 给常用路径起标签的弹窗。
 *
 * 两种来路共用:「存为常用」把当前填的路径按行列出来,只等起名;「添加」则路径也要手打。
 * 标签留空的那行不存 —— 这就是"只想存其中几条"的办法,不再另摆一列复选框。
 */
export function PresetEditorDialog({
  open,
  platform,
  rows: initial,
  manual,
  onClose,
  onSave,
}: {
  open: boolean
  platform: Platform
  rows: PresetRow[]
  manual: boolean
  onClose: () => void
  onSave: (rows: PresetRow[]) => void
}) {
  const [rows, setRows] = useState<PresetRow[]>(initial)
  // 每次打开都从调用方给的行重新开始,上一次没保存的输入不该留到这次
  useEffect(() => {
    if (open) setRows(initial)
  }, [open, initial])

  const update = (i: number, patch: Partial<PresetRow>) =>
    setRows((rs) => rs.map((r, k) => (k === i ? { ...r, ...patch } : r)))
  const isPath = (p: string) => p.trim().startsWith('/')
  const valid = rows.filter((r) => r.label.trim() && isPath(r.path))

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={manual ? '添加常用路径' : '存为常用路径'}
      description={`存到 ${platform === 'ios' ? 'iOS' : 'Android'} 的「我的常用」里；标签留空的那行不存`}
      footer={
        <>
          <span className="text-[11px] text-muted-foreground">{valid.length} 条会被保存</span>
          <Button variant="outline" size="sm" className="ml-auto" onClick={onClose}>
            取消
          </Button>
          <Button size="sm" disabled={valid.length === 0} onClick={() => onSave(valid)}>
            保存
          </Button>
        </>
      }
    >
      <div className="space-y-2">
        {rows.map((r, i) => {
          const bad = r.path.trim() !== '' && !isPath(r.path)
          return (
            <div key={i} className="grid grid-cols-[minmax(0,150px)_1fr] gap-2">
              <input
                value={r.label}
                onChange={(e) => update(i, { label: e.target.value })}
                placeholder="标签，如 邮件"
                spellCheck={false}
                autoFocus={i === 0}
                className="h-8 w-full rounded-md border border-input bg-background px-2 text-xs outline-none focus:ring-1 focus:ring-ring"
              />
              <div>
                <input
                  value={r.path}
                  readOnly={!manual}
                  onChange={(e) => update(i, { path: e.target.value })}
                  placeholder={PATH_PLACEHOLDER[platform]}
                  spellCheck={false}
                  className={cn(
                    'h-8 w-full rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring',
                    !manual && 'text-muted-foreground',
                  )}
                />
                {bad && (
                  <p className="mt-0.5 text-[11px] text-destructive">
                    不像设备内绝对路径（要以 / 开头）
                  </p>
                )}
              </div>
            </div>
          )
        })}
        {manual && (
          <button
            type="button"
            onClick={() => setRows((rs) => [...rs, { label: '', path: '' }])}
            className="text-[11px] text-muted-foreground underline-offset-2 hover:underline"
          >
            再加一行
          </button>
        )}
        <p className="text-[11px] text-muted-foreground">
          {manual
            ? '路径填设备里的绝对路径。'
            : '路径来自上面的「指定路径」，已按行拆开；只想存其中几条，把不要的标签清空即可。'}
          同一条路径再存一次会覆盖原来的标签。
        </p>
      </div>
    </Dialog>
  )
}
