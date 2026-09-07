package aichat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// 参数是分片到达的:第一帧带 id+name,后续帧只有一小段 arguments,得按 index 拼回去
func TestChatToolCallsAreReassembledFromFragments(t *testing.T) {
	acc := newToolCallAccumulator()
	for _, payload := range []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_current_time","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"time"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"zone\":\"UTC\"}"}}]}}]}`,
	} {
		collectChatToolCalls(payload, acc)
	}
	calls := acc.done()
	if len(calls) != 1 {
		t.Fatalf("应该拼成一条调用: %#v", calls)
	}
	if calls[0].ID != "call_1" || calls[0].Name != "get_current_time" {
		t.Errorf("id/name 不对: %#v", calls[0])
	}
	if calls[0].Arguments != `{"timezone":"UTC"}` {
		t.Errorf("参数没拼完整: %q", calls[0].Arguments)
	}
}

// 同一帧里可能有多个调用,顺序要保持
func TestChatToolCallsKeepOrder(t *testing.T) {
	acc := newToolCallAccumulator()
	collectChatToolCalls(
		`{"choices":[{"delta":{"tool_calls":[
			{"index":1,"id":"b","function":{"name":"second","arguments":"{}"}},
			{"index":0,"id":"a","function":{"name":"first","arguments":"{}"}}]}}]}`, acc)
	calls := acc.done()
	if len(calls) != 2 || calls[0].Name != "second" || calls[1].Name != "first" {
		t.Errorf("应按首次出现顺序: %#v", calls)
	}
}

// 只收到参数分片、没收到声明帧的残缺项要丢掉,发回去只会让请求被拒
func TestIncompleteToolCallDropped(t *testing.T) {
	acc := newToolCallAccumulator()
	collectChatToolCalls(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]}}]}`, acc)
	if calls := acc.done(); len(calls) != 0 {
		t.Errorf("没有名字的调用应该被丢掉: %#v", calls)
	}
}

func TestResponsesToolCallsAreReassembled(t *testing.T) {
	acc := newToolCallAccumulator()
	for _, payload := range []string{
		`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_x","name":"get_current_time"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"timezone\""}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":":\"UTC\"}"}`,
	} {
		collectResponsesToolCalls(payload, acc)
	}
	calls := acc.done()
	if len(calls) != 1 || calls[0].ID != "call_x" {
		t.Fatalf("got %#v", calls)
	}
	if calls[0].Arguments != `{"timezone":"UTC"}` {
		t.Errorf("参数没拼完整: %q", calls[0].Arguments)
	}
}

// Gemini 不分片,一个 part 就是一次完整调用;它也不给调用 ID,靠函数名配对
func TestGeminiToolCallParsedWhole(t *testing.T) {
	payload := `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_current_time","args":{"timezone":"UTC"}}}]}}]}`
	calls := parseGeminiToolCalls(payload)
	if len(calls) != 1 {
		t.Fatalf("got %#v", calls)
	}
	if calls[0].Name != "get_current_time" || calls[0].ID != "get_current_time" {
		t.Errorf("Gemini 用函数名当调用 ID: %#v", calls[0])
	}
	if !strings.Contains(calls[0].Arguments, "UTC") {
		t.Errorf("参数丢了: %q", calls[0].Arguments)
	}
	if got := parseGeminiToolCalls(`{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`); len(got) != 0 {
		t.Errorf("普通文本不该被当成工具调用: %#v", got)
	}
}

// 工具失败不该中断整批,错误信息本身是给模型的输入
func TestExecuteToolCallsReportsErrorsInline(t *testing.T) {
	got := executeToolCalls(context.Background(), []ToolCall{
		{ID: "1", Name: "get_current_time", Arguments: `{"timezone":"UTC"}`},
		{ID: "2", Name: "不存在的工具"},
		{ID: "3", Name: "get_current_time", Arguments: `{"timezone":"Mars/Olympus"}`},
	})
	if len(got) != 3 {
		t.Fatalf("每条调用都要有结果: %#v", got)
	}
	if got[0].Error != "" || got[0].Result == "" {
		t.Errorf("第一条应该成功: %#v", got[0])
	}
	if !strings.Contains(got[1].Error, "没有名为") {
		t.Errorf("未知工具要报出来: %#v", got[1])
	}
	if !strings.Contains(got[2].Error, "时区") {
		t.Errorf("坏参数要报出来: %#v", got[2])
	}
	// 失败的那两条也要有内容回传给模型,让它有机会换个参数重试
	for _, c := range got[1:] {
		if !strings.Contains(toolOutput(c), "失败") {
			t.Errorf("失败结果也要能回传: %q", toolOutput(c))
		}
	}
}

func TestGetCurrentTimeAcceptsMissingArgs(t *testing.T) {
	for _, args := range []string{"", "{}", `{"timezone":""}`, "不是 JSON"} {
		got, err := getCurrentTime(context.Background(), args)
		if err != nil {
			t.Errorf("args=%q 不该出错: %v", args, err)
		}
		if !strings.Contains(got, time.Now().Format("2006-01-02")) {
			t.Errorf("args=%q 结果里应该有今天的日期: %q", args, got)
		}
	}
	if _, err := getCurrentTime(context.Background(), `{"timezone":"Mars/Olympus"}`); err == nil {
		t.Error("坏时区应该报错")
	}
}

