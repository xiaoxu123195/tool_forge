import { useEffect, useState } from 'react'
import { Smartphone, Usb } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { GetPassword, SavePassword } from '../../../wailsjs/go/main/App'
import { sshPasswordKey } from '@/stores/forensic'

/**
 * 连接面板。
 *
 * 密码走系统凭据库,和「移动取证」那个工具用同一个 key —— 一边存过,
 * 另一边就不用再敲一遍。这两个工具连的本来就是同一台设备。
 */
interface Props {
  user: string
  deviceId: string
  busy: boolean
  error: string
  onUserChange: (u: string) => void
  onDeviceIdChange: (d: string) => void
  onConnect: (password: string) => void
}

/** 和取证工具默认地址保持一致,这样两边共用同一条凭据 */
const KEY_ADDR = 'root@127.0.0.1:22'

export function ConnectPanel({
  user,
  deviceId,
  busy,
  error,
  onUserChange,
  onDeviceIdChange,
  onConnect,
}: Props) {
  const [password, setPassword] = useState('')
  const [remember, setRemember] = useState(false)

  useEffect(() => {
    GetPassword(sshPasswordKey(KEY_ADDR))
      .then((pwd) => {
        if (pwd) {
          setPassword(pwd)
          setRemember(true)
        }
      })
      .catch(() => {})
  }, [])

  const submit = async () => {
    if (remember && password) {
      await SavePassword(sshPasswordKey(KEY_ADDR), password).catch(() => {})
    }
    onConnect(password)
  }

  return (
    <div className="flex flex-1 items-center justify-center">
      <div className="w-full max-w-md space-y-4">
        <div className="flex flex-col items-center gap-2 text-center">
          <Smartphone className="h-10 w-10 text-muted-foreground" />
          <div className="text-sm font-medium">连接一台设备</div>
          <div className="text-xs text-muted-foreground">
            经 USB 把设备的 SSH 端口转到本机，然后直接翻它的文件系统
          </div>
        </div>

        <div className="space-y-3 rounded-lg border border-border bg-card p-4">
          <Field label="SSH 用户名">
            <input
              value={user}
              onChange={(e) => onUserChange(e.target.value)}
              spellCheck={false}
              className="h-8 w-full rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
            />
          </Field>
          <Field label="SSH 密码">
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="越狱设备默认 alpine，改过就填改后的"
              onKeyDown={(e) => {
                if (e.key === 'Enter' && password && !busy) submit()
              }}
              className="h-8 w-full rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
            />
          </Field>
          <Field label="设备 UDID（留空 = 检测到的第一台）">
            <input
              value={deviceId}
              onChange={(e) => onDeviceIdChange(e.target.value)}
              placeholder="多台设备同时插着时才需要填"
              spellCheck={false}
              className="h-8 w-full rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
            />
          </Field>

          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            <input
              type="checkbox"
              checked={remember}
              onChange={(e) => setRemember(e.target.checked)}
              className="h-3.5 w-3.5"
            />
            记住密码（存进系统凭据库，和「移动取证」共用）
          </label>

          {error && (
            <div className="whitespace-pre-wrap break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
              {error}
            </div>
          )}

          <Button onClick={submit} disabled={!password || busy} className="w-full" size="sm">
            <Usb className="h-3.5 w-3.5" />
            {busy ? '连接中…' : '连接'}
          </Button>
        </div>

        <div className="space-y-1 text-[11px] text-muted-foreground">
          <div className="font-medium text-foreground/80">前提</div>
          <div>· 设备已越狱并装了 OpenSSH（这条路本质上就是 SSH + SFTP）</div>
          <div>· 电脑上装了 go-forensic，且设备已信任这台电脑</div>
          <div>· USB 线插着；转发端口由系统分配，不会和导出功能抢 2222</div>
        </div>
      </div>
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1">
      <label className="text-xs font-medium text-muted-foreground">{label}</label>
      {children}
    </div>
  )
}
