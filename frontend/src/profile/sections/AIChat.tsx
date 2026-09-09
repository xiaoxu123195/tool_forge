import { useState } from 'react'
import { cn } from '@/lib/utils'
import { ProvidersTab } from './aichat/ProvidersTab'
import { DefaultsTab } from './aichat/DefaultsTab'
import { AssistantsTab } from './aichat/AssistantsTab'

type SubTab = 'providers' | 'assistants' | 'defaults'

export function AIChatSection() {
  const [tab, setTab] = useState<SubTab>('providers')

  // h-full 让这一页跟着窗口长高(百分比高度算的是父级内容盒,不会被 p-6 顶出滚动条),
  // min-h 兜住窗口很矮的情况 —— 那时宁可让外层滚动,也别把两栏挤成两行
  return (
    <div className="mx-auto flex h-full min-h-[560px] w-full max-w-6xl flex-col gap-5">
      <header className="shrink-0">
        <h1 className="text-xl font-semibold">AI 配置</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          配置 OpenAI 兼容供应商,选择默认模型,供「AI 问答」工具使用。
        </p>
      </header>

      {/* 顶部子 Tab */}
      <div className="flex h-10 shrink-0 items-center gap-1 border-b border-border">
        <SubTabButton active={tab === 'providers'} onClick={() => setTab('providers')}>
          模型服务
        </SubTabButton>
        <SubTabButton active={tab === 'assistants'} onClick={() => setTab('assistants')}>
          助手预设
        </SubTabButton>
        <SubTabButton active={tab === 'defaults'} onClick={() => setTab('defaults')}>
          默认模型
        </SubTabButton>
      </div>

      <div className="flex min-h-0 flex-1 flex-col">
        {tab === 'providers' ? (
          <ProvidersTab />
        ) : tab === 'assistants' ? (
          <AssistantsTab />
        ) : (
          <DefaultsTab />
        )}
      </div>
    </div>
  )
}

function SubTabButton({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'relative h-10 px-4 text-sm transition-colors',
        active
          ? 'font-medium text-foreground'
          : 'text-muted-foreground hover:text-foreground',
      )}
    >
      {children}
      {active && (
        <span className="absolute inset-x-1 bottom-[-1px] h-0.5 rounded-full bg-info" />
      )}
    </button>
  )
}
