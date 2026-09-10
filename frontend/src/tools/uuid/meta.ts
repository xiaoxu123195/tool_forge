import { Fingerprint } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'uuid',
  path: '/tools/uuid',
  title: 'UUID 生成',
  description: 'UUID v4 批量生成，支持大小写与去短横线',
  icon: Fingerprint,
  category: 'gen',
  order: 10,
  // 默认关闭:一行命令 / 一句话就能生成。想用就在首页「管理」里打开
  defaultVisible: false,
}
