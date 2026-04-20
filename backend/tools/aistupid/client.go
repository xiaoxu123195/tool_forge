package aistupid

import (
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

const (
	endpoint = "https://aistupidlevel.info/api/drift/batch"
	referer  = "https://aistupidlevel.info/?mode=drift&period=latest&sortBy=combined"
	// 浏览器 UA；站方会基于 header 组合做软性校验，保持和已验证的 batch.go 一致。
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"
)

// httpClient 独立 http.Client，代理解析优先读 Windows 注册表 / macOS 系统代理，
// 兜底走环境变量。这样 Clash、V2rayN 等打开"系统代理"后 Tool Forge 直接可用，
// 无需用户再手动设 HTTPS_PROXY。
var httpClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		Proxy: ieproxy.GetProxyFunc(),
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
	},
}

// FetchDrift 拉取最新的漂移批量数据。遇到网络错误或非 2xx 状态码会返回 error。
func FetchDrift(ctx context.Context) (*DriftBatchResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Referer", referer)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("sec-ch-ua", `"Google Chrome";v="147", "Not.A/Brand";v="8", "Chromium";v="147"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, wrapNetErr(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var out DriftBatchResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	out.FetchedAt = time.Now().UTC().Format(time.RFC3339)
	return &out, nil
}

// wrapNetErr 对常见的网络错误追加一句中文提示，方便用户自诊断。
// 站方在部分地区需要科学上网；出现 TLS / dial 超时基本都是代理没开。
func wrapNetErr(err error) error {
	low := strings.ToLower(err.Error())
	needsProxy := strings.Contains(low, "tls handshake timeout") ||
		strings.Contains(low, "i/o timeout") ||
		strings.Contains(low, "no such host") ||
		strings.Contains(low, "connection refused") ||
		strings.Contains(low, "connectex") ||
		strings.Contains(low, "network is unreachable")
	if needsProxy {
		return fmt.Errorf("%w（若处于受限网络，请确认系统代理已开启）", err)
	}
	return err
}
