package aichat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/mattn/go-ieproxy"
)

// httpClient 用于短请求(列模型 / 探测连通性);整体 30s 超时
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		Proxy: ieproxy.GetProxyFunc(),
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
	},
}

// streamClient 用于 SSE 长流(聊天);**不**设整体 Timeout —— deepseek-r1 / o1 等
// 思考模型一次回答可能 5+ 分钟,只用 ResponseHeaderTimeout 限制握手阶段
var streamClient = &http.Client{
	Transport: &http.Transport{
		Proxy: ieproxy.GetProxyFunc(),
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
	},
}

// openAIUserAgent 请求 UA。不少中转按 UA 做白名单,空 UA 或陌生 UA 会被直接挡掉,
// 所以固定成一个常见的 SDK 形态而不是留空。
const openAIUserAgent = "ai-sdk/openai/3.0.53"

// applyOpenAIHeaders OpenAI 协议族的通用请求头。
//
// Authorization 与 X-Api-Key 两个都带:官方只认前者,部分中转只认后者,同时带最省事。
func applyOpenAIHeaders(req *http.Request, apiKey string) {
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", openAIUserAgent)
}

// 协议路径常量
const (
	pathResponses = "/responses"        // OpenAI 新版 Responses API
	pathChatCompl = "/chat/completions" // OpenAI 兼容旧 API
)

// fetchModels 调 GET {baseUrl}/models
func fetchModels(p Provider) FetchModelsResult {
	url := strings.TrimRight(p.BaseURL, "/") + "/models"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return FetchModelsResult{OK: false, Message: "构造请求失败: " + err.Error()}
	}
	applyOpenAIHeaders(req, p.APIKey)
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
		Data []ModelInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return FetchModelsResult{OK: false, Message: "解析响应失败: " + err.Error()}
	}
	return FetchModelsResult{OK: true, Models: parsed.Data}
}

// testModel 发一次最小 stream:true 请求,收到首个 SSE chunk 即视为成功。
// useResponses=true → 走 /v1/responses;false → 走 /v1/chat/completions。
func testModel(p Provider, modelID string, useResponses bool) TestResult {
	start := time.Now()
	if p.APIKey == "" {
		return TestResult{OK: false, Message: "API Key 不能为空"}
	}
	if modelID == "" {
		return TestResult{OK: false, Message: "未指定模型"}
	}

	baseURL := strings.TrimRight(p.BaseURL, "/")

	var url string
	var body map[string]any
	if useResponses {
		url = baseURL + pathResponses
		body = map[string]any{
			"model":  modelID,
			"input":  "hi",
			"stream": true,
		}
	} else {
		url = baseURL + pathChatCompl
		body = map[string]any{
			"model": modelID,
			"messages": []map[string]string{
				{"role": "user", "content": "hi"},
			},
			"stream": true,
		}
	}
	// 连通性检测只验"能不能通",不带任何思考 / 联网参数 —— 那些字段各家挑剔,
	// 带上反而会把一个本来正常的供应商测成失败
	bodyBytes, _ := json.Marshal(body)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return TestResult{OK: false, Message: "构造请求失败: " + err.Error()}
	}
	applyOpenAIHeaders(req, p.APIKey)
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
		body, _ := io.ReadAll(resp.Body)
		return TestResult{
			OK:         false,
			StatusCode: resp.StatusCode,
			DurationMs: dur,
			Message:    fmt.Sprintf("HTTP %d: %s", resp.StatusCode, extractErrorMessage(body)),
		}
	}

	// 读取流,首个非空 SSE data 行即成功
	scanner := newSSEScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		// 收到任意 chunk → 成功
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

