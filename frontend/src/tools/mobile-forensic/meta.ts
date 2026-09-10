import { Smartphone } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'mobile-forensic',
  path: '/tools/mobile-forensic',
  title: '移动取证',
  description: 'Android 内置 adb 直连拉取应用数据，iOS 走 go-forensic',
  icon: Smartphone,
  category: 'forensic',
  order: 1,
  defaultVisible: true,
}
