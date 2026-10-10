import type { mmkv } from '../../../wailsjs/go/models'

// 这个文件只管"怎么显示"——标签文字、配色、循环顺序。
// 解码本身在 Go 里(backend/tools/mmkv),页面拿到的已经是
// "这个值能读成哪几种类型、各是什么"的结果。
//
// 以前这里是一整套 TypeScript 解码器,和后端那份是两套实现;
// 同一个文件两边读出不一样的值时谁也说不清该信哪个,所以删掉了。

/** hexstring、raw 不是后端给的类型,是原样看字节的两种看法,永远都有 */
export const HEX_TYPE = 'hexstring'
export const RAW_TYPE = 'raw'

/**
 * 点类型徽章时轮换的顺序。每个值都轮一遍全部类型,读不通的显示 N/A ——
 * 自动识别只决定打开时显示哪个,别的类型照样点得到:有时候要看的正是别的读法
 */
export const TYPE_ORDER = [
  HEX_TYPE,
  'string',
  'int32',
  'int64',
  'uint32',
  'uint64',
  'float32',
  'float64',
  'bool',
  'stringSet',
  'plist',
  'bytes',
  RAW_TYPE,
]

export const TYPE_LABELS: Record<string, string> = {
  [HEX_TYPE]: 'hexstring',
  string: 'string',
  int32: 'int32',
  int64: 'int64',
  uint32: 'uint32',
  uint64: 'uint64',
  float32: 'float',
  float64: 'double',
  bool: 'bool',
  bytes: 'bytes',
  stringSet: 'Set<String>',
  plist: 'plist',
  [RAW_TYPE]: 'raw',
}

/** 每种类型的背景色（light / dark 自适应） */
export const TYPE_BG: Record<string, string> = {
  [HEX_TYPE]: 'bg-muted/50',
  string: 'bg-emerald-200/60 dark:bg-emerald-900/30',
  int32: 'bg-pink-200/60 dark:bg-pink-900/30',
  int64: 'bg-pink-300/60 dark:bg-pink-800/40',
  uint32: 'bg-sky-200/60 dark:bg-sky-900/30',
  uint64: 'bg-sky-300/60 dark:bg-sky-800/40',
  float32: 'bg-amber-200/60 dark:bg-amber-900/30',
  float64: 'bg-amber-300/60 dark:bg-amber-800/40',
  bool: 'bg-rose-200/60 dark:bg-rose-900/30',
  stringSet: 'bg-cyan-200/60 dark:bg-cyan-900/30',
  plist: 'bg-violet-200/60 dark:bg-violet-900/30',
  bytes: 'bg-slate-200/60 dark:bg-slate-800/40',
  [RAW_TYPE]: 'bg-lime-200/60 dark:bg-lime-900/30',
}

export const labelOf = (t: string) => TYPE_LABELS[t] ?? t
export const bgOf = (t: string) => TYPE_BG[t] ?? TYPE_BG[HEX_TYPE]

/** 一个值按某种类型读出来的样子 */
export interface Reading {
  /** 显示的文本;读不通时是空串 */
  text: string
  /** ok = 读通了;loose = 只用开头一部分字节硬读出来的;none = 读不通 */
  kind: 'ok' | 'loose' | 'none'
  /** 硬读时用上了开头多少字节 */
  used?: number
}

export function readingOf(v: mmkv.Value, type: string): Reading {
  if (type === HEX_TYPE) return { text: v.hex, kind: 'ok' }
  if (type === RAW_TYPE) return { text: rawText(v.hex), kind: 'ok' }
  const d = (v.decoded ?? []).find((x) => x.type === type)
  if (d) return { text: d.display, kind: 'ok' }
  const l = (v.loose ?? []).find((x) => x.type === type)
  if (l) return { text: l.display, kind: 'loose', used: l.used ?? 0 }
  return { text: '', kind: 'none' }
}

/** 硬读的那条怎么说明:用了几个字节、剩下的可能是什么 */
export function looseNote(v: mmkv.Value, r: Reading): string {
  return (
    `只用上了开头 ${r.used} 字节，后面还剩 ${v.size - (r.used ?? 0)} 字节没读：` +
    '可能后面跟着别的东西（比如 4 字节过期时间），也可能它根本不是这个类型'
  )
}

