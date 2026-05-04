package aichat

import (
	"bufio"
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

// streamAnthropic 走 Anthropic /v1/messages 流;system 用顶层 system 字段
func streamAnthropic(ctx context.Context, p Provider, conv Conversation, cb streamCallbacks) {
	if p.APIKey == "" {
		cb.onError(fmt.Errorf("API Key 不能为空"))
		return
	}
	url := anthropicBase(p) + "/v1/messages"
	body := map[string]any{
		"model":      conv.ModelID,
		"max_tokens": 4096,
		"stream":     true,
		"messages":   buildAnthropicMessages(conv),
	}
	if conv.System != "" {
		body["system"] = conv.System
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		cb.onError(fmt.Errorf("构造请求失败: %w", err))
		return
	}
	applyAnthropicHeaders(req, p.APIKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := streamClient.Do(req)
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

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
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
		text, thinking := parseAnthropicDelta(payload)
		if thinking != "" {
			cb.onThinking(thinking)
		}
		if text != "" {
			cb.onText(text)
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
func buildAnthropicMessages(conv Conversation) []map[string]any {
	msgs := contextMessages(conv)
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "system" || m.Role == RoleClear {
			continue
		}
		if m.Role == "assistant" && m.Content == "" {
			continue
		}
		if m.Role == "user" && (len(m.Images) > 0 || len(m.Files) > 0) {
			textFiles, binaryFiles := partitionFiles(m.Files, true)
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

// parseAnthropicDelta 解 (text, thinking)
//
//	text     = content_block_delta + delta.type=text_delta
//	thinking = content_block_delta + delta.type=thinking_delta(extended thinking)
func parseAnthropicDelta(payload string) (text, thinking string) {
	var ev struct {
		Type  string `json:"type"`
		Delta struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return "", ""
	}
	if ev.Type != "content_block_delta" {
		return "", ""
	}
	switch ev.Delta.Type {
	case "text_delta":
		return ev.Delta.Text, ""
	case "thinking_delta":
		return "", ev.Delta.Thinking
	}
	return "", ""
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

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
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
