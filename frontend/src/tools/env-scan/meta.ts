import { Radar } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'env-scan',
  path: '/tools/env-scan',
  title: '开发环境',
  description: '扫描本机已安装的语言、包管理器、AI CLI 与常用工具链',
  icon: Radar,
  category: 'system',
  order: 30,
  defaultVisible: true,
}
