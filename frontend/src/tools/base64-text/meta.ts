import { Binary } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'base64-text',
  path: '/tools/base64-text',
  title: 'Base64 文本',
  description: '文本与 Base64 字符串互相编解码，自动识别方向',
  icon: Binary,
  category: 'codec',
  order: 10,
  // 默认关闭:一次性编解码,问 AI 更快。想用就在首页「管理」里打开
  defaultVisible: false,
}
