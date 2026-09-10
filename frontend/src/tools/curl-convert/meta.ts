import { TerminalSquare } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'curl-convert',
  path: '/tools/curl-convert',
  title: 'cURL 转代码',
  description: 'cURL 命令转 JS / Python / Go / Java / PHP 等语言',
  icon: TerminalSquare,
  category: 'network',
  order: 10,
  // 默认关闭:一次性转换,AI 还能顺带适配你的项目风格。想用就在首页「管理」里打开
  defaultVisible: false,
}
