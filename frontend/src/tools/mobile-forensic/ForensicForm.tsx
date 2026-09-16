import { useEffect, useState } from 'react'
import { BookmarkPlus, Check, ChevronDown, FolderOpen, ListPlus, Play, Plus, Trash2, X } from 'lucide-react'
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
  type CustomPreset,
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
import { PresetEditorDialog, type PresetRow } from './PresetEditorDialog'

/** 示例路径按平台给 —— Android 页原来显示的是 iOS 的路径,照着填一条都取不到 */
const PATH_PLACEHOLDER: Record<Platform, string> = {
  android: '/data/data/com.tencent.mm/\n/sdcard/Android/data/com.tencent.mm/',
  ios: '/private/var/mobile/Library/Mail/\n/private/var/mobile/Library/Accounts/',
}

interface Props {
  form: FormState
  /** go-forensic 启用了才给引擎选择;没启用就一直走内置 */
  cliEnabled: boolean
  /** 接受函数式更新:几处 effect 可能同一帧里都在改 form,整个对象覆盖会把别人的改动吃掉 */
  onChange: (next: FormState | ((prev: FormState) => FormState)) => void
  onRun: () => void
  disabled: boolean
  /** 非空表示现在跑不了,内容就是原因;为空表示没拦 */
  blockReason?: string
}

