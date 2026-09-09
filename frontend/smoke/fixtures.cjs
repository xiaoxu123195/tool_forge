// 冒烟测试的固定数据。
//
// 特意不用 ~/.toolforge 里的真实数据:换台机器 / CI 上没有那些文件,测试就废了。
// 这份数据的价值在于把踩过坑的形状全部固化下来 —— 每个字段组合都对应一次真实事故
// 或一个边界情形,新的坑修掉后也应该往这里补一条。

/** 一张 1x1 透明 PNG,当图片附件用 */
const TINY_PNG =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=='

const providers = [
  {
    id: 'p-multi',
    name: '多密钥中转',
    type: 'openai-compatible',
    logo: '',
    baseUrl: 'https://relay.example/v1',
    apiKey: 'sk-fixture-a',
    apiKeys: [
      { id: 'k_a', key: 'sk-fixture-a' },
      { id: 'k_b', key: 'sk-fixture-b', label: '备用', disabled: true },
    ],
    enabled: true,
    models: ['gpt-5.6-luna', 'gpt-5.4-mini', 'deepseek-ai/DeepSeek-V3'],
    isSystem: false,
    createdAt: 1,
    updatedAt: 9,
  },
  {
    id: 'p-single',
    name: '单密钥官方',
    type: 'anthropic',
    logo: 'anthropic',
    baseUrl: 'https://api.anthropic.com',
    apiKey: 'sk-fixture-c',
    apiKeys: [{ id: 'k_c', key: 'sk-fixture-c' }],
    enabled: true,
    models: ['claude-sonnet-4-5'],
    isSystem: true,
    createdAt: 2,
    updatedAt: 8,
  },
  {
    id: 'p-empty',
    name: '没配密钥',
    type: 'gemini',
    logo: 'gemini',
    baseUrl: '',
    apiKey: '',
    enabled: false,
    models: [],
    isSystem: true,
    createdAt: 3,
    updatedAt: 7,
  },
]

const keysById = Object.fromEntries(providers.map((p) => [p.id, p.apiKeys || []]))

const conversations = [
  {
    // 事故重放:老会话的 thinking 是字符串。后端读盘会升格成数组,
    // 但 thinkingText 的兜底也得能扛住原始形态 —— 它崩过一次整窗白屏
    id: 'c-legacy',
    title: '老格式思考',
    providerId: 'p-multi',
    modelId: 'gpt-5.4-mini',
    messages: [
      { id: 'm1', role: 'user', content: '你好', createdAt: 1 },
      {
        id: 'm2',
        role: 'assistant',
        content: '你好!',
        thinking: '这是一段老格式的字符串思考',
        model: 'gpt-5.4-mini',
        createdAt: 2,
      },
    ],
    createdAt: 1,
    updatedAt: 2,
  },
  {
    // 全家桶:思考块 + 检索 + 工具(成功/失败/执行中) + 引用 + 用量 + 截断 + 附件
    id: 'c-rich',
    title: '富消息',
    providerId: 'p-multi',
    modelId: 'gpt-5.6-luna',
    system: '# 系统提示\n带 **Markdown**',
    messages: [
      {
        id: 'm1',
        role: 'user',
        content: '查一下黄金价格',
        // 两种形态都得画得出来:data 是"刚上传 / 流式刚推过来"的样子,
        // ref 是从磁盘读回来的样子(已外置,走 /aiblob/)。
        // 只留 data 的话,渲染 ref 那条路一次都不会被走到
        images: [
          { mimeType: 'image/png', data: TINY_PNG },
          { mimeType: 'image/png', ref: 'a'.repeat(64) + '.png' },
        ],
        files: [
          { name: 'note.txt', text: '附件文本', sizeBytes: 12 },
          {
            name: '报告.pdf',
            mimeType: 'application/pdf',
            ref: 'b'.repeat(64) + '.pdf',
            sizeBytes: 91234,
          },
        ],
        createdAt: 1,
      },
      {
        id: 'm2',
        role: 'assistant',
        content: '# 结果\n\n- 一条\n- 两条\n\n```go\nfmt.Println("hi")\n```',
        thinking: [{ text: '思考一下', signature: 'sig==' }],
        searches: [
          { query: '今日黄金价格', status: 'done', results: 5 },
          { query: 'gold price', status: 'running' },
        ],
        toolCalls: [
          { id: 't1', name: 'get_current_time', arguments: '{"timezone":"Asia/Shanghai"}', result: '2026-09-09' },
          { id: 't2', name: 'exa_web_search', arguments: '{}', error: '超时' },
          { id: 't3', name: 'ctx7_resolve', arguments: '{}', status: 'running' },
        ],
        citations: [{ url: 'https://example.com/a', title: '来源A' }],
        usage: { inputTokens: 12345, outputTokens: 678, reasoningTokens: 90, cachedTokens: 4096 },
        durationMs: 73210,
        truncated: true,
        model: 'gpt-5.6-luna',
        createdAt: 2,
      },
      { id: 'm3', role: 'clear', content: '', createdAt: 3 },
      { id: 'm4', role: 'user', content: '清除之后再问', createdAt: 4 },
      {
        // 空正文 + 没在流式:走"没有返回内容"占位分支
        id: 'm5',
        role: 'assistant',
        content: '',
        model: 'gpt-5.6-luna',
        createdAt: 5,
      },
    ],
    createdAt: 1,
    updatedAt: 5,
  },
  {
    // 最后一条是被中断的回复:只有"最后一条 assistant"才拿得到 onContinue,
    // 所以「继续写」按钮的渲染只能靠这种形状的会话覆盖
    id: 'c-truncated',
    title: '被中断的回复',
    providerId: 'p-multi',
    modelId: 'gpt-5.6-luna',
    messages: [
      { id: 't1', role: 'user', content: '写一篇很长的文章', createdAt: 1 },
      {
        id: 't2',
        role: 'assistant',
        content: '第一段写完了,第二段写到一半就被',
        model: 'gpt-5.6-luna',
        truncated: true,
        usage: { inputTokens: 12, outputTokens: 340 },
        durationMs: 4200,
        createdAt: 2,
      },
    ],
    createdAt: 1,
    updatedAt: 2,
  },
  {
    id: 'c-empty',
    title: '空会话',
    providerId: 'p-single',
    modelId: 'claude-sonnet-4-5',
    messages: [],
    createdAt: 1,
    updatedAt: 1,
  },
]