// streamOpenAI 走 OpenAI 协议的实际聊天流;按 useResponses 选择端点
func streamOpenAI(ctx context.Context, req chatRequest, useResponses bool, cb streamCallbacks) {
	cb = cb.withDefaults()
	p, conv, spec := req.Provider, req.Conv, req.Spec
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	var url string
	var body map[string]any
	if useResponses {
		url = baseURL + pathResponses
		body = map[string]any{
			"model":  conv.ModelID,
			"input":  buildOpenAIResponsesInput(req),
			"stream": true,
		}
	} else {
		url = baseURL + pathChatCompl
		body = map[string]any{
			"model":    conv.ModelID,
			"messages": buildOpenAIChatMessages(req),
			"stream":   true,
			// stream_options.include_usage:让 chat-completions 在最后一帧返回 usage
			"stream_options": map[string]any{"include_usage": true},
		}
	}
	// 思考档位:由 reasoning.go 按模型 + 端点翻译成各家自己的字段;
	// 模型不支持调档或用户没选时,resolveReasoning 返回空,请求体一个字段都不多带
	reasoning := resolveReasoning(conv.ReasoningEffort, spec, effectiveMaxTokens(conv, spec))
	applyEmissions(body, reasoning.Emissions)
	applySampling(body, conv, spec, reasoning.Enabled())
	// 供应商内置联网搜索
	if conv.WebSearch {
		applyWebSearchPatch(body, buildWebSearchPatch(spec))
	}
	bodyBytes, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		cb.onError(fmt.Errorf("构造请求失败: %w", err))
		return
	}
	applyOpenAIHeaders(httpReq, p.APIKey)
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
	// 部分中转/开源模型把思考内容嵌在 delta.content 的 <think>...</think> 里;
	// 用一个状态机把它拆出来路由到 thinking 通道。标签名各家不同(gpt-oss 用 <reasoning>,
	// seed-oss 用 <seed:think>),按模型 ID 选。仅对 chat completions 启用,
	// /responses 已经按事件类型分开了。
	splitter := newThinkSplitter(conv.ModelID)
	// 兜底:跟踪是否输出了任何文本/图片;跑完全程一个都没有 → 把最后几帧原始响应一起抛出(见 diagnose.go)
	var probe streamProbe
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
		if payload == "[DONE]" {
			break
		}
		probe.record(payload)
		var text, thinking string
		var images []ImageBlock
		var citations []Citation
		if useResponses {
			text, thinking = parseOpenAIResponsesDelta(payload)
			citations = parseOpenAIResponsesCitations(payload)
			// 中转(如 chatgpt2api 的 gpt-image)把生图结果以 markdown data:image
			// 形式塞进正文,这里抽到图片通道,避免几 MB base64 当正文渲染/落盘
			if text != "" {
				var inlineImgs []ImageBlock
				text, inlineImgs = extractInlineDataImages(text)
				images = append(images, inlineImgs...)
			}
			if u := parseOpenAIResponsesUsage(payload); u != nil {
				cb.onUsage(*u)
			}
			images = append(images, parseOpenAIResponsesImages(payload)...)
		} else {
			text, thinking = parseOpenAIChatDelta(payload)
			citations = parseOpenAIChatCitations(payload)
			// 同上:把内联在 delta.content 里的 base64 图片抽到图片通道
			if text != "" {
				var inlineImgs []ImageBlock
				text, inlineImgs = extractInlineDataImages(text)
				images = append(images, inlineImgs...)
			}
			if text != "" {
				var extra string
				text, extra = splitter.feed(text)
				if extra != "" {
					thinking += extra
				}
			}
			if u := parseOpenAIChatUsage(payload); u != nil {
				cb.onUsage(*u)
			}
			images = append(images, parseOpenAIChatImages(payload)...)
			// 通用兜底:如果仍没解析出图,从 payload 里挖一遍可能的 base64/URL 字段
			if len(images) == 0 {
				images = append(images, sniffImagesFromPayload(payload)...)
			}
		}
		if thinking != "" {
			cb.onThinking(thinking)
		}
		if text != "" {
			cb.onText(text)
			probe.mark()
		}
		for _, img := range images {
			cb.onImage(img)
			probe.mark()
		}
		for _, c := range citations {
			cb.onCitation(c)
		}
	}
	if err := scanner.Err(); err != nil {
		cb.onError(fmt.Errorf("读取流失败: %w", err))
		return
	}
	if err := probe.err(emptyReplyReason); err != nil {
		cb.onError(err)
		return
	}
	cb.onDone()
}

