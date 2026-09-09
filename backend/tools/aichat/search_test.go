package aichat

import (
	"strings"
	"testing"
)

func TestSearchOne(t *testing.T) {
	c := &Conversation{
		ID:        "c1",
		Title:     "Go 泛型笔记",
		UpdatedAt: 100,
		Messages: []Message{
			{ID: "m1", Role: RoleUser, Content: "泛型怎么用"},
			{ID: "m2", Role: RoleAssistant, Content: "用方括号声明类型参数。泛型很好用。"},
			{ID: "m3", Role: RoleClear},
			{ID: "m4", Role: RoleSystem, Content: "泛型专家"},
			{ID: "m5", Role: RoleTool, Content: "泛型的搜索结果一大堆"},
			{ID: "m6", Role: RoleAssistant, Thinking: []ThinkingBlock{{Text: "先想想泛型怎么讲"}}},
		},
	}

	r := searchOne(c, "泛型")
	if r == nil {
		t.Fatal("应该有命中")
	}
	if !r.TitleMatch {
		t.Error("标题里有「泛型」,titleMatch 应该为真")
	}
	// clear / system / tool 三种消息不该被搜到:
	// 前两个用户看不见,tool 是几万字的原始结果,搜出来全是噪音
	ids := []string{}
	for _, h := range r.Hits {
		ids = append(ids, h.MessageID)
	}
	want := []string{"m1", "m2", "m6"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("命中的消息应该是 %v, 得到 %v", want, ids)
	}
	// 思考内容也要搜到 —— "它当时为什么这么答"往往就藏在那儿
	if r.Hits[2].MessageID != "m6" {
		t.Error("思考内容里的命中没搜到")
	}
	// 一条消息里出现两次只记一次:两行结果指向同一个位置,翻起来只会更累
	if len(r.Hits) != 3 {
		t.Errorf("同一条消息只该记一次,得到 %d 处", len(r.Hits))
	}
}

func TestSearchOneNoMatch(t *testing.T) {
	c := &Conversation{ID: "c1", Title: "无关", Messages: []Message{
		{ID: "m1", Role: RoleUser, Content: "你好"},
	}}
	if searchOne(c, "泛型") != nil {
		t.Fatal("没命中就不该返回结果")
	}
	// 只有标题命中、正文一处都没有 —— 仍然要返回,前端会给一个"打开这条会话"
	if r := searchOne(c, "无关"); r == nil || !r.TitleMatch || len(r.Hits) != 0 {
		t.Fatalf("只命中标题时应返回一个空 hits 的结果: %+v", r)
	}
}

func TestSearchHitsCap(t *testing.T) {
	msgs := make([]Message, 0, 12)
	for i := 0; i < 12; i++ {
		msgs = append(msgs, Message{ID: string(rune('a' + i)), Role: RoleUser, Content: "泛型"})
	}
	r := searchOne(&Conversation{ID: "c", Title: "x", Messages: msgs}, "泛型")
	if len(r.Hits) != searchHitsPerConv {
		t.Fatalf("单条会话最多列 %d 处,得到 %d", searchHitsPerConv, len(r.Hits))
	}
	if r.More != 12-searchHitsPerConv {
		t.Fatalf("剩余处数应为 %d,得到 %d", 12-searchHitsPerConv, r.More)
	}
}

func TestSnippetRuneSafe(t *testing.T) {
	// 中文按字节切会切出半个字符。摘录必须按字符切
	hay := "这是一段很长的中文内容用来测试摘录会不会把汉字拦腰砍断泛型出现在这里后面还有很多字继续往后写"
	idx := strings.Index(hay, "泛型")
	before, match, after := snippet(hay, idx, len("泛型"))
	if match != "泛型" {
		t.Fatalf("命中段错了: %q", match)
	}
	for _, s := range []string{before, match, after} {
		if !isValidUTF8(s) {
			t.Fatalf("切出了非法 UTF-8: %q", s)
		}
	}
	if r := []rune(before); len(r) > searchSnippetPadding {
		t.Fatalf("前文最多 %d 字,得到 %d", searchSnippetPadding, len(r))
	}
	if r := []rune(after); len(r) > searchSnippetPadding {
		t.Fatalf("后文最多 %d 字,得到 %d", searchSnippetPadding, len(r))
	}
}

func TestSnippetAtBoundaries(t *testing.T) {
	// 命中在开头 / 结尾时不能越界
	for _, c := range []struct{ hay, q string }{
		{"泛型开头", "泛型"},
		{"结尾是泛型", "泛型"},
		{"泛型", "泛型"},
	} {
		idx := strings.Index(c.hay, c.q)
		before, match, after := snippet(c.hay, idx, len(c.q))
		if match != c.q {
			t.Errorf("%q: 命中段错了 %q", c.hay, match)
		}
		if before+match+after == "" {
			t.Errorf("%q: 摘录全空", c.hay)
		}
	}
}

func TestIndexFoldCase(t *testing.T) {
	if indexFold("Hello World", "world") < 0 {
		t.Error("应该不分大小写")
	}
	if got := indexFold("Hello", "xyz"); got != -1 {
		t.Errorf("没命中该返回 -1,得到 %d", got)
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}
