import { Network } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'net-tools',
  path: '/tools/net-tools',
  title: '网络探测',
  description: 'SSL 证书 / DNS / WHOIS / 端口 四合一',
  icon: Network,
  category: 'network',
  order: 31,
  // 默认关闭,想用就在首页「管理」里打开
  defaultVisible: false,
}
