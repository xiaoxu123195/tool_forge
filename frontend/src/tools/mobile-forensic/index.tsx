import { useCallback, useEffect, useRef, useState } from 'react'
import { ToolShell } from '@/components/tool/ToolShell'
import {
  CancelForensic,
  RunForensic,
  SetForensicBinaryPath,
} from '../../../wailsjs/go/main/App'
import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime'
import { Settings } from 'lucide-react'
import { useForensicStore } from '@/stores/forensic'
import { useJump } from '@/lib/jump'
import { Button } from '@/components/ui/button'
import { ConfigDialog } from './ConfigDialog'
import { SetupGuide } from './SetupGuide'
import { ForensicForm } from './ForensicForm'
import { OutputPane } from './OutputPane'
import {
  buildArgs,
  defaultFormState,
  needsCLI,
  splitList,
  type FormState,
  type LogEntry,
  type RunStatus,
} from './types'
import { meta } from './meta'

interface DoneEvent {
  jobId: string
  exitCode: number
  error?: string
  canceled?: boolean
}

interface LogEvent {
  jobId: string
  stream: 'stdout' | 'stderr'
  line: string
}

export default function MobileForensic() {
  const binaryPath = useForensicStore((s) => s.binaryPath)
  const cliEnabled = useForensicStore((s) => s.cliEnabled)
  const pushHistory = useForensicStore((s) => s.pushHistory)

  // go-forensic 装没装。只有真要用它的时候才拦人 —— 默认的内置引擎
  // 不依赖任何外部程序,再拿"先去配置 go-forensic"挡在前面纯属白挡
  const [cliReady, setCliReady] = useState(false)
  const [form, setForm] = useState<FormState>(defaultFormState)
  const [configOpen, setConfigOpen] = useState(false)
  const [status, setStatus] = useState<RunStatus>('idle')
  const [logs, setLogs] = useState<LogEntry[]>([])
  const jobIdRef = useRef<string>('')

  // 同步配置的可执行路径到后端
  useEffect(() => {
    SetForensicBinaryPath(binaryPath).catch(() => {})
  }, [binaryPath])

  // 从真机浏览跳过来:平台跟着那边的会话,路径追加进「指定路径」,已经填过的不重复
  useJump('mobile-forensic', (j) => {
    setForm((f) => {
      const have = new Set(splitList(f.specifyPaths))
      const add = j.paths.filter((p) => !have.has(p))
      const merged = [f.specifyPaths.trim(), ...add].filter(Boolean).join('\n')
      return { ...f, platform: j.platform, specifyPaths: merged }
    })
  })

  // 订阅事件
  useEffect(() => {
    EventsOn('forensic:log', (e: LogEvent) => {
      if (e.jobId !== jobIdRef.current) return
      setLogs((prev) => [...prev, { stream: e.stream, line: e.line }])
    })
    EventsOn('forensic:done', (e: DoneEvent) => {
      if (e.jobId !== jobIdRef.current) return
      if (e.canceled) {
        setStatus('canceled')
      } else if (e.exitCode === 0) {
        setStatus('success')
      } else {
        setStatus('failed')
      }
      pushHistory({
        at: Date.now(),
        platform: form.platform,
        args: buildArgs(form),
        exitCode: e.exitCode,
        canceled: e.canceled,
      })
    })
    return () => {
      EventsOff('forensic:log')
      EventsOff('forensic:done')
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [form.platform])

  const handleRun = useCallback(async () => {
    setLogs([])
    setStatus('running')
    try {
      const id = await RunForensic(buildArgs(form))
      jobIdRef.current = id
    } catch (e) {
      setStatus('failed')
      setLogs([
        {
          stream: 'stderr',
          line: e instanceof Error ? e.message : String(e),
        },
      ])
    }
  }, [form])

  const handleCancel = async () => {
    if (!jobIdRef.current) return
    await CancelForensic(jobIdRef.current).catch(() => {})
  }

  const resetLogs = () => {
    setLogs([])
    setStatus('idle')
  }

  const resetAll = () => {
    setForm(defaultFormState())
    resetLogs()
  }

  // 关掉 go-forensic 时,把已经选中的 cli 引擎退回内置 —— 选择器没了而 engine
  // 还停在 cli 的话,页面会卡在一条用户已经看不见入口的失败路径上
  useEffect(() => {
    if (!cliEnabled && form.engine === 'cli') {
      setForm((f) => ({ ...f, engine: 'builtin' }))
    }
  }, [cliEnabled, form.engine])

  const blocked = cliEnabled && needsCLI(form.platform, form.engine) && !cliReady

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      onClear={resetAll}
      actions={
        <Button variant="ghost" size="sm" onClick={() => setConfigOpen(true)} title="取证配置">
          <Settings className="h-3.5 w-3.5" />
          配置
        </Button>
      }
    >
      <ConfigDialog open={configOpen} onClose={() => setConfigOpen(false)} />
      <div className="flex flex-col gap-4">
        {blocked && (
          <SetupGuide
            onConfigure={() => setConfigOpen(true)}
            onReady={() => setCliReady(true)}
            onUseBuiltin={
              form.platform === 'android'
                ? () => setForm({ ...form, engine: 'builtin' })
                : undefined
            }
          />
        )}
        <ForensicForm
          form={form}
          cliEnabled={cliEnabled}
          onChange={setForm}
          onRun={handleRun}
          disabled={status === 'running'}
          blockReason={blocked ? '这个组合要用 go-forensic，先按上面的提示配置好' : ''}
        />
        <OutputPane
          status={status}
          logs={logs}
          outputDir={form.outputDir}
          onCancel={handleCancel}
          onClear={resetLogs}
        />
      </div>
    </ToolShell>
  )
}
