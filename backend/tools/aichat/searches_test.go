package aichat

import "testing"

// Responses 把内置联网搜索当成一个 output item 推。检索词在 added 帧可能还没填,
// 所以两帧都得收 —— 只收其中一帧,某些版本下界面上就永远看不到搜了什么。
func TestParseOpenAIResponsesSearches(t *testing.T) {
	added := `{"type":"response.output_item.added","item":{"id":"ws_1","type":"web_search_call","status":"in_progress","action":{"type":"search","query":"go 1.25 release notes"}}}`
	got := parseOpenAIResponsesSearches(added)
	if len(got) != 1 || got[0].Query != "go 1.25 release notes" || got[0].Status != "running" {
		t.Fatalf("added 帧解析结果不对: %+v", got)
	}

	done := `{"type":"response.output_item.done","item":{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"search","query":"go 1.25 release notes"}}}`
	got = parseOpenAIResponsesSearches(done)
	if len(got) != 1 || got[0].Status != "done" {
		t.Fatalf("done 帧应标记为完成: %+v", got)
	}
}

func TestParseOpenAIResponsesSearchesIgnoresOtherFrames(t *testing.T) {
	for _, payload := range []string{
		`{"type":"response.output_text.delta","delta":"你好"}`,
		// 别的内置工具也是 output item,别把它们当搜索
		`{"type":"response.output_item.added","item":{"type":"function_call","name":"get_current_time"}}`,
		// 检索词为空时不推,免得界面上出现一行空的"正在搜索"
		`{"type":"response.output_item.added","item":{"type":"web_search_call","action":{"query":"  "}}}`,
		`not json`,
	} {
		if got := parseOpenAIResponsesSearches(payload); len(got) != 0 {
			t.Fatalf("不该解析出检索词: %s → %+v", payload, got)
		}
	}
}

func TestParseGeminiSearches(t *testing.T) {
	payload := `{"candidates":[{"groundingMetadata":{"webSearchQueries":["golang 泛型","golang generics 2026"," "]}}]}`
	got := parseGeminiSearches(payload)
	if len(got) != 2 {
		t.Fatalf("期望 2 个非空检索词,得到 %+v", got)
	}
	// Gemini 是事后给的,拿到时检索早就结束了
	for _, q := range got {
		if q.Status != "done" {
			t.Fatalf("Gemini 的检索词应直接标为完成: %+v", q)
		}
	}
}

func TestParseAnthropicSearchQuery(t *testing.T) {
	if got := parseAnthropicSearchQuery(`{"query":"  claude web search  "}`); got != "claude web search" {
		t.Fatalf("检索词没去掉两侧空白: %q", got)
	}
	// 参数是分片拼起来的,模型被中断时可能拼不完整 —— 解不出来当没有
	for _, args := range []string{"", `{"query":`, `{"other":1}`, "null"} {
		if got := parseAnthropicSearchQuery(args); got != "" {
			t.Fatalf("残缺参数 %q 不该解析出检索词 %q", args, got)
		}
	}
}
