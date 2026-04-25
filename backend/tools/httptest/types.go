// Package httptest 提供一个简易的 HTTP 请求测试器,
// 类似 Postman v1:支持 GET/POST/... + headers + JSON / form / 纯文本 body,
// 历史记录持久化到 ~/.toolforge/http-history.json
package httptest

// KV 通用 key-value 项
type KV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	// Disabled 表示这一行用户暂时关掉了,但保留方便再开启
	Disabled bool `json:"disabled,omitempty"`
}

// BodyMode 请求体模式
type BodyMode string

const (
	BodyNone BodyMode = "none"
	BodyJSON BodyMode = "json"  // application/json
	BodyText BodyMode = "text"  // text/plain
	BodyForm BodyMode = "form"  // application/x-www-form-urlencoded
)

// Request 描述一次要发出的请求
type Request struct {
	Method    string   `json:"method"`
	URL       string   `json:"url"`
	Headers   []KV     `json:"headers"`
	BodyMode  BodyMode `json:"bodyMode"`
	BodyText  string   `json:"bodyText"`
	BodyForm  []KV     `json:"bodyForm"`
	TimeoutMs int      `json:"timeoutMs"` // 0 → 30000ms 默认
}

// Response 是发出请求后回给前端的结构
type Response struct {
	OK           bool     `json:"ok"`
	StatusCode   int      `json:"statusCode"`
	StatusText   string   `json:"statusText"`
	Headers      []KV     `json:"headers"`
	BodyText     string   `json:"bodyText"`     // 已尝试 utf-8 解码后的字符串
	IsBinary     bool     `json:"isBinary"`     // body 包含不可打印字符或 content-type 是二进制
	ContentType  string   `json:"contentType"`
	SizeBytes    int      `json:"sizeBytes"`
	DurationMs   int      `json:"durationMs"`
	Error        string   `json:"error,omitempty"`
	RemoteAddr   string   `json:"remoteAddr,omitempty"`
}

// HistoryItem 历史记录单项
type HistoryItem struct {
	ID         string   `json:"id"`
	SavedAt    int64    `json:"savedAt"` // unix ms
	Request    Request  `json:"request"`
	StatusCode int      `json:"statusCode"`
	DurationMs int      `json:"durationMs"`
	SizeBytes  int      `json:"sizeBytes"`
	Error      string   `json:"error,omitempty"`
}
