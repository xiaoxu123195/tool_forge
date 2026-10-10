import { Waypoints } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'llm-proxy',
  path: '/tools/llm-proxy',
  title: 'LLM 代理日志',
  sidebarTitle: 'LLM 代理',
  description: '把 base_url 指到本代理,透明转发并记录每次请求/响应(含 SSE),可复盘与重放',
  icon: Waypoints,
  category: 'ai',
  order: 39,
  // 默认关闭,想用就在首页「管理」里打开
  defaultVisible: false,
}
