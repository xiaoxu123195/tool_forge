import { DetectAILanguage } from '../../../wailsjs/go/main/App'
import { fromFrancCode, LANGUAGES, type LangItem } from './languages'

/** franc-min 算法检测;输入文本至少 ~10 字符才靠谱 */
export async function detectByFranc(text: string): Promise<LangItem | undefined> {
  if (text.trim().length < 4) return undefined
  const { franc } = await import('franc-min')
  const code = franc(text, { minLength: 3 })
  return fromFrancCode(code)
}

/** 调用模型识别文本语言;返回我们的 LangItem 或 undefined */
export async function detectByLLM(
  providerId: string,
  modelId: string,
  text: string,
): Promise<LangItem | undefined> {
  if (!providerId || !modelId) return undefined
  if (text.trim().length < 4) return undefined
  try {
    // 返回的就是语言名字符串本身。以前按 r['0'] 解,拿到的是首字符
    const name = (await DetectAILanguage(providerId, modelId, text)) as unknown as string
    if (!name) return undefined
    return matchLangByName(name)
  } catch {
    return undefined
  }
}

/** 自动模式:franc 优先;franc 给不出来再降级到 LLM */
export async function detectAuto(
  providerId: string,
  modelId: string,
  text: string,
): Promise<LangItem | undefined> {
  const f = await detectByFranc(text)
  if (f) return f
  return detectByLLM(providerId, modelId, text)
}

/** 把模型返回的英文语言名(可能含标点/复数等噪声)匹配到 LANGUAGES 表 */
function matchLangByName(raw: string): LangItem | undefined {
  const norm = raw
    .toLowerCase()
    .replace(/[^a-z\s]/g, ' ')
    .trim()
  if (!norm) return undefined
  // 精确名匹配 / 包含匹配
  for (const lang of LANGUAGES) {
    const ln = lang.name.toLowerCase()
    if (norm === ln || norm.includes(ln) || ln.includes(norm)) {
      return lang
    }
  }
  // 中文常见误差:"chinese" → 简中
  if (norm.includes('chinese') || norm.includes('mandarin')) return LANGUAGES.find((l) => l.id === 'zh')
  return undefined
}
