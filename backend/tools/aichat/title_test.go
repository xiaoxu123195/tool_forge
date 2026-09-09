package aichat

import "testing"

// 每条用例都对应一种模型真实干过的事,不是凭空想的边界
func TestSanitizeTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Go 泛型的使用", "Go 泛型的使用"},
		{"  Go 泛型的使用  ", "Go 泛型的使用"},
		{`"Go 泛型的使用"`, "Go 泛型的使用"},   // 套了英文引号
		{"“Go 泛型的使用”", "Go 泛型的使用"},   // 中文引号
		{"《Go 泛型的使用》", "Go 泛型的使用"},   // 书名号
		{"**Go 泛型的使用**", "Go 泛型的使用"}, // 加粗
		{"标题:Go 泛型的使用", "Go 泛型的使用"},  // 中文冒号前缀
		{"标题: Go 泛型的使用", "Go 泛型的使用"}, // 英文冒号前缀
		{"Title: Go generics", "Go generics"},
		{"Go 泛型的使用。", "Go 泛型的使用"},            // 结尾句号
		{"好的,标题如下:\n\nGo 泛型的使用", "Go 泛型的使用"}, // 先寒暄再给结论 → 取最后一行
		{"Go 和 Rust:选哪个", "Go 和 Rust:选哪个"},   // 冒号在后面,是标题的一部分,不能砍
		{"", ""},
		{"   \n  \n ", ""},
		{`"" `, ""}, // 只剩装饰字符,等于没给
	}
	for _, c := range cases {
		if got := sanitizeTitle(c.in); got != c.want {
			t.Errorf("sanitizeTitle(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeTitleTooLong(t *testing.T) {
	// 模型答非所问写了一整段:不能整段塞进侧边栏
	long := "这是一段非常长的回答它完全没有听从只输出标题的指令而是把整段解释都写了出来"
	got := sanitizeTitle(long)
	if r := []rune(got); len(r) != 25 { // 24 个字 + 省略号
		t.Fatalf("超长标题应截到 24 字加省略号,得到 %d 个字符: %q", len(r), got)
	}
}

func TestFirstExchange(t *testing.T) {
	// 一轮问答,中间夹着工具调用产生的 tool 消息和空的 assistant 占位。
	// 按消息总数判断会认成"好几轮",所以这里按 user 条数算
	c := &Conversation{Messages: []Message{
		{Role: RoleUser, Content: "帮我查一下天气"},
		{Role: RoleAssistant, Content: ""},
		{Role: RoleTool},
		{Role: RoleAssistant, Content: "今天晴"},
	}}
	u, a, ok := firstExchange(c)
	if !ok || u != "帮我查一下天气" || a != "今天晴" {
		t.Fatalf("单轮识别失败: user=%q assistant=%q ok=%v", u, a, ok)
	}

	// 两轮了就不该再起标题 —— 聊到一半列表里的标题自己变掉很吓人
	c.Messages = append(c.Messages, Message{Role: RoleUser, Content: "那明天呢"})
	if _, _, ok := firstExchange(c); ok {
		t.Fatal("已经第二轮了,不该再触发自动起标题")
	}

	// 回答是空的(流刚断)不该拿去起标题
	if _, _, ok := firstExchange(&Conversation{Messages: []Message{
		{Role: RoleUser, Content: "在吗"},
		{Role: RoleAssistant, Content: ""},
	}}); ok {
		t.Fatal("回答为空时不该起标题")
	}
}
