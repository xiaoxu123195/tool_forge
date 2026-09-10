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
import MmkvTool from '../src/tools/mmkv/index'
import PlistTool from '../src/tools/plist/index'
import { ConfirmProvider } from '../src/components/ui/confirm'
import { conversations } from './fixtures.cjs'
// 直接引桩本体拿事件把手。build.cjs 只把含 "wailsjs" 的路径重定向到这里,
// 相对路径原样解析 —— CJS 缓存保证跟组件用的是同一个模块实例
import { __emit, __calls } from './stub.cjs'

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

/** 往输入框里打字(React 受控组件要走原生 setter 才认) */
const type = async (placeholder: string, value: string) => {
  const el = document.querySelector(
    `input[placeholder*="${placeholder}"]`,
  ) as HTMLInputElement | null
  if (!el) return false
  // 注意用 window.Event 而不是全局 Event:Node 自己也有一个同名的 Event 类,
  // 拿它构造出来的对象 jsdom 的 dispatchEvent 不认
  const setter = Object.getOwnPropertyDescriptor(
    window.HTMLInputElement.prototype,
    'value',
  )?.set
  setter?.call(el, value)
  await act(async () => {
    el.dispatchEvent(new window.Event('input', { bubbles: true }))
  })
  await act(async () => {
    await sleep(50)
  })
  return true
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
        await type('在这个会话里查找', '的')
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
  await mount('默认与自动起标题页', <DefaultsTab />)

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
      await type('在所有会话里查找', '泛型')
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

  console.log(failed ? '\n有异常' : '\n全部通过')
  process.exit(failed ? 1 : 0)
}
void main().catch((e) => {
  note('main', e)
  process.exit(1)
})
