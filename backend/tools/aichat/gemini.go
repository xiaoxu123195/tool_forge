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
func streamGemini(ctx context.Context, req chatRequest, cb streamCallbacks) {
	cb = cb.withDefaults()
	p, conv, spec := req.Provider, req.Conv, req.Spec
	if p.APIKey == "" {
		cb.onError(fmt.Errorf("API Key 不能为空"))
		return
	}
	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s",
		geminiBase(p), conv.ModelID, p.APIKey)

	body := map[string]any{
		"contents": buildGeminiContents(req),
	}
	if conv.System != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []map[string]string{{"text": conv.System}},
		}
	}
	// 思考控制:2.x 写 thinkingConfig.thinkingBudget(token 数),3.x 写 thinkingLevel(档位词)。
	// 注意 includeThoughts 不打开的话,思考文本一个字都不会回传。
	reasoning := resolveReasoning(conv.ReasoningEffort, spec, effectiveMaxTokens(conv, spec))
	applyEmissions(body, reasoning.Emissions)
	applySampling(body, conv, spec, reasoning.Enabled())
	if conv.WebSearch {
		applyWebSearchPatch(body, buildWebSearchPatch(spec))
	}
	if conv.Tools && spec.Has(CapTools) {
		appendTools(body, geminiToolDecls())
	}
	applyCustomBody(body, p.CustomBody)
	bodyBytes, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		cb.onError(fmt.Errorf("构造请求失败: %w", err))
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	tr := startTrace("chat", p, spec, conv.ID, "POST", url)
	tr.request(httpReq.Header, bodyBytes)
	defer tr.finish()

	resp, err := streamClient.Do(httpReq)
	if err != nil {
		tr.fail(err)
		cb.onError(fmt.Errorf("%s", prettifyNetErr(err)))
		return
	}
	defer resp.Body.Close()
	tr.status(resp.StatusCode)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		// 上游的原始错误体一并留档 —— 中转返回的 HTTP 400 里往往写着
		// 到底哪个字段它不认,那句话比"HTTP 400"有用得多
		tr.frame(string(raw))
		err := fmt.Errorf("HTTP %d: %s", resp.StatusCode, extractErrorMessage(raw))
		tr.fail(err)
		cb.onError(err)
		return
	}

	scanner := newSSEScanner(resp.Body)
	// Gemini 的安全拦截同样走 HTTP 200,表现就是"回复是空的";
	// 不把 blockReason / finishReason 翻出来,用户完全不知道发生了什么
	var blocked string
	var probe streamProbe
	var toolCalls []ToolCall
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
		probe.record(payload)
		tr.frame(payload)
		if r := parseGeminiBlock(payload); r != "" {
			blocked = r
		}
		text, thinking := parseGeminiDelta(payload)
		if thinking != "" {
			cb.onThinking(thinking)
		}
		if text != "" {
			cb.onText(text)
			probe.mark()
		}
		if u := parseGeminiUsage(payload); u != nil {
			cb.onUsage(*u)
		}
		if parseGeminiLengthCapped(payload) {
			cb.onTruncated()
		}
		for _, img := range parseGeminiImages(payload) {
			cb.onImage(img)
			probe.mark()
		}
		for _, c := range parseGeminiCitations(payload) {
			cb.onCitation(c)
		}
		for _, q := range parseGeminiSearches(payload) {
			cb.onSearch(q)
		}
		toolCalls = append(toolCalls, parseGeminiToolCalls(payload)...)
	}
	if err := scanner.Err(); err != nil {
		cb.onError(fmt.Errorf("读取流失败: %w", err))
		return
	}
	// 请求了工具就不算空回复 —— 模型这一轮的产出就是"我要调用 X"
	if len(toolCalls) > 0 {
		for _, c := range toolCalls {
			cb.onToolCall(c)
		}
		cb.onDone()
		return
	}
	// 有内容就照常结束 —— MAX_TOKENS 这类"截断"也算正常产出,不该报错
	if err := probe.err(geminiEmptyReason(blocked)); err != nil {
		cb.onError(err)
		return
	}
	cb.onDone()
}

// geminiEmptyReason 空回复时优先报具体的拦截 / 中断原因,没有再回落到通用说明
func geminiEmptyReason(blocked string) string {
	if blocked != "" {
		return blocked
	}
	return emptyReplyReason
}

