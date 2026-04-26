import { useEffect, useRef, useState } from 'react'
import { AlertCircle, Check, Keyboard, RotateCcw, X } from 'lucide-react'
import {
  ListHotkeys,
  ResetHotkey,
  SetHotkey,
} from '../../../wailsjs/go/main/App'
import type { system } from '../../../wailsjs/go/models'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

type Info = system.HotkeyInfo

export function HotkeysSection() {
  const [list, setList] = useState<Info[]>([])
  const [loading, setLoading] = useState(true)
  const [recording, setRecording] = useState<string | null>(null)

  const refresh = async () => {
    const r = await ListHotkeys()
    setList(r ?? [])
    setLoading(false)
  }

  useEffect(() => {
    refresh()
  }, [])

  const onCapture = async (id: string, spec: string) => {
    setRecording(null)
    const err = await SetHotkey(id, spec)
    if (err) alert(err)
    refresh()
  }

  const onReset = async (id: string) => {
    const err = await ResetHotkey(id)
    if (err) alert(err)
    refresh()
  }

  return (
    <div className="mx-auto max-w-2xl space-y-6">
      <header>
        <h1 className="text-xl font-semibold">快捷键</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          自定义全局快捷键。修饰键至少包含 Ctrl / Shift / Alt 之一,主键支持
          A-Z / 0-9 / F1-F12。macOS 暂不支持。
        </p>
      </header>

      {loading ? (
        <div className="text-sm text-muted-foreground">正在加载...</div>
      ) : list.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
          暂无可绑定的全局热键
        </div>
      ) : (
        <div className="space-y-2">
          {list.map((it) => (
            <Row
              key={it.id}
              info={it}
              onRecord={() => setRecording(it.id)}
              onReset={() => onReset(it.id)}
              onClear={() => onCapture(it.id, '')}
            />
          ))}
        </div>
      )}

      {recording && (
        <Recorder
          onCancel={() => setRecording(null)}
          onCapture={(spec) => onCapture(recording, spec)}
        />
      )}
    </div>
  )
}

function Row({
  info,
  onRecord,
  onReset,
  onClear,
}: {
  info: Info
  onRecord: () => void
  onReset: () => void
  onClear: () => void
}) {
  const dirty = info.currentSpec !== info.defaultSpec
  return (
    <div className="flex items-center gap-3 rounded-lg border border-border bg-card p-3">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-gradient-to-br from-info/20 to-info/10 text-info">
        <Keyboard className="h-4 w-4" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium">{info.label}</div>
        <div className="mt-1 flex items-center gap-2">
          {info.currentSpec ? (
            <KeyChip spec={info.currentSpec} active={info.active} />
          ) : (
            <span className="text-xs italic text-muted-foreground">未绑定</span>
          )}
          {info.error && (
            <span className="inline-flex items-center gap-1 text-[11px] text-red-600 dark:text-red-400">
              <AlertCircle className="h-3 w-3" />
              {info.error}
            </span>
          )}
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-1.5">
        <Button size="sm" variant="outline" onClick={onRecord}>
          录制
        </Button>
        {info.currentSpec && (
          <Button
            size="sm"
            variant="ghost"
            onClick={onClear}
            title="取消绑定"
          >
            <X className="h-3.5 w-3.5" />
          </Button>
        )}
        {dirty && (
          <Button
            size="sm"
            variant="ghost"
            onClick={onReset}
            title={`恢复默认 (${info.defaultSpec})`}
          >
            <RotateCcw className="h-3.5 w-3.5" />
          </Button>
        )}
      </div>
    </div>
  )
}

function KeyChip({ spec, active }: { spec: string; active: boolean }) {
  const parts = spec.split('+')
  return (
    <span
      className={cn(
        'inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 font-mono text-[11px]',
        active
          ? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
          : 'bg-secondary text-muted-foreground'
      )}
      title={active ? '已激活' : '未激活(可能注册失败或被取消)'}
    >
      {active && <Check className="h-3 w-3" />}
      {parts.map((p, i) => (
        <span key={i} className="inline-flex items-center">
          {i > 0 && <span className="px-0.5 opacity-60">+</span>}
          <kbd className="rounded border border-border/60 bg-background/80 px-1">
            {p}
          </kbd>
        </span>
      ))}
    </span>
  )
}

