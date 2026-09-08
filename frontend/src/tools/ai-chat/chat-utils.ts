import {
  FileText,
  FileSpreadsheet,
  Presentation,
  FileCode2,
  FileType2,
} from 'lucide-react'
import { detectFileKind } from './file-parsers'

// ChatPane 及其拆出来的子组件共用的小工具。

/**
 * 对话正文列的宽度。消息列表和输入栏必须共用同一个值 —— 两处分别写死的话,
 * 改了一边另一边就错位,输入框和上面的气泡对不齐会很明显。
 *
 * 有上限而不是铺满:一行超过 90 字符左右,眼睛回行时容易串行。所以是"跟着窗口放宽,
 * 但不无限放宽" —— 窗口够大时给到 5xl(1024px),再大就只是留白变多。
 */
export const CHAT_COLUMN = 'mx-auto w-full max-w-3xl xl:max-w-4xl 2xl:max-w-5xl'

/** 根据文件类型挑一个图标 */
export function fileIcon(name: string) {
  const kind = detectFileKind(name)
  const ext = name.toLowerCase().split('.').pop() ?? ''
  if (kind === 'pdf') return FileType2
  if (ext === 'docx' || ext === 'doc') return FileText
  if (ext === 'xlsx' || ext === 'xls' || ext === 'csv') return FileSpreadsheet
  if (ext === 'pptx' || ext === 'ppt') return Presentation
  if (kind === 'text') return FileCode2
  return FileText
}

// Wails 的多返回值绑定在不同版本 / 不同调用路径下形态不一致(有时是数组,有时是
// {"0":..,"1":..},单返回值时干脆就是对象本身)。下面两个把这几种形态抹平,
// 免得每个调用点都写一遍解包。
export function pickFirst<T>(r: any): T | undefined {
  if (r == null) return undefined
  if (Array.isArray(r)) return r[0] as T
  if (r['0'] !== undefined) return r['0'] as T
  if (typeof r === 'object' && 'id' in r) return r as T
  return undefined
}

export function pickSecond(r: any): string {
  if (r == null) return ''
  if (Array.isArray(r)) return (r[1] as string) ?? ''
  if (r['1'] !== undefined) return (r['1'] as string) ?? ''
  return ''
}
