import { useEffect } from 'react'
import { EventsOn } from '../../../wailsjs/runtime/runtime'
import {
  EV_CHUNK_PREFIX,
  EV_THINKING_PREFIX,
  EV_IMAGE_PREFIX,
  EV_CITATION_PREFIX,
  EV_SEARCH_PREFIX,
  EV_TOOL_PREFIX,
  EV_DONE_PREFIX,
  EV_ERROR_PREFIX,
  EV_TITLE_PREFIX,
  type Citation,
  type Conversation,
  type ImageBlock,
  type SearchQuery,
  type ToolCall,
} from './types'

/**
 * 订阅一个会话的流式事件,把增量合并进本地会话状态。
 *
 * 抽成 hook 是因为这段逻辑和「消息怎么渲染」「输入栏长什么样」完全无关 ——
 * 它只关心"后端推了一块内容过来,该往最后一条 assistant 消息的哪个字段上加"。
 * 六种事件的合并规则各不相同,放在组件里会把主流程淹掉。
 */
export function useChatStream({
  conversationId,
  setConv,
  onStreamEnd,
  onDone,
  onError,
  onTitle,
}: {
  conversationId: string
  setConv: React.Dispatch<React.SetStateAction<Conversation | null>>
  /** 流结束(正常或出错)时调用,用于收起"生成中"状态 */
  onStreamEnd: () => void
  /** 正常结束 */
  onDone: () => void
  /** 出错;参数是后端给的错误文案 */
  onError: (err: string) => void
  /** 后端自动起好了标题;用于顺带刷新左侧列表 */
  onTitle?: (title: string) => void
}) {
  // 用 EventsOn 返回的 cancel 函数逐个退订,避免误伤同名监听
  useEffect(() => {
    if (!conversationId) return

    // 正文和思考的增量先攒在缓冲里,每 40ms 落一次状态。
    //
    // 不攒的话,每个 chunk 都是一次完整的 setConv → 整棵消息树重渲染 → 最后一条的
    // Markdown 全文重解析。快的模型一秒推几十个 chunk,长回答直接把界面拖卡。
    // 40ms(约 25 帧/秒)肉眼看仍是连续的,解析量却少了一个数量级。
    // 缓冲放在 effect 闭包里而不是 ref:换会话重订阅时天然清零,不用手工重置。
    let pendingText = ''
    let pendingThink = ''
    let flushTimer: ReturnType<typeof setTimeout> | null = null
    let disposed = false

    const flushNow = () => {
      if (flushTimer != null) {
        clearTimeout(flushTimer)
        flushTimer = null
      }
      if (disposed || (!pendingText && !pendingThink)) return
      const text = pendingText
      const think = pendingThink
      pendingText = ''
      pendingThink = ''
      setConv((prev) => {
        if (!prev) return prev
        const msgs = [...prev.messages]
        const last = msgs[msgs.length - 1]
        if (last?.role !== 'assistant') return prev
        let next = last
        if (text) next = { ...next, content: next.content + text }
        if (think) {
          const blocks = next.thinking ?? []
          const head = blocks[0] ?? {}
          next = {
            ...next,
            thinking: [{ ...head, text: (head.text ?? '') + think }, ...blocks.slice(1)],
          }
        }
        msgs[msgs.length - 1] = next
        return { ...prev, messages: msgs }
      })
    }
    const schedule = () => {
      if (flushTimer == null) flushTimer = setTimeout(flushNow, 40)
    }

    const offChunk = EventsOn(EV_CHUNK_PREFIX + conversationId, (delta: string) => {
      if (!delta) return
      pendingText += delta
      schedule()
    })
    const offImage = EventsOn(EV_IMAGE_PREFIX + conversationId, (img: ImageBlock) => {
      if (!img || (!img.data && !img.url && !img.ref)) return
      setConv((prev) => {
        if (!prev) return prev
        const msgs = [...prev.messages]
        const last = msgs[msgs.length - 1]
        if (last?.role === 'assistant') {
          msgs[msgs.length - 1] = {
            ...last,
            images: [...(last.images ?? []), img],
          }
          return { ...prev, messages: msgs }
        }
        return prev
      })
    })
    // 思考增量只用于实时渲染,统一累到第一个块上;
    // 带 signature 的结构化块由后端在流结束时落盘,下次 load 会带回来
    const offThinking = EventsOn(EV_THINKING_PREFIX + conversationId, (delta: string) => {
      if (!delta) return
      pendingThink += delta
      schedule()
    })
    const offCitation = EventsOn(EV_CITATION_PREFIX + conversationId, (c: Citation) => {
      if (!c?.url) return
      setConv((prev) => {
        if (!prev) return prev
        const msgs = [...prev.messages]
        const last = msgs[msgs.length - 1]
        if (last?.role === 'assistant') {
          const list = last.citations ?? []
          if (list.some((x) => x.url === c.url)) return prev
          msgs[msgs.length - 1] = { ...last, citations: [...list, c] }
          return { ...prev, messages: msgs }
        }
        return prev
      })
    })
    // 供应商内置联网搜索发出的检索词。同一个词会推两次(running → done),
    // 按 query 就地覆盖 —— 只追加的话界面上会出现两条一模一样的
    const offSearch = EventsOn(EV_SEARCH_PREFIX + conversationId, (q: SearchQuery) => {
      if (!q?.query) return
      setConv((prev) => {
        if (!prev) return prev
        const msgs = [...prev.messages]
        const last = msgs[msgs.length - 1]
        if (last?.role === 'assistant') {
          const list = last.searches ?? []
          const idx = list.findIndex((x) => x.query === q.query)
          const next = idx >= 0 ? list.map((x, i) => (i === idx ? q : x)) : [...list, q]
          msgs[msgs.length - 1] = { ...last, searches: next }
          return { ...prev, messages: msgs }
        }
        return prev
      })
    })
    // 同一次调用会推两次:模型请求时(status=running)、本地执行完时(带结果)。
    // 同一次提问可能连调多轮,按 id 覆盖而不是无脑追加
    const offTool = EventsOn(EV_TOOL_PREFIX + conversationId, (tc: ToolCall) => {
      if (!tc?.id) return
      setConv((prev) => {
        if (!prev) return prev
        const msgs = [...prev.messages]
        const last = msgs[msgs.length - 1]
        if (last?.role === 'assistant') {
          const list = last.toolCalls ?? []
          const idx = list.findIndex((x) => x.id === tc.id && x.name === tc.name)
          const next = idx >= 0 ? list.map((x, i) => (i === idx ? tc : x)) : [...list, tc]
          msgs[msgs.length - 1] = { ...last, toolCalls: next }
          return { ...prev, messages: msgs }
        }
        return prev
      })
    })
    const offDone = EventsOn(EV_DONE_PREFIX + conversationId, (final: string) => {
      // 先把缓冲整个落下去:思考增量 final 里没有,不落就丢了;
      // 正文落了也无妨 —— 紧接着会被 final 整体覆盖,不会拼出重复
      flushNow()
      onStreamEnd()
      setConv((prev) => {
        if (!prev) return prev
        const msgs = [...prev.messages]
        const last = msgs[msgs.length - 1]
        if (last?.role === 'assistant') {
          // final 是后端累计的完整内容;保险起见以它为准
          msgs[msgs.length - 1] = { ...last, content: final || last.content }
          return { ...prev, messages: msgs }
        }
        return prev
      })
      onDone()
    })
    // 自动起的标题。只改标题,不碰 messages —— 它到达时用户很可能已经在问下一句了,
    // 顺手把整个 conv 覆盖掉会把正在流的那条回复抹了
    const offTitle = EventsOn(EV_TITLE_PREFIX + conversationId, (title: string) => {
      if (!title) return
      setConv((prev) => (prev ? { ...prev, title, titleAuto: false } : prev))
      onTitle?.(title)
    })
    const offError = EventsOn(EV_ERROR_PREFIX + conversationId, (err: string) => {
      flushNow() // 出错也把已收到的部分显示全,别让最后一截丢在缓冲里
      onStreamEnd()
      onError(err)
    })
    return () => {
      disposed = true
      if (flushTimer != null) clearTimeout(flushTimer)
      offChunk()
      offThinking()
      offCitation()
      offSearch()
      offTool()
      offImage()
      offDone()
      offTitle()
      offError()
    }
  }, [conversationId])
}
