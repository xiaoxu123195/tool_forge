import { Type } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'unicode-codec',
  path: '/tools/unicode-codec',
  title: 'Unicode 转义',
  description: '中文与 \\uXXXX 转义字符互转',
  icon: Type,
  category: 'codec',
  order: 30,
  // 默认关闭:一次性编解码,问 AI 更快。想用就在首页「管理」里打开
  defaultVisible: false,
}
