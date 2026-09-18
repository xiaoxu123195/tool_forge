# Tool Forge 开发文档

面向二次开发者与 AI 协作者，描述项目当前的架构、目录、约定与"如何加一个新东西"。
README 面向用户，本文不重复其内容。

---

## 1. 项目定位

Tool Forge 是基于 Wails v2 的跨平台桌面工具箱。核心原则三条：

- **离线优先**：常规处理在前端完成，需要系统能力或 Go 生态的走 Go；除 AI 工具调用用户自配的远端 LLM 外，没有任何第三方网络依赖
- **统一外壳**：所有工具共用一套 ToolShell + 配色 + 快捷键，写一个新工具不用碰 layout/header
- **可拆**：每个工具一个独立目录、一份 `meta.ts`，注册表里加一行就上架

---

## 2. 技术栈

| 层 | 选型 | 备注 |
| --- | --- | --- |
| 桌面框架 | Wails v2.11 | Go ↔ JS 双向 RPC + 事件总线 |
| 后端 | Go 1.24 | 模块名 `tool_forge` |
| 前端 | React 18 + TypeScript 5 + Vite 5 | |
| UI | Tailwind CSS + shadcn/ui 风格 | 组件 copy-in，可改源码 |
| 状态 | Zustand 4 + `persist` | 配置类 store 持久化到 localStorage / Keychain |
| 路由 | React Router v6 | |
| 编辑器 | CodeMirror 6 | 按需引入语法包 |
| 图标 | lucide-react | 全站统一 |
| 凭据 | `github.com/zalando/go-keyring` | API Key / Cookie 等敏感数据 |
| PDF 文本提取 | `github.com/ledongthuc/pdf` | AI Chat 文件附件用 |

---

## 3. 目录结构

```
tool_forge/
├── main.go                        # Wails 入口
├── app.go                         # 暴露给前端的 RPC 集合
├── wails.json                     # Wails 配置（版本号写在这里）
├── go.mod / go.sum
├── README.md                      # 面向用户
├── LICENSE                        # GPL-3.0
├── docs/
│   ├── DEVELOPMENT.md             # 本文
│   └── images/                    # README 用图与截图
├── backend/
│   ├── tools/                     # 每个工具一个子包（详见 §5）
│   │   ├── adbx/                  # adb 底座：连服务端、挑设备、跑命令（内置 adb）
│   │   ├── aichat/                # AI 对话 + 翻译 + 多协议适配
│   │   ├── aiconfig/              # 本机 AI 配置总览：扫各家的 MCP / skills / 插件，每条带出处文件
│   │   ├── aistupid/              # AI 智障检测
│   │   ├── appsearch/             # App 全平台搜索（七麦等）
│   │   ├── archive/               # tar 解包（越界路径拦截、Windows 非法文件名改名），取证与真机浏览共用
│   │   ├── charles/               # Charles Key 生成
│   │   ├── claudeinsight/         # Claude 用量洞察
│   │   ├── clipboard/             # 剪贴板历史
│   │   ├── codexinsight/          # Codex 用量洞察
│   │   ├── devicefs/              # 真机文件浏览（Android adb / iOS SFTP）
│   │   ├── envscan/               # 开发环境扫描
│   │   ├── filehash/              # 文件哈希与类型识别
│   │   ├── forensic/              # 移动取证：两个平台都直连提取，go-forensic 为可选备选
│   │   ├── httptest/              # HTTP 调试
│   │   ├── iosmux/                # usbmuxd 协议 + 经 USB 的 SSH
│   │   ├── llmproxy/              # LLM 透明代理 + 请求留档
│   │   ├── mcp/                   # MCP 客户端
│   │   ├── mmkv/                  # MMKV 解析
│   │   ├── netenvcheck/           # 网络环境体检
│   │   ├── netscan/               # 网络工具集
│   │   ├── outlookmail/           # Outlook 邮箱管理
│   │   ├── plist/                 # plist / NSKeyedArchiver 解析
│   │   ├── protobuf/              # protobuf 裸字节解析
│   │   ├── providerswitch/        # AI Provider 切换
│   │   └── sqlitex/               # SQLite 关键词搜索与表浏览（只读，不改动证据）
│   ├── system/                    # 系统能力：data / hotkey / system
│   └── updater/                   # 自动更新：检查、下载、安装
└── frontend/
    ├── package.json
    ├── tailwind.config.ts
    ├── vite.config.ts
    ├── tsconfig.json
    └── src/
        ├── main.tsx               # React 入口
        ├── App.tsx                # 路由 + 全局事件订阅
        ├── layouts/               # MainLayout：左菜单 + 右内容
        ├── components/
        │   ├── ui/                # shadcn/ui 基础组件
        │   ├── tool/              # ToolShell、ToolHeader、MarkdownPreview ...
        │   ├── CommandPalette.tsx
        │   └── ToolContextMenu.tsx
        ├── tools/                 # 每个工具一个目录（详见 §5）
        │   └── registry.ts        # 全部工具的总入口
        ├── profile/               # 个人主页（独立一级页面）
        │   ├── index.tsx
        │   └── sections/          # 基础信息 / AI 配置 / AI 用量 / 数据 / 关于 ...
        ├── pages/                 # Profile 之外的非工具页（如 404）
        ├── stores/                # Zustand store（按主题切片）
        ├── lib/                   # cn、文件保存、剪贴板封装等
        └── styles/globals.css     # 全局变量、暗色主题、代码块配色
```

