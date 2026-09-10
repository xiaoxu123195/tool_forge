import { GitCompare } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'text-diff',
  path: '/tools/text-diff',
  title: '文本对比',
  description: '两段文本逐行差异对比，支持忽略空白',
  icon: GitCompare,
  category: 'text',
  order: 10,
  // 默认关闭:在编辑器 / Claude Code 里对比更顺手。想用就在首页「管理」里打开
  defaultVisible: false,
}
