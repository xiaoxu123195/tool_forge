package aichat

import (
	"fmt"
	"strings"
	"time"
)

// ExportOptions 导出 Markdown 时带哪些东西。
//
// 全做成开关而不是一股脑全导,是因为两种用途要的东西完全相反:
// 存档给人看的只要问答正文;排查模型行为的则恰恰要看思考和工具调用。
type ExportOptions struct {
	IncludeSystem    bool `json:"includeSystem"`
	IncludeThinking  bool `json:"includeThinking"`
	IncludeTools     bool `json:"includeTools"`
	IncludeCitations bool `json:"includeCitations"`
	IncludeUsage     bool `json:"includeUsage"`
	// EmbedImages 把图片以 data: URI 内联进去,导出的文件自带图、发给别人不会瞎。
	// 关掉只留一行占位 —— 一张截图就能让文件涨几百 KB
	EmbedImages bool `json:"embedImages"`
}

// DefaultExportOptions 对话框打开时的初始勾选:正文 + 引用 + 图片。
// 思考和工具调用默认不带 —— 它们通常比正文还长,存档时是噪音
func DefaultExportOptions() ExportOptions {
	return ExportOptions{
		IncludeSystem:    true,
		IncludeCitations: true,
		IncludeUsage:     true,
		EmbedImages:      true,
	}
}

// toolResultLimit 单个工具调用的参数 / 结果在 Markdown 里最多写多少字符。
// 工具结果动辄几万字(一次网页抓取就够),原样铺进去会把整份文档淹掉
const toolResultLimit = 4000