// parseGeminiUsage 从 streamGenerateContent 的 chunk 里抠 usageMetadata。
//
//	usageMetadata 通常在最后一帧出现,字段:
//	  promptTokenCount / candidatesTokenCount / thoughtsTokenCount / cachedContentTokenCount
// parseGeminiLengthCapped candidates[0].finishReason == MAX_TOKENS,
// 即回复是被输出上限掐掉的。其余取值(STOP / SAFETY / RECITATION)另有去处:
// 拦截类的由 parseGeminiBlock 翻成错误文案,正常结束的什么都不用做。
func parseGeminiLengthCapped(payload string) bool {
	var ev struct {
		Candidates []struct {
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil || len(ev.Candidates) == 0 {
		return false
	}
	return ev.Candidates[0].FinishReason == "MAX_TOKENS"
}

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
//
//	带图片时 parts 含 {inlineData:{mimeType,data}}(仅 base64,Gemini 不直接吃远程 URL)
func buildGeminiContents(req chatRequest) []map[string]any {
	conv := req.Conv
	supportsPDF := req.Spec.Has(CapPDF)
	msgs := contextMessages(conv)
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "system" || m.Role == RoleClear {
			continue
		}
		// 工具结果以 user 身份回传,调用本身挂在 model 身上
		if m.Role == RoleTool {
			out = append(out, map[string]any{"role": "user", "parts": buildGeminiToolParts(m)})
			continue
		}
		if len(m.ToolCalls) > 0 {
			out = append(out, map[string]any{"role": "model", "parts": buildGeminiToolParts(m)})
			continue
		}
		if m.Role == "assistant" && m.Content == "" {
			continue
		}
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		parts := []map[string]any{}
		text := m.Content
		if m.Role == "user" {
			textFiles, binaryFiles := partitionFiles(m.Files, supportsPDF)
			text = userContentWithFileText(m.Content, textFiles)
			if text != "" {
				parts = append(parts, map[string]any{"text": text})
			}
			for _, img := range m.Images {
				if img.Data == "" {
					// Gemini 不支持直接吃远程 URL,跳过(前端应在上传时转 base64)
					continue
				}
				mime := img.MimeType
				if mime == "" {
					mime = "image/png"
				}
				parts = append(parts, map[string]any{
					"inlineData": map[string]string{
						"mimeType": mime,
						"data":     img.Data,
					},
				})
			}
			for _, f := range binaryFiles {
				mime := f.MimeType
				if mime == "" {
					mime = "application/pdf"
				}
				parts = append(parts, map[string]any{
					"inlineData": map[string]string{
						"mimeType": mime,
						"data":     f.Data,
					},
				})
			}
		} else if text != "" {
			parts = append(parts, map[string]any{"text": text})
		}
		if len(parts) == 0 {
			parts = append(parts, map[string]any{"text": ""})
		}
		out = append(out, map[string]any{
			"role":  role,
			"parts": parts,
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

// parseGeminiCitations 从 groundingMetadata 里抠联网搜索引用到的来源。
// google_search 工具的结果不会出现在正文里,只挂在 candidates[].groundingMetadata 上。
func parseGeminiCitations(payload string) []Citation {
	var ev struct {
		Candidates []struct {
			GroundingMetadata struct {
				GroundingChunks []struct {
					Web *struct {
						URI   string `json:"uri"`
						Title string `json:"title"`
					} `json:"web"`
				} `json:"groundingChunks"`
			} `json:"groundingMetadata"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return nil
	}
	var out []Citation
	for _, c := range ev.Candidates {
		for _, chunk := range c.GroundingMetadata.GroundingChunks {
			if chunk.Web != nil && chunk.Web.URI != "" {
				out = append(out, Citation{URL: chunk.Web.URI, Title: chunk.Web.Title})
			}
		}
	}
	return out
}

// parseGeminiSearches 抠出 google_search 实际用的检索词。
//
// 和别家不同,Gemini 不在检索发生时通知,而是把用过的词和结果一起挂在
// groundingMetadata 上事后给出 —— 所以这里只能直接标成 done,做不到"正在搜索"的实时感。
func parseGeminiSearches(payload string) []SearchQuery {
	var ev struct {
		Candidates []struct {
			GroundingMetadata struct {
				WebSearchQueries []string `json:"webSearchQueries"`
			} `json:"groundingMetadata"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return nil
	}
	var out []SearchQuery
	for _, c := range ev.Candidates {
		for _, q := range c.GroundingMetadata.WebSearchQueries {
			if q = strings.TrimSpace(q); q != "" {
				out = append(out, SearchQuery{Query: q, Status: "done"})
			}
		}
	}
	return out
}

// parseGeminiImages 从 chunk 中抠 inlineData(模型生成的图)
func parseGeminiImages(payload string) []ImageBlock {
	var ev struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData *struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return nil
	}
	out := make([]ImageBlock, 0)
	for _, c := range ev.Candidates {
		for _, part := range c.Content.Parts {
			if part.InlineData != nil && part.InlineData.Data != "" {
				mime := part.InlineData.MimeType
				if mime == "" {
					mime = "image/png"
				}
				out = append(out, ImageBlock{MimeType: mime, Data: part.InlineData.Data})
			}
		}
	}
	return out
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
	applyCustomBody(body, p.CustomBody)
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
