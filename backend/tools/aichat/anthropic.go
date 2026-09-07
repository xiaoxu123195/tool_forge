package aichat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Anthropic Claude
//   list:    GET  {base}/v1/models                (header x-api-key + anthropic-version)
//   stream:  POST {base}/v1/messages              (stream: true)

const defaultAnthropicBase = "https://api.anthropic.com"
const anthropicVersion = "2023-06-01"

func anthropicBase(p Provider) string {
	b := strings.TrimRight(p.BaseURL, "/")
	if b == "" {
		b = defaultAnthropicBase
	}
	// 用户写 .../v1 也兼容
	if strings.HasSuffix(b, "/v1") {
		return strings.TrimSuffix(b, "/v1")
	}
	return b
}

func applyAnthropicHeaders(req *http.Request, apiKey string) {
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
	req.Header.Set("Content-Type", "application/json")
}

func fetchAnthropicModels(p Provider) FetchModelsResult {
	if p.APIKey == "" {
		return FetchModelsResult{OK: false, Message: "API Key 不能为空"}
	}
	url := anthropicBase(p) + "/v1/models"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return FetchModelsResult{OK: false, Message: "构造请求失败: " + err.Error()}
	}
	applyAnthropicHeaders(req, p.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return FetchModelsResult{OK: false, Message: prettifyNetErr(err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return FetchModelsResult{
			OK:      false,
			Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, extractErrorMessage(body)),
		}
	}

	var parsed struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return FetchModelsResult{OK: false, Message: "解析响应失败: " + err.Error()}
	}
	out := make([]ModelInfo, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		if m.ID == "" {
			continue
		}
		out = append(out, ModelInfo{ID: m.ID, OwnedBy: "anthropic"})
	}
	return FetchModelsResult{OK: true, Models: out}
}