function Recorder({
  onCapture,
  onCancel,
}: {
  onCapture: (spec: string) => void
  onCancel: () => void
}) {
  // 实时跟踪修饰键 + 主键。按下主键瞬间提交。
  const [mods, setMods] = useState<{ ctrl: boolean; shift: boolean; alt: boolean }>({
    ctrl: false,
    shift: false,
    alt: false,
  })
  const [hint, setHint] = useState('')
  const submittedRef = useRef(false)

  useEffect(() => {
    const isMain = (k: string) => /^[a-zA-Z0-9]$/.test(k) || /^F[1-9][0-2]?$/.test(k)
    const onKey = (e: KeyboardEvent) => {
      e.preventDefault()
      e.stopPropagation()
      if (e.key === 'Escape') {
        onCancel()
        return
      }
      const ctrl = e.ctrlKey
      const shift = e.shiftKey
      const alt = e.altKey
      setMods({ ctrl, shift, alt })
      const key = e.key
      if (isMain(key)) {
        if (!ctrl && !shift && !alt) {
          setHint('需要至少一个修饰键 (Ctrl / Shift / Alt)')
          return
        }
        if (submittedRef.current) return
        submittedRef.current = true
        const parts: string[] = []
        if (ctrl) parts.push('Ctrl')
        if (shift) parts.push('Shift')
        if (alt) parts.push('Alt')
        parts.push(key.length === 1 ? key.toUpperCase() : key.toUpperCase())
        onCapture(parts.join('+'))
      }
    }
    const onUp = (e: KeyboardEvent) => {
      // 修饰键放开时同步
      setMods({ ctrl: e.ctrlKey, shift: e.shiftKey, alt: e.altKey })
    }
    window.addEventListener('keydown', onKey, true)
    window.addEventListener('keyup', onUp, true)
    return () => {
      window.removeEventListener('keydown', onKey, true)
      window.removeEventListener('keyup', onUp, true)
    }
  }, [onCapture, onCancel])

  return (
    <div
      className="fixed inset-0 z-[150] flex items-center justify-center bg-black/40 backdrop-blur-sm"
      onClick={onCancel}
    >
      <div
        className="w-[400px] max-w-[90vw] rounded-xl border border-border bg-card p-6 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center gap-2">
          <Keyboard className="h-5 w-5 text-info" />
          <div className="text-base font-semibold">按下要绑定的组合键</div>
        </div>
        <div className="mb-4 flex h-16 items-center justify-center gap-1.5 rounded-lg bg-secondary/40 font-mono text-sm">
          {!mods.ctrl && !mods.shift && !mods.alt ? (
            <span className="text-muted-foreground">等待按键...</span>
          ) : (
            <>
              {mods.ctrl && <KeyboardChip>Ctrl</KeyboardChip>}
              {mods.shift && <KeyboardChip>Shift</KeyboardChip>}
              {mods.alt && <KeyboardChip>Alt</KeyboardChip>}
              <span className="text-muted-foreground">+ ?</span>
            </>
          )}
        </div>
        {hint && (
          <div className="mb-3 inline-flex items-center gap-1 text-xs text-amber-600 dark:text-amber-400">
            <AlertCircle className="h-3 w-3" />
            {hint}
          </div>
        )}
        <div className="text-xs text-muted-foreground">
          按下完整组合后自动保存。按 <kbd className="rounded border border-border/60 bg-background/80 px-1 font-mono">Esc</kbd>
          取消。
        </div>
      </div>
    </div>
  )
}

function KeyboardChip({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="rounded border border-border bg-background px-2 py-1 font-mono text-sm shadow-sm">
      {children}
    </kbd>
  )
}