// buildOpenAIResponsesInput Responses API 的 input 字段
//
//	带图片时 content 是 [{type:"input_text",text}, {type:"input_image",image_url}] 数组
func buildOpenAIResponsesInput(req chatRequest) []map[string]any {
	conv := req.Conv
	supportsPDF := req.Spec.Has(CapPDF)
	msgs := contextMessages(conv)
	out := make([]map[string]any, 0, len(msgs)+1)
	if conv.System != "" {
		out = append(out, map[string]any{"role": "system", "content": conv.System})
	}
	for _, m := range msgs {
		if m.Role == RoleClear {
			continue
		}
		if m.Role == "assistant" && m.Content == "" {
			continue
		}
		if m.Role == "user" && (len(m.Images) > 0 || len(m.Files) > 0) {
			textFiles, binaryFiles := partitionFiles(m.Files, supportsPDF)
			text := userContentWithFileText(m.Content, textFiles)
			parts := []map[string]any{}
			if text != "" {
				parts = append(parts, map[string]any{"type": "input_text", "text": text})
			}
			for _, img := range m.Images {
				parts = append(parts, map[string]any{
					"type":      "input_image",
					"image_url": imageDataURL(img),
				})
			}
			for _, f := range binaryFiles {
				mime := f.MimeType
				if mime == "" {
					mime = "application/pdf"
				}
				parts = append(parts, map[string]any{
					"type":      "input_file",
					"filename":  f.Name,
					"file_data": "data:" + mime + ";base64," + f.Data,
				})
			}
			if len(parts) > 0 {
				out = append(out, map[string]any{"role": m.Role, "content": parts})
				continue
			}
		}
		out = append(out, map[string]any{"role": m.Role, "content": m.Content})
	}
	return out
}

// buildOpenAIChatMessages 经典 Chat Completions 的 messages 数组
//
//	带图片时 content 是 [{type:"text",text}, {type:"image_url",image_url:{url}}] 数组
//	chat-completions 不支持原生 PDF,文件全部走文本拼接(ensureFileText 已确保 Text 存在)
func buildOpenAIChatMessages(req chatRequest) []map[string]any {
	conv := req.Conv
	msgs := contextMessages(conv)
	out := make([]map[string]any, 0, len(msgs)+1)
	if conv.System != "" {
		out = append(out, map[string]any{"role": "system", "content": conv.System})
	}
	for _, m := range msgs {
		if m.Role == RoleClear {
			continue
		}
		if m.Role == "assistant" && m.Content == "" {
			continue
		}
		if m.Role == "user" {
			textFiles, _ := partitionFiles(m.Files, false) // chat-compat 不支持原生 PDF
			text := userContentWithFileText(m.Content, textFiles)
			if len(m.Images) > 0 {
				parts := []map[string]any{}
				if text != "" {
					parts = append(parts, map[string]any{"type": "text", "text": text})
				}
				for _, img := range m.Images {
					parts = append(parts, map[string]any{
						"type":      "image_url",
						"image_url": map[string]string{"url": imageDataURL(img)},
					})
				}
				out = append(out, map[string]any{"role": m.Role, "content": parts})
				continue
			}
			if text != m.Content {
				out = append(out, map[string]any{"role": m.Role, "content": text})
				continue
			}
		}
		out = append(out, map[string]any{"role": m.Role, "content": m.Content})
	}
	return out
}

// imageDataURL 把 ImageBlock 还原成可直接放进 image_url 字段的字符串。
//
//	URL 优先(免去 base64 体积);否则 data:<mime>;base64,<data>
func imageDataURL(img ImageBlock) string {
	if img.URL != "" {
		return img.URL
	}
	mime := img.MimeType
	if mime == "" {
		mime = "image/png"
	}
	return "data:" + mime + ";base64," + img.Data
}

// parseOpenAIResponsesDelta 从 /v1/responses 的事件 payload 抠 (text, thinking) 增量
//
//	text:     response.output_text.delta
//	thinking: response.reasoning_summary_text.delta / response.reasoning.delta
func parseOpenAIResponsesDelta(payload string) (text, thinking string) {
	var ev struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return "", ""
	}
	t := ev.Type
	if strings.HasSuffix(t, "output_text.delta") {
		return ev.Delta, ""
	}
	if strings.Contains(t, "reasoning") && strings.HasSuffix(t, ".delta") {
		return "", ev.Delta
	}
	return "", ""
}

