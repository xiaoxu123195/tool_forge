package aichat

import (
	"strings"
	"testing"
)

func TestStreamProbeSilentWhenEmitted(t *testing.T) {
	var p streamProbe
	p.record(`{"a":1}`)
	p.mark()
	if err := p.err(emptyReplyReason); err != nil {
		t.Errorf("有产出就不该报错: %v", err)
	}
}

// 流跑完却一个字都没产出:必须报错并附上原始响应,否则用户面对一个空白回复无从下手
func TestStreamProbeDumpsRecentFrames(t *testing.T) {
	var p streamProbe
	for i := 0; i < probeKeepFrames+5; i++ {
		p.record(`{"frame":` + string(rune('0'+i%10)) + `}`)
	}
	err := p.err(emptyReplyReason)
	if err == nil {
		t.Fatal("没有任何产出时必须报错")
	}
	msg := err.Error()
	if !strings.Contains(msg, emptyReplyReason) {
		t.Errorf("错误信息应该以说明开头: %s", msg)
	}
	if !strings.Contains(msg, "最后 8 帧") {
		t.Errorf("应该只留档最后 %d 帧: %s", probeKeepFrames, msg)
	}
	// 只保留最后 probeKeepFrames 帧,早期的帧要被挤掉
	if strings.Count(msg, `{"frame":`) != probeKeepFrames {
		t.Errorf("留档帧数不对: %s", msg)
	}
}

// 一帧都没收到和"收到了但解析不出内容"是两种问题,提示要能区分
func TestStreamProbeNoDataAtAll(t *testing.T) {
	var p streamProbe
	err := p.err(emptyReplyReason)
	if err == nil {
		t.Fatal("必须报错")
	}
	if !strings.Contains(err.Error(), "没有收到任何数据") {
		t.Errorf("应该说明是一帧都没收到: %v", err)
	}
}

func TestStreamProbeTruncatesLongDump(t *testing.T) {
	var p streamProbe
	p.record(strings.Repeat("x", 5000))
	msg := p.err(emptyReplyReason).Error()
	if len(msg) > probeDumpLimit+300 {
		t.Errorf("原始响应留档没被截断,长度 %d", len(msg))
	}
}

// Anthropic 会在 HTTP 200 的流中间推 error 事件
func TestParseAnthropicStreamError(t *testing.T) {
	cases := map[string]string{
		`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`: "overloaded_error: Overloaded",
		`{"type":"error","error":{"type":"api_error"}}`:                              "api_error",
		`{"type":"content_block_delta","delta":{"text":"hi"}}`:                       "",
		`不是 JSON`:                                                                    "",
	}
	for payload, want := range cases {
		if got := parseAnthropicStreamError(payload); got != want {
			t.Errorf("parseAnthropicStreamError(%s) = %q, want %q", payload, got, want)
		}
	}
}

// Gemini 的安全拦截也是 HTTP 200 + 空回复,必须翻成人话
func TestParseGeminiBlock(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			"提问被安全策略拦截",
			`{"promptFeedback":{"blockReason":"SAFETY"}}`,
			"提问内容被 Gemini 的安全策略拦截",
		},
		{
			"回复因复述被中断",
			`{"candidates":[{"finishReason":"RECITATION"}]}`,
			"回复回复因为疑似大段复述受版权保护的内容被中断",
		},
		{
			"未知原因也要报出来",
			`{"candidates":[{"finishReason":"WEIRD_NEW_REASON"}]}`,
			"回复被中断,原因:WEIRD_NEW_REASON",
		},
		{"正常结束不算拦截", `{"candidates":[{"finishReason":"STOP"}]}`, ""},
		{"还没结束", `{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`, ""},
		{"不是 JSON", `garbage`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseGeminiBlock(tc.payload); got != tc.want {
				t.Errorf("parseGeminiBlock = %q, want %q", got, tc.want)
			}
		})
	}
}

// 被拦截时报具体原因,没拦截才回落到通用说明
func TestGeminiEmptyReason(t *testing.T) {
	if got := geminiEmptyReason("提问内容被拦截"); got != "提问内容被拦截" {
		t.Errorf("got %q", got)
	}
	if got := geminiEmptyReason(""); got != emptyReplyReason {
		t.Errorf("got %q, want %q", got, emptyReplyReason)
	}
}