// anthropicEvent 一帧 SSE 的并集视图。Anthropic 的事件类型多但字段稀疏,
// 用一个结构体全接住比每种事件解一次省事。
type anthropicEvent struct {
	Type         string `json:"type"`
	ContentBlock struct {
		Type string `json:"type"`
		// Data redacted_thinking 的密文
		Data string `json:"data"`
		// Content web_search_tool_result 的搜索结果条目
		Content []struct {
			Type  string `json:"type"`
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"content"`
	} `json:"content_block"`
	Delta struct {
		Type      string `json:"type"`
		Text      string `json:"text"`
		Thinking  string `json:"thinking"`
		Signature string `json:"signature"`
		Citation  struct {
			URL       string `json:"url"`
			Title     string `json:"title"`
			CitedText string `json:"cited_text"`
		} `json:"citation"`
	} `json:"delta"`
}

// anthropicBlock 正在接收中的一个 content block。
//
// 思考块是 content_block_start → 若干 thinking_delta → 一个 signature_delta →
// content_block_stop 这样一组事件;signature 必须和思考文本绑在一起落盘,
// 下一轮原样回传,否则开着 thinking 的请求会被拒。
type anthropicBlock struct {
	kind      string
	text      strings.Builder
	signature string
	redacted  string
}

// streamAnthropic 走 Anthropic /v1/messages 流;system 用顶层 system 字段
func streamAnthropic(ctx context.Context, req chatRequest, cb streamCallbacks) {
	cb = cb.withDefaults()
	p, conv, spec := req.Provider, req.Conv, req.Spec
	if p.APIKey == "" {
		cb.onError(fmt.Errorf("API Key 不能为空"))
		return
	}

	// max_tokens 是必填项(这也是 Anthropic 和其他端点不同的地方:别家不填就由模型自己决定)。
	// 用户在会话里设过就用他的,否则按模型上限来 —— 以前写死 4096,Claude 4 系列能出 64k 却被截死。
	maxTokens := effectiveMaxTokens(conv, spec)
	// 思考预算必须严格小于 max_tokens,resolveReasoning 内部会按这个上限夹一次
	reasoning := resolveReasoning(conv.ReasoningEffort, spec, maxTokens)

	url := anthropicBase(p) + "/v1/messages"
	body := map[string]any{
		"model":      conv.ModelID,
		"max_tokens": maxTokens,
		"stream":     true,
		"messages":   buildAnthropicMessages(req, reasoning.Enabled()),
	}
	if conv.System != "" {
		body["system"] = conv.System
	}
	applyEmissions(body, reasoning.Emissions)
	applySampling(body, conv, spec, reasoning.Enabled())
	if conv.WebSearch {
		applyWebSearchPatch(body, buildWebSearchPatch(spec))
	}
	// 提示词缓存:只对原厂域名发。cache_control 是 Anthropic 专有字段,
	// 中转不认的话整个请求会 400,而缓存只是省钱、不是功能,不值得冒这个险。
	if spec.family == familyAnthropic {
		applyAnthropicCache(body)
	}
	bodyBytes, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		cb.onError(fmt.Errorf("构造请求失败: %w", err))
		return
	}
	applyAnthropicHeaders(httpReq, p.APIKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := streamClient.Do(httpReq)
	if err != nil {
		cb.onError(fmt.Errorf("%s", prettifyNetErr(err)))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		cb.onError(fmt.Errorf("HTTP %d: %s", resp.StatusCode, extractErrorMessage(raw)))
		return
	}

	scanner := newSSEScanner(resp.Body)
	var block *anthropicBlock
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			cb.onError(fmt.Errorf("已取消"))
			return
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

		var ev anthropicEvent
		if err := json.Unmarshal([]byte(payload), &ev); err == nil {
			switch ev.Type {
			case "content_block_start":
				block = &anthropicBlock{kind: ev.ContentBlock.Type}
				if ev.ContentBlock.Type == "redacted_thinking" {
					block.redacted = ev.ContentBlock.Data
				}
				// 联网搜索结果整块到达(不是增量),直接抽引用
				for _, c := range ev.ContentBlock.Content {
					if c.URL != "" {
						cb.onCitation(Citation{URL: c.URL, Title: c.Title})
					}
				}
			case "content_block_delta":
				switch ev.Delta.Type {
				case "text_delta":
					cb.onText(ev.Delta.Text)
				case "thinking_delta":
					cb.onThinking(ev.Delta.Thinking)
					if block != nil {
						block.text.WriteString(ev.Delta.Thinking)
					}
				case "signature_delta":
					if block != nil {
						block.signature += ev.Delta.Signature
					}
				case "citations_delta":
					if ev.Delta.Citation.URL != "" {
						cb.onCitation(Citation{
							URL:     ev.Delta.Citation.URL,
							Title:   ev.Delta.Citation.Title,
							Snippet: ev.Delta.Citation.CitedText,
						})
					}
				}
			case "content_block_stop":
				if block != nil && (block.kind == "thinking" || block.kind == "redacted_thinking") {
					cb.onThinkingBlock(ThinkingBlock{
						Text:      block.text.String(),
						Signature: block.signature,
						Redacted:  block.redacted,
					})
				}
				block = nil
			}
		}

		if u := parseAnthropicUsage(payload); u != nil {
			cb.onUsage(*u)
		}
	}
	if err := scanner.Err(); err != nil {
		cb.onError(fmt.Errorf("读取流失败: %w", err))
		return
	}
	cb.onDone()
}

// applyAnthropicCache 给请求打上提示词缓存断点。
//
// Anthropic 缓存的是"到断点为止的整段前缀",命中后这部分输入按约 1/10 计价。
// 长对话里省得非常可观 —— 每多一轮,前面所有轮次都能走缓存。
//
// 打两个断点(上限是 4 个):
//   - system 提示词:全程不变,最稳定的一段
//   - 倒数第二条消息(上一轮的 assistant 回复):它之前的内容下一轮还会原样再发一次
//
// 前缀不够长(约 1024 token)时 Anthropic 会直接忽略断点,不报错也不计费,所以不用自己判长度;
// 只跳过消息太少、明显缓存不起来的短对话,省掉无谓的缓存写。
func applyAnthropicCache(body map[string]any) {
	if s, ok := body["system"].(string); ok && s != "" {
		body["system"] = []map[string]any{{
			"type":          "text",
			"text":          s,
			"cache_control": map[string]any{"type": "ephemeral"},
		}}
	}
	msgs, ok := body["messages"].([]map[string]any)
	if !ok || len(msgs) < 3 {
		return
	}
	markAnthropicCache(msgs[len(msgs)-2])
}

// markAnthropicCache 给一条消息的最后一个 content 块打缓存断点。
// content 是裸字符串时先转成块数组 —— cache_control 只能挂在块上。
func markAnthropicCache(msg map[string]any) {
	switch c := msg["content"].(type) {
	case string:
		if c == "" {
			return
		}
		msg["content"] = []map[string]any{{
			"type":          "text",
			"text":          c,
			"cache_control": map[string]any{"type": "ephemeral"},
		}}
	case []map[string]any:
		if len(c) == 0 {
			return
		}
		c[len(c)-1]["cache_control"] = map[string]any{"type": "ephemeral"}
	}
}

// parseAnthropicUsage 从 message_start / message_delta 中抠 usage。
//
//	message_start.message.usage:input_tokens + cache_*_input_tokens
//	message_delta.usage.output_tokens:最终 output 累计
func parseAnthropicUsage(payload string) *Usage {
	var ev struct {
		Type    string `json:"type"`
		Message struct {
			Usage struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return nil
	}
	switch ev.Type {
	case "message_start":
		u := ev.Message.Usage
		if u.InputTokens == 0 && u.OutputTokens == 0 {
			return nil
		}
		return &Usage{
			InputTokens:  u.InputTokens,
			OutputTokens: u.OutputTokens,
			CachedTokens: u.CacheReadInputTokens, // 命中缓存的部分
		}
	case "message_delta":
		if ev.Usage.OutputTokens == 0 {
			return nil
		}
		return &Usage{OutputTokens: ev.Usage.OutputTokens}
	}
	return nil
}

// buildAnthropicMessages 只能是 user/assistant 交替,system 走外层字段
//
//	带图片时 content 是 [{type:"image",source:{...}}, {type:"text",text}] 数组
//	includeThinking=true 时,assistant 消息要把上一轮的思考块连同 signature 原样带回去
func buildAnthropicMessages(req chatRequest, includeThinking bool) []map[string]any {
	conv := req.Conv
	supportsPDF := req.Spec.Has(CapPDF)
	msgs := contextMessages(conv)
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "system" || m.Role == RoleClear {
			continue
		}
		if m.Role == "assistant" && m.Content == "" {
			continue
		}
		if m.Role == "assistant" && includeThinking && len(m.Thinking) > 0 {
			// thinking 块必须排在 text 前面,且 signature 一个字都不能改
			parts := make([]map[string]any, 0, len(m.Thinking)+1)
			for _, t := range m.Thinking {
				if t.Redacted != "" {
					parts = append(parts, map[string]any{"type": "redacted_thinking", "data": t.Redacted})
					continue
				}
				// 没有 signature 的思考块是历史遗留(旧版本只存了纯文本),
				// 发回去会被拒 —— 直接跳过,让这一轮少一点上下文总比整个请求失败好
				if t.Signature == "" {
					continue
				}
				parts = append(parts, map[string]any{
					"type":      "thinking",
					"thinking":  t.Text,
					"signature": t.Signature,
				})
			}
			if len(parts) > 0 {
				parts = append(parts, map[string]any{"type": "text", "text": m.Content})
				out = append(out, map[string]any{"role": m.Role, "content": parts})
				continue
			}
		}
		if m.Role == "user" && (len(m.Images) > 0 || len(m.Files) > 0) {
			textFiles, binaryFiles := partitionFiles(m.Files, supportsPDF)
			text := userContentWithFileText(m.Content, textFiles)
			parts := []map[string]any{}
			for _, img := range m.Images {
				var source map[string]any
				if img.URL != "" {
					source = map[string]any{"type": "url", "url": img.URL}
				} else {
					mime := img.MimeType
					if mime == "" {
						mime = "image/png"
					}
					source = map[string]any{
						"type":       "base64",
						"media_type": mime,
						"data":       img.Data,
					}
				}
				parts = append(parts, map[string]any{"type": "image", "source": source})
			}
			for _, f := range binaryFiles {
				mime := f.MimeType
				if mime == "" {
					mime = "application/pdf"
				}
				parts = append(parts, map[string]any{
					"type": "document",
					"source": map[string]any{
						"type":       "base64",
						"media_type": mime,
						"data":       f.Data,
					},
				})
			}
			if text != "" {
				parts = append(parts, map[string]any{"type": "text", "text": text})
			}
			out = append(out, map[string]any{"role": m.Role, "content": parts})
			continue
		}
		out = append(out, map[string]any{"role": m.Role, "content": m.Content})
	}
	return out
}

func testAnthropicModel(p Provider, modelID string) TestResult {
	start := time.Now()
	if p.APIKey == "" {
		return TestResult{OK: false, Message: "API Key 不能为空"}
	}
	if modelID == "" {
		return TestResult{OK: false, Message: "未指定模型"}
	}
	url := anthropicBase(p) + "/v1/messages"
	body := map[string]any{
		"model":      modelID,
		"max_tokens": 8,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return TestResult{OK: false, Message: "构造请求失败: " + err.Error()}
	}
	applyAnthropicHeaders(req, p.APIKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := httpClient.Do(req)
	if err != nil {
		return TestResult{
			OK:         false,
			Message:    prettifyNetErr(err),
			DurationMs: int(time.Since(start).Milliseconds()),
		}
	}
	defer resp.Body.Close()
	dur := int(time.Since(start).Milliseconds())

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return TestResult{
			OK:         false,
			StatusCode: resp.StatusCode,
			DurationMs: dur,
			Message:    fmt.Sprintf("HTTP %d: %s", resp.StatusCode, extractErrorMessage(raw)),
		}
	}

	scanner := newSSEScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		return TestResult{
			OK:         true,
			StatusCode: resp.StatusCode,
			DurationMs: int(time.Since(start).Milliseconds()),
			Message:    "响应正常",
		}
	}
	if err := scanner.Err(); err != nil {
		return TestResult{
			OK:         false,
			StatusCode: resp.StatusCode,
			DurationMs: int(time.Since(start).Milliseconds()),
			Message:    "读取流失败: " + err.Error(),
		}
	}
	return TestResult{
		OK:         false,
		StatusCode: resp.StatusCode,
		DurationMs: int(time.Since(start).Milliseconds()),
		Message:    "流未返回任何数据",
	}
}
