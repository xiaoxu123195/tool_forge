import type { llmproxy } from '../../../wailsjs/go/models'

export function fmtBytes(n: number): string {
  if (!n) return '0'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

export function fmtTokens(n: number): string {
  if (!n) return '0'
  if (n < 1000) return String(n)
  if (n < 1_000_000) return (n / 1000).toFixed(1).replace(/\.0$/, '') + 'K'
  return (n / 1_000_000).toFixed(2).replace(/\.?0+$/, '') + 'M'
}

export function fmtMs(n: number): string {
  if (!n) return '-'
  if (n < 1000) return `${n}ms`
  return `${(n / 1000).toFixed(n < 10000 ? 2 : 1)}s`
}

export function fmtTime(ms: number): string {
  if (!ms) return '-'
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

export function statusClass(status: number): string {
  if (status === 0) return 'text-muted-foreground'
  if (status < 300) return 'text-emerald-600 dark:text-emerald-400'
  if (status < 400) return 'text-sky-600 dark:text-sky-400'
  if (status < 500) return 'text-amber-600 dark:text-amber-400'
  return 'text-red-600 dark:text-red-400'
}

export function methodClass(method: string): string {
  switch (method.toUpperCase()) {
    case 'GET':
      return 'text-sky-600 dark:text-sky-400'
    case 'POST':
      return 'text-emerald-600 dark:text-emerald-400'
    case 'DELETE':
      return 'text-red-600 dark:text-red-400'
    default:
      return 'text-muted-foreground'
  }
}

// foldBase64 把超长 base64(data URI 或裸串)折叠成占位,避免几万行刷屏。
export function foldBase64(text: string): string {
  if (!text) return text
  // data:image/png;base64,XXXX...
  let out = text.replace(
    /data:([\w/+.-]+);base64,([A-Za-z0-9+/=]{200,})/g,
    (_m, mime: string, data: string) => `data:${mime};base64,…[${Math.round((data.length * 3) / 4 / 1024)}KB 已折叠]`
  )
  // JSON 字符串里的裸长 base64(>=500 字符)
  out = out.replace(/"([A-Za-z0-9+/=]{500,})"/g, (_m, data: string) => `"…[${Math.round((data.length * 3) / 4 / 1024)}KB base64 已折叠]"`)
  return out
}

// extractDataImages 取出 body 里的 data:image URI(用于缩略预览)。
export function extractDataImages(text: string, limit = 4): string[] {
  if (!text) return []
  const re = /data:image\/[\w.+-]+;base64,[A-Za-z0-9+/=]{100,}/g
  const out: string[] = []
  let m: RegExpExecArray | null
  while ((m = re.exec(text)) && out.length < limit) out.push(m[0])
  return out
}

// prettyJSON 尝试把 body 格式化为缩进 JSON;失败则原样返回。
export function prettyJSON(text: string): string {
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return text
  }
}

// buildCurl 生成可复制的 curl(指向本代理,重跑会再次记录;敏感头已打码,需自行替换)。
export function buildCurl(detail: llmproxy.LogDetail, proxyBase: string): string {
  const e = detail.entry
  const url = `${proxyBase}/${e.upstream}${e.path}`
  const lines = [`curl -N '${url}'`, `  -X ${e.method}`]
  for (const [k, v] of Object.entries(detail.reqHeaders || {})) {
    if (k.toLowerCase() === 'content-length' || k.toLowerCase() === 'host') continue
    lines.push(`  -H '${k}: ${v}'`)
  }
  if (detail.reqBody) {
    lines.push(`  --data '${detail.reqBody.replace(/'/g, "'\\''")}'`)
  }
  return lines.join(' \\\n')
}
