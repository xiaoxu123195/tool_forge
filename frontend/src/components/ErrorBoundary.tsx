import { Component, type ErrorInfo, type ReactNode } from 'react'
import { AlertTriangle, Check, Copy, RotateCcw } from 'lucide-react'

/**
 * 兜住渲染期抛出的异常,把它显示出来,而不是让 React 卸载整棵树。
 *
 * 没有这个的时候,任何一处渲染出错的表现都是同一个:整个窗口变成白的,没有任何信息。
 * 桌面应用又不像网页那样能顺手按 F12 —— 用户能提供的全部线索就只有"白屏"两个字,
 * 而那对定位毫无帮助。所以这里的重点不是"优雅降级",是**把错误原文摆到用户面前**,
 * 让他能一键复制发过来。
 *
 * 只放在根上一层:局部边界会把出错的区域藏起来,反而让人以为是功能没做。
 */
interface State {
  error: Error | null
  stack: string
}

export class ErrorBoundary extends Component<{ children: ReactNode }, State> {
  state: State = { error: null, stack: '' }

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // componentStack 指出是哪个组件炸的,比纯 JS 调用栈好读得多
    this.setState({ stack: info.componentStack ?? '' })
    console.error('[ErrorBoundary]', error, info.componentStack)
  }

  render() {
    const { error, stack } = this.state
    if (!error) return this.props.children
    return <Fallback error={error} componentStack={stack} onReset={() => this.setState({ error: null, stack: '' })} />
  }
}

function Fallback({
  error,
  componentStack,
  onReset,
}: {
  error: Error
  componentStack: string
  onReset: () => void
}) {
  const detail = [
    error.message,
    '',
    error.stack ?? '',
    componentStack ? '\n组件栈:' + componentStack : '',
  ].join('\n')

  return (
    <div className="flex h-screen w-screen items-center justify-center bg-background p-8">
      <div className="flex max-h-full w-full max-w-2xl flex-col gap-3 overflow-hidden rounded-xl border border-destructive/40 bg-card p-5">
        <div className="flex items-center gap-2 text-destructive">
          <AlertTriangle className="h-5 w-5 shrink-0" />
          <h1 className="text-base font-semibold">界面出错了</h1>
        </div>
        <p className="text-sm text-muted-foreground">
          这一页渲染时抛了异常。下面是完整错误,复制发给开发者就能定位;
          点「重试」会重新渲染,数据不会丢。
        </p>
        <div className="min-h-0 flex-1 overflow-auto rounded-md border border-border bg-secondary/30 p-3">
          <pre className="whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed text-muted-foreground">
            {detail}
          </pre>
        </div>
        <div className="flex items-center gap-2">
          <CopyButton text={detail} />
          <button
            type="button"
            onClick={onReset}
            className="flex h-8 items-center gap-1.5 rounded-md border border-border px-3 text-xs text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          >
            <RotateCcw className="h-3.5 w-3.5" />
            重试
          </button>
          <button
            type="button"
            onClick={() => location.reload()}
            className="h-8 rounded-md px-3 text-xs text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          >
            重新加载
          </button>
        </div>
      </div>
    </div>
  )
}

function CopyButton({ text }: { text: string }) {
  return (
    <button
      type="button"
      onClick={(e) => {
        void navigator.clipboard.writeText(text)
        const el = e.currentTarget
        el.dataset.copied = '1'
        setTimeout(() => delete el.dataset.copied, 1500)
      }}
      className="group flex h-8 items-center gap-1.5 rounded-md bg-info px-3 text-xs font-medium text-info-foreground transition-colors hover:bg-info/90"
    >
      <Copy className="h-3.5 w-3.5 group-data-[copied]:hidden" />
      <Check className="hidden h-3.5 w-3.5 group-data-[copied]:block" />
      复制错误信息
    </button>
  )
}
