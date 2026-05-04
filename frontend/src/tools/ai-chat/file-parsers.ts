/**
 * 把用户上传的 File 转成可发给后端的 FileBlock。
 *
 * 路线:
 *   - 文本/代码:直接读字符串 → FileBlock.text
 *   - .docx:mammoth 提取纯文本
 *   - .xlsx:SheetJS 转 markdown 风格表格
 *   - .pptx:JSZip 解析 ppt/slides/*.xml 提取 <a:t> 文本
 *   - .pdf:base64 → FileBlock.data(由后端原生发送或 PDF 文本提取兜底)
 *
 * 不支持的扩展(.doc/.xls/.ppt 老格式)→ 抛错,提示用户另存为新格式
 */
import type { FileBlock } from './types'

export const MAX_PDF_BYTES = 20 * 1024 * 1024 // 20 MB
export const MAX_OFFICE_BYTES = 15 * 1024 * 1024 // 15 MB
export const MAX_TEXT_BYTES = 2 * 1024 * 1024 // 2 MB
export const MAX_FILES_PER_MESSAGE = 4

/** 文本/代码扩展白名单 — 直接 UTF-8 读取 */
const TEXT_EXTS = new Set([
  // 通用文本
  'txt', 'md', 'markdown', 'rst', 'log', 'csv', 'tsv',
  // 配置 / 数据
  'json', 'yaml', 'yml', 'toml', 'ini', 'env', 'conf', 'cfg', 'properties',
  // Web
  'html', 'htm', 'css', 'scss', 'sass', 'less', 'xml', 'svg',
  // 代码
  'go', 'mod', 'sum',
  'js', 'jsx', 'ts', 'tsx', 'mjs', 'cjs',
  'py', 'pyi', 'rb', 'php', 'pl',
  'rs', 'c', 'cc', 'cpp', 'cxx', 'h', 'hh', 'hpp',
  'java', 'kt', 'kts', 'swift', 'scala', 'groovy',
  'cs', 'fs', 'fsx', 'vb',
  'lua', 'r', 'jl', 'dart', 'ex', 'exs', 'erl', 'hs', 'ml', 'mli',
  'clj', 'cljs', 'cljc', 'edn',
  'sh', 'bash', 'zsh', 'fish', 'ps1', 'bat', 'cmd',
  'sql', 'graphql', 'gql', 'proto', 'thrift',
  'dockerfile', 'makefile', 'cmake',
  'tex', 'bib',
  'gitignore', 'gitattributes', 'editorconfig',
  'lock',
])

const OFFICE_EXTS = new Set(['docx', 'xlsx', 'pptx'])
const PDF_EXTS = new Set(['pdf'])
const LEGACY_OFFICE_EXTS = new Set(['doc', 'xls', 'ppt'])

export function detectFileKind(name: string): 'image' | 'pdf' | 'office' | 'text' | 'legacy' | 'unknown' {
  const lower = name.toLowerCase()
  // 没有扩展名时按 Makefile / Dockerfile 这类按全名识别
  const dot = lower.lastIndexOf('.')
  const ext = dot >= 0 ? lower.slice(dot + 1) : lower
  if (PDF_EXTS.has(ext)) return 'pdf'
  if (OFFICE_EXTS.has(ext)) return 'office'
  if (LEGACY_OFFICE_EXTS.has(ext)) return 'legacy'
  if (TEXT_EXTS.has(ext)) return 'text'
  return 'unknown'
}

export function isImageFile(file: File): boolean {
  return file.type.startsWith('image/')
}

