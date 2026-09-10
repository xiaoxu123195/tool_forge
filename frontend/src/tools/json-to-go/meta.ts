import { Braces } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'json-to-go',
  path: '/tools/json-to-go',
  title: 'JSON 转 Go',
  description: 'JSON 推断类型生成 Go struct，自动补 json tag',
  icon: Braces,
  category: 'data',
  order: 10,
  // 默认关闭:一次性生成,AI 还能顺带把字段名改合适。想用就在首页「管理」里打开
  defaultVisible: false,
}