---

## 4. 架构与数据流

```
┌─────────────────────────────────────────────────────────┐
│  React 前端                                              │
│  ├─ MainLayout（侧栏 + Profile 入口 + 全局快捷键）       │
│  ├─ /tools/* 工具页（tools/<name>/index.tsx）            │
│  └─ /profile  个人主页（profile/sections/*）             │
└─────────────────────┬───────────────────────────────────┘
                      │  Wails Binding（自动生成 TS 类型）
                      │  + Wails Events（流式输出 / 进度）
┌─────────────────────┴───────────────────────────────────┐
│  Go 后端 (app.go)                                        │
│  ├─ backend/tools/<name>     业务处理器                  │
│  ├─ backend/system           data / hotkey / system      │
│  └─ backend/updater          检查 / 下载 / 安装          │
└─────────────────────────────────────────────────────────┘
```

**前后端职责划分**

| 类型 | 在哪做 | 例子 |
| --- | --- | --- |
| 纯计算、字符串处理 | **前端** | JSON 格式化、Base64、URL 编解码、进制转换、Hash、UUID |
| 需 Go 生态或二进制 | **后端** | Protobuf 解析、PDF 文本提取、MMKV、证书解析、Hex |
| 系统能力 | **后端** | 文件对话框、剪贴板监听、全局快捷键、Keychain |
| 设备协议直连 | **后端** | adb（adbx）、usbmuxd + SSH（iosmux） |
| 第三方 CLI 集成 | **后端** | go-forensic（仅作移动取证的可选备选引擎，不装也能用） |
| AI 流式调用 | **后端** | OpenAI / Anthropic / Gemini / 自定义兼容协议 |

后端处理器尽量保持**纯函数**（输入→输出/error），有状态的（AI Chat、剪贴板、自动更新）封装成 `Service`，由 `app.go` 持有。

---

## 5. 工具模块规范

### 5.1 新增一个工具

以「JSON 格式化」为例。

**前端**

```
frontend/src/tools/json-format/
├── index.tsx       # 默认导出工具组件
├── meta.ts         # 工具元信息
├── logic.ts        # 纯函数（可选）
└── examples.ts     # 示例数据（可选）
```

**meta.ts**

```ts
import { Braces } from 'lucide-react'
import type { ToolMeta } from '@/stores/tools'

export const meta: ToolMeta = {
  id: 'json-format',
  path: '/tools/json-format',
  title: 'JSON 格式化',
  description: '格式化、压缩、转义 JSON，支持对比模式',
  icon: Braces,
  category: 'data',          // 见 §5.3
  order: 10,                 // 同类内排序
  defaultVisible: true,
}
```

**注册**：在 `frontend/src/tools/registry.ts` 顶部加 `import` + 数组里加一行。菜单、路由、命令面板自动生效。

**后端（可选）**：在 `backend/tools/jsontool/jsontool.go` 写处理器，然后在 `app.go` 暴露：

```go
func (a *App) JSONFormat(input string) (string, error) {
    return jsontool.Format(input)
}
```

修改 Go 方法后跑 `wails generate module` 重新生成前端绑定。

### 5.2 工具页统一壳

**必须**用 `ToolShell`，不要自己写 header：

