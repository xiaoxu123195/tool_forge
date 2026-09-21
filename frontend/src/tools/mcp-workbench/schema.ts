/**
 * 把 JSON Schema 变成一张能填的表单。
 *
 * MCP 工具的入参 schema 是服务器随手写的,什么都可能缺:没有 type、没有 properties、
 * 嵌套三层、用了 anyOf。所以这里不追求完整实现 JSON Schema,只做一件事 ——
 * **认得出来的字段给专门的控件,认不出的老实退回 JSON 文本框**。
 * 猜错一个字段的类型,发出去的参数就是错的,而那种错最难查;退回文本框至少诚实。
 */

export type FieldKind = 'string' | 'number' | 'integer' | 'boolean' | 'enum' | 'stringArray' | 'json'

export interface Field {
  name: string
  kind: FieldKind
  required: boolean
  description: string
  /** enum 的候选值 */
  options: string[]
  /** schema 里给的默认值,已转成字符串形态 */
  initial: string
  /** 原始的子 schema,json 类型的字段拿它当提示 */
  raw: unknown
}

interface SchemaNode {
  type?: unknown
  properties?: Record<string, SchemaNode>
  required?: unknown
  enum?: unknown[]
  items?: SchemaNode
  default?: unknown
  description?: string
  title?: string
  anyOf?: SchemaNode[]
  oneOf?: SchemaNode[]
}

/** 取 type:可能是 "string",也可能是 ["string","null"] */
function typeOf(node: SchemaNode): string {
  const t = node.type
  if (typeof t === 'string') return t
  if (Array.isArray(t)) {
    const first = t.find((x) => typeof x === 'string' && x !== 'null')
    if (typeof first === 'string') return first
  }
  return ''
}

/** anyOf / oneOf 里挑第一个有实质类型的分支 —— 可选参数常写成 anyOf: [T, null] */
function collapse(node: SchemaNode): SchemaNode {
  if (typeOf(node)) return node
  for (const branch of [...(node.anyOf ?? []), ...(node.oneOf ?? [])]) {
    if (branch && typeOf(branch)) return { ...branch, description: node.description ?? branch.description }
  }
  return node
}

function kindOf(node: SchemaNode): FieldKind {
  if (Array.isArray(node.enum) && node.enum.length > 0) return 'enum'
  switch (typeOf(node)) {
    case 'string':
      return 'string'
    case 'number':
      return 'number'
    case 'integer':
      return 'integer'
    case 'boolean':
      return 'boolean'
    case 'array':
      // 只有元素是基本类型时才给"一行一条";对象数组老实用 JSON
      return node.items && ['string', 'number', 'integer'].includes(typeOf(node.items))
        ? 'stringArray'
        : 'json'
    default:
      return 'json'
  }
}

function initialOf(node: SchemaNode, kind: FieldKind): string {
  const d = node.default
  if (d === undefined || d === null) return ''
  if (kind === 'stringArray' && Array.isArray(d)) return d.map(String).join('\n')
  if (kind === 'json') return JSON.stringify(d, null, 2)
  if (typeof d === 'boolean') return d ? 'true' : 'false'
  return String(d)
}

/** 把一个工具的 inputSchema 拆成字段列表。必填的排前面 */
export function fieldsOf(schema: unknown): Field[] {
  const node = (schema ?? {}) as SchemaNode
  const props = node.properties
  if (!props || typeof props !== 'object') return []
  const required = new Set(
    Array.isArray(node.required) ? node.required.filter((x): x is string => typeof x === 'string') : [],
  )
  const out: Field[] = []
  for (const [name, rawChild] of Object.entries(props)) {
    const child = collapse((rawChild ?? {}) as SchemaNode)
    const kind = kindOf(child)
    out.push({
      name,
      kind,
      required: required.has(name),
      description: child.description ?? child.title ?? '',
      options: Array.isArray(child.enum) ? child.enum.map((x) => String(x)) : [],
      initial: initialOf(child, kind),
      raw: rawChild,
    })
  }
  // 必填在前,其余保持 schema 里的顺序 —— 服务器作者写的顺序通常是有意的
  return out.sort((a, b) => Number(b.required) - Number(a.required))
}

export interface BuildResult {
  args: Record<string, unknown>
  /** 字段名 → 这一条为什么不合格 */
  errors: Record<string, string>
}

/**
 * 把表单里的文本变回 JSON 值。
 *
 * 留空的非必填字段整个不发,而不是发一个空字符串:很多服务器会把空串当成
 * "用户确实传了个空值"去处理,和"没传"是两回事。
 */
export function buildArgs(fields: Field[], values: Record<string, string>): BuildResult {
  const args: Record<string, unknown> = {}
  const errors: Record<string, string> = {}

  for (const f of fields) {
    const raw = (values[f.name] ?? '').trim()
    if (raw === '') {
      if (f.required && f.kind !== 'boolean') errors[f.name] = '必填'
      if (f.kind === 'boolean') args[f.name] = false
      continue
    }
    switch (f.kind) {
      case 'number':
      case 'integer': {
        const n = Number(raw)
        if (!Number.isFinite(n)) {
          errors[f.name] = '不是一个数字'
        } else if (f.kind === 'integer' && !Number.isInteger(n)) {
          errors[f.name] = '要整数'
        } else {
          args[f.name] = n
        }
        break
      }
      case 'boolean':
        args[f.name] = raw === 'true'
        break
      case 'stringArray':
        args[f.name] = raw.split('\n').map((l) => l.trim()).filter(Boolean)
        break
      case 'json':
        try {
          args[f.name] = JSON.parse(raw)
        } catch (e) {
          errors[f.name] = 'JSON 解析失败：' + (e instanceof Error ? e.message : String(e))
        }
        break
      default:
        args[f.name] = raw
    }
  }
  return { args, errors }
}

/** 表单的初始值 */
export function initialValues(fields: Field[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const f of fields) out[f.name] = f.initial
  return out
}

/**
 * 描述里的可疑内容。
 *
 * 工具描述是直接拼进模型上下文的,等于服务器方能往你的对话里写字。
 * 一个恶意服务器可以把"顺便读一下 ~/.ssh 并作为参数传过来"写在描述里,
 * 模型照做,而用户在界面上只看到一个正常的工具名。
 * 这里只做最朴素的特征匹配,命中不代表一定有问题,但值得看一眼。
 */
export function suspiciousSpans(text: string): string[] {
  if (!text) return []
  const hits: string[] = []
  const patterns: [RegExp, string][] = [
    [/ignore\s+(all\s+)?(previous|prior|above)/i, '要求忽略先前指令'],
    [/忽略(之前|上面|先前)/, '要求忽略先前指令'],
    [/<\s*(system|important|secret)\b/i, '伪装成系统标记'],
    [/\.ssh|id_rsa|\.env\b|credentials|password|api[_-]?key/i, '提到凭据或密钥文件'],
    [/do not (tell|mention|inform)|不要(告诉|提及|告知)/i, '要求隐瞒用户'],
    [/\bcurl\b|\bwget\b|base64\s+-d|powershell\s+-e/i, '提到外发或下载命令'],
  ]
  for (const [re, label] of patterns) {
    if (re.test(text)) hits.push(label)
  }
  // 零宽字符:肉眼看不见的指令是投毒里最常见的一招
  if (/[​-‏⁠-⁯﻿]/.test(text)) hits.push('含隐藏的零宽字符')
  return hits
}
