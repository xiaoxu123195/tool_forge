import { useCallback, useEffect, useRef, useState } from 'react'
import { Play, RefreshCw, Settings2, Square, Trash2 } from 'lucide-react'
import { ToolShell } from '@/components/tool/ToolShell'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import {
  ClearLLMProxyLogs,
  DeleteLLMProxyLog,
  GetLLMProxyConfig,
  GetLLMProxyLog,
  GetLLMProxyStatus,
  ListLLMProxyLogs,
  UpdateLLMProxyConfig,
} from '../../../wailsjs/go/main/App'
import { llmproxy } from '../../../wailsjs/go/models'
import { meta } from './meta'
import { Settings } from './Settings'
import { LogList } from './LogList'
import { LogDetail } from './LogDetail'
import { Dashboard } from './Dashboard'
import { Modal } from './Modal'
import { CopyMenu } from './CopyMenu'

const emptyQuery: llmproxy.LogQuery = {
  upstream: '',
  method: '',
  status: '',
  search: '',
  limit: 100,
  offset: 0,
}

export default function LlmProxy() {
  const [config, setConfig] = useState<llmproxy.Config | null>(null)
  const [status, setStatus] = useState<llmproxy.Status>({ running: false, addr: '' })
  const [page, setPage] = useState<llmproxy.LogPage>(llmproxy.LogPage.createFrom({ items: [], total: 0 }))
  const [query, setQueryState] = useState<llmproxy.LogQuery>(emptyQuery)
  const [detail, setDetail] = useState<llmproxy.LogDetail | null>(null)
  const [showSettings, setShowSettings] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [view, setView] = useState<'logs' | 'dash'>('logs')
  const [dashReload, setDashReload] = useState(0)

  const queryRef = useRef(query)
  queryRef.current = query

  const loadLogs = useCallback(async () => {
    try {
      const p = await ListLLMProxyLogs(queryRef.current)
      if (p) setPage(p)
    } catch {
      /* ignore transient */
    }
  }, [])

  const loadStatus = useCallback(async () => {
    try {
      setStatus(await GetLLMProxyStatus())
    } catch {
      /* ignore */
    }
  }, [])

  // 初始化
  useEffect(() => {
    GetLLMProxyConfig().then(setConfig).catch(() => {})
    loadStatus()
    loadLogs()
  }, [loadLogs, loadStatus])

  // 轮询状态 + 日志
  useEffect(() => {
    const t = window.setInterval(() => {
      loadStatus()
      loadLogs()
    }, 2500)
    return () => window.clearInterval(t)
  }, [loadLogs, loadStatus])

  // 过滤变化即刷新
  useEffect(() => {
    loadLogs()
  }, [query, loadLogs])

  const setQuery = (patch: Partial<llmproxy.LogQuery>) => setQueryState((q) => ({ ...q, ...patch, offset: 0 }))

  const saveConfig = async (cfg: llmproxy.Config) => {
    setSaving(true)
    setError('')
    try {
      await UpdateLLMProxyConfig(cfg)
      setConfig(cfg)
      await loadStatus()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  const toggleRunning = () => {
    if (!config) return
    saveConfig({ ...config, enabled: !config.enabled } as llmproxy.Config)
  }

  const openDetail = async (id: number) => {
    try {
      const d = await GetLLMProxyLog(id)
      if (d) setDetail(d)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  const removeLog = async (id: number) => {
    await DeleteLLMProxyLog(id)
    if (detail?.entry.id === id) setDetail(null)
    loadLogs()
  }

  const clearAll = async () => {
    await ClearLLMProxyLogs()
    setDetail(null)
    loadLogs()
  }

  const port = config?.port ?? 8788
  const proxyBase = `http://127.0.0.1:${port}`
  const upstreams = (config?.upstreams ?? []).map((u) => u.name)
  const enabledAddrs = (config?.upstreams ?? [])
    .filter((u) => !u.disabled)
    .map((u) => ({ label: u.name, value: `${proxyBase}/${u.name}` }))

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      actions={
        <div className="flex items-center gap-1.5">
          <Button variant="outline" size="sm" onClick={() => setShowSettings((v) => !v)}>
            <Settings2 className="h-3.5 w-3.5" /> 设置
          </Button>
          <Button variant="ghost" size="sm" className="w-8 px-0" title="刷新" onClick={loadLogs}>
            <RefreshCw className="h-3.5 w-3.5" />
          </Button>
          <Button variant="ghost" size="sm" onClick={clearAll} disabled={page.total === 0} title="清空全部日志">
            <Trash2 className="h-3.5 w-3.5" /> 清空
          </Button>
        </div>
      }
    >
      <div className="mx-auto max-w-6xl space-y-3">
        {/* 视图切换 */}
        <div className="inline-flex items-center rounded-md border border-border bg-background p-0.5">
          {(['logs', 'dash'] as const).map((v) => (
            <button
              key={v}
              onClick={() => {
                setView(v)
                if (v === 'dash') {
                  setDetail(null)
                  setDashReload((n) => n + 1)
                }
              }}
              className={cn(
                'rounded-sm px-3 py-1 text-xs font-medium transition-colors',
                view === v ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {v === 'logs' ? '日志' : '概览'}
            </button>
          ))}
        </div>

        {/* 控制条 */}
        <div className="flex flex-wrap items-center gap-3 rounded-xl border border-border bg-card px-4 py-3">
          <span
            className={cn(
              'inline-flex items-center gap-2 rounded-full px-2.5 py-1 text-xs font-medium',
              status.running
                ? 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
                : 'bg-secondary text-muted-foreground'
            )}
          >
            <span className={cn('h-2 w-2 rounded-full', status.running ? 'animate-pulse bg-emerald-500' : 'bg-muted-foreground/50')} />
            {status.running ? `运行中 · :${port}` : '已停止'}
          </span>
          <Button size="sm" onClick={toggleRunning} disabled={saving || !config} className="font-semibold">
            {config?.enabled ? <Square className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />}
            {config?.enabled ? '停止' : '启动'}
          </Button>

          {status.running && (
            <div className="flex items-center gap-2">
              <span className="text-[11px] text-muted-foreground">客户端 base_url →</span>
              <CopyMenu label="接入地址" items={enabledAddrs} />
            </div>
          )}

          <span className="ml-auto text-[11px] text-muted-foreground">
            {page.total > 0 ? `${page.total} 条记录` : ''}
          </span>
        </div>

        {(error || status.error) && (
          <div className="rounded-lg border border-red-500/40 bg-red-500/5 p-2.5 text-xs text-red-600 dark:text-red-400">
            {error || status.error}
          </div>
        )}
        {status.lastLogError && (
          <div className="rounded-lg border border-amber-500/40 bg-amber-500/5 p-2.5 text-xs text-amber-600 dark:text-amber-400">
            写日志失败:{status.lastLogError}(常见原因:开了多个 App 实例同时占用日志库)
          </div>
        )}

        {view === 'logs' ? (
          <LogList
            page={page}
            query={query}
            setQuery={setQuery}
            upstreams={upstreams}
            selectedId={detail?.entry.id ?? null}
            onSelect={openDetail}
          />
        ) : (
          <Dashboard reloadToken={dashReload} />
        )}
      </div>

      {detail && (
        <LogDetail
          detail={detail}
          proxyBase={proxyBase}
          onClose={() => setDetail(null)}
          onDelete={removeLog}
          onReplayed={(d) => {
            setDetail(d)
            loadLogs()
          }}
        />
      )}

      <Modal open={showSettings} onClose={() => setShowSettings(false)} title="LLM 代理设置">
        {config && <Settings config={config} proxyBase={proxyBase} onSave={saveConfig} saving={saving} />}
      </Modal>
    </ToolShell>
  )
}
