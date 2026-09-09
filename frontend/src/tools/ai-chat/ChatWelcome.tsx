import { Sparkles } from 'lucide-react'

const SUGGESTIONS = [
  '帮我用 Go 写一个简易 HTTP 服务器',
  '用一句话解释什么是 Wails',
  '把下面这段 SQL 改成 PostgreSQL 兼容写法',
  '帮我润色一段产品介绍文案',
]

export function WelcomeScreen({
  providerName,
  modelId,
  onPick,
  onOpenSettings,
}: {
  providerName: string
  modelId: string
  onPick: (s: string) => void
  /** 打开会话设置。新建对话不再弹表单,人设和预设都挪到了那里 */
  onOpenSettings?: () => void
}) {
  return (
    <div className="mx-auto flex h-full max-w-2xl flex-col items-center justify-center gap-6 px-4 py-10 text-center">
      <div className="flex h-14 w-14 items-center justify-center rounded-full bg-info/15 text-info">
        <Sparkles className="h-7 w-7" />
      </div>
      <div className="space-y-1">
        <h2 className="text-xl font-semibold">今天我能帮你做什么?</h2>
        <p className="text-xs text-muted-foreground">
          {providerName ? `${providerName} · ${modelId}` : '请先在底栏选择模型'}
        </p>
      </div>
      <div className="grid w-full grid-cols-1 gap-2 sm:grid-cols-2">
        {SUGGESTIONS.map((s) => (
          <button
            key={s}
            type="button"
            onClick={() => onPick(s)}
            className="rounded-lg border border-border bg-card p-3 text-left text-xs text-muted-foreground transition-colors hover:border-info/50 hover:bg-info/5 hover:text-foreground"
          >
            {s}
          </button>
        ))}
      </div>
      {onOpenSettings && (
        <button
          type="button"
          onClick={onOpenSettings}
          className="text-[11px] text-muted-foreground underline-offset-4 transition-colors hover:text-foreground hover:underline"
        >
          想让它扮演某个角色?到「会话设置」挑一个助手预设,或自己写提示词
        </button>
      )}
    </div>
  )
}
