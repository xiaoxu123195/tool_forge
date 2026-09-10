import { useEffect, useState } from 'react'
import { Check, ChevronDown, FolderOpen, ListPlus, Play, Plus, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { ModeToggle } from '@/components/tool/ModeToggle'
import { useConfirm } from '@/components/ui/confirm'
import {
  GetPassword,
  PickDirectory,
  SavePassword,
  DeletePassword,
} from '../../../wailsjs/go/main/App'
import {
  useForensicStore,
  sshPasswordKey,
} from '@/stores/forensic'
import {
  buildArgs,
  needsCLI,
  normalizePath,
  previewCommand,
  splitList,
  type Engine,
  type FormState,
  type Platform,
} from './types'
import { IOS_PATH_PRESETS } from './ios-presets'

/** 示例路径按平台给 —— Android 页原来显示的是 iOS 的路径,照着填一条都取不到 */
const PATH_PLACEHOLDER: Record<Platform, string> = {
  android: '/data/data/com.tencent.mm/\n/sdcard/Android/data/com.tencent.mm/',
  ios: '/private/var/mobile/Library/Mail/\n/private/var/mobile/Library/Accounts/',
}

interface Props {
  form: FormState
  onChange: (next: FormState) => void
  onRun: () => void
  disabled: boolean
  /** 非空表示现在跑不了,内容就是原因;为空表示没拦 */
  blockReason?: string
}

export function ForensicForm({ form, onChange, onRun, disabled, blockReason }: Props) {
  const confirm = useConfirm()
  const defaultSshAddr = useForensicStore((s) => s.defaultSshAddr)
  const defaultOutputBase = useForensicStore((s) => s.defaultOutputBase)
  const [passwordLoaded, setPasswordLoaded] = useState(false)

  // 进入时把 store 的默认值灌进来
  useEffect(() => {
    if (form.sshAddr === 'root@127.0.0.1:22' && defaultSshAddr) {
      onChange({ ...form, sshAddr: defaultSshAddr })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [defaultSshAddr])

  // 尝试从 keychain 加载密码
  useEffect(() => {
    if (form.platform !== 'ios' || passwordLoaded) return
    GetPassword(sshPasswordKey(form.sshAddr)).then((pwd) => {
      setPasswordLoaded(true)
      if (pwd) {
        onChange({ ...form, sshPassword: pwd, rememberPassword: true })
      }
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [form.platform, form.sshAddr])

  const setField = <K extends keyof FormState>(key: K, value: FormState[K]) => {
    onChange({ ...form, [key]: value })
  }

  const pickOutput = async () => {
    const picked = await PickDirectory('选择输出目录', defaultOutputBase || '').catch(
      () => ''
    )
    if (picked) setField('outputDir', picked)
  }

  // go-forensic 是无条件先清空输出目录的,不给关;内置引擎默认不清,由这个勾决定
  const cliAlwaysClears = needsCLI(form.platform, form.engine)
  const willClear = cliAlwaysClears || form.clearOutput

  const run = async () => {
    // 删东西之前问一句。这是用户手打的路径,少打一层就是别的目录,
    // 而且删完没有回收站可翻
    if (willClear) {
      const ok = await confirm({
        title: '导出前会清空输出目录',
        message: (
          <div className="space-y-1.5">
            <p>
              <span className="font-mono text-xs">{form.outputDir}</span>{' '}
              里现有的内容会被全部删掉，删了没法撤。
            </p>
            {cliAlwaysClears && (
              <p className="text-muted-foreground">
                go-forensic 总是先清空目录，这一步关不掉。
              </p>
            )}
          </div>
        ),
        confirmLabel: '清空并开始',
        danger: true,
      })
      if (!ok) return
    }
    if (form.platform === 'ios' && form.sshPassword) {
      const key = sshPasswordKey(form.sshAddr)
      if (form.rememberPassword) {
        await SavePassword(key, form.sshPassword).catch(() => {})
      } else {
        await DeletePassword(key).catch(() => {})
      }
    }
    onRun()
  }

  const keywordList = splitList(form.keywords)
  const pathList = splitList(form.specifyPaths).map(normalizePath)
  // 路径不以 / 开头基本就是打错了(比如把 Windows 上的输出目录填进来)
  const badPaths = pathList.filter((p) => !p.startsWith('/'))

  // 关键词和路径至少要有一个,不再强制关键词 —— iOS 系统自带应用(邮件、通讯录…)
  // 的数据不属于任何 App,没有包名可搜,只能按绝对路径取
  const canRun =
    !disabled &&
    !blockReason &&
    (keywordList.length > 0 || pathList.length > 0) &&
    form.outputDir.trim().length > 0 &&
    (form.platform === 'android' || form.sshAddr.trim().length > 0)

  // 预设按钮是开关:没加过就追加,已加过就把那一条摘掉。
  // 摘除时会把整段文本按"一行一条"重新排版 —— 想在纯文本里精确删掉某一条又不动别的,
  // 只能这么做;顺带把混着敲的逗号统一成换行,不影响最终发出去的参数。
  const togglePreset = (path: string) => {
    if (pathList.includes(path)) {
      setField('specifyPaths', pathList.filter((p) => p !== path).join('\n'))
      return
    }
    const cur = form.specifyPaths.trim()
    setField('specifyPaths', cur ? cur + '\n' + path : path)
  }

  const args = buildArgs(form)

  return (
    <div className="rounded-lg border border-border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-2">
        <span className="text-xs font-medium text-muted-foreground">任务参数</span>
        <div className="flex items-center gap-2">
          {form.platform === 'android' ? (
            <ModeToggle
              value={form.engine}
              onChange={(e) => setField('engine', e as Engine)}
              options={[
                { value: 'builtin', label: '内置' },
                { value: 'cli', label: 'go-forensic' },
              ]}
            />
          ) : (
            // iOS 还没有内置实现。这里不给选,免得选了个跑不了的组合
            <span
              className="rounded-md border border-dashed border-border px-2 py-1 text-[11px] text-muted-foreground"
              title="iOS 目前只能走 go-forensic，内置实现还没做"
            >
              go-forensic
            </span>
          )}
          <ModeToggle
            value={form.platform}
            onChange={(p) => setField('platform', p as Platform)}
            options={[
              { value: 'android', label: 'Android' },
              { value: 'ios', label: 'iOS' },
            ]}
          />
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 p-4 md:grid-cols-2">
        <Field
          label="关键词"
          hint={
            pathList.length > 0
              ? '已指定路径，关键词可以留空'
              : '应用包名或部分匹配，一行一个或用逗号分隔'
          }
        >
          <input
            value={form.keywords}
            onChange={(e) => setField('keywords', e.target.value)}
            placeholder="com.kik.chat 或 tencent,facebook"
            spellCheck={false}
            className="h-9 w-full rounded-md border border-input bg-background px-3 font-mono text-sm outline-none focus:ring-1 focus:ring-ring"
          />
        </Field>

        <Field label="输出目录" required>
          <div className="flex gap-2">
            <input
              value={form.outputDir}
              onChange={(e) => setField('outputDir', e.target.value)}
              placeholder="D:\exhibits\..."
              spellCheck={false}
              className="h-9 flex-1 rounded-md border border-input bg-background px-3 font-mono text-sm outline-none focus:ring-1 focus:ring-ring"
            />
            <Button variant="outline" size="sm" onClick={pickOutput} type="button">
              <FolderOpen className="h-3.5 w-3.5" />
              浏览
            </Button>
          </div>
          <label
            className={cn(
              'mt-1.5 flex w-fit items-center gap-1.5 text-xs',
              cliAlwaysClears ? 'text-muted-foreground' : 'cursor-pointer',
            )}
            title={
              cliAlwaysClears
                ? 'go-forensic 总是先清空输出目录，关不掉'
                : '勾上后，这次导出前会把该目录里现有的内容全部删掉'
            }
          >
            <input
              type="checkbox"
              checked={willClear}
              disabled={cliAlwaysClears}
              onChange={(e) => setField('clearOutput', e.target.checked)}
              className="h-3.5 w-3.5"
            />
            导出前清空该目录
            {cliAlwaysClears && <span className="opacity-70">· go-forensic 总是这样，关不掉</span>}
          </label>
        </Field>

        <Field
          label="指定路径（可选）"
          hint={
            pathList.length > 0 ? `设备内绝对路径 · 已填 ${pathList.length} 条` : '设备内绝对路径，一行一个'
          }
        >
          <textarea
            value={form.specifyPaths}
            onChange={(e) => setField('specifyPaths', e.target.value)}
            placeholder={PATH_PLACEHOLDER[form.platform]}
            spellCheck={false}
            rows={3}
            className="w-full resize-y rounded-md border border-input bg-background px-3 py-2 font-mono text-sm leading-relaxed outline-none focus:ring-1 focus:ring-ring"
          />
          {badPaths.length > 0 && (
            <p className="mt-1 text-[11px] text-destructive">
              这几条不像设备内绝对路径（要以 / 开头）：{badPaths.join('、')}
            </p>
          )}
        </Field>

        {form.platform === 'ios' && (
          <div className="md:col-span-2">
            <PathPresets selected={pathList} onToggle={togglePreset} />
          </div>
        )}

        {form.platform === 'ios' && (
          <>
            <Field label="SSH 地址" required>
              <input
                value={form.sshAddr}
                onChange={(e) => setField('sshAddr', e.target.value)}
                placeholder="root@127.0.0.1:22"
                spellCheck={false}
                className="h-9 w-full rounded-md border border-input bg-background px-3 font-mono text-sm outline-none focus:ring-1 focus:ring-ring"
              />
            </Field>

            <Field label="SSH 密码">
              <div className="space-y-1.5">
                <input
                  type="password"
                  value={form.sshPassword}
                  onChange={(e) => setField('sshPassword', e.target.value)}
                  placeholder="默认 alpine"
                  spellCheck={false}
                  className="h-9 w-full rounded-md border border-input bg-background px-3 font-mono text-sm outline-none focus:ring-1 focus:ring-ring"
                />
                <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <input
                    type="checkbox"
                    checked={form.rememberPassword}
                    onChange={(e) => setField('rememberPassword', e.target.checked)}
                    className="h-3.5 w-3.5"
                  />
                  记住密码（保存到系统凭据库）
                </label>
              </div>
            </Field>

            <Field label="USB 代理">
              <label
                className={cn(
                  'flex h-9 cursor-pointer items-center gap-2 rounded-md border px-3 text-sm transition-colors',
                  form.usbProxy
                    ? 'border-foreground/30 bg-accent font-medium'
                    : 'border-input bg-background hover:bg-accent'
                )}
              >
                <input
                  type="checkbox"
                  checked={form.usbProxy}
                  onChange={(e) => setField('usbProxy', e.target.checked)}
                  className="h-3.5 w-3.5"
                />
                自动通过 USB 建立代理（推荐）
              </label>
            </Field>

            <Field label="设备 ID（可选）">
              <input
                value={form.deviceId}
                onChange={(e) => setField('deviceId', e.target.value)}
                placeholder="默认使用第一台检测到的设备"
                spellCheck={false}
                className="h-9 w-full rounded-md border border-input bg-background px-3 font-mono text-sm outline-none focus:ring-1 focus:ring-ring"
              />
            </Field>
          </>
        )}
      </div>

      <div className="border-t border-border bg-muted/30 px-4 py-3">
        <div className="mb-2 text-xs font-medium text-muted-foreground">执行预览</div>
        <code className="block whitespace-pre-wrap break-all rounded bg-background px-3 py-2 font-mono text-[12px]">
          {previewCommand(form)}
        </code>
        <div className="mt-3 flex items-center justify-end gap-3">
          {blockReason && (
            <span className="text-[11px] text-muted-foreground">{blockReason}</span>
          )}
          <Button onClick={run} disabled={!canRun}>
            <Play className="h-3.5 w-3.5" />
            {disabled ? '执行中…' : '开始取证'}
          </Button>
        </div>
      </div>

      {/* args preview hidden helper for debugging; keep var used */}
      <span className="hidden">{args.length}</span>
    </div>
  )
}

/**
 * 常用路径速选。
 *
 * iOS 系统自带应用(邮件、通讯录、通话记录…)的数据不属于任何 App,没有包名可搜,
 * 只能按绝对路径取;而路径又长又容易少打一层。这一排按钮就是为了让人不必记住它们 ——
 * "指定路径"这个功能之前没人会用,缺的就是这个。
 */
function PathPresets({
  selected,
  onToggle,
}: {
  selected: string[]
  onToggle: (path: string) => void
}) {
  const [open, setOpen] = useState(false)
  return (
    <div className="rounded-md border border-border bg-secondary/20">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-secondary/40"
      >
        <ListPlus className="h-3.5 w-3.5" />
        <span className="font-medium">常用路径（iOS 系统数据）</span>
        <span className="text-[10px] opacity-60">点一下加到上面,再点一下取消</span>
        <ChevronDown
          className={cn('ml-auto h-3.5 w-3.5 transition-transform', open && 'rotate-180')}
        />
      </button>
      {open && (
        <div className="space-y-2.5 border-t border-border/60 px-3 py-2.5">
          {IOS_PATH_PRESETS.map((g) => (
            <div key={g.title} className="space-y-1.5">
              <div className="text-[11px] text-muted-foreground">{g.title}</div>
              <div className="flex flex-wrap gap-1.5">
                {g.items.map((it) => {
                  const added = selected.includes(it.path)
                  return (
                    <button
                      key={it.path}
                      type="button"
                      onClick={() => onToggle(it.path)}
                      title={`${added ? '点击移除 · ' : ''}${it.path}${it.note ? '\n' + it.note : ''}`}
                      className={cn(
                        'group/preset flex items-center gap-1 rounded-md border px-2 py-1 text-xs transition-colors',
                        added
                          ? 'border-success/40 bg-success/10 text-success hover:border-destructive/40 hover:bg-destructive/10 hover:text-destructive'
                          : 'border-border bg-card hover:bg-secondary',
                      )}
                    >
                      {added ? (
                        <>
                          {/* 悬停换成叉号,让人知道这一下是"移除"而不是"再加一次" */}
                          <Check className="h-3 w-3 group-hover/preset:hidden" />
                          <X className="hidden h-3 w-3 group-hover/preset:block" />
                        </>
                      ) : (
                        <Plus className="h-3 w-3" />
                      )}
                      {it.label}
                    </button>
                  )
                })}
              </div>
            </div>
          ))}
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
        {hint && <span className="text-muted-foreground font-normal">· {hint}</span>}
      </label>
      {children}
    </div>
  )
}
