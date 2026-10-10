import { Gauge } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'ai-stupid',
  path: '/tools/ai-stupid',
  title: 'AI 监控',
  description: 'AI 模型排行榜与性能漂移检测（数据来源 aistupidlevel.info）',
  icon: Gauge,
  category: 'ai',
  order: 20,
  // 默认关闭,想用就在首页「管理」里打开
  defaultVisible: false,
}
