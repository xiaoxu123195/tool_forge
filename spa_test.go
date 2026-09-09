package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// 前端是 BrowserRouter,地址栏会停在 /tools/ai-chat 这种路径上。资源服务器里
// 没有这个文件 —— 少了这层回退,任何一次刷新(或 webview 自己重载)都会得到
// WebView2 的原生 404 页,整个应用卡死在那儿只能重启。
func TestSPAFallback(t *testing.T) {
	cases := []struct {
		in   string
		want string
		why  string
	}{
		{"/tools/ai-chat", "/", "前端路由路径要回退到 index.html"},
		{"/profile", "/", "同上"},
		{"/tools/mobile-forensic/ios", "/", "多级路由也一样"},
		{"/", "/", "根路径本来就对"},
		{"", "", "空路径不动"},
		{"/assets/index-abc123.js", "/assets/index-abc123.js", "真实资源不能被改写"},
		{"/assets/style.css", "/assets/style.css", "同上"},
		{"/favicon.ico", "/favicon.ico", "同上"},
		{"/logo.v2.png", "/logo.v2.png", "文件名里有多个点也是资源"},
	}
	for _, c := range cases {
		var got string
		h := spaFallback(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got = r.URL.Path
		}))
		req := httptest.NewRequest("GET", "http://wails.localhost"+orSlash(c.in), nil)
		req.URL.Path = c.in
		h.ServeHTTP(httptest.NewRecorder(), req)
		if got != c.want {
			t.Errorf("%s: %q → %q, 期望 %q", c.why, c.in, got, c.want)
		}
	}
}

func orSlash(p string) string {
	if p == "" {
		return "/"
	}
	return p
}
