import { Palette } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'color',
  path: '/tools/color',
  title: '颜色转换',
  description: 'HEX / RGB / HSL 格式互转 + 色板预览',
  icon: Palette,
  category: 'dev',
  order: 20,
  // 默认关闭:一次性换算,问 AI 更快。想用就在首页「管理」里打开
  defaultVisible: false,
}
