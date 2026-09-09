package aichat

import (
	"strings"
	"testing"
)

func sampleConv() *Conversation {
	return &Conversation{
		Title:     "Go 泛型的使用",
		ModelID:   "gpt-5-mini",
		System:    "你是一个 Go 专家。\n回答要简短。",
		CreatedAt: 1757000000000,
		UpdatedAt: 1757000600000,
		Messages: []Message{
			{Role: RoleUser, Content: "泛型怎么用", CreatedAt: 1757000000000},
			{
				Role:       RoleAssistant,
				Content:    "用方括号声明类型参数。",
				Model:      "gpt-5-mini",
				Thinking:   []ThinkingBlock{{Text: "先想想怎么讲清楚"}},
				Citations:  []Citation{{URL: "https://go.dev/doc", Title: "Go 文档"}},
				Searches:   []SearchQuery{{Query: "go generics"}},
				ToolCalls:  []ToolCall{{Name: "web_fetch", Arguments: `{"url":"x"}`, Result: "内容"}},
				Usage:      &Usage{InputTokens: 120, OutputTokens: 45},
				DurationMs: 2300,
				CreatedAt:  1757000060000,
			},
			{Role: RoleClear},
			{Role: RoleTool, ToolCalls: []ToolCall{{Name: "web_fetch", Result: "内容"}}},
		},
	}
}

func TestRenderMarkdownDefault(t *testing.T) {
	md := RenderMarkdown(sampleConv(), "OpenAI", DefaultExportOptions())

	for _, want := range []string{
		"# Go 泛型的使用",
		"**模型** `gpt-5-mini` · OpenAI",
		"## 系统提示",
		"> 你是一个 Go 专家。",
		"## 用户",
		"泛型怎么用",
		"## 助手",
		"用方括号声明类型参数。",
		"**引用来源**",
		"1. [Go 文档](https://go.dev/doc)",
		"↑120 ↓45",
		"2.3s",
		"—— 上下文已清除 ——",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("默认导出里应该有 %q\n---\n%s", want, md)
		}
	}
	// 默认不带思考和工具调用:它们通常比正文还长,存档时是噪音
	for _, unwanted := range []string{"思考过程", "先想想怎么讲清楚", "工具调用", "web_fetch"} {
		if strings.Contains(md, unwanted) {
			t.Errorf("默认导出不该出现 %q", unwanted)
		}
	}
	// RoleTool 消息不单独成段,否则同一次工具调用会被写两遍
	if strings.Count(md, "## 助手") != 1 {
		t.Errorf("助手小节应该只有一个,得到 %d 个", strings.Count(md, "## 助手"))
	}
}

func TestRenderMarkdownFull(t *testing.T) {
	opt := DefaultExportOptions()
	opt.IncludeThinking = true
	opt.IncludeTools = true
	md := RenderMarkdown(sampleConv(), "OpenAI", opt)

	for _, want := range []string{
		"<summary>思考过程</summary>",
		"> 先想想怎么讲清楚",
		"<summary>工具调用 · web_fetch</summary>",
		`{"url":"x"}`,
		"*联网检索: `go generics`*",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("全量导出里应该有 %q\n---\n%s", want, md)
		}
	}
}

func TestRenderMarkdownImages(t *testing.T) {
	c := &Conversation{Title: "图", Messages: []Message{
		{Role: RoleAssistant, Images: []ImageBlock{{MimeType: "image/png", Data: "AAAA"}}},
	}}

	opt := DefaultExportOptions()
	if md := RenderMarkdown(c, "", opt); !strings.Contains(md, "data:image/png;base64,AAAA") {
		t.Errorf("开启内嵌时图片应该是 data URI:\n%s", md)
	}
	opt.EmbedImages = false
	md := RenderMarkdown(c, "", opt)
	if strings.Contains(md, "AAAA") {
		t.Errorf("关闭内嵌后不该出现 base64 数据:\n%s", md)
	}
	if !strings.Contains(md, "导出时未内嵌") {
		t.Errorf("关闭内嵌后应留一行占位说明:\n%s", md)
	}
}

func TestExportFilename(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Go 泛型的使用", "Go 泛型的使用"},
		{"a/b\\c:d*e?f\"g<h>i|j", "a_b_c_d_e_f_g_h_i_j"}, // Windows 全套非法字符
		{"结尾有点.", "结尾有点"},                                // 结尾的点会被 Windows 吃掉
		{"   ", "AI 会话"},
		{"", "AI 会话"},
	}
	for _, c := range cases {
		if got := ExportFilename(c.in); got != c.want {
			t.Errorf("ExportFilename(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
	if r := []rune(ExportFilename(strings.Repeat("长", 100))); len(r) != 60 {
		t.Errorf("超长标题应截到 60 字,得到 %d", len(r))
	}
}
