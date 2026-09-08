import { useEffect, useMemo, useState } from 'react'
import { GripVertical, Plus, Search } from 'lucide-react'
import {
  ListAIProviders,
  SaveAIProvider,
  DeleteAIProvider,
  ReorderAIProviders,
  ToggleAIProvider,
} from '../../../../wailsjs/go/main/App'
import type { Provider } from '@/tools/ai-chat/types'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/components/ui/confirm'
import { ProviderDetail } from './ProviderDetail'
import { ProviderAvatar } from './ProviderAvatar'
import { AddProviderDialog } from './AddProviderDialog'
import { TestModelDialog } from './TestModelDialog'
import { ModelManageDrawer } from './ModelManageDrawer'

export function ProvidersTab() {
  const dialog = useConfirm()
  const [providers, setProviders] = useState<Provider[]>([])
  const [activeId, setActiveId] = useState<string>('')
  const [filter, setFilter] = useState('')
  const [adding, setAdding] = useState(false)
  const [testingProvider, setTestingProvider] = useState<Provider | null>(null)
  const [managingProvider, setManagingProvider] = useState<Provider | null>(null)
  // 拖动排序:dragId 是被拖的那条,overId 是当前悬停的目标(用来画插入位置)
  const [dragId, setDragId] = useState('')
  const [overId, setOverId] = useState('')

  // 列出来 + 同步选中:每次都基于服务端最新数据来决定 activeId
  // 用 prevActive 作显式参数,避免闭包里的 activeId 在 await 后是旧值
  const reload = async (preferId?: string) => {
    const list = ((await ListAIProviders()) ?? []) as unknown as Provider[]
    setProviders(list)
    setActiveId((prev) => {
      const target = preferId ?? prev
      if (target && list.find((p) => p.id === target)) return target
      return list[0]?.id ?? ''
    })
  }

  useEffect(() => {
    void reload()
  }, [])

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (!q) return providers
    return providers.filter(
      (p) =>
        p.name.toLowerCase().includes(q) ||
        p.baseUrl.toLowerCase().includes(q),
    )
  }, [providers, filter])

  const active = providers.find((p) => p.id === activeId) ?? null

  const onAddConfirm = async (r: {
    name: string
    type: Provider['type']
    logo: string
    baseUrl: string
  }) => {
    // Wails 只把第一个返回值交给 JS(第二个是 error 时转成 reject),
    // 所以这里拿到的直接就是 Provider 对象 —— 以前按 [值, 错误] 解包会得到 undefined,
    // reload 收不到新供应商的 id,新建完就停在原来那条上
    let saved: Provider
    try {
      saved = (await SaveAIProvider({
        id: '',
        name: r.name,
        type: r.type,
        logo: r.logo,
        baseUrl: r.baseUrl,
        apiKey: '',
        enabled: true,
        models: [],
        isSystem: false,
        createdAt: 0,
        updatedAt: 0,
      } as unknown as never)) as unknown as Provider
    } catch (e) {
      await dialog({ title: '添加失败', message: String(e), confirmLabel: '知道了' })
      return
    }
    // 先刷新列表(同步拿到新 provider),再关弹窗 —— 顺序反过来会让
    // setAdding(false) 卸载 dialog,后续 setProviders 偶尔不触发重渲染
    await reload(saved?.id)
    setAdding(false)
  }

  // 搜索状态下不许拖:界面上是过滤后的子集,按它重排会让看不见的那些顺序错乱
  const canDrag = filter.trim() === ''

  const onDrop = async (targetId: string) => {
    const from = providers.findIndex((p) => p.id === dragId)
    const to = providers.findIndex((p) => p.id === targetId)
    setDragId('')
    setOverId('')
    if (from < 0 || to < 0 || from === to) return
    const next = [...providers]
    const [moved] = next.splice(from, 1)
    next.splice(to, 0, moved)
    setProviders(next) // 先动界面,拖放要跟手
    try {
      await ReorderAIProviders(next.map((p) => p.id))
    } catch (e) {
      await dialog({ title: '排序保存失败', message: String(e), confirmLabel: '知道了' })
      await reload()
    }
  }

  const onToggle = async (p: Provider, next: boolean) => {
    const err = (await ToggleAIProvider(p.id, next)) as unknown as string
    if (err) {
      await dialog({ title: '切换失败', message: err, confirmLabel: '知道了' })
      return
    }
    await reload(p.id)
  }

  const onDelete = async (p: Provider) => {
    const ok = await dialog({
      title: '删除供应商',
      message: p.isSystem
        ? `「${p.name}」是系统内置预设,删除后下次启动会自动重建。确认删除?`
        : `确认删除供应商「${p.name}」?该操作不可撤销。`,
      danger: true,
      confirmLabel: '删除',
    })
    if (!ok) return
    const err = (await DeleteAIProvider(p.id)) as unknown as string
    if (err) {
      await dialog({ title: '删除失败', message: err, confirmLabel: '知道了' })
      return
    }
    await reload()
  }

  return (
    <div className="flex min-h-0 flex-1 overflow-hidden rounded-lg border border-border bg-card">
      {/* Left: provider list */}
      <aside className="flex w-[260px] shrink-0 flex-col border-r border-border">
        <div className="p-3">
          <div className="relative">
            <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="搜索供应商..."
              className="h-8 w-full rounded-md border border-input bg-background pl-7 pr-2 text-xs outline-none focus:ring-1 focus:ring-ring"
            />
          </div>
        </div>
        <ul className="flex-1 overflow-auto px-2 pb-2">
          {filtered.length === 0 ? (
            <li className="px-3 py-6 text-center text-xs text-muted-foreground">
              没有供应商
            </li>
          ) : (
            filtered.map((p) => (
              <li
                key={p.id}
                draggable={canDrag}
                onDragStart={() => setDragId(p.id)}
                onDragEnd={() => {
                  setDragId('')
                  setOverId('')
                }}
                onDragOver={(e) => {
                  if (!canDrag || !dragId) return
                  e.preventDefault()
                  if (overId !== p.id) setOverId(p.id)
                }}
                onDrop={(e) => {
                  e.preventDefault()
                  void onDrop(p.id)
                }}
                onClick={() => setActiveId(p.id)}
                className={cn(
                  'group/item flex h-10 cursor-pointer items-center gap-1.5 rounded-md px-2 text-sm transition-colors',
                  activeId === p.id ? 'bg-info/15 text-info' : 'hover:bg-secondary',
                  dragId === p.id && 'opacity-40',
                  overId === p.id && dragId && dragId !== p.id && 'ring-1 ring-info',
                )}
              >
                {canDrag && (
                  <GripVertical className="h-3.5 w-3.5 shrink-0 cursor-grab text-muted-foreground opacity-0 transition-opacity group-hover/item:opacity-100 active:cursor-grabbing" />
                )}
                <ProviderAvatar logo={p.logo} name={p.name} size={24} />
                <span className="flex-1 truncate">{p.name}</span>
                <span
                  className={cn(
                    'shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium',
                    p.enabled
                      ? 'bg-success/15 text-success'
                      : 'bg-secondary text-muted-foreground',
                  )}
                >
                  {p.enabled ? 'ON' : 'OFF'}
                </span>
              </li>
            ))
          )}
        </ul>
        <div className="border-t border-border p-2">
          <Button onClick={() => setAdding(true)} variant="outline" size="sm" className="w-full">
            <Plus className="h-3.5 w-3.5" />
            添加供应商
          </Button>
        </div>
      </aside>

      {/* Right: detail */}
      <div className="min-w-0 flex-1 overflow-auto">
        {active ? (
          <ProviderDetail
            key={active.id}
            provider={active}
            onSaved={() => reload(active.id)}
            onToggle={(next) => onToggle(active, next)}
            onDelete={() => onDelete(active)}
            onTest={() => setTestingProvider(active)}
            onManage={() => setManagingProvider(active)}
          />
        ) : (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            从左侧选一个供应商,或点「添加供应商」开始
          </div>
        )}
      </div>

      {adding && (
        <AddProviderDialog
          onCancel={() => setAdding(false)}
          onConfirm={onAddConfirm}
        />
      )}
      {testingProvider && (
        <TestModelDialog
          provider={testingProvider}
          onClose={() => setTestingProvider(null)}
        />
      )}
      {managingProvider && (
        <ModelManageDrawer
          provider={managingProvider}
          onClose={() => {
            setManagingProvider(null)
            void reload(managingProvider.id)
          }}
        />
      )}
    </div>
  )
}
