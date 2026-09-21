package apitool

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 把一个 Op 变成本地 API server 认识的工具 handler。
//
// 同时实现 ToolHandler 和 SchemaProvider(见 backend/apiserver/types.go),
// 所以它既是一个 HTTP 接口,也会带着完整的入参 schema 出现在 MCP 的 tools/list 里。

const defaultTimeout = 30 * time.Second

// maxRespBytes 响应最多读这么多。接口返回几十 MB 的情况是有的,
// 而这东西的出口是模型的上下文 —— 塞满了不如截断并说清楚
const maxRespBytes = 512 << 10

// secretFn 取包的密钥。做成函数是为了不把密钥存在结构体里 ——
// 它只在发请求的那一刻从凭据库读出来,用完就扔
type secretFn func(packID string) string

// Handler 一个接口对应的工具
type Handler struct {
	pack   Pack
	op     Op
	secret secretFn
	client *http.Client
}

// NewHandler 造一个。pack 里不含密钥,密钥通过 secret 现取
func NewHandler(pack Pack, op Op, secret secretFn) *Handler {
	timeout := defaultTimeout
	if pack.TimeoutSec > 0 {
		timeout = time.Duration(pack.TimeoutSec) * time.Second
	}
	return &Handler{
		pack:   pack,
		op:     op,
		secret: secret,
		client: &http.Client{Timeout: timeout},
	}
}

// ToolName 工具名。带包名前缀,免得两份文档里的同名接口打架;
// 字符集限定成 MCP 和各家模型都认的 [a-z0-9_-]
func ToolName(packName, opID string) string {
	return "api-" + sanitize(packName) + "-" + sanitize(opID)
}

func (h *Handler) Name() string  { return ToolName(h.pack.Name, h.op.ID) }
func (h *Handler) Title() string { return h.pack.Name + " · " + h.opTitle() }

func (h *Handler) opTitle() string {
	if h.op.Summary != "" {
		return h.op.Summary
	}
	return h.op.Method + " " + h.op.Path
}

func (h *Handler) Description() string {
	parts := []string{h.op.Method + " " + h.op.Path}
	if h.op.Summary != "" {
		parts = append(parts, h.op.Summary)
	}
	if h.op.Description != "" {
		parts = append(parts, h.op.Description)
	}
	if h.op.Deprecated {
		parts = append(parts, "（文档标注此接口已废弃）")
	}
	return strings.Join(parts, "：")
}

func (h *Handler) Methods() []string { return []string{http.MethodPost} }

// InputSchema 实现 apiserver.SchemaProvider
func (h *Handler) InputSchema() map[string]any { return h.op.InputSchema() }

// response 统一的返回形状。
// 状态码单独给出来而不是把非 2xx 变成错误:4xx 的响应体里往往写着"为什么不行",
// 那正是调用方(不管是人还是模型)下一步需要的东西
type response struct {
	Status int               `json:"status"`
	URL    string            `json:"url"`
	Header map[string]string `json:"header,omitempty"`
	// Body 响应体。是 JSON 就原样嵌进来,不是就当字符串
	Body json.RawMessage `json:"body,omitempty"`
	Text string          `json:"text,omitempty"`
	// Truncated 响应体太大被截断了。不说的话模型会以为它拿到了完整的
	Truncated bool `json:"truncated,omitempty"`
}

func (h *Handler) Handle(ctx context.Context, body []byte) ([]byte, error) {
	args := map[string]any{}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &args); err != nil {
			return nil, fmt.Errorf("入参不是合法 JSON: %w", err)
		}
	}
	req, err := h.build(ctx, args)
	if err != nil {
		return nil, err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 %s 失败: %w", req.URL.Redacted(), err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读响应失败: %w", err)
	}
	out := response{Status: resp.StatusCode, URL: req.URL.Redacted()}
	if len(raw) > maxRespBytes {
		raw = raw[:maxRespBytes]
		out.Truncated = true
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		out.Header = map[string]string{"Content-Type": ct}
	}
	if json.Valid(raw) && !out.Truncated {
		out.Body = json.RawMessage(raw)
	} else {
		out.Text = string(raw)
	}
	return json.Marshal(out)
}

