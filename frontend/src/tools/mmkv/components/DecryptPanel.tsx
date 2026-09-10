import { useState } from 'react'
import { FolderOpen, Lock, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { PickLocalFile } from '../../../../wailsjs/go/main/App'

// 解密在 Go 里做(backend/tools/mmkv/decrypt.go),这里只负责收集三样东西:
// 加密文件路径、.crc 路径、AES key。
//
// 选文件走原生对话框而不是 <input type="file">:后端要的是路径,
// 浏览器给的 File 对象里没有路径,只能把整个文件读进 JS 再传过去。

interface Props {
  onSubmit: (mmkvPath: string, crcPath: string, keyHex: string) => Promise<void>
  onCancel: () => void
  /** 外部注入的错误（解密/解析失败） */
  externalError?: string
  /** 若已加载一个文件，允许直接用它（省得再选一遍） */
  initialMmkvPath?: string
}

export function DecryptPanel({
  onSubmit,
  onCancel,
  externalError,
  initialMmkvPath,
}: Props) {
  const [mmkvPath, setMmkvPath] = useState(initialMmkvPath ?? '')
  const [crcPath, setCrcPath] = useState('')
  const [keyHex, setKeyHex] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  const ready = !!mmkvPath && !!crcPath && keyHex.trim().length > 0 && !busy

  const submit = async () => {
    if (!mmkvPath || !crcPath) return
    setBusy(true)
    setErr('')
    try {
      await onSubmit(mmkvPath, crcPath, keyHex.trim())
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const combinedError = err || externalError || ''

  return (
    <div className="rounded-lg border border-primary/40 bg-primary/5 p-4">
      <div className="mb-3 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Lock className="h-4 w-4 text-primary" />
          <span className="text-sm font-medium">加密 MMKV（AES-128-CFB）</span>
        </div>
        <button
          onClick={onCancel}
          className="rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
        >
          <X className="h-4 w-4" />
        </button>
      </div>

      <div className="space-y-3">
        <FilePickerRow
          label="加密 MMKV 文件"
          path={mmkvPath}
          title="选择加密的 MMKV 文件"
          onChange={setMmkvPath}
        />
        <FilePickerRow
          label=".crc 文件"
          path={crcPath}
          title="选择配对的 .crc 文件"
          onChange={setCrcPath}
          hint="IV 取自 .crc 文件的第 12~27 字节"
        />
        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">
            AES key（十六进制）
          </label>
          <input
            value={keyHex}
            onChange={(e) => setKeyHex(e.target.value)}
            placeholder="例如 1A2B3C4D5E6F...（不足 32 位自动补零，多余截断）"
            spellCheck={false}
            className="h-8 w-full rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
          />
        </div>

        {combinedError && (
          <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
            {combinedError}
          </div>
        )}

        <div className="flex justify-end gap-2">
          <Button size="sm" variant="ghost" onClick={onCancel}>
            取消
          </Button>
          <Button size="sm" onClick={submit} disabled={!ready}>
            {busy ? '解密中…' : '解密并打开'}
          </Button>
        </div>
      </div>
    </div>
  )
}

function FilePickerRow({
  label,
  path,
  title,
  onChange,
  hint,
}: {
  label: string
  path: string
  title: string
  onChange: (p: string) => void
  hint?: string
}) {
  const pick = async () => {
    const p = await PickLocalFile(title)
    if (p) onChange(p)
  }
  const name = path ? path.split(/[\\/]/).pop() : ''
  return (
    <div className="space-y-1">
      <label className="text-xs font-medium text-muted-foreground">{label}</label>
      <div className="flex items-center gap-2">
        <Button variant="outline" size="sm" onClick={pick}>
          <FolderOpen className="h-3.5 w-3.5" />
          选择
        </Button>
        <span
          className={cn(
            'flex-1 truncate font-mono text-xs',
            path ? 'text-foreground' : 'text-muted-foreground'
          )}
          title={path}
        >
          {name || '未选择'}
        </span>
      </div>
      {hint && <div className="text-[10px] text-muted-foreground">{hint}</div>}
    </div>
  )
}