export function ForensicForm({ form, cliEnabled, onChange, onRun, disabled, blockReason }: Props) {
  const confirm = useConfirm()
  const defaultSshAddr = useForensicStore((s) => s.defaultSshAddr)
  const defaultOutputBase = useForensicStore((s) => s.defaultOutputBase)
  const customPresets = useForensicStore((s) => s.customPresets)
  const saveCustomPresets = useForensicStore((s) => s.saveCustomPresets)
  const removeCustomPreset = useForensicStore((s) => s.removeCustomPreset)
  const [passwordLoaded, setPasswordLoaded] = useState(false)
  const [presetEditor, setPresetEditor] = useState<{ rows: PresetRow[]; manual: boolean } | null>(
    null,
  )

  // 进入时把 store 的默认值灌进来。用函数式更新而不是拿闭包里的 form 整个覆盖:
  // 从真机浏览跳过来时,父组件在同一帧里往 form 里填路径,拿旧 form 覆盖会把路径吃掉
  useEffect(() => {
    if (!defaultSshAddr) return
    onChange((f) => (f.sshAddr === 'root@127.0.0.1:22' ? { ...f, sshAddr: defaultSshAddr } : f))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [defaultSshAddr])

  // 尝试从 keychain 加载密码
  useEffect(() => {
    if (form.platform !== 'ios' || passwordLoaded) return
    GetPassword(sshPasswordKey(form.sshAddr)).then((pwd) => {
      setPasswordLoaded(true)
      if (pwd) {
        onChange((f) => ({ ...f, sshPassword: pwd, rememberPassword: true }))
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

  // 这次跑的是不是 go-forensic。它无条件先清空输出目录、也需要本机转发端口,
  // 内置那条两样都不是,所以界面上好几处要按它分叉
  const usingCLI = needsCLI(form.platform, form.engine)
  const willClear = usingCLI || form.clearOutput

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
            {usingCLI && (
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

  // 「我的常用」的几个动作。存的时候路径先过一遍 normalizePath,和 pathList 用同一把尺子,
  // 不然文本框里多敲的 "//" 会让"已加过"的判断失灵
  const customList = customPresets[form.platform] ?? []
  const openSaveCurrent = () =>
    setPresetEditor({
      rows: pathList.map((p) => ({
        // 已经存过的沿用原标签,别把人家起好的名字换成猜的
        label: customList.find((c) => c.path === p)?.label ?? guessLabel(p),
        path: p,
      })),
      manual: false,
    })
  const openAddManual = () => setPresetEditor({ rows: [{ label: '', path: '' }], manual: true })
  const savePresets = (rows: PresetRow[]) => {
    saveCustomPresets(
      form.platform,
      rows.map((r) => ({ label: r.label, path: normalizePath(r.path.trim()) })),
    )
    setPresetEditor(null)
  }
  const removeCustom = async (it: CustomPreset) => {
    const ok = await confirm({
      title: '删掉这条常用路径？',
      message: (
        <div className="space-y-1">
          <p>「{it.label}」</p>
          <p className="break-all font-mono text-xs">{it.path}</p>
          <p className="text-muted-foreground">
            只是从「我的常用」里去掉，上面已经填进去的路径不受影响。
          </p>
        </div>
      ),
      confirmLabel: '删掉',
      danger: true,
    })
    if (ok) removeCustomPreset(form.platform, it.path)
  }

  const args = buildArgs(form)

  return (
    <div className="rounded-lg border border-border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-2">
        <span className="text-xs font-medium text-muted-foreground">任务参数</span>
        <div className="flex items-center gap-2">
          {/* go-forensic 是可选的备选引擎,没启用就不摆这个选择题 ——
              两个平台的提取都已内置,大多数人根本没装过它 */}
          {cliEnabled && (
            <ModeToggle
              value={form.engine}
              onChange={(e) => setField('engine', e as Engine)}
              options={[
                { value: 'builtin', label: '内置' },
                { value: 'cli', label: 'go-forensic' },
              ]}
            />
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
              placeholder="D:\取证\案件编号\设备导出"
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
              usingCLI ? 'text-muted-foreground' : 'cursor-pointer',
            )}
            title={
              usingCLI
                ? 'go-forensic 总是先清空输出目录，关不掉'
                : '勾上后，这次导出前会把该目录里现有的内容全部删掉'
            }
          >
            <input
              type="checkbox"
              checked={willClear}
              disabled={usingCLI}
              onChange={(e) => setField('clearOutput', e.target.checked)}
              className="h-3.5 w-3.5"
            />
            导出前清空该目录
            {usingCLI && <span className="opacity-70">· go-forensic 总是这样，关不掉</span>}
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

        <div className="md:col-span-2">
          <PathPresets
            platform={form.platform}
            selected={pathList}
            custom={customList}
            canSaveCurrent={pathList.length > 0}
            onToggle={togglePreset}
            onSaveCurrent={openSaveCurrent}
            onAddManual={openAddManual}
            onRemoveCustom={removeCustom}
          />
        </div>

        {form.platform === 'ios' && (
          <>
            <Field
              label="SSH 地址"
              required
              hint={
                usingCLI
                  ? undefined
                  : '内置引擎只取里面的用户名，走 USB 直连；填成别的机器会自动改用 go-forensic'
              }
            >
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

            {/* 内置引擎本身就是走 USB 的,再给一个"要不要用 USB 代理"的开关只会让人困惑 */}
            {usingCLI && (
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
            )}

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

      <PresetEditorDialog
        open={presetEditor !== null}
        platform={form.platform}
        rows={presetEditor?.rows ?? EMPTY_ROWS}
        manual={presetEditor?.manual ?? false}
        onClose={() => setPresetEditor(null)}
        onSave={savePresets}
      />
    </div>
  )
}

// 关着的时候给弹窗一个固定的空数组,免得每次渲染都是新引用、它的 effect 白跑
const EMPTY_ROWS: PresetRow[] = []

/**
 * 常用路径速选。
 *
 * iOS 系统自带应用(邮件、通讯录、通话记录…)的数据不属于任何 App,没有包名可搜,
 * 只能按绝对路径取;而路径又长又容易少打一层。这一排按钮就是为了让人不必记住它们 ——
 * "指定路径"这个功能之前没人会用,缺的就是这个。
 *
 * 「我的常用」是用户自己存的,两个平台各一份,排在内置那几组前面;
 * Android 没有内置组,面板里就只有这一组。
 */
function PathPresets({
  platform,
  selected,
  custom,
  canSaveCurrent,
  onToggle,
  onSaveCurrent,
  onAddManual,
  onRemoveCustom,
}: {
  platform: Platform
  selected: string[]
  custom: CustomPreset[]
  canSaveCurrent: boolean
  onToggle: (path: string) => void
  onSaveCurrent: () => void
  onAddManual: () => void
  onRemoveCustom: (it: CustomPreset) => void
}) {
  const [open, setOpen] = useState(false)
  return (
    <div className="rounded-md border border-border bg-secondary/20">
      <button
        type="button"
        title="展开常用路径"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-secondary/40"
      >
        <ListPlus className="h-3.5 w-3.5" />
        <span className="font-medium">常用路径</span>
        <span className="text-[10px] opacity-60">
          {platform === 'ios' ? '我的常用 + iOS 系统数据' : '我的常用'} · 点一下加到上面，再点一下取消
        </span>
        <ChevronDown
          className={cn('ml-auto h-3.5 w-3.5 transition-transform', open && 'rotate-180')}
        />
      </button>
      {open && (
        <div className="space-y-2.5 border-t border-border/60 px-3 py-2.5">
          <div className="space-y-1.5">
            <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
              <span>我的常用</span>
              <span className="ml-auto flex items-center gap-1">
                <button
                  type="button"
                  title="把上面填的路径存为常用"
                  disabled={!canSaveCurrent}
                  onClick={onSaveCurrent}
                  className="flex items-center gap-1 rounded px-1.5 py-0.5 transition-colors hover:bg-secondary hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
                >
                  <BookmarkPlus className="h-3 w-3" />
                  存为常用
                </button>
                <button
                  type="button"
                  title="手动添加一条常用路径"
                  onClick={onAddManual}
                  className="flex items-center gap-1 rounded px-1.5 py-0.5 transition-colors hover:bg-secondary hover:text-foreground"
                >
                  <Plus className="h-3 w-3" />
                  添加
                </button>
              </span>
            </div>
            {custom.length === 0 ? (
              <p className="text-[11px] text-muted-foreground/80">
                还没有。把上面填好的路径「存为常用」，或者点「添加」手动录一条。
                {platform === 'ios' ? 'iOS' : 'Android'} 的常用单独一份，切换平台不会混在一起。
              </p>
            ) : (
              <div className="flex flex-wrap gap-1.5">
                {custom.map((it) => (
                  <span key={it.path} className="group/custom inline-flex items-center gap-0.5">
                    <PresetChip
                      label={it.label}
                      path={it.path}
                      added={selected.includes(it.path)}
                      onToggle={onToggle}
                    />
                    <button
                      type="button"
                      title={`从我的常用里删掉「${it.label}」`}
                      onClick={() => onRemoveCustom(it)}
                      className="rounded p-0.5 text-muted-foreground opacity-0 transition-opacity hover:bg-destructive/10 hover:text-destructive focus-visible:opacity-100 group-hover/custom:opacity-100"
                    >
                      <Trash2 className="h-3 w-3" />
                    </button>
                  </span>
                ))}
              </div>
            )}
          </div>
          {platform === 'ios' &&
            IOS_PATH_PRESETS.map((g) => (
              <div key={g.title} className="space-y-1.5">
                <div className="text-[11px] text-muted-foreground">{g.title}</div>
                <div className="flex flex-wrap gap-1.5">
                  {g.items.map((it) => (
                    <PresetChip
                      key={it.path}
                      label={it.label}
                      path={it.path}
                      note={it.note}
                      added={selected.includes(it.path)}
                      onToggle={onToggle}
                    />
                  ))}
                </div>
              </div>
            ))}
        </div>
      )}
    </div>
  )
}

/** 一枚路径按钮:没加过就追加,已加过就摘掉;悬停换成叉号,让人知道这一下是"移除"而不是"再加一次" */
function PresetChip({
  label,
  path,
  note,
  added,
  onToggle,
}: {
  label: string
  path: string
  note?: string
  added: boolean
  onToggle: (path: string) => void
}) {
  return (
    <button
      type="button"
      onClick={() => onToggle(path)}
      title={`${added ? '点击移除 · ' : ''}${path}${note ? '\n' + note : ''}`}
      className={cn(
        'group/preset flex items-center gap-1 rounded-md border px-2 py-1 text-xs transition-colors',
        added
          ? 'border-success/40 bg-success/10 text-success hover:border-destructive/40 hover:bg-destructive/10 hover:text-destructive'
          : 'border-border bg-card hover:bg-secondary',
      )}
    >
      {added ? (
        <>
          <Check className="h-3 w-3 group-hover/preset:hidden" />
          <X className="hidden h-3 w-3 group-hover/preset:block" />
        </>
      ) : (
        <Plus className="h-3 w-3" />
      )}
      {label}
    </button>
  )
}

/** 从路径猜个标签:取最后一段。/private/var/mobile/Library/Mail/ → Mail */
function guessLabel(p: string): string {
  const segs = p.split('/').filter(Boolean)
  return segs[segs.length - 1] ?? p
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
