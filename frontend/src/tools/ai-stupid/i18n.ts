import { useEffect, useState } from 'react'

export type Lang = 'zh' | 'en'

const LANG_KEY = 'tool-forge:ai-stupid-lang'

export function useLang(): [Lang, (l: Lang) => void] {
  const [lang, setLangState] = useState<Lang>(() => {
    const saved = typeof localStorage !== 'undefined' ? localStorage.getItem(LANG_KEY) : null
    return saved === 'en' || saved === 'zh' ? saved : 'zh'
  })
  useEffect(() => {
    try {
      localStorage.setItem(LANG_KEY, lang)
    } catch {
      // ignore quota / denied
    }
  }, [lang])
  return [lang, setLangState]
}

type Dict = Record<string, string | ((n: number) => string)>

const ZH: Dict = {
  // 顶部
  tab_leaderboard: '排行榜',
  tab_drift: '漂移监控',
  refresh: '刷新',
  last_updated: '最后更新',
  lang_toggle: 'EN',
  // 通用状态
  stupid_meter: '整体分数',
  of_100: '/ 100',
  all_ok: '正常',
  // 趋势
  trend_up: '上升',
  trend_down: '下降',
  trend_stable: '持平',
  // Regime / Drift 严重度
  sev_stable: '稳定',
  sev_warning: '预警',
  sev_degraded: '劣化',
  sev_critical: '严重',
  sev_vola: '波动',
  sev_normal: '正常',
  // 排行榜列头
  col_rank: '排名',
  col_model: '模型',
  col_score: '分数',
  col_trend: '趋势',
  col_regime: '状态',
  col_updated: '更新',
  col_price: '输入/输出($/1M)',
  col_history: '7 日走势',
  // 漂移视图
  dim_correctness: '正确性',
  dim_spec: '规范度',
  dim_code_quality: '代码质量',
  dim_efficiency: '效率',
  dim_stability: '稳定性',
  dim_refusal: '拒答率',
  dim_recovery: '恢复力',
  // Drift 详情
  baseline: '基线',
  current: '当前',
  variance_24h: '24h 方差',
  status: '状态',
  performance_dimensions: '性能维度',
  recommendation: '建议',
  detected: '检测于',
  last_changed: '最后变化',
  expand_detail: '展开详情',
  collapse: '收起',
  // 状态
  loading_leaderboard: '正在加载排行榜...',
  loading_drift: '正在加载漂移数据...',
  fetch_failed: '加载失败',
  retry: '重试',
  empty: '暂无数据',
  prev_data_hint: '上次刷新失败：',
  prev_data_suffix: '（展示的是上一次数据）',
  // 时间
  just_now: '刚刚',
  hours_ago: (n: number) => `${n}h`,
  days_ago: (n: number) => `${n}d`,
}

const EN: Dict = {
  tab_leaderboard: 'Leaderboard',
  tab_drift: 'Drift',
  refresh: 'Refresh',
  last_updated: 'Updated',
  lang_toggle: '中',
  stupid_meter: 'Stupid Meter',
  of_100: '/ 100',
  all_ok: 'OK',
  trend_up: 'Up',
  trend_down: 'Down',
  trend_stable: 'Stable',
  sev_stable: 'STABLE',
  sev_warning: 'WARNING',
  sev_degraded: 'DEGRADED',
  sev_critical: 'CRITICAL',
  sev_vola: 'VOLA',
  sev_normal: 'NORMAL',
  col_rank: 'RK',
  col_model: 'Model',
  col_score: 'Score',
  col_trend: 'Trend',
  col_regime: 'Regime',
  col_updated: 'Updated',
  col_price: 'In/Out ($/1M)',
  col_history: '7-Day',
  dim_correctness: 'Correctness',
  dim_spec: 'Spec',
  dim_code_quality: 'Code Quality',
  dim_efficiency: 'Efficiency',
  dim_stability: 'Stability',
  dim_refusal: 'Refusal',
  dim_recovery: 'Recovery',
  baseline: 'Baseline',
  current: 'Current',
  variance_24h: 'Variance (24h)',
  status: 'Status',
  performance_dimensions: 'Performance Dimensions',
  recommendation: 'Recommendation',
  detected: 'Detected',
  last_changed: 'Last changed',
  expand_detail: 'Expand',
  collapse: 'Collapse',
  loading_leaderboard: 'Loading leaderboard...',
  loading_drift: 'Loading drift data...',
  fetch_failed: 'Fetch failed',
  retry: 'Retry',
  empty: 'No data',
  prev_data_hint: 'Last refresh failed: ',
  prev_data_suffix: ' (showing cached data)',
  just_now: 'just now',
  hours_ago: (n: number) => `${n}h`,
  days_ago: (n: number) => `${n}d`,
}

export function t(lang: Lang, key: string): string {
  const dict = lang === 'zh' ? ZH : EN
  const v = dict[key]
  return typeof v === 'string' ? v : key
}

/** hours_ago / days_ago 这种带参的词条 */
export function tf(lang: Lang, key: 'hours_ago' | 'days_ago', n: number): string {
  const dict = lang === 'zh' ? ZH : EN
  const f = dict[key]
  return typeof f === 'function' ? (f as (n: number) => string)(n) : String(n)
}