const assistants = [
  {
    id: 'builtin-1',
    name: '逆子AI',
    emoji: '😇',
    system: '# 双面\n**表面**客气,`内心`吐槽',
    temperature: 0.9,
    webSearch: true,
    tools: true,
    reasoningEffort: 'high',
    contextCount: 20,
    maxTokens: 4096,
    sortOrder: 1,
    createdAt: 1,
    updatedAt: 1,
  },
  {
    id: 'builtin-2',
    name: '素预设',
    emoji: '🧣',
    system: '只有提示词,不带参数',
    sortOrder: 2,
    createdAt: 2,
    updatedAt: 2,
  },
]

const modelSpec = (m) => ({
  id: m || 'x',
  endpoint: 'openai-chat',
  capabilities: ['vision', 'tools', 'webSearch', 'reasoning'],
  reasoning: { efforts: ['low', 'medium', 'high'] },
  sampling: { temperature: true, topP: true, maxTemp: 2 },
  maxOutput: 4096,
})

const chatTools = {
  tools: [
    { name: 'get_current_time', description: '取当前时间', source: '' },
    { name: 'exa_web_search', description: '联网搜索', source: 'exa' },
  ],
  offline: ['context7'],
}

// 请求留档。三条覆盖三种状态:成功、HTTP 失败、还在跑 ——
// 面板对这三种的画法不一样(圆点颜色、错误条、"进行中"标记)
const traces = [
  {
    id: 'tr-ok',
    ts: 1757000000000,
    kind: 'chat',
    providerName: '我的中转',
    model: 'gpt-5.6-luna',
    endpoint: 'openai-responses',
    convId: 'c-rich',
    status: 200,
    durationMs: 2400,
    frameCount: 3,
    done: true,
  },
  {
    id: 'tr-400',
    ts: 1757000010000,
    kind: 'chat',
    providerName: '我的中转',
    model: 'gpt-5.6-luna',
    endpoint: 'openai-chat',
    convId: 'c-rich',
    status: 400,
    error: 'HTTP 400: unknown field "reasoning"',
    durationMs: 120,
    frameCount: 1,
    done: true,
  },
  {
    // 还在跑的那条:done 为假、没有状态码 —— 卡住的请求就长这样
    id: 'tr-running',
    ts: 1757000020000,
    kind: 'chat',
    providerName: 'Anthropic',
    model: 'claude-sonnet-4-5',
    endpoint: 'anthropic-messages',
    convId: 'c-other',
    frameCount: 0,
  },
  {
    // 检测发的:没有 convId —— 面板勾着"只看当前会话"时它会被藏起来,
    // 那条"另有 N 条"的提示就是为它准备的
    id: 'tr-test',
    ts: 1757000030000,
    kind: 'test',
    providerName: '我的中转',
    model: 'gpt-5.6-luna',
    endpoint: 'openai-chat',
    status: 401,
    error: 'HTTP 401: invalid api key',
    durationMs: 90,
    frameCount: 1,
    done: true,
  },
]

function traceDetail(t) {
  return {
    ...t,
    kind: 'chat',
    providerId: 'p-multi',
    method: 'POST',
    url: 'https://relay.example.com/v1/responses?key=abcd****',
    headers: ['Authorization: Bearer sk-a****', 'Content-Type: application/json'],
    body: '{"model":"gpt-5.6-luna","stream":true}',
    frames:
      t.frameCount > 0
        ? ['{"type":"response.output_text.delta","delta":"你好"}', '{"type":"response.completed"}']
        : [],
    framesTruncated: t.id === 'tr-ok',
  }
}

// 跨会话搜索结果。三种形状:正常命中、只命中标题(hits 为空)、还有更多没列出来 ——
// 面板对这三种的画法不一样
function searchResults(q) {
  if (!q || q === 'zzz') return []
  return [
    {
      convId: 'c-rich',
      title: '富消息',
      updatedAt: 5,
      hits: [
        { messageId: 'm1', role: 'user', before: '前面的话 ', match: q, after: ' 后面的话', createdAt: 1 },
        { messageId: 'm2', role: 'assistant', before: '', match: q, after: '在开头', createdAt: 2 },
      ],
      more: 3,
    },
    {
      // 只命中标题:面板要给一个"打开这条会话"的入口,
      // 否则这条在结果里就是个点不动的标题。
      //
      // hits 写成 null 而不是 [] —— 这才是 Go 真正发过来的形状(nil 切片
      // 序列化成 null)。当初照着 TS 类型写了 [],结果这条崩没被拦住。
      // 样本要照着线上的字节写,不是照着类型声明写。
      convId: 'c-empty',
      title: '空会话',
      updatedAt: 1,
      titleMatch: true,
      hits: null,
    },
  ]
}

module.exports = {
  searchResults,
  providers,
  keysById,
  conversations,
  assistants,
  modelSpec,
  chatTools,
  traces,
  traceDetail,
}