```tsx
import { ToolShell } from '@/components/tool/ToolShell'

export default function JSONFormatTool() {
  return (
    <ToolShell
      title="JSON 格式化"
      actions={<>...右侧操作按钮...</>}
      onClear={() => ...}
      onLoadExample={() => ...}
    >
      {/* 内容区 */}
    </ToolShell>
  )
}
```

`ToolShell` 统一处理：标题栏、清空按钮、示例按钮、错误提示位、快捷键路由。

### 5.3 工具分类

`stores/tools.ts` 中 `ToolCategory` 枚举与 `CATEGORY_LABELS`：

| category | 中文 | 示例 |
| --- | --- | --- |
| `forensic` | 取证 | 移动取证、App 搜索 |
| `data` | 数据处理 | JSON 编辑器、Plist、JSON→Go、XML、MMKV |
| `ai` | AI 工具 | AI Chat、翻译、Provider 切换、Claude/Codex 洞察、AI 监控 |
| `codec` | 编解码 | Base64、URL、Unicode、进制 |
| `crypto` | 加解密 | Hash、Crypto Lab、Charles Key、JWT |
| `time` | 时间 | 时间戳、Cron |
| `text` | 文本 | 文本对比、正则、Protobuf |
| `network` | 网络 | HTTP 测试、网络工具集、cURL 转换 |
| `gen` | 生成 | UUID、QR 码、颜色 |
| `dev` | 开发 | Hex、环境扫描 |
| `system` | 系统 | 剪贴板 |

---

## 6. AI 工具架构（重点）

AI Chat 与翻译共用 `backend/tools/aichat`，是项目里最复杂的模块。

### 6.1 多协议适配

四套上游协议，文件按协议拆分：

| 文件 | 协议 |
| --- | --- |
| `openai.go` | OpenAI Chat Completions + Responses（含所有 OpenAI 兼容代理） |
| `anthropic.go` | Anthropic Messages |
| `gemini.go` | Google Generative Language |
| `chat.go` | 编排层：选协议、收集流、持久化 |

每个 `stream*` 函数接受统一的 `streamCallbacks`（`onText / onThinking / onImage / onUsage / onDone / onError`），调用方不感知协议差异。

### 6.2 流式与事件

后端通过 Wails 事件向前端推流：

```
ai-chat:text:<convID>       文本 delta
ai-chat:thinking:<convID>   思考过程 delta
ai-chat:image:<convID>      图片（部分模型支持）
ai-chat:usage:<convID>      token 用量（流末尾）
ai-chat:done:<convID>       结束
ai-chat:error:<convID>      错误（用户取消会路由到 done，不是 error）
```

翻译工具用相同模式但前缀是 `translate:`。

### 6.3 多模态内容构建

四套协议各自一个 `build*Messages` / `build*Input` / `build*Contents`，把统一的前端 `Message`（带 `images` / `files`）翻成上游能消化的格式。差异点：

- OpenAI Responses：`input_text` / `input_image` / `input_file`
- OpenAI Chat Completions：`content` 数组里 `image_url` / `text`
- Anthropic：`content` 数组里 `image.source.base64` / `document.source.base64`
- Gemini：`parts[].inlineData`

`file.go` 处理两件事：

1. **文本类文件**统一在前端解析（mammoth / xlsx / JSZip + `<a:t>` 正则），扔给所有协议都没问题
2. **PDF**：原生支持 PDF 的协议（Anthropic、Gemini）走 base64；纯 OpenAI 兼容代理走后端 `ledongthuc/pdf` 提取文本后塞进 prompt

### 6.4 用量记账

每次 AI 调用末尾的 `usage` 事件触发 `appendUsageRecord`，以 JSONL 追加写入用户数据目录。`ListAIUsage` 给前端用量页提供原始记录，前端聚合出今日/本月、KPI、堆叠柱、模型/供应商分布。

### 6.5 凭据存储

`Provider.APIKey` 永远只在后端访问，调用前从 keyring 取出，前端只持有"是否已配置"和脱敏展示值。

---

## 7. UI / UX 规范

### 7.1 视觉

- 配色用 `globals.css` 里的 CSS 变量（`--background` / `--foreground` / `--border` / `--info` / `--success` / `--destructive` ...），**不要写硬编码颜色**
- 双主题（浅色 + 多套深色变体）通过根元素的 class 切换
- 边框统一 `border-border`，间距偏向 16-20px 留白
- 图标统一 `lucide-react`，size 默认 `h-3.5 w-3.5` 或 `h-4 w-4`