// parseOpenAIChatDelta 从 /v1/chat/completions 的 chunk 抠 (text, thinking) 增量。
//
//	text     = delta.content
//	thinking = delta.reasoning_content (DeepSeek-R1) / delta.reasoning (其他)
func parseOpenAIChatDelta(payload string) (text, thinking string) {
	var ev struct {
		Choices []struct {
			Delta struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				Reasoning        string `json:"reasoning"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return "", ""
	}
	if len(ev.Choices) > 0 {
		d := ev.Choices[0].Delta
		return d.Content, d.ReasoningContent + d.Reasoning
	}
	return "", ""
}

// parseOpenAIChatUsage 从 chat-completions 最后一帧解析 usage
//
//	需要请求里带 stream_options.include_usage = true
func parseOpenAIChatUsage(payload string) *Usage {
	var ev struct {
		Usage *struct {
			PromptTokens        int `json:"prompt_tokens"`
			CompletionTokens    int `json:"completion_tokens"`
			PromptTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionTokensDetails struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil || ev.Usage == nil {
		return nil
	}
	if ev.Usage.PromptTokens == 0 && ev.Usage.CompletionTokens == 0 {
		return nil
	}
	return &Usage{
		InputTokens:     ev.Usage.PromptTokens,
		OutputTokens:    ev.Usage.CompletionTokens,
		CachedTokens:    ev.Usage.PromptTokensDetails.CachedTokens,
		ReasoningTokens: ev.Usage.CompletionTokensDetails.ReasoningTokens,
	}
}

// parseOpenAIResponsesUsage 从 /responses 的 response.completed 事件解析 usage
func parseOpenAIResponsesUsage(payload string) *Usage {
	var ev struct {
		Type     string `json:"type"`
		Response struct {
			Usage *struct {
				InputTokens        int `json:"input_tokens"`
				OutputTokens       int `json:"output_tokens"`
				InputTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"input_tokens_details"`
				OutputTokensDetails struct {
					ReasoningTokens int `json:"reasoning_tokens"`
				} `json:"output_tokens_details"`
			} `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return nil
	}
	// type 形如 "response.completed";包含 usage 即可
	if ev.Response.Usage == nil {
		return nil
	}
	if ev.Response.Usage.InputTokens == 0 && ev.Response.Usage.OutputTokens == 0 {
		return nil
	}
	return &Usage{
		InputTokens:     ev.Response.Usage.InputTokens,
		OutputTokens:    ev.Response.Usage.OutputTokens,
		CachedTokens:    ev.Response.Usage.InputTokensDetails.CachedTokens,
		ReasoningTokens: ev.Response.Usage.OutputTokensDetails.ReasoningTokens,
	}
}

// parseOpenAIResponsesCitations 从 /v1/responses 事件里抠联网搜索的引用来源。
//
//	增量:response.output_text.annotation.added → annotation{type:"url_citation", url, title}
//	收尾:response.completed → response.output[].content[].annotations[]
//
// 两条路都收:部分中转只在收尾那一帧给全量 annotations,不发增量事件。
func parseOpenAIResponsesCitations(payload string) []Citation {
	var ev struct {
		Type       string `json:"type"`
		Annotation *struct {
			Type  string `json:"type"`
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"annotation"`
		Response struct {
			Output []struct {
				Content []struct {
					Annotations []struct {
						Type  string `json:"type"`
						URL   string `json:"url"`
						Title string `json:"title"`
					} `json:"annotations"`
				} `json:"content"`
			} `json:"output"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return nil
	}
	var out []Citation
	if a := ev.Annotation; a != nil && a.URL != "" {
		out = append(out, Citation{URL: a.URL, Title: a.Title})
	}
	for _, item := range ev.Response.Output {
		for _, c := range item.Content {
			for _, a := range c.Annotations {
				if a.URL != "" {
					out = append(out, Citation{URL: a.URL, Title: a.Title})
				}
			}
		}
	}
	return out
}

// parseOpenAIChatCitations 从 chat-completions chunk 抠联网引用。各家写法差异很大:
//
//	OpenAI search-preview  delta.annotations[].url_citation{url,title}
//	智谱 GLM               web_search[]{title,link,content}
//	阿里百炼               search_info.search_results[]{title,url,site_name}
//	Perplexity 风格中转     citations[] 直接是一串 URL 字符串
func parseOpenAIChatCitations(payload string) []Citation {
	var ev struct {
		Choices []struct {
			Delta struct {
				Annotations []struct {
					Type        string `json:"type"`
					URLCitation struct {
						URL   string `json:"url"`
						Title string `json:"title"`
					} `json:"url_citation"`
				} `json:"annotations"`
			} `json:"delta"`
		} `json:"choices"`
		WebSearch []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"web_search"`
		SearchInfo struct {
			SearchResults []struct {
				Title    string `json:"title"`
				URL      string `json:"url"`
				SiteName string `json:"site_name"`
			} `json:"search_results"`
		} `json:"search_info"`
		Citations []string `json:"citations"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return nil
	}
	var out []Citation
	for _, ch := range ev.Choices {
		for _, a := range ch.Delta.Annotations {
			if a.URLCitation.URL != "" {
				out = append(out, Citation{URL: a.URLCitation.URL, Title: a.URLCitation.Title})
			}
		}
	}
	for _, w := range ev.WebSearch {
		url := w.Link
		if url == "" {
			url = w.URL
		}
		if url != "" {
			out = append(out, Citation{URL: url, Title: w.Title, Snippet: w.Content})
		}
	}
	for _, r := range ev.SearchInfo.SearchResults {
		if r.URL == "" {
			continue
		}
		title := r.Title
		if title == "" {
			title = r.SiteName
		}
		out = append(out, Citation{URL: r.URL, Title: title})
	}
	for _, u := range ev.Citations {
		if strings.HasPrefix(u, "http") {
			out = append(out, Citation{URL: u})
		}
	}
	return out
}

// parseOpenAIChatImages 从 chat-completions chunk 抠模型返回的图片。
//
//	兼容多种非标准代理格式:
//	  delta.images:    [{image_url:{url:"..."}} | {url:"..."} | {b64_json:"..."}]
//	  delta.image_url: 单字符串
//	  delta.images_b64:[{mime_type, data}]
//
// 后两种是 grok / xAI 风格的代理常见写法
func parseOpenAIChatImages(payload string) []ImageBlock {
	var ev struct {
		Choices []struct {
			Delta struct {
				Images []struct {
					ImageURL struct {
						URL string `json:"url"`
					} `json:"image_url"`
					URL      string `json:"url"`
					B64JSON  string `json:"b64_json"`
					MimeType string `json:"mime_type"`
					Data     string `json:"data"`
				} `json:"images"`
				ImageURL  string `json:"image_url"`
				ImagesB64 []struct {
					MimeType string `json:"mime_type"`
					Data     string `json:"data"`
				} `json:"images_b64"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return nil
	}
	if len(ev.Choices) == 0 {
		return nil
	}
	d := ev.Choices[0].Delta
	out := make([]ImageBlock, 0, 1)
	for _, im := range d.Images {
		if im.ImageURL.URL != "" {
			out = append(out, fromImageURL(im.ImageURL.URL))
		} else if im.URL != "" {
			out = append(out, fromImageURL(im.URL))
		} else if im.B64JSON != "" {
			out = append(out, ImageBlock{MimeType: "image/png", Data: im.B64JSON})
		} else if im.Data != "" {
			mime := im.MimeType
			if mime == "" {
				mime = "image/png"
			}
			out = append(out, ImageBlock{MimeType: mime, Data: im.Data})
		}
	}
	if d.ImageURL != "" {
		out = append(out, fromImageURL(d.ImageURL))
	}
	for _, im := range d.ImagesB64 {
		mime := im.MimeType
		if mime == "" {
			mime = "image/png"
		}
		out = append(out, ImageBlock{MimeType: mime, Data: im.Data})
	}
	return out
}

// parseOpenAIResponsesImages 从 /v1/responses 事件里抠图(image_generation_call.completed 等)
func parseOpenAIResponsesImages(payload string) []ImageBlock {
	var ev struct {
		Type string `json:"type"`
		// 事件 payload 字段名因版本而异,做几种兼容
		B64JSON  string `json:"b64_json"`
		ImageURL string `json:"image_url"`
		Result   string `json:"result"`
		Image    *struct {
			B64JSON  string `json:"b64_json"`
			URL      string `json:"url"`
			MimeType string `json:"mime_type"`
		} `json:"image"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return nil
	}
	if !strings.Contains(ev.Type, "image_generation") && !strings.Contains(ev.Type, "image.completed") {
		return nil
	}
	out := make([]ImageBlock, 0, 1)
	if ev.B64JSON != "" {
		out = append(out, ImageBlock{MimeType: "image/png", Data: ev.B64JSON})
	}
	if ev.Result != "" {
		// result 有时是裸 base64,有时是 data URL
		out = append(out, parseImageString(ev.Result))
	}
	if ev.ImageURL != "" {
		out = append(out, fromImageURL(ev.ImageURL))
	}
	if ev.Image != nil {
		if ev.Image.B64JSON != "" {
			mime := ev.Image.MimeType
			if mime == "" {
				mime = "image/png"
			}
			out = append(out, ImageBlock{MimeType: mime, Data: ev.Image.B64JSON})
		} else if ev.Image.URL != "" {
			out = append(out, fromImageURL(ev.Image.URL))
		}
	}
	return out
}

// 内联图片:一些中转(如 chatgpt2api 的 gpt-image-2)生图是"异步"完成后,把成品
// 以 markdown ![alt](data:image/png;base64,xxxx) 的形式塞进 delta.content,整张图
// 落在一行 data: 里。下面两个正则把它从正文里剥出来:
//   - reMarkdownDataImage:markdown 包裹形式,捕获括号里的 data URL
//   - reBareDataImage:    裸 data:image 形式(无 markdown 包裹时兜底)
var (
	reMarkdownDataImage = regexp.MustCompile(`!\[[^\]]*\]\((data:image/[A-Za-z0-9.+-]+;base64,[A-Za-z0-9+/=]+)\)`)
	reBareDataImage     = regexp.MustCompile(`data:image/[A-Za-z0-9.+-]+;base64,[A-Za-z0-9+/=]+`)
)

// extractInlineDataImages 从一段(流式)正文里剥离内联的 base64 图片。
//
//	返回 (剔除图片后的正文, 解析出的图片列表)。剔除后若仍有图片,顺手把残留的
//	空白裁掉,避免出现只剩 "\n\n" 的正文被渲染成"(空 Markdown)"。
//	无 base64 时零成本直接返回(快路径)。
func extractInlineDataImages(text string) (string, []ImageBlock) {
	if !strings.Contains(text, ";base64,") {
		return text, nil
	}
	var imgs []ImageBlock
	text = reMarkdownDataImage.ReplaceAllStringFunc(text, func(m string) string {
		if sub := reMarkdownDataImage.FindStringSubmatch(m); len(sub) == 2 {
			imgs = append(imgs, parseImageString(sub[1]))
		}
		return ""
	})
	text = reBareDataImage.ReplaceAllStringFunc(text, func(m string) string {
		imgs = append(imgs, parseImageString(m))
		return ""
	})
	if len(imgs) > 0 {
		text = strings.TrimSpace(text)
	}
	return text, imgs
}

// sniffImagesFromPayload 通用兜底:扫整个 chunk 里任何疑似图片字段。
//
//	兼容奇形怪状的代理:把整段 JSON 反序列化为 map,递归找
//	  - "url" / "image_url" 是 http(s) 或 data: 开头 → 算图片
//	  - "b64_json" / "image_base64" / 长度足够的纯 base64 字符串 → 算 PNG
//	这样即使代理用了我们没显式处理的字段名,也有可能挖出来。
func sniffImagesFromPayload(payload string) []ImageBlock {
	var v any
	if err := json.Unmarshal([]byte(payload), &v); err != nil {
		return nil
	}
	out := []ImageBlock{}
	sniffNode(v, &out)
	return out
}

func sniffNode(node any, out *[]ImageBlock) {
	switch t := node.(type) {
	case map[string]any:
		for k, val := range t {
			if s, ok := val.(string); ok {
				if isImageField(k) {
					if blk := tryImageString(s); blk != nil {
						*out = append(*out, *blk)
					}
				}
			} else {
				sniffNode(val, out)
			}
		}
	case []any:
		for _, e := range t {
			sniffNode(e, out)
		}
	}
}

func isImageField(k string) bool {
	lk := strings.ToLower(k)
	switch lk {
	case "url", "image_url", "image", "b64_json", "image_base64", "data_url":
		return true
	}
	return false
}

func tryImageString(s string) *ImageBlock {
	if len(s) < 32 {
		return nil
	}
	if strings.HasPrefix(s, "data:image/") {
		blk := parseImageString(s)
		return &blk
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		// 简单后缀过滤,避免把任意 URL 都识别成图
		low := strings.ToLower(s)
		for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp", ".gif", "/image", "imagine"} {
			if strings.Contains(low, ext) {
				return &ImageBlock{URL: s}
			}
		}
		return nil
	}
	// 看起来像裸 base64(只含 base64 字符,长度足够)
	if looksLikeBase64(s) {
		return &ImageBlock{MimeType: "image/png", Data: s}
	}
	return nil
}

func looksLikeBase64(s string) bool {
	if len(s) < 200 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '+' || c == '/' || c == '=' || c == '\n' || c == '\r' {
			continue
		}
		return false
	}
	return true
}

// fromImageURL 把字符串还原成 ImageBlock(data: 开头的 → 拆 mime+base64)
func fromImageURL(s string) ImageBlock {
	return parseImageString(s)
}

func parseImageString(s string) ImageBlock {
	if strings.HasPrefix(s, "data:") {
		// data:image/png;base64,xxxx
		semi := strings.IndexByte(s, ';')
		comma := strings.IndexByte(s, ',')
		if semi > 5 && comma > semi {
			return ImageBlock{
				MimeType: s[5:semi],
				Data:     s[comma+1:],
			}
		}
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return ImageBlock{URL: s}
	}
	// 兜底当 base64 处理
	return ImageBlock{MimeType: "image/png", Data: s}
}

// extractErrorMessage 从 OpenAI 错误响应里抠 error.message 或 message
func extractErrorMessage(body []byte) string {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return "(空响应)"
	}
	var wrap struct {
		Error struct {
			Message string `json:"message"`
			Code    any    `json:"code"`
		} `json:"error"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(body, &wrap); err == nil {
		if wrap.Error.Message != "" {
			return wrap.Error.Message
		}
		if wrap.Message != "" {
			return wrap.Message
		}
		if wrap.Msg != "" {
			return wrap.Msg
		}
	}
	// 兜底:返回前 200 字符
	s := string(body)
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// thinkSplitter 增量识别内联思考标签(<think>…</think> 之类),把里面内容路由到 thinking。
// 标签可能被 chunk 切断(比如一个 chunk 是 "<thi" 下一个是 "nk>"),所以需要
// 维护跨 chunk 的 pending 缓冲。
type thinkSplitter struct {
	openTag  string
	closeTag string
	inThink  bool
	pending  string // 上一次 feed 末尾可能是开闭标签前缀的部分,留到下一次拼接再判断
}

// newThinkSplitter 按模型 ID 选标签名(gpt-oss 用 <reasoning>,seed-oss 用 <seed:think>,其余 <think>)
func newThinkSplitter(modelID string) thinkSplitter {
	tag := reasoningTagName(modelID)
	return thinkSplitter{openTag: "<" + tag + ">", closeTag: "</" + tag + ">"}
}

// feed 输入一段文本,返回 (正文, 思考) 增量
func (s *thinkSplitter) feed(chunk string) (text, thinking string) {
	var tb, kb strings.Builder
	input := s.pending + chunk
	s.pending = ""

	i := 0
	for i < len(input) {
		if s.inThink {
			idx := strings.Index(input[i:], s.closeTag)
			if idx == -1 {
				rem := input[i:]
				if k := suffixPrefixOverlap(rem, s.closeTag); k > 0 {
					kb.WriteString(rem[:len(rem)-k])
					s.pending = rem[len(rem)-k:]
				} else {
					kb.WriteString(rem)
				}
				break
			}
			kb.WriteString(input[i : i+idx])
			i += idx + len(s.closeTag)
			s.inThink = false
		} else {
			idx := strings.Index(input[i:], s.openTag)
			if idx == -1 {
				rem := input[i:]
				if k := suffixPrefixOverlap(rem, s.openTag); k > 0 {
					tb.WriteString(rem[:len(rem)-k])
					s.pending = rem[len(rem)-k:]
				} else {
					tb.WriteString(rem)
				}
				break
			}
			tb.WriteString(input[i : i+idx])
			i += idx + len(s.openTag)
			s.inThink = true
		}
	}
	return tb.String(), kb.String()
}

// suffixPrefixOverlap 返回 s 末尾有多大一段是 target 的前缀(用于跨 chunk 缓存部分标签)
func suffixPrefixOverlap(s, target string) int {
	max := len(target) - 1
	if max > len(s) {
		max = len(s)
	}
	for n := max; n > 0; n-- {
		if strings.HasPrefix(target, s[len(s)-n:]) {
			return n
		}
	}
	return 0
}

// prettifyNetErr 把网络/超时错误翻译成中文短句
func prettifyNetErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "deadline exceeded"), strings.Contains(low, "timeout"):
		return "请求超时(15s)"
	case strings.Contains(low, "no such host"):
		return "DNS 解析失败,域名不存在"
	case strings.Contains(low, "connection refused"):
		return "目标拒绝连接"
	case strings.Contains(low, "tls"):
		return "TLS 握手失败: " + msg
	}
	if _, ok := err.(net.Error); ok {
		return "网络错误: " + msg
	}
	return msg
}
