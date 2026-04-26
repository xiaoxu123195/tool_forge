import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from 'react'
import { createPortal } from 'react-dom'
import { AlertTriangle } from 'lucide-react'
import { Button } from './button'
import { cn } from '@/lib/utils'

export interface ConfirmOptions {
  title?: string
  message: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  danger?: boolean
}

type ConfirmFn = (opts: ConfirmOptions) => Promise<boolean>

const Ctx = createContext<ConfirmFn | null>(null)

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<{
    opts: ConfirmOptions
    resolve: (v: boolean) => void
  } | null>(null)

  const confirm: ConfirmFn = useCallback((opts) => {
    return new Promise<boolean>((resolve) => {
      setState({ opts, resolve })
    })
  }, [])

  const finish = (v: boolean) => {
    if (!state) return
    state.resolve(v)
    setState(null)
  }

  return (
    <Ctx.Provider value={confirm}>
      {children}
      {state && (
        <ConfirmDialog
          opts={state.opts}
          onConfirm={() => finish(true)}
          onCancel={() => finish(false)}
        />
      )}
    </Ctx.Provider>
  )
}

export function useConfirm(): ConfirmFn {
  const c = useContext(Ctx)
  if (!c) throw new Error('useConfirm 必须在 <ConfirmProvider /> 下使用')
  return c
}

function ConfirmDialog({
  opts,
  onConfirm,
  onCancel,
}: {
  opts: ConfirmOptions
  onConfirm: () => void
  onCancel: () => void
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onCancel()
      } else if (e.key === 'Enter') {
        e.preventDefault()
        onConfirm()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onConfirm, onCancel])

  return createPortal(
    <div
      className="fixed inset-0 z-[120] flex items-center justify-center bg-black/50 backdrop-blur-sm"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onCancel()
      }}
    >
      <div
        className="w-[380px] max-w-[90vw] overflow-hidden rounded-xl border border-border bg-card shadow-2xl"
        style={{ animation: 'tf-confirm-pop 0.15s cubic-bezier(0.4, 0, 0.2, 1)' }}
      >
        <div className="flex gap-3 p-5">
          <div
            className={cn(
              'flex h-9 w-9 shrink-0 items-center justify-center rounded-full',
              opts.danger
                ? 'bg-red-500/10 text-red-600 dark:text-red-400'
                : 'bg-info/10 text-info',
            )}
          >
            <AlertTriangle className="h-4 w-4" />
          </div>
          <div className="min-w-0 flex-1">
            <h3 className="text-sm font-semibold text-foreground">
              {opts.title ?? '确认操作'}
            </h3>
            <p className="mt-1.5 text-xs leading-relaxed text-muted-foreground">
              {opts.message}
            </p>
          </div>
        </div>
        <div className="flex items-center justify-end gap-2 border-t border-border bg-secondary/30 px-4 py-3">
          <Button variant="ghost" size="sm" onClick={onCancel}>
            {opts.cancelLabel ?? '取消'}
          </Button>
          <Button
            size="sm"
            variant={opts.danger ? 'destructive' : 'default'}
            onClick={onConfirm}
            autoFocus
          >
            {opts.confirmLabel ?? '确定'}
          </Button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
