import { useState } from 'react'
import {
  BarChart3,
  MessageSquare,
  RefreshCw,
  Search,
  Settings,
  Sparkles,
} from 'lucide-react'
import { ToolShell } from '@/components/tool/ToolShell'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { meta } from './meta'
import { Dashboard } from './Dashboard'
import { Sessions } from './Sessions'
import { Skills } from './Skills'
import { Search as SearchView } from './Search'
import { Config } from './Config'

type Tab = 'dashboard' | 'sessions' | 'search' | 'skills' | 'config'

export default function ClaudeInsightTool() {
  const [tab, setTab] = useState<Tab>('dashboard')
  const [reloadToken, setReloadToken] = useState(0)

  const refresh = () => setReloadToken((n) => n + 1)

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      actions={
        <>
          <TabButton
            active={tab === 'dashboard'}
            onClick={() => setTab('dashboard')}
            icon={<BarChart3 className="h-3.5 w-3.5" />}
            label="概览"
          />
          <TabButton
            active={tab === 'sessions'}
            onClick={() => setTab('sessions')}
            icon={<MessageSquare className="h-3.5 w-3.5" />}
            label="会话"
          />
          <TabButton
            active={tab === 'search'}
            onClick={() => setTab('search')}
            icon={<Search className="h-3.5 w-3.5" />}
            label="搜索"
          />
          <TabButton
            active={tab === 'skills'}
            onClick={() => setTab('skills')}
            icon={<Sparkles className="h-3.5 w-3.5" />}
            label="Skills"
          />
          <TabButton
            active={tab === 'config'}
            onClick={() => setTab('config')}
            icon={<Settings className="h-3.5 w-3.5" />}
            label="配置"
          />
          <Button variant="ghost" size="sm" onClick={refresh}>
            <RefreshCw className="h-3.5 w-3.5" />
            刷新
          </Button>
        </>
      }
    >
      {tab === 'dashboard' && <Dashboard reloadToken={reloadToken} />}
      {tab === 'sessions' && <Sessions reloadToken={reloadToken} />}
      {tab === 'search' && <SearchView />}
      {tab === 'skills' && <Skills reloadToken={reloadToken} />}
      {tab === 'config' && <Config reloadToken={reloadToken} />}
    </ToolShell>
  )
}

function TabButton({
  active,
  onClick,
  icon,
  label,
}: {
  active: boolean
  onClick: () => void
  icon: React.ReactNode
  label: string
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'inline-flex h-8 items-center gap-1.5 rounded-md px-3 text-xs font-medium transition-colors',
        active
          ? 'bg-info/15 text-info'
          : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
      )}
    >
      {icon}
      {label}
    </button>
  )
}