### 7.2 工具页布局

```
┌────────────────────────────────────────────────┐
│ ToolShell Header (h-12)                        │
├────────────────────────────────────────────────┤
│ 内容区（默认 padding 16px）                     │
│   通常左右两栏（输入 / 输出）                    │
└────────────────────────────────────────────────┘
```

- **自动处理**：输入变化即触发，不放显式"转换"按钮（除非耗时操作）
- **错误展示**：输入错误时在输出区标红，不弹窗
- 全局快捷键：`Ctrl/Cmd+K` 命令面板、`Ctrl/Cmd+,` Profile

### 7.3 侧边栏

- 默认展开 220px，可折叠到 56px（仅图标）
- 工具按 category 分组、按 `order` 排序
- 拖拽排序与隐藏状态持久化在 `stores/tools.ts`

---

## 8. 状态管理约定

| 范围 | 工具 |
| --- | --- |
| 工具内部短暂状态（textarea 当前值） | `useState` |
| 跨页保留（用户上次输入） | Zustand store + `persist` |
| 全局偏好（主题、侧栏、工具顺序/可见） | `stores/layout.ts` + `stores/tools.ts` |
| AI Provider / API Key（敏感） | 后端 keyring，前端只读元数据 |

Store 命名：`use<Domain>Store`，文件名 kebab-case 或 domain-case，按主题切片。带 persist 的 store 必须写 `version` + `migrate`，方便后续字段调整。

---

## 9. 前后端通信约定

- 后端方法挂在 `App` 结构体上，命名 `<动词><名词>`：`SendAIChat` / `JSONFormat` / `CheckForensic`
- 错误统一 `(result, error)` 返回，前端 `try/catch`
- 流式输出走 Wails 事件，不走返回值；事件名前缀按工具走（`ai-chat:` / `translate:` / `clipboard:` ...）
- 后端**不做 UI 提示**，只返回数据 / 错误，文案由前端组装

---

## 10. 构建与发布

```bash
wails dev                         # 开发模式（前端热重载）
wails build                       # 当前平台
wails build -platform windows/amd64
wails build -platform darwin/universal
```

产物在 `build/bin/`。版本号写在 `wails.json` 的 `info.productVersion`。

发布流程：

1. 改 `wails.json` 版本
2. 写 changelog（写到 `build/manifest.json` 或自动更新接口的元数据）
3. `wails build` → 上传到对象存储 / Release
4. 客户端 `backend/updater` 拉到新版后下载 + 安装重启

---

## 11. 提交与协作

- 提交格式：`<type>(<scope>): <message>`
  - type：`feat` / `fix` / `refactor` / `docs` / `chore` / `style`
  - scope：工具或模块名（`aichat` / `translate` / `profile` / `updater`）
- 一个工具新增建议**单独一个 commit**，便于 cherry-pick / 回溯
- 提交描述写"动机"，不只是"做了什么"

---

## 12. 安全与隐私

- AI Provider Key、七麦 PHPSESSID 等敏感凭据**只走 keyring**，绝不落 localStorage / 配置文件明文
- 用户文档（PDF / Office / 代码片段）作为附件发给 AI 的瞬间会通过 HTTPS 出本地，必须在 UI 里显式化（带文件卡片 + 可移除）
- AI 用量 JSONL 只在本地，不上报
- Claude / Codex 洞察工具读取 `~/.claude` / `~/.codex` 时只读、不写、不传

---

## 13. 已知坑位与设计取舍

| 项 | 备注 |
| --- | --- |
| `tool_forge` 模块名不带 `github.com/` 前缀 | 历史原因；改名牵动所有 import，开源后保留现状 |
| OpenAI 兼容代理千差万别 | 经常出现 `delta.content:""` / 不返回 usage / 思考标签写法各异；`openai.go` 的 `sniffImagesFromPayload` 与 `recentPayloads` 兜底就是这么来的 |
| Wails 多返回值 | 部分前端绑定会把单字符串当对象返回，`pickFirst/pickSecond` 兼容多形态 |
| Windows emoji 国旗渲染失败 | 翻译工具改用 `country-flag-icons` SVG + `React.lazy` |
| context.Canceled 不弹错 | 用户主动停流时 `chat.go` 把 error 路由成 done，不弹"读取流失败" |
