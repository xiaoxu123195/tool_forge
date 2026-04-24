import { useEffect, useState } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { ChevronLeft, Home as HomeIcon, Search, User } from 'lucide-react'
import { GetAppInfo } from '../../wailsjs/go/main/App'
import type { main } from '../../wailsjs/go/models'
import { useLayoutStore } from '@/stores/layout'
import {
  CATEGORY_LABELS,
  getVisibleToolsByCategory,
  useToolsStore,
  type ToolCategory,
} from '@/stores/tools'
import { useProfileStore } from '@/stores/profile'
import { useUpdaterStore } from '@/stores/updater'
import { cn } from '@/lib/utils'

const CATEGORY_ORDER: ToolCategory[] = [
  'forensic',
  'data',
  'ai',
  'codec',
  'crypto',
  'time',
  'text',
  'network',
  'gen',
  'dev',
  'system',
]

interface SidebarProps {
  onOpenCommandPalette?: () => void
}

export function Sidebar({ onOpenCommandPalette }: SidebarProps = {}) {
  const collapsed = useLayoutStore((s) => s.sidebarCollapsed)
  const toggleSidebar = useLayoutStore((s) => s.toggleSidebar)
  const visibility = useToolsStore((s) => s.visibility)
  const order = useToolsStore((s) => s.order)
  const nickname = useProfileStore((s) => s.nickname)
  const grouped = getVisibleToolsByCategory(visibility, order)
  const [info, setInfo] = useState<main.AppInfo | null>(null)

  const updStatus = useUpdaterStore((s) => s.status)
  const updLatest = useUpdaterStore((s) => s.latestVersion)
  const check = useUpdaterStore((s) => s.check)

  useEffect(() => {
    GetAppInfo().then(setInfo)
    // 启动时静默查一次更新(不弹加载态,只默默更新徽章颜色)
    check({ silent: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const hasUpdate =
    updStatus === 'available' ||
    updStatus === 'downloading' ||
    updStatus === 'downloaded' ||
    updStatus === 'download-error'
  const isLatest = !hasUpdate
  const dotColor = isLatest ? 'bg-emerald-500' : 'bg-amber-500'
  const pillTitle = isLatest
    ? '当前已是最新版本'
    : `有新版本 v${updLatest ?? ''} 可用,点击查看`

  return (
    <aside
      className={cn(
        'flex h-full flex-col border-r border-border bg-sidebar transition-[width] duration-200',
        collapsed ? 'w-14' : 'w-56'
      )}
    >
      <div
        className={cn(
          'flex h-10 items-center border-b border-border px-2',
          collapsed ? 'justify-center' : 'justify-between'
        )}
      >
        {!collapsed && (
          <Link
            to="/profile"
            state={{ section: 'about' }}
            title={pillTitle}
            className="group/pill inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground"
          >
            <span className="relative flex h-2 w-2 shrink-0 items-center justify-center">
              {!isLatest && (
                <span className={cn('absolute inline-flex h-full w-full animate-ping rounded-full opacity-70', dotColor)} />
              )}
              <span className={cn('relative h-1.5 w-1.5 rounded-full', dotColor)} />
            </span>
            <span className="font-mono tracking-tight tabular-nums">
              v{info?.version ?? '…'}
            </span>
          </Link>
        )}
        <button
          onClick={toggleSidebar}
          className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-accent"
          title={collapsed ? '展开' : '折叠'}
        >
          <ChevronLeft
            className={cn('h-4 w-4 transition-transform', collapsed && 'rotate-180')}
          />
        </button>
      </div>

      <nav className="flex-1 overflow-y-auto py-2">
        <div className="mb-3 px-2 space-y-0.5">
          <NavLink
            to="/"
            end
            title={collapsed ? '首页' : undefined}
            className={({ isActive }) =>
              cn(
                'flex h-8 items-center gap-2 rounded-md px-2 text-sm text-foreground/80 transition-colors hover:bg-accent hover:text-foreground',
                isActive && 'bg-accent font-medium text-foreground',
                collapsed && 'justify-center'
              )
            }
          >
            <HomeIcon className="h-4 w-4 shrink-0" />
            {!collapsed && <span className="truncate">首页</span>}
          </NavLink>
          {onOpenCommandPalette && (
            <button
              type="button"
              onClick={onOpenCommandPalette}
              title={collapsed ? '快速跳转 (Ctrl+K)' : undefined}
              className={cn(
                'flex h-8 w-full items-center gap-2 rounded-md px-2 text-sm text-foreground/80 transition-colors hover:bg-accent hover:text-foreground',
                collapsed && 'justify-center'
              )}
            >
              <Search className="h-4 w-4 shrink-0" />
              {!collapsed && (
                <>
                  <span className="flex-1 truncate text-left">快速跳转</span>
                  <kbd className="shrink-0 rounded border border-border bg-background/80 px-1 font-mono text-[10px] text-muted-foreground">
                    Ctrl+K
                  </kbd>
                </>
              )}
            </button>
          )}
        </div>

        {CATEGORY_ORDER.map((cat) => {
          const tools = grouped[cat]
          if (!tools || tools.length === 0) return null
          return (
            <div key={cat} className="mb-3">
              {!collapsed && (
                <div className="px-3 pb-1 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                  {CATEGORY_LABELS[cat]}
                </div>
              )}
              <ul className="space-y-0.5 px-2">
                {tools.map((tool) => {
                  const Icon = tool.icon
                  return (
                    <li key={tool.id}>
                      <NavLink
                        to={tool.path}
                        title={collapsed ? tool.title : undefined}
                        className={({ isActive }) =>
                          cn(
                            'flex h-8 items-center gap-2 rounded-md px-2 text-sm text-foreground/80 transition-colors hover:bg-accent hover:text-foreground',
                            isActive && 'bg-accent font-medium text-foreground',
                            collapsed && 'justify-center'
                          )
                        }
                      >
                        <Icon className="h-4 w-4 shrink-0" />
                        {!collapsed && <span className="truncate">{tool.title}</span>}
                      </NavLink>
                    </li>
                  )
                })}
              </ul>
            </div>
          )
        })}
      </nav>

      <div className="border-t border-border p-2">
        <NavLink
          to="/profile"
          title={collapsed ? nickname : undefined}
          className={({ isActive }) =>
            cn(
              'flex h-10 items-center gap-2 rounded-md px-2 text-sm text-foreground/80 transition-colors hover:bg-accent hover:text-foreground',
              isActive && 'bg-accent font-medium text-foreground',
              collapsed && 'justify-center'
            )
          }
        >
          <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-background text-muted-foreground">
            <User className="h-4 w-4" />
          </div>
          {!collapsed && <span className="truncate">{nickname}</span>}
        </NavLink>
      </div>
    </aside>
  )
}
