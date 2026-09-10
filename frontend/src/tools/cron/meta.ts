import { Clock4 } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'cron',
  path: '/tools/cron',
  title: 'Cron 表达式',
  description: '解析 cron、预览下 N 次触发时间、可视化时间轴',
  icon: Clock4,
  category: 'time',
  order: 13,
  // 默认关闭:解释 cron 表达式 AI 讲得更清楚。想用就在首页「管理」里打开
  defaultVisible: false,
}
