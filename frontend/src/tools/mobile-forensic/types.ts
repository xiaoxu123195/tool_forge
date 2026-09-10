export type Platform = 'android' | 'ios'

/**
 * 用哪套实现拉数据。
 *
 * builtin 是内置的:直接说 adb 协议,不依赖外部程序。
 * cli 走 go-forensic 可执行文件 —— 内置实现在某台设备上不好使时的退路,
 * 也让用惯了它的人可以继续用。
 *
 * iOS 目前只有 cli 一条路。
 */
export type Engine = 'builtin' | 'cli'

export type RunStatus = 'idle' | 'running' | 'success' | 'canceled' | 'failed'

/** 这个平台 + 引擎的组合需不需要外部的 go-forensic */
export function needsCLI(platform: Platform, engine: Engine): boolean {
  return platform === 'ios' || engine === 'cli'
}

export interface LogEntry {
  stream: 'stdout' | 'stderr'
  line: string
}

export interface FormState {
  platform: Platform
  engine: Engine
  keywords: string
  outputDir: string
  specifyPaths: string
  // iOS only
  sshAddr: string
  sshPassword: string
  rememberPassword: boolean
  usbProxy: boolean
  deviceId: string
}

export function defaultFormState(): FormState {
  return {
    platform: 'android',
    engine: 'builtin',
    keywords: '',
    outputDir: '',
    specifyPaths: '',
    sshAddr: 'root@127.0.0.1:22',
    sshPassword: '',
    rememberPassword: false,
    usbProxy: true,
    deviceId: '',
  }
}

/**
 * 把一段文本切成一串值:逗号、分号、换行都算分隔符,每段去掉两侧空白。
 *
 * 关键词和路径都用它。
 */
export function splitList(text: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const part of text.split(/[,;\r\n]+/)) {
    const t = part.trim()
    if (!t || seen.has(t)) continue
    seen.add(t)
    out.push(t)
  }
  return out
}

/** 规范化设备内路径:折叠重复斜杠。`Contacts//Donations` 这种手抖很常见 */
export function normalizePath(p: string): string {
  return p.replace(/\/{2,}/g, '/')
}

export function buildArgs(form: FormState): string[] {
  const args: string[] = [form.platform, 'export']
  // 只在明确要走 go-forensic 时才发这个 flag。不发就是"能内置就内置",
  // 和后端的默认行为一致 —— 参数里少一样东西,MCP 那头照着抄也简单些
  if (needsCLI(form.platform, form.engine)) {
    args.push('--engine=cli')
  }
  // -k 和 -s 在 go-forensic 里都是 pflag 的 strings 类型。把整串逗号文本当**一个**
  // 参数传过去的话,pflag 会自己按 CSV 规则切 —— 而人自然会敲"A, B"(逗号后带空格),
  // 切出来第二个就成了 " B",带着前导空格,设备上根本找不到。
  //
  // 所以这里自己切好,一个值发一次 flag。重复 flag 是 pflag 明确支持的累加写法,
  // 顺带也躲开了 CSV 的引号规则 —— 路径里真出现空格或逗号时不会被再切一次。
  for (const k of splitList(form.keywords)) {
    args.push('-k', k)
  }
  if (form.outputDir.trim()) {
    args.push('-o', form.outputDir.trim())
  }
  for (const path of splitList(form.specifyPaths)) {
    args.push('-s', normalizePath(path))
  }
  if (form.platform === 'ios') {
    if (form.sshAddr.trim()) {
      args.push('-a', form.sshAddr.trim())
    }
    if (form.sshPassword) {
      args.push('-p', form.sshPassword)
    }
    if (form.deviceId.trim()) {
      args.push('-d', form.deviceId.trim())
    }
    if (!form.usbProxy) {
      args.push('-u=false')
    }
  }
  return args
}

/**
 * 执行预览。
 *
 * 走 go-forensic 时给的是真能复制到终端里跑的命令行,所以要把 --engine 摘掉 ——
 * 那是我们自己加的伪 flag,它不认识。走内置实现时干脆不摆命令的样子:
 * 那条命令并不存在,照着敲只会发现没这个程序。
 */
export function previewCommand(form: FormState): string {
  if (!needsCLI(form.platform, form.engine)) {
    return previewBuiltin(form)
  }
  const args = buildArgs(form).filter((a) => !a.startsWith('--engine='))
  const masked = args.map((a, i) => {
    if (args[i - 1] === '-p' && a) return '***'
    if (/\s/.test(a)) return `"${a}"`
    return a
  })
  return 'go-forensic ' + masked.join(' ')
}

function previewBuiltin(form: FormState): string {
  const parts = ['内置引擎 · 直接走 adb 协议，不依赖外部程序']
  const paths = splitList(form.specifyPaths).map(normalizePath)
  const keywords = splitList(form.keywords)
  if (paths.length > 0) {
    parts.push(`按路径取 ${paths.length} 条`)
  }
  if (keywords.length > 0) {
    parts.push(`按关键词搜 ${keywords.join('、')}`)
  }
  parts.push(`导出到 ${form.outputDir.trim() || '（还没选输出目录）'}，按设备上的原路径存放`)
  return parts.join('\n')
}
