import { readVarintSigned, readVarintU32, readVarintU64 } from './varint'

export type MMKVType =
  | 'hexstring'
  | 'string'
  | 'int32'
  | 'int64'
  | 'uint32'
  | 'uint64'
  | 'float32'
  | 'float64'
  | 'bool'
  | 'bytes'
  | 'stringSet'

/** 点击循环的顺序；hexstring 作为默认起点 */
export const TYPE_ORDER: MMKVType[] = [
  'hexstring',
  'string',
  'int32',
  'int64',
  'uint32',
  'uint64',
  'float32',
  'float64',
  'bool',
  'stringSet',
  'bytes',
]

export const TYPE_LABELS: Record<MMKVType, string> = {
  hexstring: 'hexstring',
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
}

/** 每种类型的背景色（light / dark 自适应） */
export const TYPE_BG: Record<MMKVType, string> = {
  hexstring: 'bg-muted/50',
  string: 'bg-emerald-200/60 dark:bg-emerald-900/30',
  int32: 'bg-pink-200/60 dark:bg-pink-900/30',
  int64: 'bg-pink-300/60 dark:bg-pink-800/40',
  uint32: 'bg-sky-200/60 dark:bg-sky-900/30',
  uint64: 'bg-sky-300/60 dark:bg-sky-800/40',
  float32: 'bg-amber-200/60 dark:bg-amber-900/30',
  float64: 'bg-amber-300/60 dark:bg-amber-800/40',
  bool: 'bg-rose-200/60 dark:bg-rose-900/30',
  stringSet: 'bg-cyan-200/60 dark:bg-cyan-900/30',
  bytes: 'bg-slate-200/60 dark:bg-slate-800/40',
}

export function nextType(current: MMKVType): MMKVType {
  const idx = TYPE_ORDER.indexOf(current)
  return TYPE_ORDER[(idx + 1) % TYPE_ORDER.length]
}

/** 解码结果：null 表示该类型解不通；否则用字符串形式呈现以便 UI 展示。 */
export interface DecodeOk {
  ok: true
  display: string
  raw: unknown
}
export interface DecodeErr {
  ok: false
  reason: string
}
export type DecodeResult = DecodeOk | DecodeErr

const ok = (display: string, raw: unknown): DecodeOk => ({ ok: true, display, raw })
const err = (reason: string): DecodeErr => ({ ok: false, reason })

export function decodeAs(value: Uint8Array, type: MMKVType): DecodeResult {
  switch (type) {
    case 'hexstring':
      return ok(toHex(value), value)
    case 'string':
      return decodeAsString(value)
    case 'int32':
      return decodeAsInt(value, 32, true)
    case 'int64':
      return decodeAsInt(value, 64, true)
    case 'uint32':
      return decodeAsInt(value, 32, false)
    case 'uint64':
      return decodeAsInt(value, 64, false)
    case 'float32':
      return decodeAsFloat32(value)
    case 'float64':
      return decodeAsFloat64(value)
    case 'bool':
      return decodeAsBool(value)
    case 'bytes':
      return decodeAsBytes(value)
    case 'stringSet':
      return decodeAsStringSet(value)
  }
}

function decodeAsString(v: Uint8Array): DecodeResult {
  try {
    const { value: len, bytesRead } = readVarintU32(v, 0)
    if (bytesRead + len > v.length) return err('长度越界')
    const s = new TextDecoder('utf-8', { fatal: true }).decode(
      v.subarray(bytesRead, bytesRead + len)
    )
    return ok(s, s)
  } catch (e) {
    return err(e instanceof Error ? e.message : '解码失败')
  }
}

function decodeAsInt(v: Uint8Array, bits: 32 | 64, signed: boolean): DecodeResult {
  try {
    if (signed) {
      const res = readVarintSigned(v, 0, bits)
      const display = typeof res.value === 'bigint' ? res.value.toString() : String(res.value)
      return ok(display, res.value)
    }
    if (bits === 32) {
      const { value } = readVarintU32(v, 0)
      return ok(String(value), value)
    }
    const { value } = readVarintU64(v, 0)
    return ok(value.toString(), value)
  } catch (e) {
    return err(e instanceof Error ? e.message : '不是有效的 varint')
  }
}

function decodeAsBool(v: Uint8Array): DecodeResult {
  if (v.length !== 1) return err(`长度应为 1，实际 ${v.length}`)
  if (v[0] === 0x00) return ok('false', false)
  if (v[0] === 0x01) return ok('true', true)
  return err(`非 0/1 字节：0x${v[0].toString(16)}`)
}

function decodeAsFloat32(v: Uint8Array): DecodeResult {
  if (v.length !== 4) return err(`float 长度应为 4，实际 ${v.length}`)
  const n = new DataView(v.buffer, v.byteOffset, 4).getFloat32(0, true)
  return ok(String(n), n)
}

function decodeAsFloat64(v: Uint8Array): DecodeResult {
  if (v.length !== 8) return err(`double 长度应为 8，实际 ${v.length}`)
  const n = new DataView(v.buffer, v.byteOffset, 8).getFloat64(0, true)
  return ok(String(n), n)
}

function decodeAsBytes(v: Uint8Array): DecodeResult {
  try {
    const { value: len, bytesRead } = readVarintU32(v, 0)
    if (bytesRead + len > v.length) return err('长度越界')
    const slice = v.subarray(bytesRead, bytesRead + len)
    return ok(toHex(slice), slice)
  } catch (e) {
    return err(e instanceof Error ? e.message : '解码失败')
  }
}

function decodeAsStringSet(v: Uint8Array): DecodeResult {
  try {
    const { value: totalLen, bytesRead } = readVarintU32(v, 0)
    let pos = bytesRead
    const end = Math.min(pos + totalLen, v.length)
    const list: string[] = []
    const dec = new TextDecoder('utf-8', { fatal: true })
    while (pos < end) {
      const { value: itemLen, bytesRead: b } = readVarintU32(v, pos)
      pos += b
      if (pos + itemLen > end) return err('元素长度越界')
      list.push(dec.decode(v.subarray(pos, pos + itemLen)))
      pos += itemLen
    }
    return ok('[' + list.map((s) => JSON.stringify(s)).join(', ') + ']', list)
  } catch (e) {
    return err(e instanceof Error ? e.message : '解码失败')
  }
}

export function toHex(bytes: Uint8Array, max = 1024): string {
  const n = Math.min(bytes.length, max)
  let s = ''
  for (let i = 0; i < n; i++) s += bytes[i].toString(16).padStart(2, '0')
  if (bytes.length > max) s += `… (+${bytes.length - max} bytes)`
  return s
}

export function toHexSpaced(bytes: Uint8Array, max = 256): string {
  const n = Math.min(bytes.length, max)
  const parts: string[] = []
  for (let i = 0; i < n; i++) parts.push(bytes[i].toString(16).padStart(2, '0'))
  const s = parts.join(' ')
  return bytes.length > max ? `${s} … (+${bytes.length - max} bytes)` : s
}
