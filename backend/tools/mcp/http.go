package mcp

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
	"sync"
	"time"

	"github.com/mattn/go-ieproxy"
)

// Streamable HTTP 传输:每条 JSON-RPC 请求 POST 一次。
//
// 响应有两种形态,取决于服务器:
//
//	application/json    直接就是一条 JSON-RPC 响应
//	text/event-stream   一串 SSE,要读到 id 对得上的那条为止
//
// 旧的 HTTP+SSE 双端点传输(单独一个 GET /sse 拿回信通道)已经废弃,不做兼容。

var httpClient = &http.Client{
	Timeout: 120 * time.Second,
	Transport: &http.Transport{
		Proxy: ieproxy.GetProxyFunc(),
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
	},
}

type httpTransport struct {
	url     string
	headers map[string]string
	ids     idGen

	mu sync.Mutex
	// sessionID 服务器在 initialize 响应头里给的,后续请求要带回去
	sessionID string
}

func newHTTPTransport(s Server) (transport, error) {
	url := strings.TrimSpace(s.URL)
	if url == "" {
		return nil, fmt.Errorf("未指定服务器地址")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("地址必须以 http:// 或 https:// 开头")
	}
	return &httpTransport{url: url, headers: s.Headers}, nil
}

func (t *httpTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := t.ids.next()
	resp, err := t.post(ctx, rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params}, id)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp.Result, nil
}

func (t *httpTransport) notify(ctx context.Context, method string, params any) error {
	_, err := t.post(ctx, rpcRequest{JSONRPC: "2.0", Method: method, Params: params}, -1)
	return err
}

// post 发一条请求。wantID < 0 表示这是通知,不等回应。
func (t *httpTransport) post(ctx context.Context, req rpcRequest, wantID int64) (*rpcResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// 两种都收 —— 服务器挑一种回
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}
	t.mu.Lock()
	if t.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	t.mu.Unlock()

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 会话 id 只在 initialize 的响应头里出现一次,之后每条请求都要带
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.mu.Lock()
		t.sessionID = sid
		t.mu.Unlock()
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	// 通知没有响应体(202 Accepted),不用解析
	if wantID < 0 {
		return nil, nil
	}

	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return readSSEResponse(resp.Body, wantID)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	var out rpcResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("响应解析失败: %w", err)
	}
	return &out, nil
}

// readSSEResponse 从 SSE 流里读到 id 对得上的那条响应。
// 中间可能夹着服务器推的通知(进度之类),按 id 过滤掉。
func readSSEResponse(body io.Reader, wantID int64) (*rpcResponse, error) {
	r := bufio.NewReaderSize(body, 64*1024)
	for {
		line, err := r.ReadString('\n')
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			if payload != "" {
				var out rpcResponse
				if json.Unmarshal([]byte(payload), &out) == nil &&
					out.ID != nil && *out.ID == wantID {
					return &out, nil
				}
			}
		}
		if err != nil {
			return nil, fmt.Errorf("SSE 流结束但没等到对应的响应")
		}
	}
}

// close HTTP 传输没有常驻连接要关
func (t *httpTransport) close() error { return nil }
