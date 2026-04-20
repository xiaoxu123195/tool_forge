// 各模型 1M token 的输入/输出价格（美元）。
// 上游接口没有返回，这里按官网 UI 显示的硬编码；新模型或价格调整时统一维护这一张表。

interface Price {
  input: number
  output: number
}

const PRICING: Record<string, Price> = {
  'kimi-latest': { input: 0.6, output: 2.5 },
  'kimi-thinking-preview': { input: 0.6, output: 2.5 },
  'kimi-k2-0905-preview': { input: 0.6, output: 2.5 },
  'kimi-k2-turbo-preview': { input: 0.6, output: 2.5 },

  'claude-sonnet-4-20250514': { input: 3, output: 15 },
  'claude-sonnet-4-5-20250929': { input: 3, output: 15 },
  'claude-sonnet-4-6': { input: 3, output: 15 },
  'claude-opus-4-1-20250805': { input: 15, output: 75 },
  'claude-opus-4-5-20251101': { input: 5, output: 25 },
  'claude-opus-4-6': { input: 5, output: 25 },
  'claude-opus-4-7': { input: 5, output: 25 },

  'gpt-5.2': { input: 1.75, output: 14 },
  'gpt-5.3-codex': { input: 1.25, output: 10 },
  'gpt-5.4': { input: 1.25, output: 10 },

  'grok-4-0709': { input: 3, output: 15 },
  'grok-4-latest': { input: 3, output: 15 },
  'grok-code-fast-1': { input: 0.2, output: 1.5 },

  'gemini-3-pro-preview': { input: 2, output: 12 },
  'gemini-2.5-flash': { input: 0.3, output: 2.5 },

  'glm-4.6': { input: 0.6, output: 2.2 },
  'glm-4.7': { input: 0.6, output: 2.2 },

  'deepseek-chat': { input: 0.28, output: 0.42 },
  'deepseek-reasoner': { input: 0.28, output: 0.42 },
}

export function priceOf(modelName: string): Price | undefined {
  return PRICING[modelName]
}

/** "$0.60/$2.50" 这种格式；小于 1 保留 2 位，其余去尾零 */
export function fmtPrice(p: Price | undefined): string {
  if (!p) return '—'
  return `$${fmtNum(p.input)}/$${fmtNum(p.output)}`
}

function fmtNum(n: number): string {
  if (n < 1) return n.toFixed(2).replace(/0$/, '')
  if (Number.isInteger(n)) return String(n)
  return String(n)
}