// RenderMarkdown 把一条会话渲染成 Markdown。providerName 只用于抬头那行,可以为空。
//
// 输出面向的是 Typora / Obsidian / GitHub 这类常见渲染器:标题用 ##,
// 折叠块用 <details>(这三家都支持),不用任何某一家特有的语法。
func RenderMarkdown(c *Conversation, providerName string, opt ExportOptions) string {
	var b strings.Builder
	title := strings.TrimSpace(c.Title)
	if title == "" {
		title = "未命名会话"
	}
	fmt.Fprintf(&b, "# %s\n\n", title)

	// 抬头。每行一个 > 是为了让它们渲染成同一个引用块里的多行
	meta := []string{}
	if c.ModelID != "" {
		m := "**模型** `" + c.ModelID + "`"
		if providerName != "" {
			m += " · " + providerName
		}
		meta = append(meta, m)
	}
	if c.CreatedAt > 0 {
		span := "**时间** " + fmtTime(c.CreatedAt)
		if c.UpdatedAt > c.CreatedAt {
			span += " — " + fmtTime(c.UpdatedAt)
		}
		meta = append(meta, span)
	}
	meta = append(meta, fmt.Sprintf("**消息** %d 条 · 导出于 %s", countVisible(c), fmtTime(time.Now().UnixMilli())))
	for _, m := range meta {
		b.WriteString("> " + m + "\n")
	}
	b.WriteString("\n")

	if opt.IncludeSystem && strings.TrimSpace(c.System) != "" {
		b.WriteString("## 系统提示\n\n")
		b.WriteString(blockquote(c.System) + "\n\n")
	}

	for _, m := range c.Messages {
		switch m.Role {
		case RoleClear:
			b.WriteString("---\n\n*—— 上下文已清除 ——*\n\n")
			continue
		case RoleSystem:
			continue // 已经在抬头下面单独写过了
		case RoleTool:
			continue // 工具结果跟着发起调用的那条 assistant 一起渲染,不单独成段
		}
		renderMessage(&b, m, c.ModelID, opt)
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func renderMessage(b *strings.Builder, m Message, fallbackModel string, opt ExportOptions) {
	who := "助手"
	if m.Role == RoleUser {
		who = "用户"
	}
	fmt.Fprintf(b, "## %s\n\n", who)

	// 副标题行:时间 / 模型 / 用量。信息不多,合成一行斜体,不占版面
	sub := []string{}
	if m.CreatedAt > 0 {
		sub = append(sub, fmtTime(m.CreatedAt))
	}
	if m.Role == RoleAssistant {
		model := m.Model
		if model == "" {
			model = fallbackModel
		}
		if model != "" {
			sub = append(sub, "`"+model+"`")
		}
		if opt.IncludeUsage && m.Usage != nil {
			u := *m.Usage
			parts := fmt.Sprintf("↑%d ↓%d", u.InputTokens, u.OutputTokens)
			if u.ReasoningTokens > 0 {
				parts += fmt.Sprintf(" 思考%d", u.ReasoningTokens)
			}
			if u.CachedTokens > 0 {
				parts += fmt.Sprintf(" 缓存%d", u.CachedTokens)
			}
			sub = append(sub, parts)
		}
		if opt.IncludeUsage && m.DurationMs > 0 {
			sub = append(sub, fmt.Sprintf("%.1fs", float64(m.DurationMs)/1000))
		}
	}
	if len(sub) > 0 {
		b.WriteString("*" + strings.Join(sub, " · ") + "*\n\n")
	}

	if opt.IncludeThinking {
		if t := thinkingOf(m); t != "" {
			b.WriteString("<details>\n<summary>思考过程</summary>\n\n")
			b.WriteString(blockquote(t) + "\n\n</details>\n\n")
		}
	}

	if opt.IncludeTools && len(m.ToolCalls) > 0 {
		for _, tc := range m.ToolCalls {
			fmt.Fprintf(b, "<details>\n<summary>工具调用 · %s</summary>\n\n", tc.Name)
			if a := strings.TrimSpace(tc.Arguments); a != "" {
				b.WriteString("参数:\n\n```json\n" + clip(a, toolResultLimit) + "\n```\n\n")
			}
			switch {
			case tc.Error != "":
				b.WriteString("失败:\n\n```\n" + clip(tc.Error, toolResultLimit) + "\n```\n\n")
			case tc.Result != "":
				b.WriteString("结果:\n\n```\n" + clip(tc.Result, toolResultLimit) + "\n```\n\n")
			}
			b.WriteString("</details>\n\n")
		}
	}

	if len(m.Searches) > 0 {
		queries := make([]string, 0, len(m.Searches))
		for _, q := range m.Searches {
			queries = append(queries, "`"+q.Query+"`")
		}
		b.WriteString("*联网检索: " + strings.Join(queries, " ") + "*\n\n")
	}

	if body := strings.TrimSpace(m.Content); body != "" {
		b.WriteString(body + "\n\n")
	}
	if m.Truncated {
		b.WriteString("*(这条回复没有写完)*\n\n")
	}

	for i, img := range m.Images {
		switch {
		case img.URL != "":
			fmt.Fprintf(b, "![图片 %d](%s)\n\n", i+1, img.URL)
		case img.Data != "" && opt.EmbedImages:
			mime := img.MimeType
			if mime == "" {
				mime = "image/png"
			}
			fmt.Fprintf(b, "![图片 %d](data:%s;base64,%s)\n\n", i+1, mime, img.Data)
		case img.Data != "" || img.Ref != "":
			// Ref 也要认:不勾内嵌时图片只有引用、没有 Data,
			// 只判 Data 的话这里什么都不写,导出的文档里图就凭空消失了
			fmt.Fprintf(b, "> [图片 %d · %s,导出时未内嵌]\n\n", i+1, orDefault(img.MimeType, "image"))
		}
	}
	for _, f := range m.Files {
		size := ""
		if f.SizeBytes > 0 {
			size = fmt.Sprintf(" · %s", humanSize(f.SizeBytes))
		}
		fmt.Fprintf(b, "> 📎 附件: %s%s\n\n", f.Name, size)
	}

	if opt.IncludeCitations && len(m.Citations) > 0 {
		b.WriteString("**引用来源**\n\n")
		for i, c := range m.Citations {
			t := strings.TrimSpace(c.Title)
			if t == "" {
				t = c.URL
			}
			fmt.Fprintf(b, "%d. [%s](%s)\n", i+1, t, c.URL)
		}
		b.WriteString("\n")
	}
}

// RenderConversationMarkdown 按 ID 取会话并渲染;供应商名从当前配置里查,查不到就空着
func (s *Service) RenderConversationMarkdown(convID string, opt ExportOptions) (string, error) {
	c, err := loadConversation(convID)
	if err != nil {
		return "", err
	}
	name := ""
	if p, err := s.providerSnapshot(c.ProviderID); err == nil {
		name = p.Name
	}
	// 勾了内嵌图片才回填 —— 不勾的话只写一行占位,把几 MB base64 读进来纯属白费
	if opt.EmbedImages {
		c.Messages = hydrateMessages(c.Messages)
	}
	return RenderMarkdown(c, name, opt), nil
}

// ExportFilename 由会话标题推一个安全的默认文件名(不含扩展名)。
//
// Windows 的非法字符 \ / : * ? " < > | 一个都不能留,否则保存对话框会直接拒绝
func ExportFilename(title string) string {
	t := strings.TrimSpace(title)
	if t == "" {
		return "AI 会话"
	}
	var b strings.Builder
	for _, r := range t {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|', '\n', '\r', '\t':
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	// 结尾的点和空格在 Windows 上会被静默吃掉,顺手削了
	name := strings.TrimRight(b.String(), ". ")
	if name == "" {
		return "AI 会话"
	}
	if r := []rune(name); len(r) > 60 {
		name = string(r[:60])
	}
	return name
}

// ConversationTitle 取会话标题(导出时用来拼默认文件名)
func (s *Service) ConversationTitle(convID string) string {
	c, err := loadConversation(convID)
	if err != nil {
		return ""
	}
	return c.Title
}

// ---- 小工具 ----

// blockquote 把多行文本整体变成引用块
func blockquote(s string) string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")), "\n")
	for i, l := range lines {
		lines[i] = "> " + l
	}
	return strings.Join(lines, "\n")
}

// thinkingOf 把思考块拼成一段可读文本(加密隐藏的块跳过 —— 它本来就不可读)
func thinkingOf(m Message) string {
	var b strings.Builder
	for _, t := range m.Thinking {
		b.WriteString(t.Text)
	}
	return strings.TrimSpace(b.String())
}

func countVisible(c *Conversation) int {
	n := 0
	for _, m := range c.Messages {
		if m.Role == RoleUser || m.Role == RoleAssistant {
			n++
		}
	}
	return n
}

func fmtTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).Format("2006-01-02 15:04")
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func humanSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
