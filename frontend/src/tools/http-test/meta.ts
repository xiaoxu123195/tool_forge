import { Send } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'http-test',
  path: '/tools/http-test',
  title: 'HTTP 测试',
  description: '类 Postman 的简易 HTTP 请求测试器,支持历史记录',
  icon: Send,
  category: 'network',
  order: 30,
  defaultVisible: true,
}
