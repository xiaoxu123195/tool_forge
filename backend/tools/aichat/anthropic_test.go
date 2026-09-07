package aichat

import "testing"

// cacheMarked 这条消息的最后一个 content 块上有没有缓存断点
func cacheMarked(msg map[string]any) bool {
	blocks, ok := msg["content"].([]map[string]any)
	if !ok || len(blocks) == 0 {
		return false
	}
	_, has := blocks[len(blocks)-1]["cache_control"]
	return has
}

func longConversationBody(t *testing.T) map[string]any {
	t.Helper()
	conv := Conversation{
		ModelID: "claude-sonnet-4-5",
		System:  "你是一个助手",
		Messages: []Message{
			{Role: "user", Content: "第一问"},
			{Role: "assistant", Content: "第一答"},
			{Role: "user", Content: "第二问"},
		},
	}
	spec := InferModelSpec(Provider{Type: TypeAnthropic, BaseURL: "https://api.anthropic.com"}, conv.ModelID)
	return map[string]any{
		"system":   conv.System,
		"messages": buildAnthropicMessages(chatRequest{Conv: conv, Spec: spec}, false),
	}
}

func TestApplyAnthropicCacheMarksStablePrefix(t *testing.T) {
	body := longConversationBody(t)
	applyAnthropicCache(body)

	// system 全程不变,是最稳定的一段
	sys, ok := body["system"].([]map[string]any)
	if !ok || len(sys) != 1 {
		t.Fatalf("system 应该被转成带缓存标记的块数组: %#v", body["system"])
	}
	if _, has := sys[0]["cache_control"]; !has {
		t.Error("system 上应该有缓存断点")
	}
	if sys[0]["text"] != "你是一个助手" {
		t.Errorf("system 文本被改坏了: %#v", sys[0])
	}

	msgs := body["messages"].([]map[string]any)
	// 断点应该落在倒数第二条(上一轮的 assistant 回复),它之前的内容下轮还会原样再发
	if !cacheMarked(msgs[len(msgs)-2]) {
		t.Errorf("倒数第二条消息上应该有缓存断点: %#v", msgs[len(msgs)-2])
	}
	// 最后一条是本轮新内容,标了也没用
	if cacheMarked(msgs[len(msgs)-1]) {
		t.Error("最后一条消息不该标缓存断点")
	}
	// 断点数量不能超过 Anthropic 的 4 个上限
	marked := 0
	for _, m := range msgs {
		if cacheMarked(m) {
			marked++
		}
	}
	if marked+1 > 4 {
		t.Errorf("断点数 %d 超过上限 4", marked+1)
	}
}

// 消息太少的短对话缓存不起来(前缀达不到最小长度),不用白写一次缓存
func TestApplyAnthropicCacheSkipsShortConversation(t *testing.T) {
	conv := Conversation{
		ModelID:  "claude-sonnet-4-5",
		Messages: []Message{{Role: "user", Content: "你好"}},
	}
	spec := InferModelSpec(Provider{Type: TypeAnthropic, BaseURL: "https://api.anthropic.com"}, conv.ModelID)
	body := map[string]any{
		"messages": buildAnthropicMessages(chatRequest{Conv: conv, Spec: spec}, false),
	}
	applyAnthropicCache(body)
	for _, m := range body["messages"].([]map[string]any) {
		if cacheMarked(m) {
			t.Errorf("单轮对话不该打缓存断点: %#v", m)
		}
	}
}

// cache_control 是 Anthropic 专有字段,中转不认会整个请求 400 —— 只对原厂发
func TestPromptCacheOnlyForOfficialHost(t *testing.T) {
	official := InferModelSpec(
		Provider{Type: TypeAnthropic, BaseURL: "https://api.anthropic.com"}, "claude-sonnet-4-5")
	if official.family != familyAnthropic {
		t.Error("原厂域名应该被认成 anthropic 家族(才会发缓存字段)")
	}
	relay := InferModelSpec(
		Provider{Type: TypeAnthropic, BaseURL: "https://claude-relay.example.com"}, "claude-sonnet-4-5")
	if relay.family == familyAnthropic {
		t.Error("中转不该被认成原厂,否则会给它发它不认识的 cache_control")
	}
	// 中转仍然要能正常用思考和联网 —— 那些是端点级能力,跟家族无关
	if !relay.CanTuneReasoning() {
		t.Error("中转上的 Claude 仍应支持思考档位")
	}
	if !relay.Has(CapWebSearch) {
		t.Error("中转上的 Claude 仍应支持内置联网")
	}
}

// 缓存标记不能破坏已有的多模态 content 块
func TestMarkAnthropicCacheKeepsExistingBlocks(t *testing.T) {
	msg := map[string]any{"role": "user", "content": []map[string]any{
		{"type": "image", "source": map[string]any{"type": "base64"}},
		{"type": "text", "text": "看这张图"},
	}}
	markAnthropicCache(msg)
	blocks := msg["content"].([]map[string]any)
	if len(blocks) != 2 {
		t.Fatalf("块数被改了: %#v", blocks)
	}
	if _, has := blocks[0]["cache_control"]; has {
		t.Error("只该标最后一个块")
	}
	if _, has := blocks[1]["cache_control"]; !has {
		t.Error("最后一个块上应该有缓存断点")
	}
}