/** 把 File 转 FileBlock。失败时抛错(由调用方提示用户) */
export async function fileToFileBlock(file: File): Promise<FileBlock> {
  const kind = detectFileKind(file.name)
  const base = {
    name: file.name,
    mimeType: file.type || guessMimeFromExt(file.name),
    sizeBytes: file.size,
  }

  if (kind === 'legacy') {
    throw new Error(`不支持旧格式 ${file.name},请另存为 .docx / .xlsx / .pptx`)
  }
  if (kind === 'pdf') {
    if (file.size > MAX_PDF_BYTES) {
      throw new Error(`PDF 文件超过 20 MB,请压缩后再上传`)
    }
    const data = await fileToBase64(file)
    return { ...base, mimeType: 'application/pdf', data }
  }
  if (kind === 'office') {
    if (file.size > MAX_OFFICE_BYTES) {
      throw new Error(`文件超过 15 MB`)
    }
    const text = await extractOfficeText(file)
    return { ...base, text }
  }
  if (kind === 'text' || kind === 'unknown') {
    if (file.size > MAX_TEXT_BYTES) {
      throw new Error(`文本文件超过 2 MB`)
    }
    // unknown 也尝试当文本读 — 大多数情况下用户拖的是配置/代码文件,UTF-8 都能读
    const text = await file.text()
    return { ...base, text }
  }
  throw new Error(`暂不支持文件类型: ${file.name}`)
}

/** File → base64(无 data: 前缀) */
function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const dataUrl = reader.result as string
      const comma = dataUrl.indexOf(',')
      resolve(comma >= 0 ? dataUrl.slice(comma + 1) : '')
    }
    reader.onerror = () => reject(new Error('读取文件失败'))
    reader.readAsDataURL(file)
  })
}

async function extractOfficeText(file: File): Promise<string> {
  const lower = file.name.toLowerCase()
  if (lower.endsWith('.docx')) return extractDocxText(file)
  if (lower.endsWith('.xlsx')) return extractXlsxText(file)
  if (lower.endsWith('.pptx')) return extractPptxText(file)
  return ''
}

async function extractDocxText(file: File): Promise<string> {
  // mammoth 主入口在浏览器里也能跑(extractRawText 不依赖 fs)
  const mammoth: any = await import('mammoth')
  const arrayBuffer = await file.arrayBuffer()
  const result = await mammoth.extractRawText({ arrayBuffer })
  return (result.value ?? '').trim()
}

async function extractXlsxText(file: File): Promise<string> {
  const XLSX = await import('xlsx')
  const arrayBuffer = await file.arrayBuffer()
  const wb = XLSX.read(arrayBuffer, { type: 'array' })
  const out: string[] = []
  for (const sheetName of wb.SheetNames) {
    const sheet = wb.Sheets[sheetName]
    const csv = XLSX.utils.sheet_to_csv(sheet, { strip: true })
    if (csv.trim()) {
      out.push(`# ${sheetName}\n${csv}`)
    }
  }
  return out.join('\n\n').trim()
}

async function extractPptxText(file: File): Promise<string> {
  const JSZip = (await import('jszip')).default
  const zip = await JSZip.loadAsync(await file.arrayBuffer())
  const slideEntries = Object.keys(zip.files)
    .filter((n) => /^ppt\/slides\/slide\d+\.xml$/.test(n))
    .sort((a, b) => slideIndex(a) - slideIndex(b))
  const slides: string[] = []
  for (const name of slideEntries) {
    const xml = await zip.files[name].async('string')
    // <a:t>xxx</a:t> 是 PPT 文本运行,逐个抽出来
    const matches = [...xml.matchAll(/<a:t[^>]*>([\s\S]*?)<\/a:t>/g)]
    const text = matches
      .map((m) => decodeXMLEntities(m[1]))
      .filter(Boolean)
      .join('\n')
    if (text.trim()) {
      slides.push(`--- Slide ${slideIndex(name)} ---\n${text}`)
    }
  }
  return slides.join('\n\n').trim()
}

function slideIndex(name: string): number {
  const m = name.match(/slide(\d+)\.xml$/)
  return m ? Number(m[1]) : 0
}

function decodeXMLEntities(s: string): string {
  return s
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&amp;/g, '&')
    .replace(/&quot;/g, '"')
    .replace(/&apos;/g, "'")
}

function guessMimeFromExt(name: string): string {
  const ext = name.toLowerCase().split('.').pop() ?? ''
  switch (ext) {
    case 'pdf': return 'application/pdf'
    case 'docx': return 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
    case 'xlsx': return 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'
    case 'pptx': return 'application/vnd.openxmlformats-officedocument.presentationml.presentation'
    case 'json': return 'application/json'
    case 'md': return 'text/markdown'
    default: return 'text/plain'
  }
}

export function formatFileSize(b: number): string {
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`
  return `${(b / 1024 / 1024).toFixed(1)} MB`
}
