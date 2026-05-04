package aichat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
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

// applyOpenAIHeaders 复刻 Cherry-studio 实测有效的请求头组合(对 OpenAI 官方 / 部分中转都生效)
func applyOpenAIHeaders(req *http.Request, apiKey string) {
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("X-Api-Key", apiKey) // Cherry 给 openai 类 provider 必带这个头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://cherry-ai.com")
	req.Header.Set("X-Title", "Cherry Studio")
	req.Header.Set("User-Agent", "ai-sdk/openai/3.0.53")
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
		if isReasoningModel(modelID) {
			body["reasoning_effort"] = "medium"
		}
	}
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
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
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
func streamOpenAI(ctx context.Context, p Provider, conv Conversation, useResponses bool, cb streamCallbacks) {
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
			"input":  buildOpenAIResponsesInput(conv),
			"stream": true,
		}
	} else {
		url = baseURL + pathChatCompl
		body = map[string]any{
			"model":    conv.ModelID,
			"messages": buildOpenAIChatMessages(conv),
			"stream":   true,
			// stream_options.include_usage:让 chat-completions 在最后一帧返回 usage
			"stream_options": map[string]any{"include_usage": true},
		}
		if isReasoningModel(conv.ModelID) {
			body["reasoning_effort"] = "medium"
		}
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		cb.onError(fmt.Errorf("构造请求失败: %w", err))
		return
	}
	applyOpenAIHeaders(req, p.APIKey)
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
	// 部分中转/Grok 代理把思考内容嵌在 delta.content 的 <think>...</think> 里;
	// 用一个状态机把它拆出来路由到 thinking 通道。仅对 chat completions 启用,
	// /responses 已经按事件类型分开了。
	var splitter thinkSplitter
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
		var text, thinking string
		if useResponses {
			text, thinking = parseOpenAIResponsesDelta(payload)
			if u := parseOpenAIResponsesUsage(payload); u != nil {
				cb.onUsage(*u)
			}
		} else {
			text, thinking = parseOpenAIChatDelta(payload)
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
		}
		if thinking != "" {
			cb.onThinking(thinking)
		}
		if text != "" {
			cb.onText(text)
		}
	}
	if err := scanner.Err(); err != nil {
		cb.onError(fmt.Errorf("读取流失败: %w", err))
		return
	}
	cb.onDone()
}

// buildOpenAIResponsesInput Responses API 的 input 字段:用 [{role, content}] 数组
func buildOpenAIResponsesInput(conv Conversation) []map[string]any {
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
			// 流式占位的 assistant 消息别发出去
			continue
		}
		out = append(out, map[string]any{"role": m.Role, "content": m.Content})
	}
	return out
}

// buildOpenAIChatMessages 经典 Chat Completions 的 messages 数组
func buildOpenAIChatMessages(conv Conversation) []map[string]string {
	msgs := contextMessages(conv)
	out := make([]map[string]string, 0, len(msgs)+1)
	if conv.System != "" {
		out = append(out, map[string]string{"role": "system", "content": conv.System})
	}
	for _, m := range msgs {
		if m.Role == RoleClear {
			continue
		}
		if m.Role == "assistant" && m.Content == "" {
			continue
		}
		out = append(out, map[string]string{"role": m.Role, "content": m.Content})
	}
	return out
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
				InputTokens         int `json:"input_tokens"`
				OutputTokens        int `json:"output_tokens"`
				InputTokensDetails  struct {
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

// isReasoningModel 识别 gpt-5 / o1 / o3 / o4 这类带 reasoning 的模型
func isReasoningModel(modelID string) bool {
	m := strings.ToLower(modelID)
	if strings.HasPrefix(m, "gpt-5") {
		return true
	}
	for _, p := range []string{"o1", "o3", "o4"} {
		if m == p || strings.HasPrefix(m, p+"-") {
			return true
		}
	}
	return false
}

// thinkSplitter 增量识别 <think>...</think>,把里面内容路由到 thinking。
// 标签可能被 chunk 切断(比如一个 chunk 是 "<thi" 下一个是 "nk>"),所以需要
// 维护跨 chunk 的 pending 缓冲。
type thinkSplitter struct {
	inThink bool
	pending string // 上一次 feed 末尾可能是开闭标签前缀的部分,留到下一次拼接再判断
}

const (
	thinkOpenTag  = "<think>"
	thinkCloseTag = "</think>"
)

// feed 输入一段文本,返回 (正文, 思考) 增量
func (s *thinkSplitter) feed(chunk string) (text, thinking string) {
	var tb, kb strings.Builder
	input := s.pending + chunk
	s.pending = ""

	i := 0
	for i < len(input) {
		if s.inThink {
			idx := strings.Index(input[i:], thinkCloseTag)
			if idx == -1 {
				rem := input[i:]
				if k := suffixPrefixOverlap(rem, thinkCloseTag); k > 0 {
					kb.WriteString(rem[:len(rem)-k])
					s.pending = rem[len(rem)-k:]
				} else {
					kb.WriteString(rem)
				}
				break
			}
			kb.WriteString(input[i : i+idx])
			i += idx + len(thinkCloseTag)
			s.inThink = false
		} else {
			idx := strings.Index(input[i:], thinkOpenTag)
			if idx == -1 {
				rem := input[i:]
				if k := suffixPrefixOverlap(rem, thinkOpenTag); k > 0 {
					tb.WriteString(rem[:len(rem)-k])
					s.pending = rem[len(rem)-k:]
				} else {
					tb.WriteString(rem)
				}
				break
			}
			tb.WriteString(input[i : i+idx])
			i += idx + len(thinkOpenTag)
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
