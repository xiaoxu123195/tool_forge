import { Plug } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'provider-switch',
  path: '/tools/provider-switch',
  title: 'Provider 切换',
  description: '管理多个 Claude Code / Codex API 配置,一键写入 ~/.claude/settings.json 或 ~/.codex/config.toml',
  icon: Plug,
  category: 'ai',
  order: 13,
  // 默认关闭,想用就在首页「管理」里打开
  defaultVisible: false,
}
