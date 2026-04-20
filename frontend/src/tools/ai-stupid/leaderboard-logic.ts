import type { aistupid } from '../../../wailsjs/go/models'
import { severityOf, type Severity } from './logic'

export type LeaderModel = aistupid.LeaderboardModel
export type HistoryPoint = aistupid.HistoryPoint

/** 把 LastUpdated 转成 "29m" / "1h" / "2d" 的相对时间 */
export function fmtUpdatedAgo(iso: string): string {
  if (!iso) return '—'
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return '—'
  const diffMs = Date.now() - t
  if (diffMs < 0) return 'just now'
  const m = Math.round(diffMs / 60_000)
  if (m < 1) return '<1m'
  if (m < 60) return `${m}m`
  const h = Math.round(m / 60)
  if (h < 24) return `${h}h`
  const d = Math.round(h / 24)
  return `${d}d`
}

/** 根据 leaderboard 的 status 字段归一化（warning / critical / stable 等） */
export function leaderSeverity(status: string): Severity {
  return severityOf(status)
}

/** 排行榜分数一般是 0-100，保持一致 */
export function fmtScore(v: number): string {
  if (!Number.isFinite(v)) return '—'
  return Math.round(v).toString()
}

/** 从 historyMap 取出 hourly 序列的最近 N 个点，用于 sparkline */
export function pickSparkline(
  points: HistoryPoint[] | undefined,
  limit = 25
): number[] {
  if (!points || points.length === 0) return []
  const hourly = points.filter((p) => (p.suite || '').toLowerCase() === 'hourly')
  const sorted = [...hourly].sort((a, b) => {
    const ta = new Date(a.timestamp).getTime()
    const tb = new Date(b.timestamp).getTime()
    return ta - tb
  })
  const tail = sorted.slice(-limit)
  return tail.map((p) => p.score)
}

/** trend 字符串归一化 */
export type TrendDir = 'up' | 'down' | 'stable'
export function trendDir(s: string): TrendDir {
  const v = (s || '').toLowerCase()
  if (v === 'up' || v === 'rising' || v === 'improving') return 'up'
  if (v === 'down' || v === 'falling' || v === 'degrading') return 'down'
  return 'stable'
}
