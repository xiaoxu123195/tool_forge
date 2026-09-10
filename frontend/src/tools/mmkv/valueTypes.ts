import type { mmkv } from '../../../wailsjs/go/models'

// 这个文件只管"怎么显示"——标签文字、配色、循环顺序。
// 解码本身在 Go 里(backend/tools/mmkv),页面拿到的已经是
// "这个值能读成哪几种类型、各是什么"的结果。
//
// 以前这里是一整套 TypeScript 解码器,和后端那份是两套实现;
// 同一个文件两边读出不一样的值时谁也说不清该信哪个,所以删掉了。

/** hexstring 不是后端给的类型,是"原样看字节"这个视角,永远可选 */
export const HEX_TYPE = 'hexstring'

/** 循环顺序。实际展示时会按这个顺序过滤出该值真正解得通的类型 */
const TYPE_ORDER = [
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
  'bytes',
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
  bytes: 'bg-slate-200/60 dark:bg-slate-800/40',
}

export const labelOf = (t: string) => TYPE_LABELS[t] ?? t
export const bgOf = (t: string) => TYPE_BG[t] ?? TYPE_BG[HEX_TYPE]

/**
 * 这个值可以被看成哪几种类型。
 *
 * 只列解得通的 —— 以前是固定 11 种轮着切,一半都停在 "N/A" 上,
 * 点起来像在瞎撞。现在轮到的每一个都有东西可看。
 */
export function optionsOf(v: mmkv.Value): string[] {
  const has = new Set((v.decoded ?? []).map((d) => d.type))
  return TYPE_ORDER.filter((t) => t === HEX_TYPE || has.has(t))
}

/** 默认选中哪个类型：后端猜的那个；猜不出就看原始字节 */
export function defaultTypeOf(v: mmkv.Value): string {
  const opts = optionsOf(v)
  return v.best && opts.includes(v.best) ? v.best : HEX_TYPE
}

export function nextTypeOf(v: mmkv.Value, current: string): string {
  const opts = optionsOf(v)
  const i = opts.indexOf(current)
  return opts[(i + 1) % opts.length]
}

/** 取某个类型下的显示文本；该类型解不通时返回空串 */
export function displayOf(v: mmkv.Value, type: string): string {
  if (type === HEX_TYPE) return v.hex
  return (v.decoded ?? []).find((d) => d.type === type)?.display ?? ''
}
