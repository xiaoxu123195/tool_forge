import { ClipboardList } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'clipboard',
  path: '/tools/clipboard',
  title: '剪贴板',
  description: '记录复制历史,支持文本、图片、置顶与一键回写。Ctrl+Shift+V 唤起',
  icon: ClipboardList,
  category: 'system',
  order: 10,
  defaultVisible: true,
}
