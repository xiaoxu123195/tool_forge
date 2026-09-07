package aichat

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 流跑完了却什么都没产出,是这个工具最常见也最难查的失败模式:
// HTTP 200、SSE 也一直在推,但字段名我们不认识,于是用户看到一个空白回复,毫无线索。
//
// 对策是留档最后几帧原始响应,在判定"这次没产出"时一起抛给用户 ——
// 有这几帧就能对照着补一条解析规则,没有就只能靠猜。

// probeKeepFrames 留档多少帧。够看清格式,又不会把错误弹窗撑爆。
const probeKeepFrames = 8

// probeDumpLimit 拼进错误信息的原始响应最多多少字符
const probeDumpLimit = 1500

// streamProbe 跟踪一条流有没有真的产出内容,并留档最近几帧原始响应
type streamProbe struct {
	emitted bool
	recent  []string
}

// record 记一帧原始响应(滚动保留最后 probeKeepFrames 帧)
func (p *streamProbe) record(payload string) {
	p.recent = append(p.recent, payload)
	if len(p.recent) > probeKeepFrames {
		p.recent = p.recent[1:]
	}
}

// mark 标记"这次流确实产出了内容"
func (p *streamProbe) mark() { p.emitted = true }

// err 流正常结束时调用;返回非 nil 表示整条流没有任何产出,应当报错而不是给一个空回复。
// reason 是给用户的第一句话,后面跟原始响应留档。
func (p *streamProbe) err(reason string) error {
	if p.emitted {
		return nil
	}
	dump := strings.Join(p.recent, "\n")
	if len(dump) > probeDumpLimit {
		dump = dump[len(dump)-probeDumpLimit:]
	}
	if dump == "" {
		return fmt.Errorf("%s\n\n整条流没有收到任何数据 —— 多半是供应商直接断开了连接", reason)
	}
	return fmt.Errorf("%s\n\n最后 %d 帧原始响应:\n%s", reason, len(p.recent), dump)
}

// emptyReplyReason 空回复的默认说明
const emptyReplyReason = "模型未返回可识别的文本或图片"

// parseAnthropicStreamError 解 Anthropic 在流中间推来的 error 事件。
// 这类错误 HTTP 状态码是 200,不看流内容根本发现不了。
func parseAnthropicStreamError(payload string) string {
	var ev struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil || ev.Type != "error" {
		return ""
	}
	if ev.Error.Message == "" {
		return ev.Error.Type
	}
	if ev.Error.Type != "" {
		return ev.Error.Type + ": " + ev.Error.Message
	}
	return ev.Error.Message
}

// geminiBlockReasons 把 Gemini 的拦截 / 截断原因翻成人话。
// 这些同样走 HTTP 200,表现就是"回复是空的",不翻出来用户完全不知道发生了什么。
var geminiBlockReasons = map[string]string{
	"SAFETY":                "内容被 Gemini 的安全策略拦截",
	"BLOCKLIST":             "内容命中了 Gemini 的屏蔽词表",
	"PROHIBITED_CONTENT":    "内容被判定为违禁内容",
	"RECITATION":            "回复因为疑似大段复述受版权保护的内容被中断",
	"SPII":                  "内容涉及敏感个人信息被拦截",
	"MAX_TOKENS":            "回复达到输出上限被截断(可以在会话设置里调大)",
	"IMAGE_SAFETY":          "生成的图片被安全策略拦截",
	"OTHER":                 "被 Gemini 以未说明的原因中断",
	"MALFORMED_FUNCTION_CALL": "模型给出的函数调用格式不合法",
}

// parseGeminiBlock 从 chunk 里找出"为什么没有内容"。
// 优先看 promptFeedback(整个请求被拦),再看 candidates 的 finishReason(回复被中断)。
func parseGeminiBlock(payload string) string {
	var ev struct {
		PromptFeedback struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
		Candidates []struct {
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return ""
	}
	if r := ev.PromptFeedback.BlockReason; r != "" {
		return explainGeminiReason("提问", r)
	}
	for _, c := range ev.Candidates {
		switch c.FinishReason {
		case "", "STOP":
			continue
		}
		return explainGeminiReason("回复", c.FinishReason)
	}
	return ""
}

func explainGeminiReason(subject, reason string) string {
	if text, ok := geminiBlockReasons[reason]; ok {
		return subject + text
	}
	return fmt.Sprintf("%s被中断,原因:%s", subject, reason)
}
