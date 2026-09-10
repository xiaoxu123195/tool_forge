import { Clock } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'timestamp',
  path: '/tools/timestamp',
  title: '时间戳转换',
  description: 'Unix 时间戳与日期互转，支持秒 / 毫秒',
  icon: Clock,
  category: 'time',
  order: 10,
  // 默认关闭:一次性换算,问 AI 更快。想用就在首页「管理」里打开
  defaultVisible: false,
}
