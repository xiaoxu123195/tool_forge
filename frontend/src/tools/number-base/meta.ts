import { Hash } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'number-base',
  path: '/tools/number-base',
  title: '进制转换',
  description: '二进制 / 八进制 / 十进制 / 十六进制互转',
  icon: Hash,
  category: 'dev',
  order: 10,
  // 默认关闭:一次性换算,问 AI 更快。想用就在首页「管理」里打开
  defaultVisible: false,
}
