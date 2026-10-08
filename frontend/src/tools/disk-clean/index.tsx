import { useState } from 'react'
import { ToolShell } from '@/components/tool/ToolShell'
import { ModeToggle } from '@/components/tool/ModeToggle'
import { cn } from '@/lib/utils'
import { meta } from './meta'
import { CachePane } from './CachePane'
import { LargePane } from './LargePane'
import { DupPane } from './DupPane'
import { MorePane } from './MorePane'

type Tab = 'cache' | 'large' | 'dup' | 'more'

export default function DiskClean() {
  const [tab, setTab] = useState<Tab>('cache')

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      fullBleed
      actions={
        <ModeToggle
          value={tab}
          onChange={setTab}
          options={[
            { value: 'cache', label: '缓存清理' },
            { value: 'large', label: '大文件' },
            { value: 'dup', label: '重复文件' },
            { value: 'more', label: '更多清理' },
          ]}
        />
      }
    >
      {/* 三页都挂着,只是藏起来:扫一遍全盘要一两分钟,切个标签就把结果扔了谁都受不了 */}
      <div data-tab="cache" className={cn('min-h-0 flex-1 flex-col', tab === 'cache' ? 'flex' : 'hidden')}>
        <CachePane active={tab === 'cache'} />
      </div>
      <div data-tab="large" className={cn('min-h-0 flex-1 flex-col', tab === 'large' ? 'flex' : 'hidden')}>
        <LargePane active={tab === 'large'} />
      </div>
      <div data-tab="dup" className={cn('min-h-0 flex-1 flex-col', tab === 'dup' ? 'flex' : 'hidden')}>
        <DupPane active={tab === 'dup'} />
      </div>
      <div data-tab="more" className={cn('min-h-0 flex-1 flex-col', tab === 'more' ? 'flex' : 'hidden')}>
        <MorePane active={tab === 'more'} />
      </div>
    </ToolShell>
  )
}