// 工具声明顺序必须稳定,否则每次请求 tools 顺序都不同,白白打断供应商侧的提示词缓存
func TestToolDeclOrderIsStable(t *testing.T) {
	a, _ := json.Marshal(openAIToolDecls(false))
	b, _ := json.Marshal(openAIToolDecls(false))
	if string(a) != string(b) {
		t.Error("两次声明应该完全一致")
	}
}

func TestToolDeclShapesPerProtocol(t *testing.T) {
	chat := openAIToolDecls(false)[0].(map[string]any)
	if _, ok := chat["function"]; !ok {
		t.Errorf("chat-completions 要嵌一层 function: %#v", chat)
	}
	resp := openAIToolDecls(true)[0].(map[string]any)
	if _, ok := resp["function"]; ok {
		t.Errorf("Responses 是平的,不该有 function: %#v", resp)
	}
	if resp["name"] == nil {
		t.Errorf("Responses 的 name 应该在顶层: %#v", resp)
	}
	ant := anthropicToolDecls()[0].(map[string]any)
	if _, ok := ant["input_schema"]; !ok {
		t.Errorf("Anthropic 用 input_schema: %#v", ant)
	}
	gem := geminiToolDecls()[0].(map[string]any)
	if _, ok := gem["functionDeclarations"]; !ok {
		t.Errorf("Gemini 包在 functionDeclarations 里: %#v", gem)
	}
}

// 回传形状:各家对"调用"和"结果"的要求完全不同
func TestToolResultShapesPerProtocol(t *testing.T) {
	call := ToolCall{ID: "call_1", Name: "get_current_time", Arguments: `{"timezone":"UTC"}`, Result: "2026-09-07"}
	asst := Message{Role: RoleAssistant, Content: "让我查一下", ToolCalls: []ToolCall{call}}
	result := Message{Role: RoleTool, ToolCalls: []ToolCall{call}}

	// Chat Completions:结果是独立的 role=tool 消息
	got := buildOpenAIChatToolMessages(result)
	if len(got) != 1 || got[0]["role"] != "tool" || got[0]["tool_call_id"] != "call_1" {
		t.Errorf("chat 结果形状不对: %#v", got)
	}

	// Responses:调用和结果都是平铺的 input 项
	items := buildOpenAIResponsesToolItems(asst)
	if items[len(items)-1]["type"] != "function_call" {
		t.Errorf("Responses 调用形状不对: %#v", items)
	}
	if out := buildOpenAIResponsesToolItems(result); out[0]["type"] != "function_call_output" {
		t.Errorf("Responses 结果形状不对: %#v", out)
	}

	// Anthropic:input 必须是对象而不是字符串
	blocks := buildAnthropicToolBlocks(asst)
	last := blocks[len(blocks)-1]
	if last["type"] != "tool_use" {
		t.Fatalf("Anthropic 调用形状不对: %#v", blocks)
	}
	if _, ok := last["input"].(map[string]any); !ok {
		t.Errorf("input 必须是对象: %#v", last["input"])
	}
	if rb := buildAnthropicToolBlocks(result); rb[0]["type"] != "tool_result" || rb[0]["tool_use_id"] != "call_1" {
		t.Errorf("Anthropic 结果形状不对: %#v", rb)
	}

	// Gemini:结果的 response 必须是对象
	parts := buildGeminiToolParts(result)
	fr, ok := parts[0]["functionResponse"].(map[string]any)
	if !ok {
		t.Fatalf("Gemini 结果形状不对: %#v", parts)
	}
	if _, ok := fr["response"].(map[string]any); !ok {
		t.Errorf("response 必须是对象: %#v", fr["response"])
	}
}

// 参数解不动时给空对象,别让整个请求因为格式不合法被拒
func TestDecodeToolArgsFallsBackToEmptyObject(t *testing.T) {
	for _, args := range []string{"", "不是 JSON", "[1,2,3]", "null"} {
		got := decodeToolArgs(args)
		if got == nil {
			t.Errorf("args=%q 应该给空对象而不是 nil", args)
		}
		if len(got) != 0 {
			t.Errorf("args=%q 不该解出内容: %#v", args, got)
		}
	}
	if got := decodeToolArgs(`{"a":1}`); got["a"] != float64(1) {
		t.Errorf("正常参数要解出来: %#v", got)
	}
}

// 上下文截断可能把「调用」和「结果」拆散,落单的结果会让整个请求被拒
func TestContextMessagesDropsOrphanToolResult(t *testing.T) {
	conv := Conversation{
		ContextCount: 2,
		Messages: []Message{
			{Role: RoleUser, Content: "现在几点"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "1", Name: "get_current_time"}}},
			{Role: RoleTool, ToolCalls: []ToolCall{{ID: "1", Name: "get_current_time", Result: "x"}}},
		},
	}
	// 只留最后 2 条时,调用和结果刚好都在
	got := contextMessages(conv)
	if len(got) != 2 || got[0].Role != RoleAssistant {
		t.Fatalf("got %#v", got)
	}

	conv.ContextCount = 1 // 只留结果,调用被切掉了
	if got := contextMessages(conv); len(got) != 0 {
		t.Errorf("落单的工具结果应该被丢掉: %#v", got)
	}
}

// 无参工具模型经常什么都不给,而各家 schema 校验要求这里是合法 JSON
func TestEmptyObjectIfBlank(t *testing.T) {
	if emptyObjectIfBlank("") != "{}" {
		t.Error("空参数要补成 {}")
	}
	if emptyObjectIfBlank(`{"a":1}`) != `{"a":1}` {
		t.Error("非空参数要原样透传")
	}
}
