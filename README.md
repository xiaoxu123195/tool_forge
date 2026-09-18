<div align="center">
  <img src="docs/images/logo.png" alt="Tool Forge" width="96" height="96" />

  <h1>Tool Forge</h1>

  <p><b>给程序员的一站式桌面工具箱</b> · 离线 · 轻量 · 一致</p>

  <p>
    <a href="https://github.com/xiaoxu123195/tool_forge/releases"><img alt="Release" src="https://img.shields.io/github/v/release/xiaoxu123195/tool_forge?style=flat-square&color=informational" /></a>
    <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-GPL--3.0-green?style=flat-square" /></a>
    <a href="https://github.com/xiaoxu123195/tool_forge/stargazers"><img alt="Stars" src="https://img.shields.io/github/stars/xiaoxu123195/tool_forge?style=flat-square&color=yellow" /></a>
    <img alt="Wails" src="https://img.shields.io/badge/Wails-v2-DF0067?style=flat-square" />
    <img alt="Go" src="https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square&logo=go&logoColor=white" />
    <img alt="React" src="https://img.shields.io/badge/React-18-61DAFB?style=flat-square&logo=react&logoColor=white" />
    <img alt="TypeScript" src="https://img.shields.io/badge/TypeScript-5-3178C6?style=flat-square&logo=typescript&logoColor=white" />
  </p>

  <p>
    <a href="#-特性">特性</a> ·
    <a href="#-工具一览">工具一览</a> ·
    <a href="#-截图">截图</a> ·
    <a href="#-安装">安装</a> ·
    <a href="#-本地开发">开发</a> ·
    <a href="docs/DEVELOPMENT.md">开发文档</a>
  </p>
</div>

---

## ✨ 特性

- 🧰 **40+ 内置工具**：覆盖 AI / 编解码 / 加密 / 取证 / 网络 / 文本生成 / 系统等场景，还在持续增加
- 🔒 **全本地运行**：所有处理都在本机完成，敏感内容不出本地（AI 工具调用的是用户自配的供应商）
- 🤖 **完整的 AI 工作流**：内置 AI Chat（多轮对话、视觉、文件附件、思考折叠）+ AI 翻译，支持 OpenAI / Anthropic / Gemini / OpenAI 兼容 4 套协议
- 📊 **AI 用量看板**：自动记录每次调用的 token / 时长，提供仪表盘、堆叠柱状图、模型/供应商占比
- 🧭 **本机 AI 配置总览**：一处看全 Claude Code / Codex / Gemini CLI / Cline / Continue / Trae 等各家的 MCP、skills 与插件，每条都标着出自哪个文件——同名多处重复、插件启用了却没装、skills 软链指向共享池，这些分开看时发现不了的状态一眼可见
- ⚡ **轻量启动快**：Wails 打包产物 ~20MB，常驻内存 < 150MB，远低于 Electron 同类
- 🎨 **统一的设计语言**：所有工具共用一套 UI 壳，操作习惯零迁移成本
- 🧩 **可扩展架构**：新增一个工具 = 一个独立前端路由 + 可选的 Go 后端处理器，互不干扰
- 🌗 **暗色 / 亮色双主题**，全局快捷键，剪贴板历史，自动更新

## 🧰 工具一览

> 当前内置 **44** 个工具，按 12 个分类组织。

| 分类 | 工具 |
| --- | --- |
| 🤖 **AI 工具** | AI 问答 · 翻译 · Provider 切换 · AI 监控 · Claude 洞察 · Codex 洞察 · LLM 代理日志 · 本机 AI 配置 |
| 🧪 **取证** | 移动取证 · 真机数据浏览器 · SQLite 搜索 · MMKV 解析器 · 包名搜索 |

> 移动取证两个平台的提取都是**内置直连**（Android 走 adb 协议、iOS 走 usbmuxd + SSH），设备上打包成一条流、边收边解，不逐个文件拉。实测 iOS 闲鱼（1598 个文件 / 92 MB）内置引擎比 go-forensic 快三倍多——后者逐文件走 SSH，每个文件一次协议往返。go-forensic 仍可作为备选引擎在取证页「配置」里启用，默认关闭。
| 🗂 **数据处理** | JSON 编辑器 · XML 编辑器 · Plist 解析器 · JSON 转 Go |
| 🔣 **编解码** | Base64 文本 · Base64 图片 · URL 编解码 · Unicode 转义 · Protobuf 编解码 |
| 🔐 **加密哈希** | 加密/解密 · 文件哈希 · JWT 解析 |
| ✏️ **文本** | 文本对比 · 正则表达式 |
| 🌐 **网络** | HTTP 测试 · 网络探测 · 网络环境体检 · cURL 转代码 |
| 🛠 **生成** | UUID 生成 · 二维码生成 · 图片拼接 |
| ⏱ **时间** | 时间戳转换 · Cron 表达式 |
| 💻 **开发辅助** | Hex 编辑器 · 进制转换 · 颜色转换 · Charles 激活码 |
| 👤 **账号管理** | Outlook 邮箱管理 · ChatGPT 凭据转换 |
| 🖥 **系统** | 剪贴板 · 开发环境 |

