/**
 * AI 问答的渲染冒烟测试(`npm run smoke`)。
 *
 * 存在的理由:这个应用没有浏览器控制台,渲染期抛异常的表现就是整窗白屏,
 * 用户只能说出"白了"三个字。这套测试在 jsdom 里把主要界面真实挂载一遍
 * (真 effect、真点击),渲染炸了当场红 —— 它抓到过密钥列表白屏的根因。
 *
 * 数据来自 fixtures.cjs 的固定样本;新坑修掉后往 fixtures 里补对应形状。
 */
import { inflateSync } from 'node:zlib'
import { StrictMode } from 'react'
import { MemoryRouter, useNavigate } from 'react-router-dom'
import { createRoot } from 'react-dom/client'
import { act } from 'react'
import { ChatPane } from '../src/tools/ai-chat/ChatPane'
import AIChat from '../src/tools/ai-chat/index'
import { ProvidersTab } from '../src/profile/sections/aichat/ProvidersTab'
import { AssistantsTab } from '../src/profile/sections/aichat/AssistantsTab'
import { ConversationDialog } from '../src/tools/ai-chat/ConversationDialog'
import { ExportDialog } from '../src/tools/ai-chat/ExportDialog'
import { TraceDialog } from '../src/tools/ai-chat/TraceDialog'
import { GlobalSearchDialog } from '../src/tools/ai-chat/GlobalSearchDialog'
import { DefaultsTab } from '../src/profile/sections/aichat/DefaultsTab'
import AIConfigTool from '../src/tools/ai-config/index'
import { Profile } from '../src/profile'
import MmkvTool from '../src/tools/mmkv/index'
import PlistTool from '../src/tools/plist/index'
import DeviceBrowser from '../src/tools/device-browser/index'
import { DISMISS_MS } from '../src/tools/device-browser/mirror-ui'
import MobileForensic from '../src/tools/mobile-forensic/index'
import AppSearch from '../src/tools/app-search/index'
import SQLiteSearch from '../src/tools/sqlite-search/index'
import ClipboardTool from '../src/tools/clipboard/index'
import MCPWorkbench from '../src/tools/mcp-workbench/index'
import { useWorkbenchLayout, WB_DEFAULT, WB_MIN } from '../src/stores/mcp-workbench'
import DiskClean from '../src/tools/disk-clean/index'
import { ConfirmProvider } from '../src/components/ui/confirm'
import { LocalAPISection } from '../src/profile/sections/LocalAPI'
import { useForensicStore } from '../src/stores/forensic'
import { getToolById, isVisible, useToolsStore } from '../src/stores/tools'
import { conversations } from './fixtures.cjs'
// 直接引桩本体拿事件把手。build.cjs 只把含 "wailsjs" 的路径重定向到这里,
// 相对路径原样解析 —— CJS 缓存保证跟组件用的是同一个模块实例
import { __emit, __calls, __last } from './stub.cjs'
import * as wailsStub from './stub.cjs'
// 假的 noVNC:组件里的 '@novnc/novnc' 被 build.cjs 指到同一个文件,这里直接拿它造出来的实例
import { instances as vncInstances } from './novnc-stub.cjs'
import { modelGroup } from '../src/profile/sections/aichat/modelGroup'
import { sm2GenerateKeyPair, sm2Encrypt, sm2Decrypt, sm2Sign, sm2Verify } from '../src/tools/crypto-lab/lib/sm'

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
let failed = false
const note = (where: string, e: unknown) => {
  failed = true
  const err = e as { message?: string; stack?: string } | null
  console.log('  FAIL ' + where + ' :: ' + String(err?.message ?? e))
  console.log(String(err?.stack ?? '').split('\n').slice(1, 8).join('\n'))
}
process.on('uncaughtException', (e) => note('uncaught', e))
process.on('unhandledRejection', (e) => note('rejection', e))

// React 把渲染期异常自己吞了,只往 console.error 打一条 "The above error occurred in
// the <Xxx> component" —— 于是场景照样报 OK,而真实表现正是整窗白屏。
// 这个测试存在的全部意义就是抓这种崩,所以把这条日志升级成失败。
const realError = console.error
console.error = (...args: unknown[]) => {
  const first = String(args[0] ?? '')
  if (first.includes('The above error occurred in')) {
    failed = true
    console.log('  FAIL 组件渲染期抛异常 :: ' + first.split('\n')[0])
  }
  realError(...(args as []))
}

/**
 * 按标签找按钮。三级回退,顺序不能反:
 *   1. title 或文字精确相等 —— 图标按钮只有 title
 *   2. 只看文字精确相等 —— 带 title 的文字按钮(预设那排就是,title 是整段提示词,
 *      按第 1 条会拿 title 去比名字,永远匹配不上,click 静悄悄返回 false)
 *   3. 文字包含 —— 名字前面带 emoji 的那种
 */
const btn = (label: string) => {
  const all = Array.from(document.querySelectorAll('button')) as HTMLElement[]
  const attr = (b: HTMLElement) => ((b.getAttribute('title') || b.textContent) || '').trim()
  const text = (b: HTMLElement) => (b.textContent || '').trim()
  return (
    all.find((b) => attr(b) === label) ??
    all.find((b) => text(b) === label) ??
    all.find((b) => text(b).includes(label))
  )
}

/**
 * 往输入框里打字(React 受控组件要走原生 setter 才认)。
 *
 * 找不到框时返回 false,不抛 —— 所以几乎所有地方都该用下面的 mustType。
 * 名字里带 try 是故意的:省得有人顺手写 type() 又掉进静默失败里
 */
const fillEl = async (el: HTMLInputElement | HTMLTextAreaElement, value: string) => {
  // 注意用 window.Event 而不是全局 Event:Node 自己也有一个同名的 Event 类,
  // 拿它构造出来的对象 jsdom 的 dispatchEvent 不认
  const proto =
    el instanceof window.HTMLTextAreaElement
      ? window.HTMLTextAreaElement.prototype
      : window.HTMLInputElement.prototype
  const setter = Object.getOwnPropertyDescriptor(proto, 'value')?.set
  setter?.call(el, value)
  await act(async () => {
    el.dispatchEvent(new window.Event('input', { bubbles: true }))
  })
  await act(async () => {
    await sleep(50)
  })
}

const tryType = async (placeholder: string, value: string) => {
  const el = document.querySelector(
    `input[placeholder*="${placeholder}"]`,
  ) as HTMLInputElement | null
  if (!el) return false
  await fillEl(el, value)
  return true
}

/** 按 CSS 选择器填一个框。表单控件没有 placeholder 时用它(schema 生成的那种) */
const fillBy = async (selector: string, value: string) => {
  const el = document.querySelector(selector) as HTMLInputElement | HTMLTextAreaElement | null
  if (!el) throw new Error('找不到控件「' + selector + '」')
  await fillEl(el, value)
}

async function mount(name: string, el: React.ReactNode, after?: () => Promise<void>) {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const root = createRoot(host)
  try {
    await act(async () => {
      root.render(
        <StrictMode>
          <ConfirmProvider>{el}</ConfirmProvider>
        </StrictMode>,
      )
    })
    await act(async () => {
      await sleep(90)
    })
    if (after) await after()
    console.log('  OK   ' + name)
  } catch (e) {
    note(name, e)
  }
  await act(async () => {
    root.unmount()
  })
  host.remove()
}

const click = async (label: string) => {
  const b = btn(label)
  if (!b) return false
  await act(async () => {
    b.click()
  })
  await act(async () => {
    await sleep(50)
  })
  return true
}

/** 勾一个复选框(按它旁边的文字找)。它不是 button,click() 抓不到 */
const check = async (labelText: string) => {
  const label = (Array.from(document.querySelectorAll('label')) as HTMLElement[]).find((l) =>
    (l.textContent || '').includes(labelText),
  )
  const box = label?.querySelector('input[type=checkbox]') as HTMLElement | undefined
  if (!box) throw new Error('找不到复选框「' + labelText + '」')
  await act(async () => {
    box.click()
  })
  await act(async () => {
    await sleep(50)
  })
}

/** 往 textarea 里打字(受控组件要走原生 setter) */
const typeArea = async (placeholderPart: string, value: string) => {
  const el = document.querySelector(
    `textarea[placeholder*="${placeholderPart}"]`,
  ) as HTMLTextAreaElement | null
  if (!el) throw new Error('找不到文本域「' + placeholderPart + '」')
  const setter = Object.getOwnPropertyDescriptor(
    window.HTMLTextAreaElement.prototype,
    'value',
  )?.set
  setter?.call(el, value)
  await act(async () => {
    el.dispatchEvent(new window.Event('input', { bubbles: true }))
  })
  await act(async () => {
    await sleep(50)
  })
}

/**
 * 拖一根分隔条。
 *
 * 三个事件要分三次 act:按下之后 React 得先提交状态、effect 才会把 mousemove
 * 挂到 window 上。挤在同一个 act 里的话 mousemove 派发时还没人听,拖动静悄悄失败
 */
const drag = async (sep: HTMLElement, from: number, to: number, axis: 'x' | 'y' = 'x') => {
  const at = (v: number) => (axis === 'x' ? { clientX: v } : { clientY: v })
  await act(async () => {
    sep.dispatchEvent(new window.MouseEvent('mousedown', { bubbles: true, ...at(from) }))
  })
  await act(async () => {
    window.dispatchEvent(new window.MouseEvent('mousemove', { bubbles: true, ...at(to) }))
  })
  await act(async () => {
    window.dispatchEvent(new window.MouseEvent('mouseup', { bubbles: true }))
  })
}

/**
 * 点确认框里的按钮。
 *
 * 确认框挂在 body 末尾,而页面上常有同名的按钮(底栏的「移到回收站」点开确认框,
 * 确认框里的确认按钮还叫「移到回收站」)。按文字全局找会点回底栏那个
 */
const confirmIn = async (label: string) => {
  const overlays = Array.from(document.querySelectorAll('div.fixed.inset-0')) as HTMLElement[]
  const box = overlays[overlays.length - 1]
  const b = box
    ? (Array.from(box.querySelectorAll('button')) as HTMLElement[]).find((x) => (x.textContent || '').trim() === label)
    : undefined
  if (!b) throw new Error('确认框里找不到按钮「' + label + '」')
  await act(async () => {
    b.click()
  })
  await act(async () => {
    await sleep(80)
  })
}

/** 按 data-* 属性的值找元素。值里有反斜杠的路径,写进 CSS 选择器要转义得眼花 */
const byData = (attr: string, value: string) =>
  (Array.from(document.querySelectorAll(`[${attr}]`)) as HTMLElement[]).find((e) => e.getAttribute(attr) === value)

// ---- 投屏用的假货:手机端发来的包、解码器、WebSocket、画布 ----

/** 帧头第一个 u32 里的标志位:配置包、关键帧 */
const MIRROR_CONFIG = 0x40000000
const MIRROR_KEY = 0x20000000

/** 会话包:12 字节,最高位是 1,后面是宽、高 */
const mirrorSession = (w: number, h: number) => {
  const b = new Uint8Array(12)
  const dv = new DataView(b.buffer)
  dv.setUint32(0, 0x80000000)
  dv.setUint32(4, w)
  dv.setUint32(8, h)
  return b.buffer
}

/** 帧包:标志和时间戳(这里只用低 32 位)、负载长度,再跟负载 */
const mirrorMedia = (flags: number, pts: number, payload: number[]) => {
  const b = new Uint8Array(12 + payload.length)
  const dv = new DataView(b.buffer)
  dv.setUint32(0, flags)
  dv.setUint32(4, pts)
  dv.setUint32(8, payload.length)
  b.set(payload, 12)
  return b.buffer
}

interface FakeSocket {
  url: string
  sent: string[]
  emit: (data: unknown) => void
}
interface FakeDecoder {
  configs: { codec: string }[]
  chunks: { type: string; size: number }[]
}

/**
 * 换上假的 WebCodecs、WebSocket 和画布上下文。jsdom 这三样都没有(或者是真的网络连接),
 * 换掉之后投屏面板的整条前端链路就能在这里跑。用完必须 restore,别漏给后面的用例
 */
function installMirrorFakes() {
  const g = globalThis as unknown as Record<string, unknown>
  const saved = { VideoDecoder: g.VideoDecoder, EncodedVideoChunk: g.EncodedVideoChunk, WebSocket: g.WebSocket }
  const proto = window.HTMLCanvasElement.prototype
  const savedGetContext = proto.getContext
  const state = {
    sockets: [] as FakeSocket[],
    decoders: [] as FakeDecoder[],
    draws: 0,
    restore() {
      Object.assign(g, saved)
      proto.getContext = savedGetContext
    },
  }
  g.WebSocket = class {
    static OPEN = 1
    readyState = 1
    binaryType = ''
    sent: string[] = []
    onmessage: ((ev: { data: unknown }) => void) | null = null
    onclose: (() => void) | null = null
    listeners: ((ev: { data: unknown }) => void)[] = []
    constructor(public url: string) {
      state.sockets.push(this)
    }
    send(s: string) {
      this.sent.push(s)
    }
    close() {
      this.readyState = 3
    }
    addEventListener(type: string, fn: (ev: { data: unknown }) => void) {
      if (type === 'message') this.listeners.push(fn)
    }
    emit(data: unknown) {
      this.onmessage?.({ data })
      for (const fn of this.listeners) fn({ data })
    }
  }
  g.VideoDecoder = class {
    state = 'unconfigured'
    decodeQueueSize = 0
    configs: { codec: string }[] = []
    chunks: { type: string; size: number }[] = []
    constructor(private init: { output: (f: unknown) => void }) {
      state.decoders.push(this)
    }
    configure(c: { codec: string }) {
      this.configs.push(c)
      this.state = 'configured'
    }
    decode(chunk: { type: string; byteLength: number }) {
      this.chunks.push({ type: chunk.type, size: chunk.byteLength })
      this.init.output({ displayWidth: 540, displayHeight: 1200, close() {} })
    }
    close() {
      this.state = 'closed'
    }
  }
  g.EncodedVideoChunk = class {
    type: string
    timestamp: number
    byteLength: number
    constructor(o: { type: string; timestamp: number; data: Uint8Array }) {
      this.type = o.type
      this.timestamp = o.timestamp
      this.byteLength = o.data.byteLength
    }
  }
  proto.getContext = function () {
    return { drawImage: () => state.draws++ }
  } as unknown as typeof proto.getContext
  return state
}

/** 点一个必须存在的按钮;找不到就是回归 —— 静悄悄跳过等于这条用例白测 */
const mustClick = async (label: string) => {
  if (!(await click(label))) throw new Error('找不到按钮「' + label + '」')
}

/**
 * 往一个必须存在的输入框里打字。
 *
 * type 找不到框时只是返回 false,不报错 —— 于是占位符一改,
 * 用例就在一个没填过的表单上继续跑,后面的断言全是空对空。
 * 这条用例本身就是这么红的,所以给它一个会喊的版本
 */
const mustType = async (placeholder: string, value: string) => {
  if (!(await tryType(placeholder, value))) {
    throw new Error('找不到输入框「' + placeholder + '」')
  }
}

