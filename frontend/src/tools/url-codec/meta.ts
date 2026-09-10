import { Link2 } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'url-codec',
  path: '/tools/url-codec',
  title: 'URL 编解码',
  description: 'URL 组件编解码，处理中文与特殊字符',
  icon: Link2,
  category: 'codec',
  order: 20,
  // 默认关闭:一次性编解码,问 AI 更快。想用就在首页「管理」里打开
  defaultVisible: false,
}
