package httptest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout = 30 * time.Second
	historyCap     = 50
	bodyMaxSize    = 4 * 1024 * 1024 // 4 MB,超出截断防止前端 OOM
)

// Service 是 HTTP 测试器的状态持有者(历史记录持久化)
type Service struct {
	mu      sync.Mutex
	dir     string
	file    string
	history []HistoryItem
	client  *http.Client
}

// New 创建 service。configDir 是 ~/.toolforge,文件落在 configDir/http-history.json
func New() (*Service, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".toolforge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Service{
		dir:    dir,
		file:   filepath.Join(dir, "http-history.json"),
		client: &http.Client{},
	}
	s.loadHistory()
	return s, nil
}

// Send 发出一次 HTTP 请求并返回响应,同时把这一笔记录到历史里
func (s *Service) Send(req Request) Response {
	start := time.Now()
	resp := Response{}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}
	rawURL := strings.TrimSpace(req.URL)
	if rawURL == "" {
		resp.Error = "URL 不能为空"
		s.addHistoryFor(req, resp)
		return resp
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "http://" + rawURL
	}
	if _, err := url.Parse(rawURL); err != nil {
		resp.Error = "URL 解析失败: " + err.Error()
		s.addHistoryFor(req, resp)
		return resp
	}

	// 构造 body + 自动 Content-Type
	var body io.Reader
	autoCT := ""
	switch req.BodyMode {
	case BodyJSON:
		if strings.TrimSpace(req.BodyText) != "" {
			body = strings.NewReader(req.BodyText)
		}
		autoCT = "application/json; charset=utf-8"
	case BodyText:
		if req.BodyText != "" {
			body = strings.NewReader(req.BodyText)
		}
		autoCT = "text/plain; charset=utf-8"
	case BodyForm:
		values := url.Values{}
		for _, kv := range req.BodyForm {
			if kv.Disabled || kv.Key == "" {
				continue
			}
			values.Add(kv.Key, kv.Value)
		}
		if encoded := values.Encode(); encoded != "" {
			body = strings.NewReader(encoded)
		}
		autoCT = "application/x-www-form-urlencoded"
	}

	timeout := defaultTimeout
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		resp.Error = "构造请求失败: " + err.Error()
		s.addHistoryFor(req, resp)
		return resp
	}

	// 用户传的 headers 优先级最高,只有当用户没指定 Content-Type 时才填 autoCT
	hasCT := false
	for _, kv := range req.Headers {
		if kv.Disabled || kv.Key == "" {
			continue
		}
		if strings.EqualFold(kv.Key, "content-type") {
			hasCT = true
		}
		httpReq.Header.Add(kv.Key, kv.Value)
	}
	if autoCT != "" && !hasCT && body != nil {
		httpReq.Header.Set("Content-Type", autoCT)
	}
	if httpReq.Header.Get("User-Agent") == "" {
		httpReq.Header.Set("User-Agent", "Tool-Forge HTTP Tester / 1.0")
	}

	httpResp, err := s.client.Do(httpReq)
	if err != nil {
		resp.Error = err.Error()
		resp.DurationMs = int(time.Since(start).Milliseconds())
		s.addHistoryFor(req, resp)
		return resp
	}
	defer httpResp.Body.Close()

	limited := io.LimitReader(httpResp.Body, bodyMaxSize+1)
	bodyBytes, _ := io.ReadAll(limited)
	truncated := false
	if len(bodyBytes) > bodyMaxSize {
		bodyBytes = bodyBytes[:bodyMaxSize]
		truncated = true
	}

	contentType := httpResp.Header.Get("Content-Type")
	isBinary := isBinaryBody(contentType, bodyBytes)
	bodyStr := ""
	if isBinary {
		bodyStr = fmt.Sprintf("(二进制内容,共 %d 字节,未展示)", len(bodyBytes))
	} else {
		bodyStr = string(bodyBytes)
		if truncated {
			bodyStr += fmt.Sprintf("\n\n…(已截断,响应体超过 %d MB)", bodyMaxSize/1024/1024)
		}
	}

	resp.OK = httpResp.StatusCode >= 200 && httpResp.StatusCode < 400
	resp.StatusCode = httpResp.StatusCode
	resp.StatusText = httpResp.Status
	resp.Headers = headersToKVs(httpResp.Header)
	resp.BodyText = bodyStr
	resp.IsBinary = isBinary
	resp.ContentType = contentType
	resp.SizeBytes = len(bodyBytes)
	resp.DurationMs = int(time.Since(start).Milliseconds())

	s.addHistoryFor(req, resp)
	return resp
}

// History 返回当前历史(最新在前)
func (s *Service) History() []HistoryItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]HistoryItem, len(s.history))
	copy(out, s.history)
	return out
}

// DeleteHistory 删除单条
func (s *Service) DeleteHistory(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.history[:0]
	for _, it := range s.history {
		if it.ID == id {
			continue
		}
		out = append(out, it)
	}
	s.history = out
	s.persistLocked()
}

// ClearHistory 清空所有历史
func (s *Service) ClearHistory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = nil
	s.persistLocked()
}

// addHistoryFor 把一次请求 + 响应记录到历史(最多 historyCap 条)
func (s *Service) addHistoryFor(req Request, resp Response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := HistoryItem{
		ID:         randID(),
		SavedAt:    time.Now().UnixMilli(),
		Request:    req,
		StatusCode: resp.StatusCode,
		DurationMs: resp.DurationMs,
		SizeBytes:  resp.SizeBytes,
		Error:      resp.Error,
	}
	s.history = append([]HistoryItem{item}, s.history...)
	if len(s.history) > historyCap {
		s.history = s.history[:historyCap]
	}
	s.persistLocked()
}

func (s *Service) persistLocked() {
	if s.file == "" {
		return
	}
	data, _ := json.MarshalIndent(s.history, "", "  ")
	_ = os.WriteFile(s.file, data, 0o644)
}

func (s *Service) loadHistory() {
	data, err := os.ReadFile(s.file)
	if err != nil {
		return
	}
	var saved []HistoryItem
	if err := json.Unmarshal(data, &saved); err != nil {
		return
	}
	s.history = saved
}

// ---------- helpers ----------

func headersToKVs(h http.Header) []KV {
	out := make([]KV, 0, len(h))
	for k, vs := range h {
		out = append(out, KV{Key: k, Value: strings.Join(vs, ", ")})
	}
	return out
}

func isBinaryBody(contentType string, body []byte) bool {
	ct := strings.ToLower(contentType)
	if ct != "" {
		// 常见文本类
		if strings.HasPrefix(ct, "text/") {
			return false
		}
		if strings.Contains(ct, "json") || strings.Contains(ct, "xml") ||
			strings.Contains(ct, "javascript") || strings.Contains(ct, "yaml") ||
			strings.Contains(ct, "html") || strings.Contains(ct, "form-urlencoded") {
			return false
		}
		// 明显二进制
		if strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "audio/") ||
			strings.HasPrefix(ct, "video/") || strings.HasPrefix(ct, "application/octet-stream") ||
			strings.HasPrefix(ct, "application/pdf") || strings.HasPrefix(ct, "application/zip") ||
			strings.HasPrefix(ct, "application/x-protobuf") {
			return true
		}
	}
	// 兜底:扫前 1KB,如果有大量不可打印字符就当作二进制
	check := body
	if len(check) > 1024 {
		check = check[:1024]
	}
	bad := 0
	for _, b := range check {
		if b == 0 {
			return true
		}
		if b < 0x09 || (b > 0x0d && b < 0x20) {
			bad++
		}
	}
	return bad > len(check)/8
}

func randID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