async function main() {
  // 1) 每条 fixture 会话过一遍消息渲染(老格式思考 / 富消息 / 空会话)
  for (const c of conversations as { id: string; title: string }[]) {
    await mount(
      `会话「${c.title}」`,
      <ChatPane conversationId={c.id} onTitleChange={() => {}} onExport={() => {}} />,
      async () => {
        await click('看看这一轮会带哪些工具')
        // 会话内搜索:开、输入、翻页。命中要跳转 + 高亮,跳转会碰 scrollIntoView
        await click('在会话里查找 (Ctrl+F)')
        await mustType('在这个会话里查找', '的')
        await click('下一条 (Enter)')
        await click('上一条 (Shift+Enter)')
        await click('关闭 (Esc)')
      },
    )
  }

  // 2) 问答整页(侧栏 + 会话切换)。它用了 useNavigate,得给个路由环境
  await mount(
    'AI 问答整页',
    <MemoryRouter>
      <AIChat />
    </MemoryRouter>,
  )

  // 3) 配置页
  // 模型分组是纯函数,直接断言 —— 原来是一张写死的前缀表,中转上新出的
  // grok / gpt-6 / codex 全掉进「其他」,二十几个挤一堆等于没分组
  try {
    const cases: [string, string][] = [
      // 表里从来没有 grok 这一条
      ['grok-4.1-fast', 'Grok-4'],
      ['grok-code-fast-1', 'Grok'],
      // 新一代:旧表只列到 gpt-5
      ['gpt-6-astra', 'GPT-6'],
      ['gpt-5.6-luna', 'GPT-5'],
      ['gpt-4o-mini', 'GPT-4'],
      ['gpt-3.5-turbo', 'GPT-3'],
      // 认不出版本就只按家族分,不进「其他」
      ['codex-auto-review', 'Codex'],
      ['deepseek-chat', 'DeepSeek'],
      ['claude-opus-4-5', 'Claude-4'],
      ['claude-3-7-sonnet', 'Claude-3'],
      ['gemini-2.5-pro', 'Gemini-2'],
      // 家族名自带版本号,显示不加横杠
      ['qwen3-max', 'Qwen3'],
      ['o3-mini', 'o3'],
      // 中转的命名空间前缀是渠道不是型号,按它分会把一家糊成一堆
      ['openai/gpt-4o', 'GPT-4'],
      ['anthropic/claude-sonnet-4-5', 'Claude-4'],
      // 按用途分的几类横跨各家,先于家族判断
      ['text-embedding-3-small', 'Embedding'],
      ['gpt-4o-mini-tts', 'TTS'],
      ['gpt-image-1', 'Image'],
      ['gemini-3-pro-image-preview', 'Image'],
    ]
    for (const [id, want] of cases) {
      const got = modelGroup(id)
      if (got !== want) throw new Error(`modelGroup(${id}) = ${got},应为 ${want}`)
    }
    if (modelGroup('gpt-6-astra') === '其他') throw new Error('新家族仍然掉进「其他」')
    console.log('  OK   模型分组')
  } catch (e) {
    note('模型分组', e)
  }

  await mount('供应商页', <ProvidersTab />, async () => {
    await click('API 密钥管理')
    await click('关闭')
    await click('检测')
    await click('关闭')
  })
  await mount('供应商页 · 自定义参数校验', <ProvidersTab />, async () => {
    await typeArea('stream_options', '{ 这不是 JSON')
    const shown = document.body.textContent || ''
    if (!shown.includes('JSON 解析失败')) throw new Error('非法 JSON 没有当场提示')
    await typeArea('stream_options', '[1,2,3]')
    if (!(document.body.textContent || '').includes('最外层要是一个对象')) {
      throw new Error('顶层不是对象时没有提示')
    }
  })

  await mount('助手预设页', <AssistantsTab />)
  await mount('默认与自动起标题页', <DefaultsTab />, async () => {
    const txt = document.body.textContent || ''
    if (!txt.includes('工具箱工具')) throw new Error('没有工具箱工具开关')
    // 打开这个开关等于让模型读本机文件、且读到的内容会外发。
    // 代价必须写在开关旁边 —— 藏进文档就等于没说
    if (!txt.includes('发给模型供应商')) throw new Error('没有说明工具结果会外发')

    // 勾一下就该落库。这一页原来只有一张卡片里有保存按钮,却管着三张卡片的设置 ——
    // 在别的卡片上改完不滚回去点它就白改,而那个按钮在没启用供应商时还是禁用的
    delete __last.aiConfig
    const boxes = Array.from(
      document.querySelectorAll('input[type=checkbox]'),
    ) as HTMLInputElement[]
    const toolBox = boxes[boxes.length - 1]
    if (!toolBox) throw new Error('找不到工具箱工具的复选框')
    await act(async () => {
      toolBox.click()
    })
    await act(async () => {
      await sleep(60)
    })
    const saved = __last.aiConfig as { localTools?: boolean } | undefined
    if (!saved) throw new Error('勾选后没有落库 —— 还得再点一次保存才生效')
    if (saved.localTools !== true) {
      throw new Error('落库的 localTools 不是 true: ' + JSON.stringify(saved))
    }
    if (!(document.body.textContent || '').includes('已保存')) {
      throw new Error('自动保存没有任何回显,用户没法确认存上了')
    }
  })

  // 4) 导出弹窗:切勾选项要重新渲染预览,点保存要走"用户取消"那条分支
  await mount(
    '导出弹窗',
    <ExportDialog
      conversationId={(conversations as { id: string }[])[1].id}
      title="导出测试"
      onClose={() => {}}
      onError={() => {}}
    />,
    async () => {
      const box = (Array.from(
        document.querySelectorAll('input[type=checkbox]'),
      ) as HTMLElement[])[1]
      if (box) {
        await act(async () => {
          box.click()
        })
        await act(async () => {
          await sleep(60)
        })
      }
      await click('保存为文件')
    },
  )

  // 5) 会话设置弹窗:套一个带参数的预设,再切 Markdown 预览。
  // 提示词留空 —— 非空时套预设会先弹替换确认,那条路单独测
  await mount(
    '会话设置弹窗',
    <ConversationDialog
      initial={{ title: '新对话', system: '', contextCount: 10 }}
      onClose={() => {}}
      onSave={() => {}}
    />,
    async () => {
      await mustClick('逆子AI')
      await mustClick('预览')
    },
  )

  // 6) 已经写了提示词时套预设:要先弹「替换系统提示词」确认
  await mount(
    '会话设置弹窗 · 预设覆盖确认',
    <ConversationDialog
      initial={{ title: '有人设的会话', system: '你是一个 Go 专家', contextCount: 10 }}
      onClose={() => {}}
      onSave={() => {}}
    />,
    async () => {
      await mustClick('素预设')
      await mustClick('替换') // 提示词非空,必须先过这道确认
    },
  )

  // 7) 被中断的回复必须给出「继续写」入口
  await mount(
    '截断回复的继续写按钮',
    <ChatPane conversationId="c-truncated" onTitleChange={() => {}} onExport={() => {}} />,
    async () => {
      if (!btn('继续写')) throw new Error('truncated 的最后一条没有渲染出「继续写」')
    },
  )

  // 8) 跨会话搜索:输入后要出结果,只命中标题的那条要给可点的入口
  await mount(
    '跨会话搜索',
    <GlobalSearchDialog onPick={() => {}} onClose={() => {}} />,
    async () => {
      await mustType('在所有会话里查找', '泛型')
      // 防抖 200ms,得等过去
      await act(async () => {
        await sleep(320)
      })
      const txt = document.body.textContent || ''
      if (!txt.includes('富消息')) throw new Error('搜索结果没出来')
      if (!txt.includes('这条会话里另有 3 处')) throw new Error('剩余处数没显示')
      if (!txt.includes('标题匹配')) throw new Error('只命中标题的那条没标出来')
      await mustClick('打开这条会话')
    },
  )

  // 9) 请求留档面板:切到失败那条、翻到响应页。
  // 拿不到详情的那条要显示后端给的话,不能白着
  await mount(
    '请求留档面板',
    <TraceDialog conversationId="c-rich" onClose={() => {}} />,
    async () => {
      await mustClick('gpt-5.6-luna') // 列表里第一条
      await mustClick('响应 (2 帧)')
      // 检测那条没有 convId,勾着过滤时必须有"另有 N 条"的提示,
      // 否则用户会以为检测根本没被记下来
      if (!(document.body.textContent || '').includes('另有')) {
        throw new Error('被过滤掉的记录没有给出提示')
      }
      await check('只看当前会话') // 取消过滤:检测标记和"进行中"那条都要画出来
      if (!(document.body.textContent || '').includes('检测')) {
        throw new Error('检测来源的记录没有标出来')
      }
    },
  )

  // 10) 流结束后必须回头重读会话。
  //
  // done 事件的载荷只有正文 —— 截断标记、token 用量、耗时、后端定下的标题
  // 全都不在里面。少了这次重读,用户点"停止"之后就看不到「继续写」,
  // 而切走再切回来又一切正常,是个极难查的表现。
  {
    const conv = (conversations as { id: string }[])[1]
    await mount('流结束后重读会话', <ChatPane conversationId={conv.id} onTitleChange={() => {}} onExport={() => {}} />, async () => {
      const before = __calls.GetAIConversation || 0
      await act(async () => {
        __emit('ai-chat:done:' + conv.id, '写到一半被停掉的正文')
      })
      await act(async () => {
        await sleep(60)
      })
      const after = __calls.GetAIConversation || 0
      if (after <= before) {
        throw new Error('流结束后没有重读会话 —— 截断标记 / 用量 / 标题都会停在旧值')
      }
    })
  }

  // 11) MMKV 工具页。解析搬到 Go 之后这个页面整个重写过一遍,
  // 而它以前完全没有冒烟覆盖 —— 白屏了也没人知道
  await mount('MMKV 页', <MmkvTool />, async () => {
    await mustClick('选择文件')
    const txt = () => document.body.textContent || ''
    if (!txt().includes('user_name')) throw new Error('表格没渲染出 key')
    if (!txt().includes('张三')) throw new Error('值没有按后端猜的类型显示')
    // 默认按 best 显示,而不是一律甩一串十六进制让人一个个点回去
    if (txt().includes('06e5bca0e4b889')) throw new Error('默认显示成了原始十六进制,没用 best')
    if (!txt().includes('2 个历史值')) throw new Error('历史值条数没显示')
    if (!txt().includes('1 个删除标记')) throw new Error('删除标记数没显示')

    // 点类型徽章循环:只在解得通的类型之间转。
    // user_name 能读成 hexstring / string / bytes,从 string 点一下应该到 bytes
    if (txt().includes('(bytes)')) throw new Error('初始就有 bytes,这条断言失去意义')
    await mustClick('(string)')
    if (!txt().includes('(bytes)')) {
      throw new Error('点徽章没有切到下一个解得通的类型')
    }
    // 只有一种读法的值,徽章不该能点出别的来
    if (!txt().includes('(bool)')) throw new Error('单一读法的值没按 best 显示')
  })

  // 12) MMKV 详情弹窗:后端一次给全所有读法,这里要都列出来
  await mount('MMKV 值详情', <MmkvTool />, async () => {
    // store 是模块级的,上一条用例加载的文件还在,空状态的「选择文件」按钮不一定在;
    // 工具栏的「打开」任何时候都在
    await mustClick('打开')
    const expands = Array.from(document.querySelectorAll('button[title="展开查看完整值"]'))
    if (expands.length === 0) throw new Error('没有展开按钮')
    await act(async () => {
      ;(expands[0] as HTMLElement).click()
    })
    await act(async () => {
      await sleep(60)
    })
    const txt = document.body.textContent || ''
    if (!txt.includes('其它可能的读法')) throw new Error('详情里没列出别的读法')
    if (!txt.includes('原始字节')) throw new Error('详情里没有原始字节')
  })

  // 12b) 被截断的大值:详情里要能回后端读完整字节。
  // 表格里那份是截断过的,少了这条路就等于"完整十六进制再也拿不到"
  await mount('MMKV 完整字节', <MmkvTool />, async () => {
    await mustClick('打开')
    const expands = Array.from(
      document.querySelectorAll('button[title="展开查看完整值"]'),
    ) as HTMLElement[]
    // blob 是第 3 个 key(user_name / token 两个值 / blob / enabled),
    // 按顺序数它的展开按钮排在第 4 个
    const target = expands[3]
    if (!target) throw new Error('找不到 blob 那行的展开按钮')
    await act(async () => {
      target.click()
    })
    await act(async () => {
      await sleep(60)
    })
    if (!(document.body.textContent || '').includes('读取完整 4098 字节')) {
      throw new Error('截断的值没有给出「读取完整」入口')
    }
    await mustClick('读取完整 4098 字节')
    if (!(document.body.textContent || '').includes('完整字节:ff00ff00deadbeef')) {
      throw new Error('点了「读取完整」但没显示后端返回的完整字节')
    }
  })

  // 13) plist 工具页:状态栏按后端给的 format 显示,
  // notes 里的提醒(循环引用之类)不能吞掉
  await mount('plist 页', <PlistTool />, async () => {
    await mustClick('导入')
    let txt = document.body.textContent || ''
    if (!txt.includes('二进制 Plist')) throw new Error('状态栏没显示后端给的格式')
    if (!txt.includes('NSKeyedArchive')) throw new Error('归档标记没显示')
    if (!txt.includes('循环引用')) throw new Error('后端的 notes 被吞了')

    await mustClick('解析结果')
    txt = document.body.textContent || ''
    if (!txt.includes('第一项')) throw new Error('解析结果视图没渲染后端给的 parsed')

    await mustClick('原始结构')
    if (!(document.body.textContent || '').includes('$archiver')) {
      throw new Error('原始结构视图没渲染后端给的 raw')
    }
  })

  // 14) plist 编辑器打字:防抖后应该真的调一次后端,而不是前端自己解。
  // 前端那套 TypeScript 解析器已经删了,这条断言就是"确实删干净了"的证据
  await mount('plist 编辑器防抖解析', <PlistTool />, async () => {
    // 不直接对 CodeMirror 打字 —— 它在 jsdom 里不是个普通输入框。
    // 「示例」按钮走的是同一条路:setXmlText -> 防抖 -> 调后端解析
    const before = __calls.ParsePlistText || 0
    await mustClick('示例')
    await act(async () => {
      await sleep(400) // 防抖 250ms
    })
    if ((__calls.ParsePlistText || 0) <= before) {
      throw new Error('编辑器打字后没有调后端解析')
    }
  })

  // 15) 真机数据浏览器。连接那一层没法在 jsdom 里跑(要一台插着的越狱手机),
  // 但界面这一层能:连上之后列目录、点开文件按类型预览、搜索。
  // 真机端到端是另外验的(直连设备跑过 List/Preview/Search)
  await mount('真机浏览 · 连接与列目录', <MemoryRouter><DeviceBrowser /></MemoryRouter>, async () => {
    // 没连接时应该是连接面板,而不是一个空的浏览器
    if (!(document.body.textContent || '').includes('连接一台设备')) {
      throw new Error('未连接时没有显示连接面板')
    }
    await mustType('越狱设备默认', '123456')
    await mustClick('连接')
    const txt = document.body.textContent || ''
    if (!txt.includes('com.apple.springboard.plist')) throw new Error('没有列出目录内容')
    if (!txt.includes('Accounts')) throw new Error('目录条目没画出来')
    // 软链必须标出来:iOS 上到处是软链,不标的话人会以为看到了两份数据
    if (!txt.includes('/private/var/mobile/LegacyData')) {
      throw new Error('软链的指向没有显示')
    }
  })

  // 16) 点开一个 plist:预览面板要按后端给的 kind 画,并说清楚凭什么这么判
  await mount('真机浏览 · 预览 plist', <MemoryRouter><DeviceBrowser /></MemoryRouter>, async () => {
    await mustClick('com.apple.springboard.plist')
    const txt = document.body.textContent || ''
    if (!txt.includes('SBHomeScreenPageCount')) throw new Error('没有渲染后端解出来的 plist')
    if (!txt.includes('文件头是 bplist')) throw new Error('没有说明凭什么判成 plist')
    // 导出走的是原生目录选择 + 后端整文件拉取,成功后给行内提示而不是弹窗
    await mustClick('导出')
    if (!(document.body.textContent || '').includes('已导出到')) {
      throw new Error('导出成功后没有给出落地路径')
    }
  })

  // 17) 搜索:结果列表要能出来,并且能切回目录
  await mount('真机浏览 · 搜索', <MemoryRouter><DeviceBrowser /></MemoryRouter>, async () => {
    await mustType('从这里往下找', 'plist')
    const input = document.querySelector(
      'input[placeholder*="从这里往下找"]',
    ) as HTMLInputElement
    await act(async () => {
      input.dispatchEvent(
        new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }),
      )
    })
    await act(async () => {
      await sleep(80)
    })
    const txt = document.body.textContent || ''
    if (!txt.includes('com.apple.mobilesafari.plist')) throw new Error('搜索结果没出来')
    if (!txt.includes('找到 2 条')) throw new Error('没有显示命中条数')
    // 搜索是递归的,结果来自各个层级 —— 不写明从哪儿开始搜,
    // 人会以为这些文件都在当前目录里
    if (!txt.includes('往下递归查找')) throw new Error('没有说明搜索范围')
    await mustClick('返回目录')
    if (!(document.body.textContent || '').includes('Accounts')) {
      throw new Error('返回目录后没有回到列表')
    }
  })

  // 真机浏览 · iOS 投屏(接着上面留下的 iOS 会话)。noVNC、WebSocket、画布、录屏都换成假的:
  // 没装 TrollVNC 时教人装、装好接着投;连上以后键盘(含中文经剪贴板)、按钮、截图、录屏、断线重连都走一遍
  await mount(
    '真机浏览 · iOS 投屏',
    <MemoryRouter initialEntries={['/tools/device-browser']}>
      <DeviceBrowser />
    </MemoryRouter>,
    async () => {
      const txt = () => document.body.textContent || ''
      const fake = installMirrorFakes()
      const g = globalThis as unknown as Record<string, unknown>
      const saved = { MediaRecorder: g.MediaRecorder, FileReader: g.FileReader, HTMLCanvasElement: g.HTMLCanvasElement }
      const canvasProto = window.HTMLCanvasElement.prototype as unknown as Record<string, unknown>
      const savedCapture = canvasProto.captureStream
      // 假的录屏:开始后交一段,停的时候再交一段,然后报停
      g.FileReader = window.FileReader
      g.HTMLCanvasElement = window.HTMLCanvasElement
      g.MediaRecorder = class {
        static isTypeSupported(t: string) {
          return t.startsWith('video/mp4')
        }
        state = 'inactive'
        ondataavailable: ((e: { data: Blob }) => void) | null = null
        private stopFns: (() => void)[] = []
        start() {
          this.state = 'recording'
          setTimeout(() => this.ondataavailable?.({ data: new window.Blob([new Uint8Array([1, 2, 3])]) }), 5)
        }
        addEventListener(type: string, fn: () => void) {
          if (type === 'stop') this.stopFns.push(fn)
        }
        stop() {
          this.state = 'inactive'
          this.ondataavailable?.({ data: new window.Blob([new Uint8Array([4])]) })
          for (const fn of this.stopFns) fn()
        }
      }
      canvasProto.captureStream = () => ({})
      const wait = async (ms = 30) => {
        await act(async () => {
          await sleep(ms)
        })
      }
      const titled = (prefix: string) =>
        (Array.from(document.querySelectorAll('button')) as HTMLElement[]).find((b) =>
          (b.getAttribute('title') || '').startsWith(prefix),
        )
      const clickTitled = async (prefix: string) => {
        const b = titled(prefix)
        if (!b) throw new Error('找不到按钮「' + prefix + '…」')
        await act(async () => {
          b.click()
        })
        await wait(50)
      }
      try {
        // ---- 没装 TrollVNC:说清楚下哪个包,选了就经 SSH 装上,装好接着投 ----
        __last.trollMissing = true
        const startsBefore = (__last.iosStarts as number | undefined) ?? 0
        await mustClick('投屏')
        await wait()
        if (!txt().includes('还没装 TrollVNC') || !txt().includes('packages-rootless')) {
          throw new Error('没装时没说清楚要下哪个包: ' + txt())
        }
        if (((__last.iosStarts as number | undefined) ?? 0) !== startsBefore) throw new Error('没装就去启动了')
        await mustClick('选择安装包并安装')
        await wait(80)
        const inst = __last.trollInstall as { id: string; pkg: string } | undefined
        if (inst?.pkg !== 'D:/下载/packages-rootless.zip') throw new Error('选的安装包没交给后端: ' + JSON.stringify(inst))
        const start = __last.iosStart as { id: string; opt: { keepAwake: boolean } } | undefined
        if (!start || (__last.iosStarts as number) <= startsBefore) throw new Error('装好以后没接着投屏')
        // 投的是真机浏览连着的那一条会话;默认保持亮屏
        if (start.id !== inst.id || start.opt.keepAwake !== true) throw new Error('开投屏的参数不对: ' + JSON.stringify(start))

        // 窗口藏起来再回来会换一条新的连接,后面的用例跟着换
        let rfb = vncInstances[vncInstances.length - 1]
        let ws = fake.sockets[fake.sockets.length - 1]
        if (!rfb || !ws?.url.includes('/vnc/')) {
          throw new Error(
            `没去连 VNC(noVNC 实例 ${vncInstances.length} 个,连接 ${fake.sockets.map((s) => s.url).join()}):` +
              (document.querySelector('[data-mirror-panel]')?.textContent || '').slice(0, 300),
          )
        }
        if (rfb.channel !== ws) throw new Error('noVNC 该用面板自己建的 WebSocket')
        if (rfb.options.credentials?.password !== 'Ab3dEf7h') throw new Error('这次的密码没交给 VNC 客户端')
        // 键盘归面板自己的输入框(要接输入法),画面按面板大小缩放
        if (rfb.focusOnClick !== false || rfb.scaleViewport !== true || rfb.qualityLevel !== 7) {
          throw new Error('noVNC 的设置不对: ' + JSON.stringify([rfb.focusOnClick, rfb.scaleViewport, rfb.qualityLevel]))
        }
        await act(async () => {
          rfb.connect(1244, 2212)
          await sleep(30)
        })
        // 带省略号的是启动中的那一屏;装好时的提示条里也有「正在启动投屏」几个字
        if (txt().includes('正在启动投屏…')) throw new Error('连上了还挂着「启动中」')
        if (fake.draws === 0) throw new Error('画面没画到显示用的画布上')

        // ---- 窗口藏起来:只断开画面,手机上的服务留着;回来直接接上,不重开服务 ----
        // (重开要经 SSH 改设置、让 cfprefsd 重读,紧挨着重开在手机上要卡二十秒)
        const hideShow = async () => {
          Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true })
          try {
            await act(async () => {
              document.dispatchEvent(new window.Event('visibilitychange'))
              await sleep(30)
            })
          } finally {
            delete (document as unknown as Record<string, unknown>).visibilityState
          }
          await act(async () => {
            document.dispatchEvent(new window.Event('visibilitychange'))
            await sleep(60)
          })
        }
        const sessionNow = 'i' + String(__last.iosStarts)
        const startsBeforeHide = __last.iosStarts as number
        const stopsBeforeHide = ((__last.iosStops as string[] | undefined) ?? []).length
        await hideShow()
        if (!rfb.disconnected) throw new Error('窗口藏起来该断开画面')
        if (!(__last.iosPauses as string[] | undefined)?.includes(sessionNow)) throw new Error('藏起来没告诉后端把服务留着')
        if (((__last.iosStops as string[] | undefined) ?? []).length !== stopsBeforeHide) throw new Error('藏起来不该停手机上的服务')
        if ((__last.iosStarts as number) !== startsBeforeHide) throw new Error('回来不该重开服务')
        if (!(__last.iosResumes as string[] | undefined)?.includes(sessionNow)) throw new Error('回来没问后端这一路还在不在')
        const resumed = vncInstances[vncInstances.length - 1]
        if (resumed === rfb || resumed.options.credentials?.password !== 'Ab3dEf7h') {
          throw new Error('回来该用原来的地址和密码重新接上画面')
        }
        rfb = resumed
        ws = fake.sockets[fake.sockets.length - 1]
        await act(async () => {
          rfb.connect(1244, 2212)
          await sleep(30)
        })
        if (txt().includes('正在接回画面')) throw new Error('接上了还挂着「接回画面」')
        // 藏太久,后端已经把服务收掉了:回来要重开一路
        __last.iosGone = true
        try {
          await hideShow()
        } finally {
          __last.iosGone = false
        }
        if ((__last.iosStarts as number) !== startsBeforeHide + 1) throw new Error('后端已经收掉了,回来该重开一路')
        rfb = vncInstances[vncInstances.length - 1]
        ws = fake.sockets[fake.sockets.length - 1]
        await act(async () => {
          rfb.connect(1244, 2212)
          await sleep(30)
        })

        // ---- 键盘:点过画面,键盘就归手机 ----
        const host = document.querySelector('[data-vnc-host]') as HTMLElement
        await act(async () => {
          host.dispatchEvent(new window.MouseEvent('pointerdown', { bubbles: true, cancelable: true, clientX: 10, clientY: 10 }))
        })
        const kbd = document.querySelector('[data-mirror-keyboard]') as HTMLTextAreaElement
        if (document.activeElement !== kbd) throw new Error('点了画面键盘没归手机')
        const keydown = async (key: string, init: Record<string, unknown> = {}) => {
          await act(async () => {
            kbd.dispatchEvent(new window.KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...init }))
            await sleep(10)
          })
        }
        const typeIn = async (value: string) => {
          await act(async () => {
            kbd.value = value
            kbd.dispatchEvent(new window.Event('input', { bubbles: true }))
            await sleep(10)
          })
        }
        const keys = () => rfb.keys.map(([k, d]: [number, boolean]) => (d ? '+' : '-') + k.toString(16)).join(' ')
        // 放进手机剪贴板的字:面板自己发的扩展剪贴板消息(类型 6,长度取负),解开 zlib 取出 UTF-8
        const clipsSent = () =>
          (ws.sent as unknown as unknown[])
            .filter((m): m is Uint8Array => m instanceof Uint8Array && m[0] === 6)
            .map((m) => {
              const dv = new DataView(m.buffer, m.byteOffset, m.byteLength)
              if (dv.getInt32(4) !== -(m.length - 8) || dv.getUint32(8) !== 0x10000001) {
                throw new Error('剪贴板消息的头不对: ' + Array.from(m.subarray(0, 12)).join(','))
              }
              const plain = inflateSync(m.subarray(12))
              if (plain.length !== 4 + plain.readUInt32BE(0)) throw new Error('剪贴板消息里的长度不对')
              // 末尾不补 0:TrollVNC 按长度原样收,补了会跟着进 iPhone 剪贴板
              if (plain[plain.length - 1] === 0) throw new Error('剪贴板消息末尾不该有 0')
              return plain.subarray(4).toString('utf8')
            })
        // 大写字母要自己按住 Shift:手机那头不替我们补
        rfb.keys.length = 0
        await typeIn('a')
        await typeIn('B')
        if (keys() !== '+61 -61 +ffe1 +42 -42 -ffe1') throw new Error('英文按键不对: ' + keys())
        rfb.keys.length = 0
        await keydown('Enter')
        if (keys() !== '+ff0d -ff0d') throw new Error('回车不对: ' + keys())
        // 中文:上屏了才发,先放进手机剪贴板,等一下再按 Command+V
        rfb.keys.length = 0
        await act(async () => {
          kbd.dispatchEvent(new window.CompositionEvent('compositionstart', { bubbles: true }))
        })
        await typeIn('ni')
        if (clipsSent().length || rfb.clipboard.length) throw new Error('拼音还没上屏就发出去了')
        await act(async () => {
          kbd.value = '你好'
          kbd.dispatchEvent(new window.CompositionEvent('compositionend', { bubbles: true, data: '你好' }))
        })
        await wait(50)
        // 走扩展剪贴板(UTF-8);noVNC 自己的 clipboardPasteFrom 碰上 TrollVNC 会把汉字变成「?」
        if (clipsSent().join() !== '你好') throw new Error('中文没按 UTF-8 放进手机剪贴板: ' + clipsSent().join())
        if (rfb.clipboard.length) throw new Error('中文不该走 noVNC 的老格式: ' + rfb.clipboard.join())
        if (keys()) throw new Error('剪贴板还没写好就按了粘贴')
        // 中文后面紧跟着打的英文,得排在粘贴后面
        await typeIn('x')
        await wait(300)
        if (keys() !== '+ffeb +76 -76 -ffeb +78 -78') throw new Error('粘贴或者排队的顺序不对: ' + keys())
        if (!txt().includes('手机剪贴板里原来的内容会被替换')) throw new Error('没提醒剪贴板会被替换')
        if (!txt().includes('从其他 App 粘贴')) throw new Error('没说手机问「允许粘贴」时怎么办')
        // Ctrl+V:电脑剪贴板粘到手机。Windows 的换行是 \r\n,到 iPhone 上统一成 \n
        __last.pcClipboard = '电脑上\r\n复制的'
        await keydown('v', { ctrlKey: true })
        await wait(300)
        if (clipsSent().pop() !== '电脑上\n复制的') throw new Error('Ctrl+V 没把电脑剪贴板粘过去: ' + clipsSent().pop())
        // Ctrl+C:手机上按 Command+C,手机推回来的内容进电脑剪贴板
        rfb.keys.length = 0
        await keydown('c', { ctrlKey: true })
        await wait(20)
        if (keys() !== '+ffeb +63 -63 -ffeb') throw new Error('Ctrl+C 没变成 Command+C: ' + keys())
        await act(async () => {
          rfb.pushClipboard('手机上选中的')
        })
        if (__last.pcClipboardSet !== '手机上选中的' || !txt().includes('已复制到电脑')) {
          throw new Error('手机上复制的字没进电脑剪贴板')
        }
        // Alt 当 Command
        rfb.keys.length = 0
        await keydown('h', { altKey: true })
        await wait(20)
        if (keys() !== '+ffeb +68 -68 -ffeb') throw new Error('Alt+H 该变成 Command+H: ' + keys())
        // 没认领的 Ctrl 组合键不发给手机,留给工具箱自己的快捷键
        rfb.keys.length = 0
        await keydown('k', { ctrlKey: true })
        await wait(20)
        if (keys()) throw new Error('Ctrl+K 不该发给手机')
        // 对方没报扩展剪贴板:中文报错、不按粘贴(按了也只是一串「?」);西文还能走老格式
        const caps = rfb._clipboardServerCapabilitiesActions
        rfb._clipboardServerCapabilitiesActions = {}
        const clipsBefore = clipsSent().length
        await typeIn('中文')
        await wait(300)
        if (keys() || clipsSent().length !== clipsBefore || !txt().includes('没开 UTF-8 剪贴板')) {
          throw new Error('对方收不了 UTF-8 时该报错、不按粘贴: ' + keys())
        }
        __last.pcClipboard = 'café'
        await keydown('v', { ctrlKey: true })
        await wait(300)
        if (rfb.clipboard.pop() !== 'café' || keys() !== '+ffeb +76 -76 -ffeb') throw new Error('西文该走老格式粘过去: ' + keys())
        rfb._clipboardServerCapabilitiesActions = caps

        // ---- 手机剪贴板:连上以后复制过的 ----
        await clickTitled('手机剪贴板')
        if (!(document.querySelector('[data-mirror-clipboard]')?.textContent || '').includes('手机上选中的')) {
          throw new Error('剪贴板卡片没显示手机上复制的')
        }
        await clickTitled('收起')

        // ---- 按钮:Home、锁屏是 VNC 的鼠标消息,音量是媒体键 ----
        const pointerMsgs = () =>
          (ws.sent as unknown as unknown[])
            .filter((m): m is Uint8Array => m instanceof Uint8Array && m[0] === 5)
            .map((m) => Array.from(m).slice(0, 2).join(','))
            .join(' ')
        await clickTitled('主页')
        if (pointerMsgs() !== '5,4 5,0') throw new Error('Home 没按出去: ' + pointerMsgs())
        await clickTitled('锁屏')
        if (!pointerMsgs().endsWith('5,2 5,0')) throw new Error('锁屏没按出去: ' + pointerMsgs())
        rfb.keys.length = 0
        await clickTitled('音量 +')
        if (keys() !== '+1008ff13 -1008ff13') throw new Error('音量键不对: ' + keys())

        // ---- 画质:不用重连,直接改 ----
        const startsNow = __last.iosStarts as number
        await clickTitled('画质：')
        if (rfb.qualityLevel !== 9 || (__last.iosStarts as number) !== startsNow) throw new Error('换画质该直接改,不该重连')

        // ---- 截图:原尺寸 PNG 交给后端存 ----
        await clickTitled('截图：')
        await wait(50)
        const shot = __last.iosShot as { dir: string; label: string; b64: string } | undefined
        if (shot?.dir !== 'D:/导出' || shot.label !== 'iPhone 8 Plus' || shot.b64 !== 'iVBORw==') {
          throw new Error('截图没交给后端: ' + JSON.stringify(shot))
        }
        if (!txt().includes('截图已保存（1244×2212）')) throw new Error('截图存好了没说')

        // ---- 录屏:能录 MP4 就录 MP4,一段段交给后端,停了说存了多长 ----
        await clickTitled('录屏：')
        await wait(50)
        const rec = __last.iosRec as { ext: string; chunks: number } | undefined
        if (rec?.ext !== '.mp4' || rec.chunks < 1) throw new Error('录屏没开起来,或者没交数据: ' + JSON.stringify(rec))
        if (!titled('保持亮屏：录屏中不能改')) throw new Error('录屏中保持亮屏该锁住')
        // 录着的时候转屏:noVNC 的画布换了尺寸。一个 MP4 只能有一种尺寸,得收掉这一段、另起一个文件
        await act(async () => {
          rfb.canvas.width = 2212
          rfb.canvas.height = 1244
          ws.emit(new ArrayBuffer(4))
          await sleep(80)
        })
        if ((__last.iosRecParts as number) !== 2) throw new Error('转屏后录屏没换到下一个文件: ' + __last.iosRecParts)
        if (!btn('横屏了，全屏看更大')) throw new Error('横过来了没给全屏的入口')
        await clickTitled('停止录屏')
        await wait(50)
        if (!__last.iosRecEnd || rec.chunks < 2) throw new Error('录屏没停干净: ' + JSON.stringify(rec))
        if (!txt().includes('录屏已保存（12 秒，中间转过屏，分成了 2 个文件）')) throw new Error('录屏存好了没说清楚: ' + txt())

        // ---- 拖文件进来:推到「文件」App 的「我的 iPhone › Downloads」 ----
        const dropFn = __last.fileDrop as ((x: number, y: number, paths: string[]) => void) | null
        if (!dropFn) throw new Error('iOS 投屏面板没接上原生拖放')
        const keepPoint = document.elementFromPoint
        document.elementFromPoint = () => host
        try {
          await act(async () => {
            dropFn(10, 10, ['D:/数据/照片.jpg', 'D:/数据/资料'])
            await sleep(5)
          })
          if (!txt().includes('正在推送 照片.jpg 50%')) throw new Error('推送进度没显示')
          await wait(60)
        } finally {
          document.elementFromPoint = keepPoint
        }
        const dropReq = __last.iosDrop as { id: string; paths: string[] } | undefined
        if (dropReq?.id !== inst.id || dropReq.paths.length !== 2) throw new Error('拖进来的文件没交给后端: ' + JSON.stringify(dropReq))
        if (!txt().includes('推了 4 个文件到手机「文件」App › 我的 iPhone › Downloads')) throw new Error('拖放结果没说清楚: ' + txt())
        if (!txt().includes('存储图像')) throw new Error('推了照片该提醒怎么存进相册')

        // ---- 断了:说原因,能重新连接 ----
        await act(async () => {
          rfb.drop()
        })
        if (!txt().includes('投屏断开了')) throw new Error('断开了没说')
        const beforeRe = __last.iosStarts as number
        await mustClick('重新连接')
        await wait(80)
        if ((__last.iosStarts as number) <= beforeRe) throw new Error('重新连接没重开一路')

        // ---- 换安装包(更新自己编的版本):断开、装、重新开 ----
        __last.trollInstall = undefined
        const beforeSwap = __last.iosStarts as number
        await clickTitled('TrollVNC 3.2-272：换一个安装包')
        await wait(80)
        if (!__last.trollInstall || (__last.iosStarts as number) <= beforeSwap) throw new Error('换安装包没装、或者装完没重新投屏')
        if (!txt().includes('TrollVNC 换成了 3.2-272')) throw new Error('换完没说')

        // ---- 关面板:后端停掉这一路,手机上的服务跟着停 ----
        await clickTitled('关闭投屏')
        const current = 'i' + String(__last.iosStarts)
        if (!(__last.iosStops as string[] | undefined)?.includes(current)) {
          throw new Error('关面板没停掉正在投的那一路: ' + JSON.stringify(__last.iosStops))
        }
      } finally {
        fake.restore()
        Object.assign(g, saved)
        canvasProto.captureStream = savedCapture
      }
    },
  )

  // 18) Android:换平台后连接面板要变(不要 SSH 密码、要 adb 路径),
  // 连上之后 root 状态必须一眼看得到 —— 没 root 就看不到 /data,这是最关键的状态
  await mount('真机浏览 · Android', <MemoryRouter><DeviceBrowser /></MemoryRouter>, async () => {
    await mustClick('断开') // 上一条用例留着 iOS 会话,先断掉回到连接面板
    await mustClick('Android')
    const panel = document.body.textContent || ''
    if (panel.includes('SSH 密码')) throw new Error('Android 不该要 SSH 密码')
    if (!panel.includes('adb 路径')) throw new Error('Android 该给 adb 路径的入口')
    await mustClick('连接')
    const txt = document.body.textContent || ''
    // 面包屑是一段段渲染的,整条路径不会作为连续文本出现;断言列出来的内容
    if (!txt.includes('com.tencent.mm')) throw new Error('没有列出 Android 起始目录的内容')
    if (txt.includes('com.apple.springboard')) throw new Error('还在显示 iOS 那台的目录')
    if (!txt.includes('root')) throw new Error('root 状态没显示')
    if (!txt.includes('22041216C')) throw new Error('设备型号没显示')
    // 常用位置要换成 Android 那套
    if (!txt.includes('应用数据')) throw new Error('常用位置没换成 Android 的')
    // 文件夹整个导出:以前只有选中单个文件才导得了
    if (!txt.includes('导出此目录')) throw new Error('没有导出当前目录的入口')
    if (txt.includes('通讯录捐赠')) throw new Error('Android 下还在显示 iOS 的常用位置')

    // 工具间跳转:翻到的目录直接送去移动取证
    if (!btn('用移动取证导出')) throw new Error('没有跳去移动取证的入口')

    // 监视模式:拍基线 → 去手机上操作 → 列出变化。点「立即检查」不用等定时
    delete __last.diffMode
    await mustClick('监视此目录')
    const t2 = () => document.body.textContent || ''
    if (!t2().includes('监视中')) throw new Error('开始监视后没有面板')
    // 开始监视的第一下必须是重新拍基线,不然拿到的是上一次留下的旧基线
    if (__last.diffMode !== 'reset') {
      throw new Error('开始监视没有先拍基线: ' + __last.diffMode)
    }

    // 默认是基线对比:现场的问法几乎都是"这一趟操作总共动了哪些文件"
    await mustClick('立即检查')
    if (__last.diffMode !== 'baseline') throw new Error('默认该是基线对比: ' + __last.diffMode)
    if (!t2().includes('msg.db')) throw new Error('检查后没列出变化的文件')
    if (!t2().includes('修改')) throw new Error('变化类型没标出来')
    if (!t2().includes('净变化')) throw new Error('基线模式该说清楚这是净变化')
    if (!t2().includes('基线 ')) throw new Error('没显示基线是什么时候拍的')
    // 变化在 com.tencent.mm 底下两层,列表里那个目录要标出"底下有变化" ——
    // 目录自己的修改时间不会变,不标的话人看不出该往哪儿点
    if (!t2().includes('内有变化')) throw new Error('列表行没标出底下有变化')

    // 基线模式下再查一次,拿到的是同一批净变化,不能累积成两倍
    const before = (document.querySelectorAll('button[title*="点击跳到它所在的目录"]') || []).length
    await mustClick('立即检查')
    const after = (document.querySelectorAll('button[title*="点击跳到它所在的目录"]') || []).length
    if (after !== before) {
      throw new Error(`基线模式给的是全量净变化,不该累积:${before} → ${after}`)
    }

    // 重新拍基线:以此刻为准,记录清空
    await mustClick('以此刻为准重新拍基线 —— 去手机上做操作之前按一下')
    if (__last.diffMode !== 'reset') throw new Error('没有重新拍基线: ' + __last.diffMode)
    if (t2().includes('msg.db')) throw new Error('重新拍基线后旧记录该清掉')

    // 切到持续监视:回答的是另一个问题,记录也要清掉
    await mustClick('持续监视')
    await mustClick('立即检查')
    if (__last.diffMode !== 'rolling') throw new Error('切模式后没按新模式比: ' + __last.diffMode)
    if (t2().includes('净变化')) throw new Error('持续监视不该还说净变化')

    await mustClick('停止监视')
    if (t2().includes('监视中')) throw new Error('停止后面板还在')
  })

  // 真机浏览 · 投屏(接着上一条留下的安卓会话)。
  // jsdom 里没有 WebCodecs:先验证用不了时说清楚、不去启动手机端程序;
  // 再换上假的解码器、WebSocket 和画布,把「收包 → 解码 → 画出来 → 鼠标变成操作」走一遍
  // 页面是不是正在显示,看的是路由:挂在真机浏览自己的地址上,并且留一个能切走的把手
  let go: ((path: string) => void) | null = null
  const NavHandle = () => {
    go = useNavigate()
    return null
  }
  await mount(
    '真机浏览 · 投屏',
    <MemoryRouter initialEntries={['/tools/device-browser']}>
      <NavHandle />
      <DeviceBrowser />
    </MemoryRouter>,
    async () => {
      const txt = () => document.body.textContent || ''
      delete __calls.StartMirror
      await mustClick('投屏')
      if (!txt().includes('不支持视频解码')) throw new Error('没有 WebCodecs 时该说清楚投屏用不了')
      if (__calls.StartMirror) throw new Error('解不了视频还去启动手机端程序')
      await mustClick('关闭投屏')

      const fake = installMirrorFakes()
      try {
        await mustClick('投屏')
        await act(async () => {
          await sleep(30)
        })
        const req = __last.mirrorStart as { serial: string; options: { maxSize: number; keepAwake: boolean } } | undefined
        // 投的必须是连着的那一台:连接时序列号留空(= 第一台)也一样,不能再按「第一台」猜一次
        if (req?.serial !== 'Y9U469XKRK6XNFGY') throw new Error('没投连着的那一台: ' + JSON.stringify(req))
        // 默认档要传得够大:手机先缩一遍、电脑再缩一遍,细字就糊了
        if (req.options.maxSize !== 1920) throw new Error('默认画质不对: ' + JSON.stringify(req.options))
        // 默认保持亮屏:手机到点自己息屏,一边翻文件一边看着的画面就黑了
        if (req.options.keepAwake !== true) throw new Error('默认该保持亮屏: ' + JSON.stringify(req.options))
        const ws = fake.sockets[fake.sockets.length - 1]
        if (!ws?.url.includes('/mirror/')) throw new Error('没去连视频通道')

        await act(async () => {
          ws.emit(mirrorSession(540, 1200))
          ws.emit(mirrorMedia(MIRROR_CONFIG, 0, [0, 0, 0, 1, 0x67, 0x64, 0x00, 0x20, 0xac, 0, 0, 0, 1, 0x68, 0xee]))
          ws.emit(mirrorMedia(MIRROR_KEY, 1000, [0, 0, 0, 1, 0x65, 0x88]))
        })
        const dec = fake.decoders[fake.decoders.length - 1]
        if (dec?.configs[0]?.codec !== 'avc1.640020') throw new Error('编码串没从 SPS 里拼对: ' + JSON.stringify(dec?.configs))
        // 关键帧前面要垫上配置包(15 字节),解码器才认
        if (dec.chunks[0]?.type !== 'key' || dec.chunks[0].size !== 15 + 6) {
          throw new Error('关键帧没带上配置包: ' + JSON.stringify(dec.chunks))
        }
        if (fake.draws === 0) throw new Error('解出来的画面没画到画布上')
        if (txt().includes('正在启动投屏')) throw new Error('出画面了还挂着「启动中」')

        // 鼠标 → 触摸:画面显示成 270x600,点正中间 = 视频里的 (270, 600)
        const canvas = document.querySelector('[data-mirror-canvas]') as HTMLCanvasElement
        canvas.getBoundingClientRect = () =>
          ({ left: 0, top: 0, width: 270, height: 600, right: 270, bottom: 600, x: 0, y: 0, toJSON() {} }) as DOMRect
        const pointer = async (type: string, button: number, x: number, y: number) => {
          await act(async () => {
            canvas.dispatchEvent(new window.MouseEvent(type, { bubbles: true, cancelable: true, button, clientX: x, clientY: y }))
          })
        }
        const sent = () => ws.sent.map((s) => JSON.parse(s) as Record<string, number | string>)
        await pointer('pointerdown', 0, 135, 300)
        await pointer('pointerup', 0, 135, 300)
        const touches = sent().filter((e) => e.t === 'touch')
        if (touches.length !== 2 || touches[0].a !== 0 || touches[1].a !== 1 || touches[0].x !== 270 || touches[0].y !== 600 || touches[0].w !== 540) {
          throw new Error('点击没变成正确的触摸: ' + JSON.stringify(touches))
        }
        // 右键 = 返回
        await pointer('pointerdown', 2, 10, 10)
        await pointer('pointerup', 2, 10, 10)
        if (sent().filter((e) => e.t === 'back').length !== 2) throw new Error('右键没变成返回')
        await mustClick('最近任务')
        if (!sent().some((e) => e.t === 'key' && e.k === 187 && e.a === 1)) throw new Error('最近任务键没发出去')
        // 滚轮往下一格 = 安卓里往下滑,值是负的
        await act(async () => {
          canvas.dispatchEvent(new window.WheelEvent('wheel', { bubbles: true, cancelable: true, deltaY: 100, clientX: 135, clientY: 300 }))
          await sleep(20)
        })
        const scroll = sent().find((e) => e.t === 'scroll')
        if (scroll?.vs !== -1) throw new Error('滚轮没变成滑动: ' + JSON.stringify(scroll))

        // 按钮的提示很长,按开头找
        const titled = (prefix: string) =>
          (Array.from(document.querySelectorAll('button')) as HTMLElement[]).find((b) =>
            (b.getAttribute('title') || '').startsWith(prefix),
          )
        const clickTitled = async (prefix: string) => {
          const b = titled(prefix)
          if (!b) throw new Error('找不到按钮「' + prefix + '…」')
          await act(async () => {
            b.click()
          })
          await act(async () => {
            await sleep(50)
          })
        }
        const count = (pred: (e: Record<string, number | string>) => boolean) => sent().filter(pred).length

        // ---- 卡键:按着的键在画面外松开、或者窗口被切走,都要替它抬起来 ----
        // 右键是返回;窗口切走时收不到抬起
        const backUps = count((e) => e.t === 'back' && e.a === 1)
        await pointer('pointerdown', 2, 10, 10)
        await act(async () => {
          window.dispatchEvent(new window.Event('blur'))
        })
        if (count((e) => e.t === 'back' && e.a === 1) !== backUps + 1) throw new Error('窗口切走时右键(返回)没抬起来')
        // 中键是主页:一直按着手机会当成长按,把语音助手叫出来
        await pointer('pointerdown', 1, 10, 10)
        await act(async () => {
          canvas.dispatchEvent(new window.MouseEvent('lostpointercapture', { bubbles: true }))
        })
        if (!sent().some((e) => e.t === 'key' && e.k === 3 && e.a === 1)) throw new Error('中键(主页)丢了指针后没抬起来')
        // 左键在别的键还按着时松开:浏览器不报抬起,只看得出 buttons 变了
        await pointer('pointerdown', 0, 135, 300)
        const touchUps = count((e) => e.t === 'touch' && e.a === 1)
        await act(async () => {
          canvas.dispatchEvent(
            new window.MouseEvent('pointermove', { bubbles: true, cancelable: true, buttons: 2, clientX: 140, clientY: 300 }),
          )
        })
        if (count((e) => e.t === 'touch' && e.a === 1) !== touchUps + 1) throw new Error('左键先松开时手指没抬起来')
        await pointer('pointerup', 2, 140, 300)

        // ---- 键盘:点过画面,键盘就归手机 ----
        const kbd = document.querySelector('[data-mirror-keyboard]') as HTMLTextAreaElement
        if (document.activeElement !== kbd) throw new Error('点了画面键盘没归手机')
        const keydown = async (key: string, init: Record<string, unknown> = {}) => {
          await act(async () => {
            kbd.dispatchEvent(new window.KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...init }))
            await sleep(10)
          })
        }
        const typeIn = async (value: string) => {
          await act(async () => {
            kbd.value = value
            kbd.dispatchEvent(new window.Event('input', { bubbles: true }))
          })
        }
        await keydown('Enter')
        const enter = sent().filter((e) => e.t === 'key' && e.k === 66)
        if (enter.length !== 2 || enter[0].a !== 0 || enter[1].a !== 1) throw new Error('回车没发成按键: ' + JSON.stringify(enter))
        await keydown('a', { ctrlKey: true })
        if (!sent().some((e) => e.t === 'key' && e.k === 29 && e.m === 0x3000)) throw new Error('Ctrl+A 没变成全选')
        // 英文按字直接打过去;发出去的字要从输入框里清掉,不然下一个字会把前面的再带一遍
        await typeIn('a')
        await typeIn('b')
        const texts = sent().filter((e) => e.t === 'text').map((e) => e.s)
        if (texts.join() !== 'a,b' || kbd.value !== '') throw new Error('英文没按字发出去: ' + JSON.stringify(texts))
        // 中文:输入法拼字时不发,上屏了才发,而且经剪贴板粘贴
        await act(async () => {
          kbd.dispatchEvent(new window.CompositionEvent('compositionstart', { bubbles: true }))
        })
        await typeIn('ni')
        if (sent().some((e) => e.s === 'ni')) throw new Error('拼音还没上屏就发出去了')
        await act(async () => {
          kbd.value = '你好'
          kbd.dispatchEvent(new window.CompositionEvent('compositionend', { bubbles: true, data: '你好' }))
        })
        if (!sent().some((e) => e.t === 'paste' && e.s === '你好')) throw new Error('中文没经剪贴板粘贴过去')
        // 第一次粘贴前手机剪贴板里原来的东西:要提醒一句,而且留着能看
        await act(async () => {
          ws.emit(JSON.stringify({ type: 'clipboard', code: 'replaced', text: '原来复制着的' }))
        })
        if (!txt().includes('手机剪贴板里原来的内容已被替换')) throw new Error('没提醒剪贴板被替换了')
        // Ctrl+V:电脑剪贴板粘到手机
        __last.pcClipboard = '电脑上复制的'
        await keydown('v', { ctrlKey: true })
        await act(async () => {
          await sleep(20)
        })
        if (!sent().some((e) => e.t === 'paste' && e.s === '电脑上复制的')) throw new Error('Ctrl+V 没把电脑剪贴板粘过去')
        // Ctrl+C:手机复制选中的字,回来的内容放进电脑剪贴板
        await keydown('c', { ctrlKey: true })
        if (!sent().some((e) => e.t === 'getclip' && e.a === 1)) throw new Error('Ctrl+C 没让手机复制')
        await act(async () => {
          ws.emit(JSON.stringify({ type: 'clipboard', text: '手机上选中的' }))
        })
        if (__last.pcClipboardSet !== '手机上选中的') throw new Error('手机上复制的字没进电脑剪贴板')
        // 没认领的 Ctrl 组合键不发给手机,留给工具箱自己的快捷键
        const beforeCtrlK = ws.sent.length
        await keydown('k', { ctrlKey: true })
        if (ws.sent.length !== beforeCtrlK) throw new Error('Ctrl+K 不该发给手机')
        // 没全屏时 Esc 发给手机(手机上当返回用)
        await keydown('Escape')
        if (!sent().some((e) => e.t === 'key' && e.k === 111)) throw new Error('Esc 没发给手机')

        // ---- 手机剪贴板:读一下,原来那份也在 ----
        await clickTitled('手机剪贴板')
        if (!sent().some((e) => e.t === 'getclip' && e.a === 0)) throw new Error('读剪贴板没发出去')
        await act(async () => {
          ws.emit(JSON.stringify({ type: 'clipboard', text: '剪贴板里的字' }))
        })
        const card = () => document.querySelector('[data-mirror-clipboard]')?.textContent || ''
        if (!card().includes('剪贴板里的字') || !card().includes('原来复制着的')) throw new Error('剪贴板没显示全: ' + card())
        await clickTitled('收起')
        if (card()) throw new Error('剪贴板收不起来')

        // ---- 通知栏、音量 ----
        await clickTitled('下拉通知栏')
        if (!sent().some((e) => e.t === 'panel' && e.a === 0)) throw new Error('通知栏没拉下来')
        await clickTitled('音量 +')
        await clickTitled('音量 -')
        if (!sent().some((e) => e.t === 'key' && e.k === 24) || !sent().some((e) => e.t === 'key' && e.k === 25)) {
          throw new Error('音量键没发出去')
        }

        // ---- Ctrl+滚轮 = 双指缩放 ----
        const pinchMark = ws.sent.length
        await act(async () => {
          canvas.dispatchEvent(
            new window.WheelEvent('wheel', { bubbles: true, cancelable: true, deltaY: -100, ctrlKey: true, clientX: 135, clientY: 300 }),
          )
        })
        const fingers = () =>
          ws.sent
            .slice(pinchMark)
            .map((s) => JSON.parse(s) as Record<string, number | string>)
            .filter((e) => e.t === 'touch' && e.p)
        const downs = fingers().filter((e) => e.a === 0)
        if (downs.length !== 2 || downs[0].p !== 1 || downs[1].p !== 2) throw new Error('两根手指没按下去: ' + JSON.stringify(fingers()))
        const moves = fingers().filter((e) => e.a === 2)
        const spread = (a: Record<string, number | string>, b: Record<string, number | string>) =>
          Math.abs((a.x as number) - (b.x as number))
        if (moves.length !== 6 || !(spread(moves[4], moves[5]) > spread(downs[0], downs[1]))) {
          throw new Error('往前滚该把两指张开: ' + JSON.stringify(moves))
        }
        if (ws.sent.slice(pinchMark).some((s) => s.includes('"scroll"'))) throw new Error('Ctrl+滚轮不该再当成滑动')
        await act(async () => {
          await sleep(350)
        })
        if (fingers().filter((e) => e.a === 1).length !== 2) throw new Error('停下来之后两根手指没抬起来')

        // ---- 截图:第一次问存哪儿,存好了能打开文件夹、能复制 ----
        await clickTitled('截图：')
        if ((__last.mirrorShot as { dir: string } | undefined)?.dir !== 'D:/导出') {
          throw new Error('截图没存到选的文件夹: ' + JSON.stringify(__last.mirrorShot))
        }
        if (!txt().includes('截图已保存（1080×2400）')) throw new Error('截图存好了没说')
        await mustClick('打开所在文件夹')
        if (!String(__last.revealed).includes('截图')) throw new Error('打开所在文件夹没指到截图: ' + __last.revealed)
        await mustClick('复制图片')
        if (!String(__last.copiedImage).endsWith('.png')) throw new Error('复制图片没调到')
        delete __calls.PickDirectory
        await clickTitled('截图：')
        if (__calls.PickDirectory) throw new Error('第二次截图又问了一遍存哪儿')

        // ---- 录屏:录着的时候不能换画质;停下来说存了多长 ----
        await clickTitled('录屏：')
        if (!__last.mirrorRecStart) throw new Error('录屏没开起来')
        if (!titled('画质：')?.hasAttribute('disabled')) throw new Error('录屏中画质按钮该锁住')
        await clickTitled('停止录屏')
        if (!__last.mirrorRecStop) throw new Error('录屏没停')
        if (!txt().includes('录屏已保存（1 分 5 秒）')) throw new Error('录屏存好了没说: ' + txt())
        // 录着屏手机拔了:后端先把录好的文件交代清楚
        await clickTitled('录屏：')
        await act(async () => {
          ws.emit(JSON.stringify({ type: 'recorded', files: ['D:/导出/a.mp4', 'D:/导出/a_2.mp4'], ms: 3000 }))
        })
        if (!txt().includes('分成了 2 个文件')) throw new Error('转屏分段没说清楚')
        if (titled('停止录屏')) throw new Error('录屏已经停了,按钮还是「停止」')

        // ---- 拖文件进来:APK 安装,其它推到手机的 Download ----
        const drop = __last.fileDrop as ((x: number, y: number, paths: string[]) => void) | null
        if (!drop) throw new Error('投屏面板没接上原生拖放')
        const savedFromPoint = document.elementFromPoint
        document.elementFromPoint = () => canvas
        try {
          await act(async () => {
            drop(10, 10, ['D:/数据/微信.apk', 'D:/数据/旧版old.apk', 'D:/数据/照片.jpg'])
            await sleep(30)
          })
        } finally {
          document.elementFromPoint = savedFromPoint
        }
        const dropReq = __last.mirrorDrop as { paths: string[] } | undefined
        if (dropReq?.paths.length !== 3) throw new Error('拖进来的文件没交给后端: ' + JSON.stringify(dropReq))
        if (!txt().includes('装好了 1 个应用，推了 1 个文件到手机的 Download 文件夹') || !txt().includes('旧版old.apk：手机上装着更新的版本')) {
          throw new Error('拖放的结果没说清楚: ' + txt())
        }
        await act(async () => {
          ws.emit(JSON.stringify({ type: 'transfer', text: '正在安装 微信.apk' }))
        })
        if (!txt().includes('正在安装 微信.apk')) throw new Error('处理进度没显示')
        await act(async () => {
          ws.emit(JSON.stringify({ type: 'transfer', text: '' }))
        })

        // ---- 横屏:停靠着看太小,给一个一键全屏 ----
        await act(async () => {
          ws.emit(mirrorSession(1200, 540))
        })
        if (!btn('横屏了，全屏看更大')) throw new Error('横屏时没给全屏的入口')
        await act(async () => {
          ws.emit(mirrorSession(540, 1200))
        })
        if (btn('横屏了，全屏看更大')) throw new Error('转回竖屏了还挂着横屏提示')

        // 全屏:画面盖住整个窗口,窗口也进系统全屏;Esc 退出,两样都要还原
        const panel = () => document.querySelector('[data-mirror-panel]') as HTMLElement
        delete __calls.WindowFullscreen
        delete __calls.WindowUnfullscreen
        await mustClick('全屏：画面铺满整个屏幕')
        if (!panel().className.includes('fixed') || !__calls.WindowFullscreen) {
          throw new Error('全屏没铺满窗口,或者没进系统全屏')
        }
        await act(async () => {
          window.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Escape' }))
        })
        if (panel().className.includes('fixed') || !__calls.WindowUnfullscreen) throw new Error('Esc 没退出全屏')

        // 小米一类不让模拟点击:手机端的提醒要摆出来
        await act(async () => {
          ws.emit(JSON.stringify({ type: 'notice', code: 'inject-denied', text: '手机拒绝了模拟点击……「USB 调试（安全设置）」' }))
        })
        if (!txt().includes('USB 调试（安全设置）')) throw new Error('提醒没显示出来')

        // 提示条到点自己消失:提醒、成功、失败各有各的时长,鼠标停在上面时不走;
        // 「点不动」这种不处理就用不了的一直留着。把时长调短,不用真等几秒
        const savedDismiss = { ...DISMISS_MS }
        Object.assign(DISMISS_MS, { hint: 150, ok: 100, error: 400 })
        try {
          const hintBar = () => document.querySelector('[data-mirror-hint]') as HTMLElement | null
          const toastBar = () => document.querySelector('[data-mirror-toast]') as HTMLElement | null
          const wait = async (ms: number) => {
            await act(async () => {
              await sleep(ms)
            })
          }
          await clickTitled('怎么操作')
          if (!hintBar()?.textContent?.includes('Ctrl+滚轮')) throw new Error('操作说明没显示')
          // 成功的提示:Ctrl+C 复制到电脑
          await keydown('c', { ctrlKey: true })
          await act(async () => {
            ws.emit(JSON.stringify({ type: 'clipboard', text: '再复制一次' }))
          })
          if (!toastBar()?.textContent?.includes('已复制到电脑')) throw new Error('复制成功没提示')
          await wait(250)
          if (hintBar()) throw new Error('操作说明到点没消失')
          if (toastBar()) throw new Error('成功的提示到点没消失')
          if (!txt().includes('USB 调试（安全设置）')) throw new Error('「点不动」的提醒不该自己消失')

          // 失败的留得久一点
          const dropAgain = __last.fileDrop as (x: number, y: number, paths: string[]) => void
          const keepFromPoint = document.elementFromPoint
          document.elementFromPoint = () => canvas
          try {
            await act(async () => {
              dropAgain(10, 10, ['D:/数据/旧版old.apk'])
              await sleep(30)
            })
          } finally {
            document.elementFromPoint = keepFromPoint
          }
          await wait(200)
          if (!toastBar()?.textContent?.includes('旧版old.apk')) throw new Error('失败的提示消失得太早')
          await wait(350)
          if (toastBar()) throw new Error('失败的提示到点没消失')

          // 鼠标停在上面:不计时;挪开再重新数
          await clickTitled('怎么操作')
          await act(async () => {
            hintBar()?.dispatchEvent(new window.MouseEvent('mouseover', { bubbles: true }))
          })
          await wait(250)
          if (!hintBar()) throw new Error('鼠标停在提示上,它不该消失')
          await act(async () => {
            hintBar()?.dispatchEvent(new window.MouseEvent('mouseout', { bubbles: true }))
          })
          await wait(250)
          if (hintBar()) throw new Error('鼠标挪开后提示没接着消失')
        } finally {
          Object.assign(DISMISS_MS, savedDismiss)
        }

        // 手机拔了:说清楚为什么,给一个重新连接
        await act(async () => {
          ws.emit(JSON.stringify({ type: 'ended', text: '投屏断开了:手机拔掉了' }))
        })
        if (!txt().includes('手机拔掉了')) throw new Error('断开的原因没显示')
        const before = __last.mirrorStarts as number
        await mustClick('重新连接')
        await act(async () => {
          await sleep(30)
        })
        if ((__last.mirrorStarts as number) <= before) throw new Error('点了重新连接没有重开一路')

        // 保持亮屏是开机参数:关掉要重开一路,带上新的设置
        const beforeAwake = __last.mirrorStarts as number
        await clickTitled('保持亮屏：开着')
        await act(async () => {
          await sleep(30)
        })
        const awakeReq = __last.mirrorStart as { options: { keepAwake: boolean } }
        if ((__last.mirrorStarts as number) <= beforeAwake || awakeReq.options.keepAwake !== false) {
          throw new Error('关掉保持亮屏没带着新设置重开: ' + JSON.stringify(awakeReq.options))
        }
        await clickTitled('保持亮屏：关着')
        await act(async () => {
          await sleep(30)
        })

        // 窗口藏起来(最小化)也要停;重新显示时自动接上
        const hiddenRun = 'm' + String(__last.mirrorStarts)
        Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true })
        try {
          await act(async () => {
            document.dispatchEvent(new window.Event('visibilitychange'))
            await sleep(30)
          })
          if (!(__last.mirrorStops as string[] | undefined)?.includes(hiddenRun)) throw new Error('窗口藏起来之后投屏还在跑')
        } finally {
          delete (document as unknown as Record<string, unknown>).visibilityState
        }
        const beforeShown = __last.mirrorStarts as number
        await act(async () => {
          document.dispatchEvent(new window.Event('visibilitychange'))
          await sleep(50)
        })
        if ((__last.mirrorStarts as number) <= beforeShown) throw new Error('窗口重新显示后没自动接上')

        // 切到别的工具:这一页只是藏起来,投屏得停,不然手机在后台一直编码;切回来自动接上
        const running = 'm' + String(__last.mirrorStarts)
        await act(async () => {
          go?.('/tools/sqlite-search')
        })
        if (!(__last.mirrorStops as string[] | undefined)?.includes(running)) {
          throw new Error('切走之后投屏还在后台跑')
        }
        const beforeBack = __last.mirrorStarts as number
        await act(async () => {
          go?.('/tools/device-browser')
          await sleep(30)
        })
        if ((__last.mirrorStarts as number) <= beforeBack) throw new Error('切回来没有自动接上')

        // 关掉面板:正在投的那一路要在后端停掉,手机端程序才会退出
        await mustClick('关闭投屏')
        const current = 'm' + String(__last.mirrorStarts)
        if (!(__last.mirrorStops as string[] | undefined)?.includes(current)) {
          throw new Error('关掉面板没停掉正在投的那一路: ' + JSON.stringify(__last.mirrorStops))
        }
      } finally {
        fake.restore()
      }
    },
  )

  // 工具间跳转:真机浏览翻到的目径直接填进移动取证;取证的输出目录直接填进 SQLite 搜索。
  // 路由是 keep-alive 的,参数走 location.state,目标工具在 key 变化时接
  await mount(
    '跳转 · 真机浏览 → 移动取证',
    <MemoryRouter
      initialEntries={[
        {
          pathname: '/tools/mobile-forensic',
          state: { jump: { to: 'mobile-forensic', platform: 'ios', paths: ['/private/var/mobile/Library/SMS/'] } },
        },
      ]}
    >
      <MobileForensic />
    </MemoryRouter>,
    async () => {
      const area = document.querySelector('textarea') as HTMLTextAreaElement | null
      if (!area?.value.includes('/private/var/mobile/Library/SMS/')) {
        throw new Error('跳过来的路径没填进指定路径')
      }
      if (!(document.body.textContent || '').includes('SSH 地址')) {
        throw new Error('平台没跟着跳转切到 iOS')
      }
    },
  )
  await mount(
    '跳转 · 移动取证 → SQLite 搜索',
    <MemoryRouter
      initialEntries={[
        { pathname: '/tools/sqlite-search', state: { jump: { to: 'sqlite-search', root: 'D:/导出/设备' } } },
      ]}
    >
      <SQLiteSearch />
    </MemoryRouter>,
    async () => {
      const input = document.querySelector('input[placeholder*="案件编号"]') as HTMLInputElement | null
      if (input?.value !== 'D:/导出/设备') throw new Error('跳过来的目录没填进「搜哪儿」')
    },
  )

  // 19) 移动取证:Android 默认走内置引擎,没装 go-forensic 也照样能用。
  //
  // 这一页原来整个被"先去配置 go-forensic"挡在前面 —— 内置实现做出来之后
  // 那道门就是白挡一道;而挡住之后连切引擎的开关都摸不到,是条死路
  // SetupGuide 里的「去配置」是个 <Link>,没有 Router 上下文会当场炸
  await mount('移动取证 · 默认不露出 go-forensic', <MemoryRouter><MobileForensic /></MemoryRouter>, async () => {
    const txt = () => document.body.textContent || ''
    if (!txt().includes('任务参数')) throw new Error('表单没出来,还被 go-forensic 的配置页挡着')
    if (txt().includes('这次的选择需要 go-forensic')) {
      throw new Error('默认的内置引擎不该要求配置 go-forensic')
    }
    if (!txt().includes('内置引擎')) throw new Error('执行预览没说清楚这次走的是内置')

    // go-forensic 默认不启用:两个平台的提取都已内置,绝大多数人没装过它,
    // 不该让每个人先面对一道「内置 / go-forensic」的选择题
    if (btn('go-forensic')) {
      throw new Error('没启用 go-forensic 却摆出了引擎选择')
    }

    // 配置入口在页面自己身上,不再跳设置页
    await mustClick('取证配置')
    if (!txt().includes('默认 iOS SSH 地址')) {
      throw new Error('配置弹窗里没有默认 SSH 地址 —— 它不属于 go-forensic,不能跟着一起消失')
    }
    if (!txt().includes('先把上面检测通过才能启用')) {
      throw new Error('检测没过时「启用」应当是点不动的,否则这个开关就是在撒谎')
    }
    const box = document.querySelector(
      'input[type=checkbox]:disabled',
    ) as HTMLInputElement | null
    if (!box) throw new Error('检测没过,启用开关却是可点的')
    await mustClick('关闭（Esc）')
  })

  // 20) 移动取证:自定义常用路径。
  //
  // 内置那几组是 iOS 系统数据的固定路径;办案的人各有各的常用位置,存个标签比每次翻记录省事。
  // Android 原来根本没有这个面板,现在两个平台都有「我的常用」,各存各的
  await mount('移动取证 · 自定义常用路径', <MemoryRouter><MobileForensic /></MemoryRouter>, async () => {
    const txt = () => document.body.textContent || ''
    await mustClick('展开常用路径')
    if (!txt().includes('我的常用')) throw new Error('Android 下没有「我的常用」')
    if (txt().includes('邮件与账号')) throw new Error('Android 下不该出现 iOS 的系统路径组')

    // 手动添加一条
    await mustClick('手动添加一条常用路径')
    await mustType('标签', '聊天数据')
    await mustType('/data/data/', '/data/data/com.example.chat/databases/')
    await mustClick('保存')
    if (!btn('聊天数据')) throw new Error('存完没出现在「我的常用」里')
    const stored = useForensicStore.getState().customPresets.android
    if (stored.length !== 1 || stored[0].path !== '/data/data/com.example.chat/databases/') {
      throw new Error('没存进 store: ' + JSON.stringify(stored))
    }

    // 点一下加到指定路径
    await mustClick('聊天数据')
    const area = document.querySelector('textarea') as HTMLTextAreaElement | null
    if (!area?.value.includes('/data/data/com.example.chat/databases/')) {
      throw new Error('点了没加到指定路径里')
    }

    // 「存为常用」:把当前填的按行列出来;存过的沿用原标签,没存过的按最后一段猜;
    // 同路径覆盖,不同路径新增
    await typeArea(
      '/data/data/',
      '/sdcard/Android/data/com.example.chat/\n/data/data/com.example.chat/databases/',
    )
    await mustClick('把上面填的路径存为常用')
    if (!txt().includes('2 条会被保存')) throw new Error('存为常用没把两条都列出来')
    await mustClick('保存')
    const after = useForensicStore.getState().customPresets.android
    if (after.length !== 2) throw new Error('存为常用没存对(同路径应覆盖,不同路径应新增)')
    if (!after.some((c) => c.label === '聊天数据')) throw new Error('已存过的那条标签被猜的名字覆盖了')
    if (!btn('com.example.chat')) throw new Error('没存过的那条没按最后一段起名')

    // 删掉一条要先确认
    await mustClick('从我的常用里删掉「聊天数据」')
    if (!txt().includes('删掉这条常用路径')) throw new Error('删除没有确认')
    await mustClick('删掉')
    if (btn('聊天数据')) throw new Error('确认删掉后还在')

    // iOS 那边是另一份,而且内置组还在
    await mustClick('iOS')
    if (!txt().includes('邮件与账号')) throw new Error('iOS 的内置路径组不见了')
    if (btn('com.example.chat')) throw new Error('Android 的常用跑到 iOS 这边来了')
  })

  // 本地 API · MCP 端点:两家的配置都给,并且能一键写进去。
  //
  // 写别家的配置文件是敏感动作:先试算、把将写入的那段和目标文件摆出来让人确认,
  // 确认之前不能落盘;确认之后要说清写到了哪、备份在哪。
  // 这一页的 RPC 走 window.go 而不是 wailsjs 导入,得把桩挂上去
  ;(window as any).go = { main: { App: wailsStub } }
  await mount('本地 API · MCP 端点一键写入', <LocalAPISection />, async () => {
    const txt = () => document.body.textContent || ''
    if (!txt().includes('[mcp_servers.tool-forge]')) throw new Error('没有给 Codex 的 TOML 配置')
    if (!txt().includes('"mcpServers"')) throw new Error('没有给 Claude Code 的 JSON 配置')

    delete __last.mcpInstall
    await mustClick('写入 Codex')
    const planned = __last.mcpInstall as { target: string; apply: boolean } | undefined
    if (!planned || planned.apply !== false) throw new Error('确认之前就落盘了: ' + JSON.stringify(planned))
    if (!txt().includes('~/.codex/config.toml')) throw new Error('确认框没说写到哪个文件')
    if (!txt().includes('其它内容一个字节不动')) throw new Error('确认框没说清只动这一段')

    await mustClick('写入')
    const done = __last.mcpInstall as { target: string; apply: boolean } | undefined
    if (!done || done.apply !== true || done.target !== 'codex') {
      throw new Error('确认后没有真的写入: ' + JSON.stringify(done))
    }
    if (!txt().includes('已写入')) throw new Error('写完没有告诉用户结果')
    if (!txt().includes('.bak')) throw new Error('没有告诉用户备份在哪')
  })
  delete (window as any).go

  // 设置页本身:每一栏都要真的挂得上去。
  //
  // 这条是补出来的 —— 前面那些用例都是把 section 组件单独挂载来测的,
  // 于是「组件能渲染」和「它真的被注册进了设置页」成了两件事,
  // 后者一直没人验。注册漏一步的表现是:功能做完了、测试全绿、用户找不到入口
  await mount('设置页 · 每一栏都挂得上', <MemoryRouter><Profile /></MemoryRouter>, async () => {
    const labels = [
      '基础信息', '剪贴板', '快捷键', 'AI 配置', 'AI 用量',
      'MCP 服务器', '本地 API', '数据', '关于',
    ]
    const txt = () => document.body.textContent || ''
    for (const label of labels) {
      if (!btn(label)) throw new Error(`设置页左栏少了「${label}」`)
    }

    // 逐栏点开,任何一栏渲染炸了都会被上面那个 console.error 钩子抓成失败
    for (const label of labels) {
      await mustClick(label)
    }

    // 随便停在一栏,确认内容是真的而不是「即将推出」的占位
    await mustClick('MCP 服务器')
    if (!txt().includes('添加服务器')) {
      throw new Error('点了「MCP 服务器」但内容没出来 —— 多半是渲染分支没接上')
    }
  })

  // 重置工具偏好:回到每个工具自己的默认,不是一律显示。
  // 默认关着、被打开的也算改过 —— 只数被关掉的,只打开过默认关着的工具时重置按钮是灰的,点不了
  await mount('设置页 · 重置工具偏好回到默认', <MemoryRouter><Profile /></MemoryRouter>, async () => {
    // LLM 代理默认关、被打开了;真机浏览默认开、被关掉了;AI 问答本来就开着,不算改过
    useToolsStore.setState({ visibility: { 'llm-proxy': true, 'device-browser': false, 'ai-chat': true }, order: [] })
    try {
      await mustClick('数据')
      const row = () => {
        const label = (Array.from(document.querySelectorAll('div')) as HTMLElement[]).find(
          (d) => d.textContent === '重置工具偏好',
        )
        return label?.parentElement?.parentElement ?? null
      }
      if (!row()?.textContent?.includes('开关改过 2 个')) throw new Error('改过几个没数对: ' + row()?.textContent)
      const reset = row()?.querySelector('button') as HTMLButtonElement | null
      if (!reset || reset.disabled) throw new Error('改过开关,重置按钮却点不了')
      await act(async () => {
        reset.click()
      })
      await confirmIn('确定')
      const { visibility } = useToolsStore.getState()
      if (Object.keys(visibility).length) throw new Error('重置后还留着开关记录: ' + JSON.stringify(visibility))
      // 回到默认:真机浏览第一次用就在侧边栏里,LLM 代理这些默认收起来
      for (const [id, want] of [['device-browser', true], ['llm-proxy', false], ['ai-chat', true]] as const) {
        if (isVisible(id, visibility, getToolById(id)?.defaultVisible) !== want) throw new Error(`重置后「${id}」不是默认的样子`)
      }
      if (!row()?.textContent?.includes('当前为默认')) throw new Error('重置完没显示「当前为默认」: ' + row()?.textContent)
    } finally {
      useToolsStore.setState({ visibility: {}, order: [] })
    }
  })

  // 本机 AI 配置:一处看全三家的 MCP / skills / 插件。
  // 这一页的全部价值在「每条都标明出自哪个文件」——少了它就只是把四处混成一锅,
  // 看到一条不对劲的却不知道该去改哪儿,比分开看还难查
  await mount('本机 AI 配置', <MemoryRouter><AIConfigTool /></MemoryRouter>, async () => {
    const txt = () => document.body.textContent || ''

    // 来源墙:每家一张卡,没装的也要在 —— 否则用户会以为漏扫了
    for (const name of ['Claude Code', 'Codex', 'Gemini CLI', 'Cline', 'Continue', 'Trae', 'Cursor', '工具箱', '共享池']) {
      if (!txt().includes(name)) throw new Error(`来源墙上少了「${name}」`)
    }
    if (!txt().includes('未安装')) throw new Error('没装的那家要标「未安装」,不能凭空消失')
    if (!txt().includes('装了,没配东西')) throw new Error('装了但没配的那家要说清楚,不能和没装混在一起')
    // 名单外按形状发现的也要上墙,名字就是目录名 —— 否则装了的工具凭空消失
    if (!btn('~/.factory —— 按目录形状自动发现的,名单里还没有它的说明')) {
      throw new Error('按形状发现的名单外来源没有上墙')
    }

    // 三种类型都列出来,而且每条都带来源和出处文件
    if (!txt().includes('acemcp')) throw new Error('没有列出 Claude 的 MCP')
    if (!txt().includes('node_repl')) throw new Error('没有列出 Codex 的 MCP')
    if (!txt().includes('chrome-mcp-stdio')) throw new Error('没有列出 Gemini 的 MCP')
    if (!txt().includes('config.toml')) throw new Error('没有显示出处文件')
    if (!txt().includes('多处重复')) throw new Error('同名配在多处没有被标出来')
    // 重复项要能并排比:标签只说了"有好几份",接着要问的是它们一样吗
    await mustClick('多处重复 · 对比')
    if (!txt().includes('处的配置')) throw new Error('点了「对比」没有弹出并排比较')
    if (!txt().includes('出处')) throw new Error('比较表没有列出处')
    await mustClick('关闭（Esc）')
    if (txt().includes('处的配置')) throw new Error('比较弹窗关不掉')
    if (!txt().includes('没读成')) throw new Error('解析失败的文件没有报出来')
    if (!txt().includes('插件 codex@openai-codex')) throw new Error('插件自带的 skill 没有标明出自哪个插件')
    if (!txt().includes('缺 SKILL.md')) throw new Error('没有 SKILL.md 的 skill 没被点出来')
    if (!txt().includes('启用了但没装')) throw new Error('启用与安装状态对不上时没有点破')
    // 软链要标:Continue 的 skills 全指向共享池,不标会被当成独立的一份
    if (!txt().includes('软链')) throw new Error('软链 skill 没有标出真正指向哪里')

    // 点一家只看它的
    await mustClick('Gemini CLI')
    if (txt().includes('acemcp')) throw new Error('选了 Gemini 还在显示 Claude 的 MCP')
    if (!txt().includes('chrome-mcp-stdio')) throw new Error('选了 Gemini 却没显示它的 MCP')
    // 再点一次回到全部
    await mustClick('Gemini CLI')
    if (!txt().includes('acemcp')) throw new Error('再点一次没有回到全部')

    // 类型筛选
    await mustClick('筛选：插件')
    if (txt().includes('node_repl')) throw new Error('切到「插件」还在显示 MCP')
    if (!txt().includes('codex@openai-codex')) throw new Error('切到「插件」没显示插件')

    // 启停:只有工具箱自己的 MCP 给开关,别家的不给 —— 它们的「停用」语义各不相同,
    // 替它猜一个只会把配置改坏。fixture 里 exa 是工具箱的且已停用。
    // 回到「全部」视图再数:上面刚切到「插件」,MCP 区是不渲染的
    await mustClick('筛选：全部')
    // 来源过滤要真的清掉 —— 上面「再点一次回到全部」那步靠的是文字匹配,
    // 匹配歪了就会一直停在某一家,后面数出来永远是 0 个
    await click('清除')
    if (!txt().includes('node_repl')) throw new Error('数开关前没有回到全部来源')
    const toggles = (Array.from(document.querySelectorAll('button')) as HTMLElement[]).filter((b) =>
      ['点击启用', '点击停用'].includes(b.getAttribute('title') || ''),
    )
    if (toggles.length !== 1) {
      throw new Error(`应该只有工具箱那 1 条 MCP 有启停开关,实际 ${toggles.length} 个`)
    }
    delete __last.mcpToggle
    await act(async () => {
      toggles[0].click()
    })
    await act(async () => {
      await sleep(80)
    })
    const t = __last.mcpToggle as { id: string; enabled: boolean } | undefined
    if (!t || t.id !== 's2' || t.enabled !== true) {
      throw new Error('点了停用的那条,应该调 ToggleMCPServer(s2, true),实际 ' + JSON.stringify(t))
    }

    // md 要渲染着看,不是一屏 # 和 ---。切回 Skills,点开一个 SKILL.md
    await mustClick('筛选：Skills')
    const link = (Array.from(document.querySelectorAll('button')) as HTMLElement[]).find((b) =>
      (b.getAttribute('title') || '').includes('git-commit-helper') &&
      (b.getAttribute('title') || '').endsWith('SKILL.md'),
    )
    if (!link) throw new Error('找不到 git-commit-helper 的 SKILL.md 出处按钮')
    await act(async () => {
      link.click()
    })
    await act(async () => {
      await sleep(120)
    })
    const dialogTxt = () => document.body.textContent || ''
    // 渲染后标题是真标题,不是裸的 "# 提交助手"
    if (dialogTxt().includes('# 提交助手')) throw new Error('md 没有渲染,还是源码')
    if (!document.querySelector('h1')) throw new Error('md 预览里没有渲染出标题')
    // frontmatter 拆成了元数据卡:key 单独一格,列表变成标签
    if (!dialogTxt().includes('allowed-tools')) throw new Error('frontmatter 没拆出来')
    if (dialogTxt().includes('---')) throw new Error('frontmatter 的分隔线漏进了正文')
    // 能切到编辑,切过去就是源码
    await mustClick('看源码并编辑')
    if (!dialogTxt().includes('---')) throw new Error('切到编辑后应该看到原文,含 frontmatter')
    await mustClick('关闭（Esc）')
  })

  // 20) 包名搜索:七麦要登录态,配置入口和"配没配"都得在这一页上看得见
  await mount('包名搜索 · 配置入口', <MemoryRouter><AppSearch /></MemoryRouter>, async () => {
    const txt = () => document.body.textContent || ''
    // 桩里 HasQimaiCredential 返回 false —— 源列表要当场说清楚,并且就地给入口。
    // 原来这里写死一句"Profile 里配置",既不说配没配,也要自己去找那一页
    if (!txt().includes('未配置登录态')) {
      throw new Error('七麦源没有显示未配置状态')
    }
    await mustClick('未配置登录态 · 点此配置')
    if (!txt().includes('PHPSESSID')) throw new Error('点了提示没打开配置弹窗')
    await mustClick('关闭（Esc）')

    await mustClick('包名搜索配置')
    if (!txt().includes('系统凭据库')) throw new Error('配置弹窗没说清楚密钥存在哪')
  })

  // 包名搜索 · 图标:列表里是缩略图,保存和复制拿的是原图。
  // 存完要说清存了多大、存在哪;右键图标也得有反应
  await mount('包名搜索 · 保存图标', <MemoryRouter><AppSearch /></MemoryRouter>, async () => {
    const txt = () => document.body.textContent || ''
    await mustType('微信 / wechat', '微信')
    await mustClick('搜索')
    const pic = document.querySelector('img[src*="mzstatic"]')
    if (!pic) throw new Error('结果里没画出图标')

    // 这两个按钮悬停才显形,但一直在 DOM 里(jsdom 不管透明度)
    await mustClick('保存原图')
    const req = __last.iconSave as { icon: string; name: string; id: string } | undefined
    if (!req || req.name !== '微信' || req.id !== 'com.tencent.xin' || !req.icon.includes('mzstatic')) {
      throw new Error('保存发出去的不对: ' + JSON.stringify(req))
    }
    if (!txt().includes('已保存 · 1024×1024 PNG')) throw new Error('存完没说存下来的有多大')
    await mustClick('在文件夹中显示')
    if (!String(__last.revealed).endsWith('微信_com.tencent.xin.png')) {
      throw new Error('「在文件夹中显示」没指向刚存的文件: ' + String(__last.revealed))
    }

    await mustClick('复制图标')
    if (__last.iconCopy !== req.icon) throw new Error('复制的不是这一张')
    if (!txt().includes('已复制 · 1024×1024 PNG')) throw new Error('复制完没有回话')

    // 在保存框里点了取消:上一句提示收掉,也不能当成失败报出来
    __last.iconCancelNext = true
    await mustClick('保存原图')
    if (txt().includes('已复制') || txt().includes('没保存成')) throw new Error('取消保存之后界面不对')

    // 右键:正式版里 WebView 自带的右键菜单是关的,原来右键图标什么反应都没有
    const rightClick = async () => {
      await act(async () => {
        pic.dispatchEvent(
          new window.MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 40, clientY: 40 }),
        )
      })
      await act(async () => {
        await sleep(20)
      })
    }
    await rightClick()
    if (!document.querySelector('[role="menu"]')) throw new Error('右键图标没弹菜单')
    await act(async () => {
      window.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Escape' }))
    })
    if (document.querySelector('[role="menu"]')) throw new Error('按 Esc 菜单没收起来')
    await rightClick()
    await mustClick('保存原图…')
    if (document.querySelector('[role="menu"]')) throw new Error('点了菜单项,菜单没收起来')
    if (!txt().includes('已保存 · 1024×1024 PNG')) throw new Error('从右键菜单保存没生效')

    // 下不来要就地说出来:第二条(应用宝)的地址里带 broken
    const saves = Array.from(document.querySelectorAll('button[title="保存原图"]')) as HTMLElement[]
    if (saves.length < 2) throw new Error('每条结果都该有保存按钮')
    await act(async () => {
      saves[1].click()
    })
    await act(async () => {
      await sleep(50)
    })
    if (!txt().includes('没保存成：下载图标失败: http 400')) throw new Error('下载失败没有说出来')
  })

  // 20) SQLite 搜索:命中要给出整行,读不了的库要摆出来,点表名能翻表。
  //
  // 这一页最容易崩的地方是 NULL 和 BLOB —— 真实证据库里到处都是,
  // 而它们在渲染里长得和普通字符串不一样。fixture 里各放了一条
  await mount('SQLite 搜索', <MemoryRouter><SQLiteSearch /></MemoryRouter>, async () => {
    const txt = () => document.body.textContent || ''
    await mustType('案件编号', 'D:/取证/示例')
    await mustType('13800138000', '收款')
    await mustClick('搜索')

    if (!txt().includes('扫了')) throw new Error('没有汇总行')
    if (!txt().includes('明天把收款码发我')) {
      throw new Error('命中的那一行内容没画出来 —— 只报表名的话这个工具就没用了')
    }
    // 读不了的库必须摆出来:不说的话"没搜到"和"根本没打开"长得一模一样
    if (!txt().includes('没读成')) throw new Error('跳过的库没有提示')

    // 展开整行:NULL 那一格要显示成 NULL,不能是空白
    await mustClick('展开整行（4 列）')
    if (!txt().includes('NULL')) throw new Error('NULL 没标出来')

    // 点表名进表浏览器
    await mustClick('messages')
    if (!txt().includes('broken_table')) throw new Error('表列表没画出来')
    if (!txt().includes('读不了')) throw new Error('读不出列的表没标出来')
    if (!txt().includes('共 128')) throw new Error('总行数没显示')
    if (!txt().includes('BLOB') && !txt().includes('0x0001')) {
      throw new Error('BLOB 没画出来')
    }
  })

  // 剪贴板:图片条目能认字。截图里的账号、电话以前只能手打一遍;
  // 认出来的放在能改的框里,不直接塞回剪贴板 —— 识别不会全对
  await mount('剪贴板 · 图片识别文字', <MemoryRouter><ClipboardTool /></MemoryRouter>, async () => {
    const txt = () => document.body.textContent || ''
    if (!txt().includes('第一条文字')) throw new Error('剪贴板列表没出来')
    const ocrBtns = Array.from(document.querySelectorAll('button')).filter(
      (b) => (b.getAttribute('title') || b.textContent || '').trim() === '识别文字',
    )
    if (ocrBtns.length !== 1) throw new Error(`只有图片条目该有「识别文字」,现在有 ${ocrBtns.length} 个`)
    await mustClick('识别文字')
    if (!txt().includes('zh-Hans-CN')) throw new Error('没显示用的语言包')
    const area = document.querySelector('textarea') as HTMLTextAreaElement | null
    if (!area?.value.includes('收款方 张三')) throw new Error('认出来的文字没放进可改的框里')
    await mustClick('关闭')
  })

  // MCP 工作台:MCP 版的 Postman。
  //
  // 这一页的全部价值在「照 schema 生成表单 + 原始报文可见」——
  // 在 AI 那头出问题时只看得到"工具调用失败",发出去的参数和服务器的回复都被吞了
  await mount('MCP 工作台', <MemoryRouter><MCPWorkbench /></MemoryRouter>, async () => {
    const txt = () => document.body.textContent || ''
    await mustClick('demo')
    if (!txt().includes('已连接')) throw new Error('连上了却没显示连接状态')
    if (!txt().includes('2025-06-18')) throw new Error('没显示协议版本 —— 版本对不上是最难猜的症状')
    if (!txt().includes('resources 列表拉取失败')) throw new Error('可选能力的告警没显示')
    if (!txt().includes('search_files')) throw new Error('没列出工具')

    // 挑一个工具,表单要照 schema 生成,每种类型给对应的控件
    await mustClick('search_files')
    const form = document.body.textContent || ''
    for (const label of ['query', 'limit', 'recursive', 'mode', 'paths', 'filter']) {
      if (!form.includes(label)) throw new Error(`表单少了字段「${label}」`)
    }
    if (!form.includes('枚举')) throw new Error('enum 字段没标成枚举')
    if (!document.querySelector('select')) throw new Error('enum 该给下拉框')
    if (!document.querySelector('input[type=checkbox]')) throw new Error('boolean 该给复选框')
    if (!form.includes('必填')) throw new Error('必填字段没标出来')

    // 必填留空时不该发出请求
    delete __last.mcpCall
    await mustClick('调用')
    if (__last.mcpCall) throw new Error('必填留空却把请求发出去了')

    await fillBy('input[name="query"]', 'wechat')
    await mustClick('调用')
    if (!txt().includes('命中 3 个文件')) throw new Error('没显示调用结果')
    if (!txt().includes('成功')) throw new Error('没显示调用成败')

    // 三个页签分别是"模型看到的""服务器真回的""我们真发的",调试时缺一不可
    await mustClick('查看：请求')
    if (!txt().includes('"query"')) throw new Error('看不到发出去的参数')
    await mustClick('查看：原始响应')
    if (!txt().includes('"content"')) throw new Error('看不到原始响应')

    // 描述里带注入特征的工具要标出来 —— 描述是直接进模型上下文的
    await mustClick('read_note')
    if (!txt().includes('要求忽略先前指令')) throw new Error('投毒的工具描述没有被标出来')

    // 历史
    await mustClick('调用历史')
    if (!txt().includes('search_files')) throw new Error('调用历史里没有记录')

    // 分栏:尺寸不是写死的,分隔条能拖、拖完记住,而且每一栏自己管滚动。
    //
    // 中间那栏当初没有滚动条,根子是它拿不到确定高度。jsdom 不做真实排版,
    // 量不出像素,所以这里守两样能守的:结构上每一栏都是有界的滚动容器,
    // 以及拖动确实改到了尺寸
    const seps = Array.from(document.querySelectorAll('[role="separator"]')) as HTMLElement[]
    if (seps.length !== 2) throw new Error(`三栏之间该有 2 根分隔条,现在有 ${seps.length} 根`)

    const panes = Array.from(document.querySelectorAll('[data-pane]')) as HTMLElement[]
    if (panes.length !== 3) throw new Error(`该是三栏,现在有 ${panes.length} 栏`)
    for (const pane of panes) {
      const bounded = pane.className.includes('min-h-0')
      const scrolls =
        pane.className.includes('overflow-auto') || pane.className.includes('overflow-hidden')
      if (!bounded || !scrolls) {
        throw new Error(`「${pane.dataset.pane}」栏没有自己管滚动: ${pane.className}`)
      }
    }
    // 中间那栏内部还要有一块真正滚动的内容区 —— 它才是当初看不到后面内容的地方
    const mid = panes.find((p) => p.dataset.pane === 'params')
    if (!mid?.querySelector('.overflow-auto')) {
      throw new Error('参数栏内部没有滚动区,内容长了就看不到后面')
    }

    // 拖一下左边的分隔条,宽度要真的跟着变并记下来
    useWorkbenchLayout.getState().reset()
    const before = useWorkbenchLayout.getState().leftWidth
    await drag(seps[0], 300, 360)
    const after = useWorkbenchLayout.getState().leftWidth
    if (after !== before + 60) throw new Error(`拖动没改到宽度: ${before} → ${after}`)

    // 再怎么拖也不能把一栏压到没法读
    await drag(seps[0], 300, -2000)
    const narrow = document.querySelector('[data-pane="list"]') as HTMLElement
    if (parseFloat(narrow.style.width) < WB_MIN.left) {
      throw new Error(`拖过头了,左栏被压到 ${narrow.style.width},下限是 ${WB_MIN.left}px`)
    }

    // 双击恢复默认
    useWorkbenchLayout.getState().setLeftWidth(999)
    await act(async () => {
      seps[0].dispatchEvent(new window.MouseEvent('dblclick', { bubbles: true }))
    })
    if (useWorkbenchLayout.getState().leftWidth !== WB_DEFAULT.leftWidth) {
      throw new Error('双击分隔条没有恢复默认宽度')
    }
  })

  // OpenAPI 导入:文档里已经写清楚的东西(路径、方法、参数、类型)不该让人再抄一遍。
  // 三步走:给文档 → 勾接口 → 填地址和认证
  delete __last.apiPacks
  delete __last.savedPack
  await mount('MCP 工作台 · 导入 OpenAPI', <MemoryRouter><MCPWorkbench /></MemoryRouter>, async () => {
    const txt = () => document.body.textContent || ''
    if (!txt().includes('接口包')) throw new Error('没有接口包这一栏')
    await mustClick('从 OpenAPI 文档导入接口')

    await typeArea('openapi: 3.0.0', '{"openapi":"3.0.0"}')
    await mustClick('解析粘贴的内容')
    if (!txt().includes('/orders/{orderId}')) throw new Error('没列出解析到的接口')
    if (!txt().includes('已废弃')) throw new Error('文档标了废弃的接口没标出来')
    // 跳过了什么必须说 —— 跳过的接口在列表里是看不见的
    if (!txt().includes('multipart')) throw new Error('解析时跳过的内容没有说明')

    await mustClick('下一步')
    if (!txt().includes('api-')) throw new Error('没有预览会生成什么工具名')
    // 认证方式选了才问密钥,而且要说清楚密钥存哪儿
    const sel = document.querySelectorAll('select')
    const auth = sel[sel.length - 1] as HTMLSelectElement
    const setter = Object.getOwnPropertyDescriptor(window.HTMLSelectElement.prototype, 'value')?.set
    setter?.call(auth, 'bearer')
    await act(async () => {
      auth.dispatchEvent(new window.Event('change', { bubbles: true }))
    })
    if (!txt().includes('系统凭据库')) throw new Error('没说清楚密钥存在哪儿')

    await mustClick('保存并生成工具')
    const saved = __last.savedPack as { name: string; ops: unknown[] } | undefined
    if (!saved || saved.ops.length !== 2) throw new Error('保存的接口包不对: ' + JSON.stringify(saved))
    if (!txt().includes('2 个')) throw new Error('保存后列表里没显示这个包')
  })

  // 磁盘清理 · 缓存。删除类的用例只认一件事:真正发到后端的是什么 ——
  // 界面上勾的、确认框里说的、请求里带的,三者必须对得上
  delete __last.cleanIds
  delete __last.cacheScans
  await mount('磁盘清理 · 缓存', <DiskClean />, async () => {
    const txt = () => document.body.textContent || ''
    if (!__last.cacheScans) throw new Error('打开缓存页没有自动统计')
    if (!txt().includes('用户临时文件')) throw new Error('没列出缓存规则')
    if (txt().includes('Brave 缓存')) throw new Error('本机没有的规则不该列出来')
    if (!txt().includes('另有 1 项')) throw new Error('隐藏了几项没说')
    if (!txt().includes('chrome.exe 正在运行')) throw new Error('程序开着没提示')
    if (!txt().includes('以管理员身份运行')) throw new Error('不是管理员时没说怎么办')

    const boxOf = (name: string) =>
      (Array.from(document.querySelectorAll('label')) as HTMLElement[])
        .find((l) => (l.textContent || '').includes(name))
        ?.querySelector('input[type=checkbox]') as HTMLInputElement | undefined
    if (!boxOf('用户临时文件')?.checked) throw new Error('系统那几项应该默认勾上')
    if (!boxOf('系统临时文件')?.disabled) throw new Error('要管理员的那项现在不该能勾')
    if (!boxOf('npm 缓存')?.disabled) throw new Error('没东西可清的那项不该能勾')

    await check('系统临时文件') // 点了也不该勾上
    await check('回收站')
    await check('Chrome 缓存')
    await mustClick('清理已选')
    if (!txt().includes('不进回收站')) throw new Error('确认框没说缓存是直接删的')
    if (!txt().includes('chrome.exe')) throw new Error('确认框没提开着的程序')
    if (!txt().includes('回收站会被清空')) throw new Error('勾了回收站,确认框没单独提醒')
    await confirmIn('清理')

    const ids = ((__last.cleanIds as string[] | undefined) ?? []).slice().sort()
    if (ids.join(',') !== 'browser-chrome,sys-recycle-bin,sys-user-temp') {
      throw new Error('发出去的清理项不对: ' + ids.join(','))
    }
    if (!txt().includes('清掉了')) throw new Error('清完没给结果')
    if (!txt().includes('正被占用')) throw new Error('被占用跳过的没说')
    if ((__last.cacheScans as number) < 2) throw new Error('清完没重新统计,列表上的数字对不上')

    // 自定义目录:能移除(只是不再清它),能新加
    delete __last.customDeleted
    delete __last.customSaved
    await mustClick('不再清这个目录（目录本身不动）')
    if (__last.customDeleted !== 'abc') throw new Error('移除自定义目录发错了: ' + String(__last.customDeleted))
    await mustClick('添加自定义目录')
    await mustClick('选目录')
    await mustClick('保存')
    const saved = __last.customSaved as { dir: string; minAgeDays: number } | undefined
    if (!saved || saved.dir !== 'D:/导出' || saved.minAgeDays !== 0) {
      throw new Error('新加的自定义目录发错了: ' + JSON.stringify(saved))
    }
  })

  // 磁盘清理 · 大文件:系统文件勾不上、虚拟机磁盘要在确认框里点名、默认进回收站
  delete __last.largeOpts
  delete __last.deleteReq
  await mount('磁盘清理 · 大文件', <DiskClean />, async () => {
    const txt = () => document.body.textContent || ''
    await mustClick('大文件')
    await mustClick('开始扫描')
    const opts = __last.largeOpts as { roots: string[]; minSize: number } | undefined
    if (!opts || opts.roots.join() !== 'C:\\') throw new Error('默认该扫系统盘: ' + JSON.stringify(opts))
    // 已经是管理员了:不能再劝人「以管理员身份运行」,要说清楚进不去的是什么地方
    const pane = () => document.querySelector('[data-tab="large"]')?.textContent || ''
    if (!pane().includes('连管理员身份也进不去')) throw new Error('管理员身份下进不去的目录没说对')
    if (!pane().includes('本来也删不了')) throw new Error('进不去的都在系统目录里,却没说不影响结果')
    if (pane().includes('以管理员身份运行工具箱')) throw new Error('已经是管理员了还劝人以管理员身份运行')
    if (!pane().includes('C:\\Windows\\CSC')) throw new Error('进不去的是哪些目录没列出来')
    if (!txt().includes('网盘同步')) throw new Error('跳过的网盘文件没说')

    const row = (p: string) => byData('data-path', p)?.querySelector('input[type=checkbox]') as HTMLInputElement | undefined
    const page = row('C:\\pagefile.sys')
    if (!page?.disabled) throw new Error('pagefile.sys 不该能勾')
    if (!txt().includes('不能删：虚拟内存文件')) throw new Error('勾不上的没说为什么')

    // 全选不带上虚拟机磁盘这种带提醒的:那类要一个个看清楚了单独勾
    const all = document.querySelector('input[title^="全选当前列表"]') as HTMLInputElement | null
    if (!all) throw new Error('没有全选')
    await act(async () => {
      all.click()
    })
    if (row('C:\\Users\\demo\\vm\\ubuntu.vhdx')?.checked) throw new Error('全选把虚拟机磁盘也勾上了')
    if (!row('C:\\Users\\demo\\Downloads\\old.iso')?.checked) throw new Error('全选没勾上普通文件')
    await act(async () => {
      all.click()
    })
    if (row('C:\\Users\\demo\\Downloads\\old.iso')?.checked) throw new Error('再点一次全选没有全部取消')

    for (const p of ['C:\\Users\\demo\\vm\\ubuntu.vhdx', 'C:\\Users\\demo\\Videos\\movie.mkv']) {
      const box = row(p)
      if (!box) throw new Error('找不到这一行: ' + p)
      await act(async () => {
        box.click()
      })
    }
    // 永久删除是个看得见的开关,按钮上的字要跟着变
    await check('永久删除（不进回收站）')
    if (!btn('永久删除')) throw new Error('勾了永久删除,按钮没跟着变')
    await check('永久删除（不进回收站）')

    await mustClick('移到回收站')
    if (!txt().includes('磁盘镜像')) throw new Error('删虚拟机磁盘之前没点名提醒')
    if (!txt().includes('清空回收站之后')) throw new Error('没说进回收站不等于腾出了空间')
    await confirmIn('移到回收站')

    const req = __last.deleteReq as { permanent: boolean; files: { path: string; size: number; modTime: number }[] } | undefined
    if (!req || req.permanent) throw new Error('默认应该进回收站: ' + JSON.stringify(req))
    if (req.files.length !== 2 || req.files.some((f) => !f.size || !f.modTime)) {
      throw new Error('删除请求要带着扫描时的大小和时间,后端拿它核对文件变没变: ' + JSON.stringify(req.files))
    }
    if (byData('data-path', 'C:\\Users\\demo\\Videos\\movie.mkv')) throw new Error('删掉的还留在列表里')
    if (!txt().includes('没删：文件正被别的程序占用')) throw new Error('没删成的没在那一行说原因')

    // 按目录看:同一次扫描,一层层点进去
    delete __last.usageDirs
    await mustClick('按目录')
    // StrictMode 下副作用会跑两遍,取几次不要紧,要紧的是取的都是最顶层
    const level = () => (__last.usageDirs as string[] | undefined) ?? []
    if (level().length === 0 || level().some((d) => d !== '')) {
      throw new Error('切到按目录没有先取最顶层: ' + JSON.stringify(level()))
    }
    const open = async (attr: string) => {
      const row = byData('data-usage', attr)
      if (!row) throw new Error('找不到这一项: ' + attr)
      await act(async () => {
        row.click()
      })
      await act(async () => {
        await sleep(50)
      })
    }
    await open('C:\\')
    if (!pane().includes('Users') || !pane().includes('Windows 系统目录')) throw new Error('点进 C 盘没列出下一层')
    await open('C:\\Users')
    if (!pane().includes('demo') || !pane().includes('全部')) throw new Error('再点一层没有面包屑或内容')
    await mustClick('全部')
    await open('C:\\')
    // 「直接放在这里的文件」那一项:切回按文件,按这个目录筛
    await open('files')
    const filter = document.querySelector('input[placeholder^="按路径筛选"]') as HTMLInputElement | null
    if (!filter || filter.value !== 'C:\\') throw new Error('点「直接放在这里的文件」没切回文件列表并筛到这个目录')
  })

  // 磁盘清理 · 重复文件:每组至少留一份 —— 前端勾不上,发出去的请求里也必须有
  delete __last.dupOpts
  delete __last.dupReq
  await mount('磁盘清理 · 重复文件', <DiskClean />, async () => {
    const txt = () => document.body.textContent || ''
    await mustClick('重复文件')
    await mustClick('开始查找')
    const opts = __last.dupOpts as { roots: string[]; skipDevDirs: boolean } | undefined
    if (!opts || opts.roots.join() !== 'C:\\Users\\demo' || !opts.skipDevDirs) {
      throw new Error('默认该扫个人目录并跳过开发目录: ' + JSON.stringify(opts))
    }
    if (!txt().includes('硬链接')) throw new Error('硬链接不算重复,没说')
    // 不是管理员、而且有进不去的不在系统目录里:这才该劝人提权
    const pane = () => document.querySelector('[data-tab="dup"]')?.textContent || ''
    if (!pane().includes('不在系统目录里')) throw new Error('真漏扫的目录没单独说')
    if (!pane().includes('以管理员身份运行工具箱能看到')) throw new Error('不是管理员时没说怎么补全')

    const boxes = (id: string) =>
      Array.from(document.querySelectorAll(`[data-group="${id}"] input[type=checkbox]`)) as HTMLInputElement[]
    const ga = 'a'.repeat(64)
    const gb = 'b'.repeat(64)
    await mustClick('每组只留一份')
    if (boxes(ga).filter((b) => b.checked).length !== 2) throw new Error('三份里应该勾两份')
    // 留的是最早的那份:原件一般最早
    if (boxes(ga)[2].checked) throw new Error('该留修改时间最早的那份(Videos 下的)')

    // 再勾最后一份没勾的:勾不上,并说明为什么
    const lastKept = boxes(gb).find((b) => !b.checked)!
    await act(async () => {
      lastKept.click()
    })
    await act(async () => {
      await sleep(30)
    })
    if (boxes(gb).every((b) => b.checked)) throw new Error('一组全勾上了 —— 这份内容会彻底没了')
    if (!txt().includes('每组至少要留一份')) throw new Error('勾不上没说为什么')

    await mustClick('移到回收站')
    if (!txt().includes('逐个重新核对')) throw new Error('确认框没说删之前会核对内容')
    await confirmIn('移到回收站')
    const req = __last.dupReq as { permanent: boolean; groups: { id: string; keep: string[]; delete: string[] }[] } | undefined
    if (!req || req.permanent || req.groups.length !== 2) throw new Error('删除请求不对: ' + JSON.stringify(req))
    for (const g of req.groups) {
      if (g.keep.length < 1) throw new Error('有一组一份都没留: ' + g.id)
    }
    const a = req.groups.find((g) => g.id === ga)!
    if (a.keep.join() !== 'C:\\Users\\demo\\Videos\\trip.mp4' || a.delete.length !== 2) {
      throw new Error('第一组留错了: ' + JSON.stringify(a))
    }
    if (!txt().includes('移到了回收站')) throw new Error('删完没给结果')
    if (byData('data-group', ga)) throw new Error('只剩一份的组还挂在列表里')
  })

  // 磁盘清理 · 更多清理:空文件夹直接删、核过再删;无效快捷方式默认进回收站
  delete __last.emptyOpts
  delete __last.emptyReq
  delete __last.shortcutScans
  delete __last.shortcutReq
  await mount('磁盘清理 · 更多清理', <DiskClean />, async () => {
    const txt = () => document.body.textContent || ''
    await mustClick('更多清理')
    await mustClick('查找空文件夹')
    const opts = __last.emptyOpts as { roots: string[]; skipDevDirs: boolean } | undefined
    if (!opts || opts.roots.join() !== 'C:\\Users\\demo' || !opts.skipDevDirs) {
      throw new Error('空文件夹默认该扫个人目录: ' + JSON.stringify(opts))
    }
    if (!txt().includes('一天以内动过的')) throw new Error('刚动过的空目录没列出来,没说')
    if (!txt().includes('里面还套着 2 个空文件夹')) throw new Error('套着的空子目录会一起删,没说')
    const all = document.querySelector('[data-list="empty"] input[title="全选"]') as HTMLInputElement | null
    if (!all) throw new Error('空文件夹列表没有全选')
    await act(async () => {
      all.click()
    })
    await mustClick('删除选中的空文件夹')
    if (!txt().includes('不进回收站')) throw new Error('确认框没说空文件夹是直接删的')
    if (!txt().includes('逐个确认还是空的')) throw new Error('确认框没说删之前会核对')
    await confirmIn('删除')
    const req = __last.emptyReq as { paths: string[] } | undefined
    if (!req || req.paths.length !== 2) throw new Error('发出去的空文件夹不对: ' + JSON.stringify(req))

    await mustClick('无效快捷方式')
    if (!__last.shortcutScans) throw new Error('切到无效快捷方式没有自动检查')
    if (!txt().includes('判断不了')) throw new Error('判断不了的没说')
    if (!txt().includes('D:\\tools\\old\\tool.exe')) throw new Error('没说它指向哪儿')
    const row = byData('data-path', 'C:\\Users\\demo\\Start Menu\\Programs\\Old Tool.lnk')?.querySelector(
      'input[type=checkbox]',
    ) as HTMLInputElement | undefined
    if (!row) throw new Error('找不到这个快捷方式')
    await act(async () => {
      row.click()
    })
    await mustClick('移到回收站')
    if (!txt().includes('删的只是快捷方式本身')) throw new Error('确认框没说删的是什么')
    await confirmIn('移到回收站')
    const sreq = __last.shortcutReq as { permanent: boolean; files: { size: number; modTime: number }[] } | undefined
    if (!sreq || sreq.permanent || sreq.files[0]?.size !== 1200 || !sreq.files[0]?.modTime) {
      throw new Error('快捷方式默认该进回收站,并带着扫描时的样子去核对: ' + JSON.stringify(sreq))
    }
  })

  // SM2 加解密与签名验签往返。
  //
  // sm-crypto 0.3.13 → 0.3.14 修的是 SM2 解密里的私钥可恢复漏洞(CVE-2026-23966),
  // 改的正是 doDecrypt 那条路。加解密工具页不在冒烟里,光靠"版本号升了"说明不了
  // 解出来的还是原文 —— 两种密文格式都要走一遍,解密逻辑是按 mode 分支的
  try {
    const kp = sm2GenerateKeyPair()
    const plain = new TextEncoder().encode('国密往返:中文 + ascii + \u0000 字节')
    for (const mode of ['C1C3C2', 'C1C2C3'] as const) {
      const cipher = sm2Encrypt(kp.publicKey, plain, mode)
      if (cipher.length <= plain.length) throw new Error(`${mode} 密文不该比明文短`)
      const back = sm2Decrypt(kp.privateKey, cipher, mode)
      if (new TextDecoder().decode(back) !== new TextDecoder().decode(plain)) {
        throw new Error(`${mode} 解出来的和原文不一样`)
      }
    }
    // 用错钥匙解必须失败或得不到原文,不能静默给个看着像的东西
    const other = sm2GenerateKeyPair()
    const cipher = sm2Encrypt(kp.publicKey, plain)
    let wrongKeyLeaked = false
    try {
      const back = sm2Decrypt(other.privateKey, cipher)
      wrongKeyLeaked = new TextDecoder().decode(back) === new TextDecoder().decode(plain)
    } catch {
      // 抛错是可接受的结果
    }
    if (wrongKeyLeaked) throw new Error('用别人的私钥居然解出了原文')

    const sig = sm2Sign(kp.privateKey, plain, { publicKey: kp.publicKey })
    if (!sm2Verify(kp.publicKey, plain, sig)) throw new Error('自己签的自己验不过')
    const tampered = new Uint8Array(plain)
    tampered[0] ^= 0xff
    if (sm2Verify(kp.publicKey, tampered, sig)) throw new Error('改了一个字节还能验过')
    console.log('  OK   SM2 加解密与签名往返')
  } catch (e) {
    note('SM2 加解密与签名往返', e)
  }

  console.log(failed ? '\n有异常' : '\n全部通过')
  process.exit(failed ? 1 : 0)
}
void main().catch((e) => {
  note('main', e)
  process.exit(1)
})
