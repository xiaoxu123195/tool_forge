import { Image } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'base64-image',
  path: '/tools/base64-image',
  title: 'Base64 图片',
  description: '图片与 Base64 data URL 互转，支持拖拽与粘贴',
  icon: Image,
  category: 'codec',
  order: 40,
  // 默认关闭:一次性编解码,问 AI 更快。想用就在首页「管理」里打开
  defaultVisible: false,
}
