import { Braces } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'protobuf',
  path: '/tools/protobuf',
  title: 'Protobuf 编解码',
  description: '无需 .proto 递归裸解析(强于 protoc --decode_raw);也支持 .proto/.pb 按名编解码',
  icon: Braces,
  category: 'codec',
  order: 15,
  defaultVisible: true,
}
