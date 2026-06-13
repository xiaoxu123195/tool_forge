import { Images } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'image-merge',
  path: '/tools/image-merge',
  title: '图片拼接',
  description: '多图竖排 / 横排 / 网格拼接，保比不放大、可调间距背景圆角，导出 PNG/JPEG/WebP',
  sidebarTitle: '图片拼接',
  icon: Images,
  category: 'gen',
  order: 45,
  defaultVisible: true,
}
