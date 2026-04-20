import { useEffect, useState } from 'react'
import { Languages, Loader2, RefreshCw } from 'lucide-react'
import { ToolShell } from '@/components/tool/ToolShell'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { meta } from './meta'
import { Leaderboard } from './Leaderboard'
import { Drift } from './Drift'
import { t, useLang } from './i18n'
import { fmtTimeLocal } from './logic'
import {
  FetchAIStupidDrift,
  FetchAIStupidLeaderboard,
} from '../../../wailsjs/go/main/App'
import type { aistupid } from '../../../wailsjs/go/models'

type Tab = 'leaderboard' | 'drift'

interface TabSlot<T> {
  data: T | null
  loading: boolean
  error: string
  loadedOnce: boolean
}

function emptySlot<T>(): TabSlot<T> {
  return { data: null, loading: false, error: '', loadedOnce: false }
}

export default function AIStupidTool() {
  const [tab, setTab] = useState<Tab>('leaderboard')
  const [lang, setLang] = useLang()

  const [leader, setLeader] = useState<TabSlot<aistupid.LeaderboardResponse>>(emptySlot)
  const [drift, setDrift] = useState<TabSlot<aistupid.DriftBatchResponse>>(emptySlot)

  const loadLeaderboard = async () => {
    setLeader((s) => ({ ...s, loading: true, error: '' }))
    try {
      const r = await FetchAIStupidLeaderboard()
      setLeader({ data: r, loading: false, error: '', loadedOnce: true })
    } catch (e) {
      setLeader((s) => ({
        ...s,
        loading: false,
        error: String(e instanceof Error ? e.message : e),
        loadedOnce: true,
      }))
    }
  }

  const loadDrift = async () => {
    setDrift((s) => ({ ...s, loading: true, error: '' }))
    try {
      const r = await FetchAIStupidDrift()
      setDrift({ data: r, loading: false, error: '', loadedOnce: true })
    } catch (e) {
      setDrift((s) => ({
        ...s,
        loading: false,
        error: String(e instanceof Error ? e.message : e),
        loadedOnce: true,
      }))
    }
  }

  // 切到某个 tab 且它还没加载过 → 首次拉取
  useEffect(() => {
    if (tab === 'leaderboard' && !leader.loadedOnce && !leader.loading) {
      loadLeaderboard()
    } else if (tab === 'drift' && !drift.loadedOnce && !drift.loading) {
      loadDrift()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab])

  const onRefresh = () => {
    if (tab === 'leaderboard') loadLeaderboard()
    else loadDrift()
  }

  const active =
    tab === 'leaderboard'
      ? { loading: leader.loading, updatedIso: leader.data?.fetchedAt }
      : { loading: drift.loading, updatedIso: drift.data?.meta?.timestamp }

  const toggleLang = () => setLang(lang === 'zh' ? 'en' : 'zh')

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      actions={
        <>
          <TabButton
            active={tab === 'leaderboard'}
            onClick={() => setTab('leaderboard')}
            label={t(lang, 'tab_leaderboard')}
          />
          <TabButton
            active={tab === 'drift'}
            onClick={() => setTab('drift')}
            label={t(lang, 'tab_drift')}
          />
          <div className="mx-1 h-5 w-px bg-border" />
          {active.updatedIso && (
            <span className="mr-1 text-xs text-muted-foreground">
              {t(lang, 'last_updated')} {fmtTimeLocal(active.updatedIso)}
            </span>
          )}
          <Button variant="ghost" size="sm" onClick={toggleLang} title="Language">
            <Languages className="h-3.5 w-3.5" />
            {t(lang, 'lang_toggle')}
          </Button>
          <Button
            variant="default"
            size="sm"
            onClick={onRefresh}
            disabled={active.loading}
          >
            {active.loading ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <RefreshCw className="h-3.5 w-3.5" />
            )}
            {t(lang, 'refresh')}
          </Button>
        </>
      }
    >
      {tab === 'leaderboard' ? (
        <Leaderboard
          lang={lang}
          data={leader.data}
          loading={leader.loading}
          errMsg={leader.error}
          onRetry={loadLeaderboard}
        />
      ) : (
        <Drift
          lang={lang}
          data={drift.data}
          loading={drift.loading}
          errMsg={drift.error}
          onRetry={loadDrift}
        />
      )}
    </ToolShell>
  )
}

function TabButton({
  active,
  onClick,
  label,
}: {
  active: boolean
  onClick: () => void
  label: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'rounded-md px-2.5 py-1 text-xs font-medium transition-colors',
        active
          ? 'bg-foreground/10 text-foreground'
          : 'text-muted-foreground hover:bg-muted/50 hover:text-foreground'
      )}
    >
      {label}
    </button>
  )
}
