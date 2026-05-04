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

// Google Gemini 走 generativelanguage.googleapis.com
//   list:    GET  {base}/v1beta/models?key={apiKey}
//   stream:  POST {base}/v1beta/models/{model}:streamGenerateContent?alt=sse&key={apiKey}
//
// 我们只需要"列模型 + 发一次最小请求收首 chunk"两件事

const defaultGeminiBase = "https://generativelanguage.googleapis.com"

func geminiBase(p Provider) string {
	b := strings.TrimRight(p.BaseURL, "/")
	if b == "" {
		b = defaultGeminiBase
	}
	// 用户填到 v1beta 也兼容
	if strings.HasSuffix(b, "/v1beta") {
		return strings.TrimSuffix(b, "/v1beta")
	}
	return b
}

func fetchGeminiModels(p Provider) FetchModelsResult {
	if p.APIKey == "" {
		return FetchModelsResult{OK: false, Message: "API Key 不能为空"}
	}
	url := geminiBase(p) + "/v1beta/models?key=" + p.APIKey
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return FetchModelsResult{OK: false, Message: "构造请求失败: " + err.Error()}
	}
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
		Models []struct {
			Name        string `json:"name"`        // models/gemini-1.5-pro
			DisplayName string `json:"displayName"` // 可能为空
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return FetchModelsResult{OK: false, Message: "解析响应失败: " + err.Error()}
	}
	out := make([]ModelInfo, 0, len(parsed.Models))
	for _, m := range parsed.Models {
		id := strings.TrimPrefix(m.Name, "models/")
		if id == "" {
			continue
		}
		out = append(out, ModelInfo{ID: id, OwnedBy: "google"})
	}
	return FetchModelsResult{OK: true, Models: out}
}

// streamGemini 走 Gemini 协议的实际聊天流;system 走 systemInstruction 字段
func streamGemini(ctx context.Context, p Provider, conv Conversation, cb streamCallbacks) {
	if p.APIKey == "" {
		cb.onError(fmt.Errorf("API Key 不能为空"))
		return
	}
	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s",
		geminiBase(p), conv.ModelID, p.APIKey)

	body := map[string]any{
		"contents": buildGeminiContents(conv),
	}
	if conv.System != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []map[string]string{{"text": conv.System}},
		}
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		cb.onError(fmt.Errorf("构造请求失败: %w", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
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
		text, thinking := parseGeminiDelta(payload)
		if thinking != "" {
			cb.onThinking(thinking)
		}
		if text != "" {
			cb.onText(text)
		}
		if u := parseGeminiUsage(payload); u != nil {
			cb.onUsage(*u)
		}
	}
	if err := scanner.Err(); err != nil {
		cb.onError(fmt.Errorf("读取流失败: %w", err))
		return
	}
	cb.onDone()
}

// parseGeminiUsage 从 streamGenerateContent 的 chunk 里抠 usageMetadata。
//
//	usageMetadata 通常在最后一帧出现,字段:
//	  promptTokenCount / candidatesTokenCount / thoughtsTokenCount / cachedContentTokenCount
func parseGeminiUsage(payload string) *Usage {
	var ev struct {
		UsageMetadata *struct {
			PromptTokenCount        int `json:"promptTokenCount"`
			CandidatesTokenCount    int `json:"candidatesTokenCount"`
			ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
			CachedContentTokenCount int `json:"cachedContentTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil || ev.UsageMetadata == nil {
		return nil
	}
	u := ev.UsageMetadata
	if u.PromptTokenCount == 0 && u.CandidatesTokenCount == 0 {
		return nil
	}
	return &Usage{
		InputTokens:     u.PromptTokenCount,
		OutputTokens:    u.CandidatesTokenCount,
		ReasoningTokens: u.ThoughtsTokenCount,
		CachedTokens:    u.CachedContentTokenCount,
	}
}

// buildGeminiContents Gemini 用 user/model 角色,system 走单独字段
func buildGeminiContents(conv Conversation) []map[string]any {
	msgs := contextMessages(conv)
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "system" || m.Role == RoleClear {
			continue
		}
		if m.Role == "assistant" && m.Content == "" {
			continue
		}
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		out = append(out, map[string]any{
			"role":  role,
			"parts": []map[string]string{{"text": m.Content}},
		})
	}
	return out
}

// parseGeminiDelta 从 streamGenerateContent 的 SSE chunk 抠 (text, thinking)。
// Gemini 2.5 Pro Thinking 用 part.thought=true 标记 thinking part
func parseGeminiDelta(payload string) (text, thinking string) {
	var ev struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return "", ""
	}
	var sbText, sbThink strings.Builder
	for _, c := range ev.Candidates {
		for _, part := range c.Content.Parts {
			if part.Thought {
				sbThink.WriteString(part.Text)
			} else {
				sbText.WriteString(part.Text)
			}
		}
	}
	return sbText.String(), sbThink.String()
}

func testGeminiModel(p Provider, modelID string) TestResult {
	start := time.Now()
	if p.APIKey == "" {
		return TestResult{OK: false, Message: "API Key 不能为空"}
	}
	if modelID == "" {
		return TestResult{OK: false, Message: "未指定模型"}
	}
	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s",
		geminiBase(p), modelID, p.APIKey)
	body := map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]string{{"text": "hi"}}},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return TestResult{OK: false, Message: "构造请求失败: " + err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
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
