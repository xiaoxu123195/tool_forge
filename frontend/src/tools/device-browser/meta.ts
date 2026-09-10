import { FolderTree } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'device-browser',
  path: '/tools/device-browser',
  title: '真机数据浏览器',
  sidebarTitle: '真机浏览',
  description: '直接翻连着的手机:列目录、按名字搜、点开就按类型解析',
  icon: FolderTree,
  category: 'forensic',
  order: 15,
  defaultVisible: true,
}
