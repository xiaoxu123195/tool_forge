import { HardDrive } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'disk-clean',
  path: '/tools/disk-clean',
  title: '磁盘清理',
  sidebarTitle: '磁盘清理',
  description:
    '清缓存，按文件和按目录找出谁占了空间，找重复文件、空文件夹和无效快捷方式。系统目录删不掉，用户文件默认进回收站',
  icon: HardDrive,
  category: 'system',
  order: 20,
}
