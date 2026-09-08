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
}: {
  conversationId: string
  setConv: React.Dispatch<React.SetStateAction<Conversation | null>>
  /** 流结束(正常或出错)时调用,用于收起"生成中"状态 */
  onStreamEnd: () => void
  /** 正常结束 */
  onDone: () => void
  /** 出错;参数是后端给的错误文案 */
  onError: (err: string) => void
}) {
  // 用 EventsOn 返回的 cancel 函数逐个退订,避免误伤同名监听
  useEffect(() => {
    if (!conversationId) return
    const offChunk = EventsOn(EV_CHUNK_PREFIX + conversationId, (delta: string) => {
      if (!delta) return
      setConv((prev) => {
        if (!prev) return prev
        const msgs = [...prev.messages]
        const last = msgs[msgs.length - 1]
        if (last?.role === 'assistant') {
          msgs[msgs.length - 1] = { ...last, content: last.content + delta }
          return { ...prev, messages: msgs }
        }
        return prev
      })
    })
    const offImage = EventsOn(EV_IMAGE_PREFIX + conversationId, (img: ImageBlock) => {
      if (!img || (!img.data && !img.url)) return
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
      setConv((prev) => {
        if (!prev) return prev
        const msgs = [...prev.messages]
        const last = msgs[msgs.length - 1]
        if (last?.role === 'assistant') {
          const blocks = last.thinking ?? []
          const head = blocks[0] ?? {}
          msgs[msgs.length - 1] = {
            ...last,
            thinking: [{ ...head, text: (head.text ?? '') + delta }, ...blocks.slice(1)],
          }
          return { ...prev, messages: msgs }
        }
        return prev
      })
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
    const offError = EventsOn(EV_ERROR_PREFIX + conversationId, (err: string) => {
      onStreamEnd()
      onError(err)
    })
    return () => {
      offChunk()
      offThinking()
      offCitation()
      offSearch()
      offTool()
      offImage()
      offDone()
      offError()
    }
  }, [conversationId])
}
