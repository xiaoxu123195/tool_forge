import { useEffect, useState } from 'react'
import { Smartphone, Usb } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { GetPassword, PickLocalFile, SavePassword } from '../../../wailsjs/go/main/App'
import { sshPasswordKey } from '@/stores/forensic'

/**
 * 连接面板。
 *
 * 两个平台要的东西完全不一样:
 *   iOS      SSH 用户名 + 密码(越狱设备上的 OpenSSH)
 *   Android  什么都不用填,adb 认得出设备就行;root 是连上之后探出来的
 *
 * iOS 的密码走系统凭据库,和「移动取证」用同一个 key —— 一边存过,
 * 另一边就不用再敲一遍。这两个工具连的本来就是同一台设备。
 */
interface Props {
  platform: string
  user: string
  deviceId: string
  adbPath: string
  busy: boolean
  error: string
  onPlatformChange: (p: string) => void
  onUserChange: (u: string) => void
  onDeviceIdChange: (d: string) => void
  onAdbPathChange: (p: string) => void
  onConnect: (password: string) => void
}

/** 和取证工具默认地址保持一致,这样两边共用同一条凭据 */
const KEY_ADDR = 'root@127.0.0.1:22'

export function ConnectPanel({
  platform,
  user,
  deviceId,
  adbPath,
  busy,
  error,
  onPlatformChange,
  onUserChange,
  onDeviceIdChange,
  onAdbPathChange,
  onConnect,
}: Props) {
  const [password, setPassword] = useState('')
  const [remember, setRemember] = useState(false)
  const isIOS = platform !== 'android'

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

  const canSubmit = !busy && (!isIOS || !!password)

  const submit = async () => {
    if (isIOS && remember && password) {
      await SavePassword(sshPasswordKey(KEY_ADDR), password).catch(() => {})
    }
    onConnect(isIOS ? password : '')
  }

  const pickAdb = async () => {
    const p = await PickLocalFile('选择 adb 可执行文件')
    if (p) onAdbPathChange(p)
  }

  return (
    <div className="flex flex-1 items-center justify-center overflow-auto">
      <div className="w-full max-w-md space-y-4 py-4">
        <div className="flex flex-col items-center gap-2 text-center">
          <Smartphone className="h-10 w-10 text-muted-foreground" />
          <div className="text-sm font-medium">连接一台设备</div>
          <div className="text-xs text-muted-foreground">
            经 USB 直接翻设备的文件系统，不用先导出
          </div>
        </div>

        <div className="flex gap-1 rounded-md bg-muted p-1">
          {[
            { id: 'ios', label: 'iOS' },
            { id: 'android', label: 'Android' },
          ].map((p) => (
            <button
              key={p.id}
              onClick={() => onPlatformChange(p.id)}
              className={cn(
                'flex-1 rounded-sm px-3 py-1.5 text-xs font-medium transition-colors',
                platform === p.id
                  ? 'bg-background text-foreground shadow-sm'
                  : 'text-muted-foreground hover:text-foreground'
              )}
            >
              {p.label}
            </button>
          ))}
        </div>

        <div className="space-y-3 rounded-lg border border-border bg-card p-4">
          {isIOS ? (
            <>
              <Field label="SSH 用户名">
                <Input value={user} onChange={onUserChange} />
              </Field>
              <Field label="SSH 密码">
                <input
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="越狱设备默认 alpine，改过就填改后的"
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && canSubmit) submit()
                  }}
                  className={inputCls}
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
            </>
          ) : (
            // 留空并不是"走 PATH":自带的那份优先,没有才回退 PATH。
            // 这个区别是要紧的 —— Windows 系统目录里常年躺着一个 2012 年的
            // adb.exe,真走了 PATH 就是设备列表一直空着
            <Field label="adb 路径（留空 = 优先用自带的，没有再走 PATH）">
              <div className="flex items-center gap-2">
                <input
                  value={adbPath}
                  onChange={(e) => onAdbPathChange(e.target.value)}
                  placeholder="adb"
                  spellCheck={false}
                  className={inputCls}
                />
                <Button variant="outline" size="sm" onClick={pickAdb}>
                  选择
                </Button>
              </div>
            </Field>
          )}

          <Field
            label={
              isIOS ? '设备 UDID（留空 = 第一台）' : '设备序列号（留空 = 第一台）'
            }
          >
            <Input
              value={deviceId}
              onChange={onDeviceIdChange}
              placeholder="多台设备同时插着时才需要填"
            />
          </Field>

          {error && (
            <div className="whitespace-pre-wrap break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
              {error}
            </div>
          )}

          <Button onClick={submit} disabled={!canSubmit} className="w-full" size="sm">
            <Usb className="h-3.5 w-3.5" />
            {busy ? '连接中…' : '连接'}
          </Button>
        </div>

        <div className="space-y-1 text-[11px] text-muted-foreground">
          <div className="font-medium text-foreground/80">前提</div>
          {isIOS ? (
            <>
              <div>· 设备已越狱并装了 OpenSSH（这条路本质上就是 SSH + SFTP）</div>
              <div>· 电脑上装了 go-forensic，且设备已信任这台电脑</div>
              <div>· 转发端口由系统分配，不会和导出功能抢 2222</div>
            </>
          ) : (
            <>
              <div>· 手机开了 USB 调试，并在弹框里允许了这台电脑</div>
              <div>· 要看 /data 下面的应用数据需要 root；没 root 只能看 /sdcard</div>
              <div>
                · adb 版本太老会认不出现代设备。如果连不上，先确认用的是新版
                （<span className="font-mono">adb version</span> 应该是 1.0.41）
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}

const inputCls =
  'h-8 w-full rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring'

function Input({
  value,
  onChange,
  placeholder,
}: {
  value: string
  onChange: (v: string) => void
  placeholder?: string
}) {
  return (
    <input
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      spellCheck={false}
      className={inputCls}
    />
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
