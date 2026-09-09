package aichat

import (
	"encoding/json"
	"strings"
	"testing"
)

// Go 的 nil 切片 json.Marshal 出来是 null 而不是 []。前端拿到 null
// 再去读 .length / .map,就是一次白屏 —— 而且只在"恰好没有元素"那条路上发生,
// 平时测不出来。跨会话搜索就这么崩过一次:搜到一条只有标题命中的会话,
// hits 是 nil → "Cannot read properties of null"。
//
// 这个测试盯住那些"前端一定当数组用"的字段:序列化出来必须是 []。
func TestNoNullArraysInAPIShapes(t *testing.T) {
	t.Run("搜索结果只命中标题时 hits 是空数组", func(t *testing.T) {
		c := &Conversation{ID: "c1", Title: "逆子AI 的会话", Messages: []Message{
			{ID: "m1", Role: RoleUser, Content: "完全无关的正文"},
		}}
		r := searchOne(c, "逆子")
		if r == nil {
			t.Fatal("标题命中就该有结果")
		}
		mustNoNullField(t, r, "hits")
	})

	t.Run("工具清单为空时 tools 是空数组", func(t *testing.T) {
		// ToolsView 至少带内置工具,但形状约定要在这儿钉住:
		// 以后有人把内置工具去掉,这条会立刻红
		mustNoNullField(t, ToolsView(), "tools")
	})
}

// mustNoNullField 序列化后断言某个字段不是 null
func mustNoNullField(t *testing.T, v any, field string) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if strings.Contains(string(b), `"`+field+`":null`) {
		t.Fatalf("%s 序列化成了 null,前端 .length 会当场崩:\n%s", field, b)
	}
}
