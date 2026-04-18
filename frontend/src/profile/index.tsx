import { useEffect, useState } from 'react'
import { useLocation } from 'react-router-dom'
import {
  Bot,
  Database,
  Info,
  Palette,
  Plug,
  SlidersHorizontal,
  User,
  type LucideIcon,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { AboutSection } from './sections/About'
import { BasicSection } from './sections/Basic'
import { ExternalSection } from './sections/External'
import { PlaceholderSection } from './sections/Placeholder'

type SectionId =
  | 'basic'
  | 'external'
  | 'ai'
  | 'usage'
  | 'preferences'
  | 'data'
  | 'about'

interface Section {
  id: SectionId
  label: string
  icon: LucideIcon
  comingSoon?: boolean
}

const SECTIONS: Section[] = [
  { id: 'basic', label: '基础信息', icon: User },
  { id: 'external', label: '外部工具', icon: Plug },
  { id: 'ai', label: 'AI 配置', icon: Bot, comingSoon: true },
  { id: 'usage', label: 'AI 用量', icon: SlidersHorizontal, comingSoon: true },
  { id: 'preferences', label: '工具偏好', icon: Palette, comingSoon: true },
  { id: 'data', label: '数据', icon: Database, comingSoon: true },
  { id: 'about', label: '关于', icon: Info },
]

export function Profile() {
  const location = useLocation()
  const incoming = (location.state as { section?: SectionId } | null)?.section
  const [active, setActive] = useState<SectionId>(incoming ?? 'basic')
  const section = SECTIONS.find((s) => s.id === active)!

  useEffect(() => {
    if (incoming) setActive(incoming)
  }, [location.key, incoming])

  return (
    <div className="ambient flex h-full">
      <aside className="w-52 shrink-0 border-r border-border bg-card/80 p-3">
        <h2 className="mb-3 px-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          个人主页
        </h2>
        <ul className="space-y-0.5">
          {SECTIONS.map((s) => {
            const Icon = s.icon
            return (
              <li key={s.id}>
                <button
                  onClick={() => setActive(s.id)}
                  className={cn(
                    'flex h-9 w-full items-center gap-2 rounded-md px-2 text-sm text-foreground/80 transition-colors hover:bg-accent hover:text-foreground',
                    active === s.id && 'bg-accent font-medium text-foreground'
                  )}
                >
                  <Icon className="h-4 w-4 shrink-0" />
                  <span className="flex-1 text-left">{s.label}</span>
                  {s.comingSoon && (
                    <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                      即将推出
                    </span>
                  )}
                </button>
              </li>
            )
          })}
        </ul>
      </aside>

      <div className="flex-1 overflow-auto p-6">
        {active === 'basic' ? (
          <BasicSection />
        ) : active === 'external' ? (
          <ExternalSection />
        ) : active === 'about' ? (
          <AboutSection />
        ) : (
          <PlaceholderSection title={section.label} />
        )}
      </div>
    </div>
  )
}
