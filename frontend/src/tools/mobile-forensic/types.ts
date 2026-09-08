export type Platform = 'android' | 'ios'

export type RunStatus = 'idle' | 'running' | 'success' | 'canceled' | 'failed'

export interface LogEntry {
  stream: 'stdout' | 'stderr'
  line: string
}

export interface FormState {
  platform: Platform
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

export function previewCommand(form: FormState): string {
  const args = buildArgs(form)
  const masked = args.map((a, i) => {
    if (args[i - 1] === '-p' && a) return '***'
    if (/\s/.test(a)) return `"${a}"`
    return a
  })
  return 'go-forensic ' + masked.join(' ')
}
