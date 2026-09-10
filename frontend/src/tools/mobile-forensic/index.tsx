import { useCallback, useEffect, useRef, useState } from 'react'
import { ToolShell } from '@/components/tool/ToolShell'
import {
  CancelForensic,
  RunForensic,
  SetForensicBinaryPath,
} from '../../../wailsjs/go/main/App'
import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime'
import { useForensicStore } from '@/stores/forensic'
import { SetupGuide } from './SetupGuide'
import { ForensicForm } from './ForensicForm'
import { OutputPane } from './OutputPane'
import {
  buildArgs,
  defaultFormState,
  needsCLI,
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
  const pushHistory = useForensicStore((s) => s.pushHistory)

  // go-forensic 装没装。只有真要用它的时候才拦人 —— 默认的内置引擎
  // 不依赖任何外部程序,再拿"先去配置 go-forensic"挡在前面纯属白挡
  const [cliReady, setCliReady] = useState(false)
  const [form, setForm] = useState<FormState>(defaultFormState)
  const [status, setStatus] = useState<RunStatus>('idle')
  const [logs, setLogs] = useState<LogEntry[]>([])
  const jobIdRef = useRef<string>('')

  // 同步配置的可执行路径到后端
  useEffect(() => {
    SetForensicBinaryPath(binaryPath).catch(() => {})
  }, [binaryPath])

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

  const blocked = needsCLI(form.platform, form.engine) && !cliReady

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      onClear={resetAll}
    >
      <div className="flex flex-col gap-4">
        {blocked && (
          <SetupGuide
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
