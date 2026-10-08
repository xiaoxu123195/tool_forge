import { useRef, useState } from 'react'
import { CancelDiskJob } from '../../../wailsjs/go/main/App'
import { EventsOn } from '../../../wailsjs/runtime/runtime'

/** 后端进度事件名前缀,后面拼 jobId */
export const EV_PROGRESS = 'diskclean:progress:'

/** 进度只走事件,不在任何绑定的签名里,所以生成的 models 里没有它 */
export interface Progress {
  jobId: string
  phase: string
  files: number
  bytes: number
  done: number
  total: number
  current: string
  elapsedMs: number
}

export function fmtBytes(n: number): string {
  if (!n || n < 0) return '0 B'
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v >= 100 ? 0 : v >= 10 ? 1 : 2)} ${units[i]}`
}

export const fmtCount = (n: number) => (n || 0).toLocaleString('zh-CN')

export function fmtDate(sec: number): string {
  if (!sec) return '—'
  const d = new Date(sec * 1000)
  const p = (x: number) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

export const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

let seq = 0

/**
 * 跑一个长任务。
 *
 * 后端的扫描、删除调用会一直阻塞到做完才返回,进度从事件里来。
 * jobId 在这边生成,订阅必须在发起调用之前 —— 不然开头那几帧进度就丢在没人听的地方
 */
export function useDiskJob() {
  const [running, setRunning] = useState(false)
  const [progress, setProgress] = useState<Progress | null>(null)
  const jobRef = useRef('')

  const run = async <T>(call: (jobId: string) => Promise<T>): Promise<T> => {
    const id = `disk-${Date.now()}-${seq++}`
    jobRef.current = id
    setProgress(null)
    setRunning(true)
    const off = EventsOn(EV_PROGRESS + id, (p: Progress) => setProgress(p))
    try {
      return await call(id)
    } finally {
      off()
      jobRef.current = ''
      setRunning(false)
    }
  }

  const cancel = () => {
    if (jobRef.current) void CancelDiskJob(jobRef.current)
  }

  return { run, cancel, running, progress }
}
