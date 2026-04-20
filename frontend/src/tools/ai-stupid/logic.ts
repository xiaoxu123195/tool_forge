import type { aistupid } from '../../../wailsjs/go/models'

export type Model = aistupid.ModelDrift
export type Axes = aistupid.AxisBundle
export type Axis = aistupid.AxisMetric

/** 7 个评分维度键；不用 keyof AxisBundle，避免把 wails 生成的辅助方法也纳入 */
export type AxisKey =
  | 'correctness'
  | 'spec'
  | 'codeQuality'
  | 'efficiency'
  | 'stability'
  | 'refusal'
  | 'recovery'

/** 按模型名前缀推断厂商，用于徽章与分组 */
export function vendorOf(modelName: string): { key: string; label: string } {
  const n = modelName.toLowerCase()
  if (n.startsWith('claude')) return { key: 'anthropic', label: 'Anthropic' }
  if (n.startsWith('gpt')) return { key: 'openai', label: 'OpenAI' }
  if (n.startsWith('gemini')) return { key: 'google', label: 'Google' }
  if (n.startsWith('grok')) return { key: 'xai', label: 'xAI' }
  if (n.startsWith('deepseek')) return { key: 'deepseek', label: 'DeepSeek' }
  if (n.startsWith('glm')) return { key: 'glm', label: 'GLM' }
  if (n.startsWith('kimi')) return { key: 'kimi', label: 'Kimi' }
  return { key: 'other', label: 'Other' }
}

/** 把 driftStatus / status 字符串归一化到四档 */
export type Severity = 'stable' | 'warning' | 'degraded' | 'critical'

export function severityOf(status: string): Severity {
  const s = (status || '').toLowerCase()
  if (s.includes('critical')) return 'critical'
  if (s.includes('degrad')) return 'degraded'
  if (s.includes('warn')) return 'warning'
  return 'stable'
}

/** 状态对应的 Tailwind 颜色类（文字 / 背景 / 边框） */
export const severityTheme: Record<Severity, {
  text: string
  bg: string
  bar: string
  ring: string
  label: string
}> = {
  stable:   { text: 'text-emerald-500', bg: 'bg-emerald-500/10', bar: 'bg-emerald-500', ring: 'ring-emerald-500/30', label: 'STABLE' },
  warning:  { text: 'text-amber-500',   bg: 'bg-amber-500/10',   bar: 'bg-amber-500',   ring: 'ring-amber-500/30',   label: 'WARNING' },
  degraded: { text: 'text-orange-500',  bg: 'bg-orange-500/10',  bar: 'bg-orange-500',  ring: 'ring-orange-500/30',  label: 'DEGRADED' },
  critical: { text: 'text-red-500',     bg: 'bg-red-500/10',     bar: 'bg-red-500',     ring: 'ring-red-500/30',     label: 'CRITICAL' },
}

/** 进度条按值分档着色，和状态解耦，单纯表达"分数高低" */
export function scoreBarColor(value: number): string {
  if (value >= 70) return 'bg-emerald-500'
  if (value >= 50) return 'bg-amber-500'
  if (value >= 30) return 'bg-orange-500'
  return 'bg-red-500'
}

export function clampPct(v: number): number {
  if (!Number.isFinite(v)) return 0
  if (v < 0) return 0
  if (v > 100) return 100
  return v
}

/** 轴值后端是 0-1 范围，这里统一转 0-100 给 UI 用 */
export function axisPct(a?: Axis | null): number {
  const v = a?.value
  if (v == null || !Number.isFinite(v)) return 0
  const pct = v * 100
  if (pct < 0) return 0
  if (pct > 100) return 100
  return pct
}

export function fmtCI(ci?: number[] | null): string {
  if (!ci || ci.length < 2) return ''
  const half = Math.abs(ci[1] - ci[0]) / 2
  if (!Number.isFinite(half)) return ''
  return `±${half.toFixed(0)}`
}

/** "18d" / "2d" / "3h" / "just now" */
export function fmtLastChanged(hours: number): string {
  if (!Number.isFinite(hours) || hours <= 0) return 'just now'
  if (hours < 1) return '<1h'
  if (hours < 24) return `${Math.round(hours)}h`
  const d = Math.round(hours / 24)
  return `${d}d`
}

export function fmtTimeLocal(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

/** 7 个维度的展示顺序与显示名 */
export const AXIS_ORDER: AxisKey[] = [
  'correctness',
  'spec',
  'codeQuality',
  'efficiency',
  'stability',
  'refusal',
  'recovery',
]

export const AXIS_LABEL: Record<AxisKey, string> = {
  correctness: 'Correctness',
  spec: 'Spec',
  codeQuality: 'Code Quality',
  efficiency: 'Efficiency',
  stability: 'Stability',
  refusal: 'Refusal',
  recovery: 'Recovery',
}

/** 总览：按厂商聚合出 OK / WARN 标签 */
export interface VendorSummary {
  key: string
  label: string
  total: number
  stable: number
  worst: Severity
}

export function summarizeVendors(models: Model[]): VendorSummary[] {
  const bySeverityRank: Record<Severity, number> = {
    stable: 0, warning: 1, degraded: 2, critical: 3,
  }
  const map = new Map<string, VendorSummary>()
  for (const m of models) {
    const v = vendorOf(m.modelName)
    const sev = severityOf(m.driftStatus)
    let row = map.get(v.key)
    if (!row) {
      row = { key: v.key, label: v.label, total: 0, stable: 0, worst: 'stable' }
      map.set(v.key, row)
    }
    row.total++
    if (sev === 'stable') row.stable++
    if (bySeverityRank[sev] > bySeverityRank[row.worst]) row.worst = sev
  }
  return Array.from(map.values()).sort((a, b) => a.label.localeCompare(b.label))
}

/** 整体 Stupid Meter：取所有 currentScore 的均值 */
export function overallScore(models: Model[]): number {
  if (!models.length) return 0
  const sum = models.reduce((acc, m) => acc + (m.currentScore || 0), 0)
  return Math.round(sum / models.length)
}