// build 把入参绑到 URL、query、header、body 上
func (h *Handler) build(ctx context.Context, args map[string]any) (*http.Request, error) {
	base := strings.TrimRight(h.pack.BaseURL, "/")
	if base == "" {
		return nil, fmt.Errorf("这个接口包没有配置请求地址")
	}

	path := h.op.Path
	query := url.Values{}
	header := http.Header{}
	bodyFields := map[string]any{}
	var rawBody any
	var missing []string

	for _, prm := range h.op.Params {
		v, ok := args[prm.ArgName]
		if !ok || v == nil {
			if prm.Required {
				missing = append(missing, prm.ArgName)
			}
			continue
		}
		switch prm.In {
		case InPath:
			// 路径段要转义,否则参数里的 / 会凭空多出一层路径
			path = strings.ReplaceAll(path, "{"+prm.Name+"}", url.PathEscape(scalar(v)))
		case InQuery:
			for _, s := range scalars(v) {
				query.Add(prm.Name, s)
			}
		case InHeader:
			header.Set(prm.Name, scalar(v))
		case InBody:
			if h.op.BodyRaw {
				rawBody = v
			} else {
				bodyFields[prm.Name] = v
			}
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("缺少必填参数: %s", strings.Join(missing, "、"))
	}
	// 模板里没被填上的占位符:发出去会变成字面量 {id},服务端多半回 404,
	// 而那种 404 看不出是自己的问题
	if i := strings.Index(path, "{"); i >= 0 {
		if j := strings.Index(path[i:], "}"); j > 0 {
			return nil, fmt.Errorf("路径参数 %s 没有值", path[i+1:i+j])
		}
	}

	var payload io.Reader
	hasBody := false
	if h.op.BodyRaw && rawBody != nil {
		b, err := json.Marshal(rawBody)
		if err != nil {
			return nil, fmt.Errorf("请求体序列化失败: %w", err)
		}
		payload, hasBody = bytes.NewReader(b), true
	} else if len(bodyFields) > 0 {
		b, err := json.Marshal(bodyFields)
		if err != nil {
			return nil, fmt.Errorf("请求体序列化失败: %w", err)
		}
		payload, hasBody = bytes.NewReader(b), true
	}

	full := base + path
	if _, err := url.Parse(full); err != nil {
		return nil, fmt.Errorf("拼出来的地址不合法: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, h.op.Method, full, payload)
	if err != nil {
		return nil, err
	}
	for k, vs := range header {
		req.Header[k] = vs
	}
	for k, v := range h.pack.Headers {
		req.Header.Set(k, v)
	}
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	h.applyAuth(req, query)

	// 已有的 query(base 或 path 里自带的)要保留
	if existing := req.URL.RawQuery; existing != "" && len(query) > 0 {
		req.URL.RawQuery = existing + "&" + query.Encode()
	} else if len(query) > 0 {
		req.URL.RawQuery = query.Encode()
	}
	return req, nil
}

// applyAuth 密钥到这一刻才从凭据库读出来
func (h *Handler) applyAuth(req *http.Request, query url.Values) {
	if h.pack.Auth.Kind == AuthNone || h.secret == nil {
		return
	}
	secret := h.secret(h.pack.ID)
	if secret == "" {
		return
	}
	switch h.pack.Auth.Kind {
	case AuthBearer:
		req.Header.Set("Authorization", "Bearer "+secret)
	case AuthHeader:
		if h.pack.Auth.Name != "" {
			req.Header.Set(h.pack.Auth.Name, secret)
		}
	case AuthQuery:
		if h.pack.Auth.Name != "" {
			query.Set(h.pack.Auth.Name, secret)
		}
	case AuthBasic:
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(secret)))
	}
}

// scalar 把一个值变成能放进 URL 的字符串
func scalar(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		// JSON 的数字都是 float64。整数别输出成 3.000000
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case nil:
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// scalars 数组型的 query 参数展开成多个同名键
func scalars(v any) []string {
	if a, ok := v.([]any); ok {
		out := make([]string, 0, len(a))
		for _, item := range a {
			out = append(out, scalar(item))
		}
		return out
	}
	return []string{scalar(v)}
}

// sanitize 规整成 [a-z0-9_-]
func sanitize(s string) string {
	out := make([]rune, 0, len(s))
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
			prevDash = false
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
			prevDash = false
		case r == '_' || r == '-':
			out = append(out, r)
			prevDash = false
		default:
			// 连续的非法字符只留一个分隔符,免得 /users/{id} 变成 __users__id_
			if !prevDash && len(out) > 0 {
				out = append(out, '-')
				prevDash = true
			}
		}
	}
	res := strings.Trim(string(out), "-")
	if res == "" {
		return "x"
	}
	return res
}
