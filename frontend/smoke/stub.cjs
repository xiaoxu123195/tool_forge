// Wails 绑定的替身。build.cjs 把所有 wailsjs 导入都指到这个文件。
//
// 导出名不是手写死的,而是运行时从真实绑定文件里扫出来的 —— 以后每加一个 RPC,
// 这里自动多一个默认桩,不用改冒烟测试。默认桩 resolve('');要有真实行为的
// 才在 special 里点名。
//
// 契约提醒:后端 RPC 现在是 (T, error) —— Wails 只把第一个值给 JS,出错 reject。
// 桩也必须照这个来:resolve 单值,绝不 resolve [值, 错误] 那种数组。
const fs = require('fs')
const path = require('path')
const fx = require('./fixtures.cjs')

const names = new Set()
for (const rel of ['../wailsjs/go/main/App.js', '../wailsjs/runtime/runtime.js']) {
  const src = fs.readFileSync(path.join(__dirname, rel), 'utf8')
  for (const m of src.matchAll(/export function (\w+)/g)) names.add(m[1])
}

const off = () => () => {}
const nop = () => {}

const special = {
  // ---- AI 配置 ----
  ListAIProviders: () => Promise.resolve(fx.providers),
  ListAIProviderKeys: (id) => Promise.resolve(fx.keysById[id] || []),
  SaveAIProviderKeys: (_id, k) => Promise.resolve(k),
  SaveAIProvider: (p) => Promise.resolve({ ...p, id: p.id || 'new-p' }),
  CheckAIProviderKeys: (_id, _m, ids) =>
    Promise.resolve((ids || []).map((k) => ({ keyId: k, ok: true, durationMs: 9 }))),
  ListAIModelSpecs: (id) => {
    const p = fx.providers.find((x) => x.id === id)
    return Promise.resolve(((p && p.models) || []).map(fx.modelSpec))
  },
  GetAIModelSpec: (_pid, mid) => Promise.resolve(fx.modelSpec(mid)),
  GetAIConfig: () => Promise.resolve({ defaultProviderId: '', defaultModelId: '' }),

  // ---- 会话 ----
  ListAIConversations: () =>
    Promise.resolve(
      fx.conversations.map((c) => ({
        id: c.id,
        title: c.title,
        providerId: c.providerId,
        modelId: c.modelId,
        updatedAt: c.updatedAt,
        messageCount: (c.messages || []).length,
      })),
    ),
  GetAIConversation: (id) => {
    const c = fx.conversations.find((x) => x.id === id)
    return c ? Promise.resolve(c) : Promise.reject(new Error('会话不存在: ' + id))
  },
  CreateAIConversation: (providerID, modelID, title, system, contextCount) =>
    Promise.resolve({
      id: 'c-created',
      title: title || '新对话',
      providerId: providerID,
      modelId: modelID,
      system,
      contextCount,
      messages: [],
      createdAt: 1,
      updatedAt: 1,
    }),
  ContinueAILastChat: () => Promise.resolve({}),
  // 导出预览:给一段像样的 Markdown,好让预览区真有内容可渲染
  RenderAIConversationMarkdown: (id, opt) =>
    Promise.resolve(
      '# 导出预览\n\n> **模型** `gpt-5`\n\n## 用户\n\n你好\n\n## 助手\n\n你好呀' +
        (opt && opt.includeThinking ? '\n\n<details><summary>思考过程</summary>\n\n> 想了想\n\n</details>' : ''),
    ),
  // 用户在保存对话框里点了取消 = 空路径。这是最容易被当成"失败"处理错的分支
  ExportAIConversation: () => Promise.resolve(''),

  // ---- 助手预设 ----
  ListAIAssistants: () => Promise.resolve(fx.assistants),
  SaveAIAssistant: (a) => Promise.resolve({ ...a, id: a.id || 'new-a' }),

  // ---- 工具 ----
  ListAIChatTools: () => Promise.resolve(fx.chatTools),

  // ---- 运行时事件 ----
  EventsOn: off,
  EventsOnMultiple: off,
  EventsOnce: off,
  EventsEmit: nop,
  EventsOff: nop,
  EventsOffAll: nop,
  BrowserOpenURL: nop,
  LogPrint: nop,
  LogDebug: nop,
  LogInfo: nop,
  LogWarning: nop,
  LogError: nop,
  LogFatal: nop,
}

for (const n of names) {
  module.exports[n] = special[n] || (() => Promise.resolve(''))
}
