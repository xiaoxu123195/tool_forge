/**
 * AI 问答的渲染冒烟测试(`npm run smoke`)。
 *
 * 存在的理由:这个应用没有浏览器控制台,渲染期抛异常的表现就是整窗白屏,
 * 用户只能说出"白了"三个字。这套测试在 jsdom 里把主要界面真实挂载一遍
 * (真 effect、真点击),渲染炸了当场红 —— 它抓到过密钥列表白屏的根因。
 *
 * 数据来自 fixtures.cjs 的固定样本;新坑修掉后往 fixtures 里补对应形状。
 */
import { StrictMode } from 'react'
import { MemoryRouter } from 'react-router-dom'
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
import MobileForensic from '../src/tools/mobile-forensic/index'
import AppSearch from '../src/tools/app-search/index'
import SQLiteSearch from '../src/tools/sqlite-search/index'
import ClipboardTool from '../src/tools/clipboard/index'
import MCPWorkbench from '../src/tools/mcp-workbench/index'
import { ConfirmProvider } from '../src/components/ui/confirm'
import { LocalAPISection } from '../src/profile/sections/LocalAPI'
import { useForensicStore } from '../src/stores/forensic'
import { conversations } from './fixtures.cjs'
// 直接引桩本体拿事件把手。build.cjs 只把含 "wailsjs" 的路径重定向到这里,
// 相对路径原样解析 —— CJS 缓存保证跟组件用的是同一个模块实例
import { __emit, __calls, __last } from './stub.cjs'
import * as wailsStub from './stub.cjs'
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