完整列表与每个工具的能力说明见应用内"工具总览"页。

## 📸 截图

<table>
  <tr>
    <td align="center" width="50%">
      <img src="docs/images/screenshots/01-home.png" alt="主界面" />
      <sub><b>首页 · 36 个工具的可视化网格，可拖拽调整顺序</b></sub>
    </td>
    <td align="center" width="50%">
      <img src="docs/images/screenshots/02-ai-chat.png" alt="AI Chat" />
      <sub><b>AI 问答 · 多供应商 · 文件附件 · 思考折叠 · Markdown 渲染</b></sub>
    </td>
  </tr>
  <tr>
    <td align="center" width="50%">
      <img src="docs/images/screenshots/03-claude-insight.png" alt="Claude 洞察" />
      <sub><b>Claude 洞察 · 本地扫描 ~/.claude，会话/Token/活跃度统计</b></sub>
    </td>
    <td align="center" width="50%">
      <img src="docs/images/screenshots/04-ai-providers.png" alt="AI 配置" />
      <sub><b>AI 配置 · 多供应商 · 凭据加密落 OS Keychain</b></sub>
    </td>
  </tr>
  <tr>
    <td colspan="2" align="center">
      <img src="docs/images/screenshots/05-themes.png" alt="主题与外观" width="60%" />
      <br />
      <sub><b>多套主题 · 浅色 / 深色 / 跟随系统</b></sub>
    </td>
  </tr>
</table>

## 🚀 安装

### 从 Release 下载（推荐）

到 [Releases](https://github.com/xiaoxu123195/tool_forge/releases) 下载对应平台的安装包：

- **Windows**：`Tool-Forge-Setup-x.y.z.exe`（NSIS 安装器，自带自动更新）
- **macOS**：`Tool-Forge-x.y.z.dmg`（Universal，Apple Silicon + Intel）

### 自行构建

见下方 [本地开发](#-本地开发)。

## 🛠 本地开发

### 环境要求

| 组件 | 版本 |
| --- | --- |
| Go | 1.24+ |
| Node.js | 18+ |
| Wails CLI | v2.11+ |

安装 Wails CLI：

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

### 启动开发模式

```bash
# 1. 安装前端依赖
cd frontend && npm install && cd ..

# 2. 安装 Go 依赖
go mod tidy

# 3. 启动（前端 Vite 热重载 + 后端实时绑定）
wails dev
```

### 构建发布产物

```bash
# 当前平台
wails build

# Windows 64-bit
wails build -platform windows/amd64

# macOS Universal
wails build -platform darwin/universal
```

产物在 `build/bin/`。

## 🧱 技术栈

| 层 | 选型 |
| --- | --- |
| 桌面框架 | [Wails v2](https://wails.io) |
| 后端 | Go 1.24 |
| 前端 | React 18 + TypeScript 5 + Vite |
| UI | Tailwind CSS + shadcn/ui 风格组件 |
| 状态 | Zustand（带 persist） |
| 路由 | React Router v6 |
| 编辑器 | CodeMirror 6 |
| 图标 | lucide-react |

选型理由与架构细节见 [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md)。

## 🤝 贡献

欢迎提 Issue / PR。开始前推荐先看 [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md)，里面有：

- 新增工具的"骨架样板"
- UI / 命名规范
- 后端 RPC 暴露方式
- 提交格式

## 🔗 友链

[LINUX DO](https://linux.do/)

## 💬 反馈

- Issue：[github.com/xiaoxu123195/tool_forge/issues](https://github.com/xiaoxu123195/tool_forge/issues)
- 邮箱：cherrytump@gmail.com

## 📄 License

[GPL-3.0-or-later](LICENSE) © 2026 xiaoxu123195

自由使用、修改、分发；**基于本项目改出来的东西再分发时，也要以 GPL-3.0 开源**。
公司内部自己用、不对外分发的，不受此限。

> v0.2.8 及更早的版本按当时的 MIT 协议发布，不受影响；GPL-3.0 从 v0.3.0 起生效。

---

<div align="center">
  <sub>A unified developer toolbox for the Chinese-speaking dev community — built with Wails, Go and React.</sub>
</div>
