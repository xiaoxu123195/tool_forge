import { TestTubeDiagonal } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'mcp-workbench',
  path: '/tools/mcp-workbench',
  title: 'MCP 工作台',
  sidebarTitle: 'MCP 工作台',
  description:
    '连上任意 MCP 服务器，按参数表单直接调用工具，看到原始 JSON-RPC 往返、错误码和调用历史',
  icon: TestTubeDiagonal,
  category: 'ai',
  order: 13,
}