/** 打开时显示哪个类型：自动识别出的那个（要读得通才算）；一个都读不通就看原始字节 */
export function defaultTypeOf(v: mmkv.Value): string {
  return v.best && (v.decoded ?? []).some((d) => d.type === v.best) ? v.best : HEX_TYPE
}

export function nextTypeOf(current: string): string {
  return TYPE_ORDER[(TYPE_ORDER.indexOf(current) + 1) % TYPE_ORDER.length]
}

/** 取某个类型下的显示文本；读不通时返回空串 */
export function displayOf(v: mmkv.Value, type: string): string {
  return readingOf(v, type).text
}

/**
 * 详情里看的文本：plist 是一行 JSON，排成缩进的多行。
 * full 是回后端读来的完整十六进制，有它时原始字节的两种看法用完整的
 */
export function detailOf(v: mmkv.Value, type: string, full = ''): string {
  if (full && type === HEX_TYPE) return full
  if (full && type === RAW_TYPE) return rawText(full)
  const text = displayOf(v, type)
  return type === 'plist' ? indentJson(text) : text
}

const utf8 = new TextDecoder('utf-8', { fatal: true })
const ESCAPES: Record<number, string> = { 0x09: '\\t', 0x0a: '\\n', 0x0d: '\\r', 0x5c: '\\\\' }

/**
 * 原样字节按文字显示：认得出的字（ASCII、合法 UTF-8 的中文等）照常显示，其余字节写成 \xNN。
 * 二进制里夹着的英文、中文一眼就能看到，十六进制里得一个字节一个字节地认。
 * 十六进制被截断过的话，末尾那句「还有 N 字节」原样接上
 */
export function rawText(hex: string): string {
  const cut = hex.search(/[^0-9a-f]/i)
  const digits = cut < 0 ? hex : hex.slice(0, cut)
  const bytes = new Uint8Array(digits.length >> 1)
  for (let i = 0; i < bytes.length; i++) bytes[i] = parseInt(digits.slice(i * 2, i * 2 + 2), 16)
  let out = ''
  for (let i = 0; i < bytes.length; ) {
    const c = bytes[i]
    if (c >= 0x20 && c < 0x7f && c !== 0x5c) {
      out += String.fromCharCode(c)
      i++
      continue
    }
    // 多字节的字：按开头字节该有几个字节，整段解得开、又不是控制字符才当字显示
    const n = c >= 0xf0 ? 4 : c >= 0xe0 ? 3 : c >= 0xc2 ? 2 : 0
    if (n && i + n <= bytes.length) {
      try {
        const s = utf8.decode(bytes.subarray(i, i + n))
        if (!/\p{C}/u.test(s)) {
          out += s
          i += n
          continue
        }
      } catch {
        // 不是合法的 UTF-8，下面按字节写
      }
    }
    out += ESCAPES[c] ?? '\\x' + c.toString(16).padStart(2, '0')
    i++
  }
  return cut < 0 ? out : out + hex.slice(cut)
}

/**
 * 把一行 JSON 排成缩进的多行。只动字符串外面的标点,不经过 JSON.parse:
 * 归档里超过 2^53 的整数(ID、时间戳)在 JS 里一转成数字就会差几位
 */
export function indentJson(text: string): string {
  let out = ''
  let depth = 0
  let inString = false
  const newline = () => '\n' + '  '.repeat(depth)
  for (let i = 0; i < text.length; i++) {
    const c = text[i]
    if (inString) {
      out += c
      if (c === '\\') out += text[++i] ?? ''
      else if (c === '"') inString = false
      continue
    }
    switch (c) {
      case '"':
        inString = true
        out += c
        break
      case '{':
      case '[': {
        // 空的 {} [] 不拆行
        const close = c === '{' ? '}' : ']'
        if (text[i + 1] === close) {
          out += c + close
          i++
        } else {
          depth++
          out += c + newline()
        }
        break
      }
      case '}':
      case ']':
        depth--
        out += newline() + c
        break
      case ',':
        out += c + newline()
        break
      case ':':
        out += ': '
        break
      case ' ':
      case '\n':
      case '\r':
      case '\t':
        break
      default:
        out += c
    }
  }
  return out
}
