import { Gauge } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'ai-stupid',
  path: '/tools/ai-stupid',
  title: 'AI 漂移监控',
  description: '监控主流 AI 模型的性能漂移与能力降级（数据来源 aistupidlevel.info）',
  icon: Gauge,
  category: 'ai',
  order: 20,
  defaultVisible: true,
}
