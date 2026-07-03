package llmproxy

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeBody(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(`{"data":"hello"}`))
	_ = zw.Close()
	if got := string(decodeBody(buf.Bytes(), "gzip")); got != `{"data":"hello"}` {
		t.Fatalf("gzip 解压失败: %q", got)
	}
	if string(decodeBody([]byte("plain"), "")) != "plain" {
		t.Error("identity 应原样返回")
	}
	if string(decodeBody([]byte("notgzip"), "gzip")) != "notgzip" {
		t.Error("坏 gzip 应回退原样,不应崩")
	}
}

// 上游无视 identity、仍 gzip 返回时,捕获侧应解压成可读 JSON 入库。
func TestForward_GzipDecoded(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var b bytes.Buffer
		zw := gzip.NewWriter(&b)
		_, _ = zw.Write([]byte(`{"object":"list","data":[1,2,3]}`))
		_ = zw.Close()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(200)
		_, _ = w.Write(b.Bytes())
	}))
	defer up.Close()

	st, err := openStore(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{store: st, cfg: Config{MaxBodyKB: 8192, Upstreams: []Upstream{
		{Name: "u", Target: up.URL, OutboundProxy: "direct"},
	}}}

	s.handle(httptest.NewRecorder(), httptest.NewRequest("GET", "/u/v1/models", nil))
	page, _ := st.Query(LogQuery{})
	if page.Total != 1 {
		t.Fatalf("应 1 条,实际 %d", page.Total)
	}
	d, _ := st.Detail(page.Items[0].ID)
	if !strings.Contains(d.RespBody, `"object":"list"`) {
		t.Fatalf("gzip 响应未解压入库: %q", d.RespBody)
	}
}

// 端到端:流式响应应被边流边回填,最终落库为 200 + 合并文本,而不是卡在"进行中"。
func TestForward_StreamingLogged(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		for _, c := range []string{"Hello", " world"} {
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"" + c + "\"}}]}\n\n"))
			if fl != nil {
				fl.Flush()
			}
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if fl != nil {
			fl.Flush()
		}
	}))
	defer up.Close()

	st, err := openStore(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{store: st, cfg: Config{MaxBodyKB: 8192, Upstreams: []Upstream{
		{Name: "u", Target: up.URL, OutboundProxy: "direct"},
	}}}

	req := httptest.NewRequest("POST", "/u/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	rec := httptest.NewRecorder()
	s.handle(rec, req)

	if rec.Code != 200 {
		t.Fatalf("客户端收到 code=%d", rec.Code)
	}
	page, _ := st.Query(LogQuery{})
	if page.Total != 1 {
		t.Fatalf("应有 1 条日志,实际 %d", page.Total)
	}
	e := page.Items[0]
	if e.Status != 200 {
		t.Fatalf("不应卡在进行中,status=%d", e.Status)
	}
	if !e.Stream {
		t.Errorf("应标记为流式")
	}
	d, _ := st.Detail(e.ID)
	if d.RespBody != "Hello world" {
		t.Errorf("合并文本=%q,want %q", d.RespBody, "Hello world")
	}
}

// 端到端:非流式响应正常落库 + 提取 usage。
func TestForward_NonStreamLogged(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"model":"gpt-x","usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`))
	}))
	defer up.Close()

	st, err := openStore(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{store: st, cfg: Config{MaxBodyKB: 8192, Upstreams: []Upstream{
		{Name: "u", Target: up.URL, OutboundProxy: "direct"},
	}}}

	req := httptest.NewRequest("POST", "/u/v1/chat", strings.NewReader(`{"model":"gpt-x"}`))
	s.handle(httptest.NewRecorder(), req)

	page, _ := st.Query(LogQuery{})
	if page.Total != 1 || page.Items[0].Status != 200 || page.Items[0].TotalTokens != 7 {
		t.Fatalf("非流式落库错: %+v", page.Items)
	}
	if page.Items[0].Model != "gpt-x" {
		t.Errorf("model=%q", page.Items[0].Model)
	}
}

func TestSplitUpstream(t *testing.T) {
	cases := []struct{ in, name, rest string }{
		{"/openai/v1/chat/completions", "openai", "/v1/chat/completions"},
		{"/openai", "openai", "/"},
		{"/openai/", "openai", "/"},
		{"/", "", "/"},
	}
	for _, c := range cases {
		n, r := splitUpstream(c.in)
		if n != c.name || r != c.rest {
			t.Errorf("splitUpstream(%q)=(%q,%q),want (%q,%q)", c.in, n, r, c.name, c.rest)
		}
	}
}

func TestSingleJoinPath(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"", "/v1/x", "/v1/x"},
		{"/", "/v1/x", "/v1/x"},
		{"/base", "/v1/x", "/base/v1/x"},
		{"/base/", "/v1/x", "/base/v1/x"},
	}
	for _, c := range cases {
		if got := singleJoinPath(c.a, c.b); got != c.want {
			t.Errorf("singleJoinPath(%q,%q)=%q,want %q", c.a, c.b, got, c.want)
		}
	}
}

func TestMaskSecret_NeverLeaksFullKey(t *testing.T) {
	full := "Bearer sk-proj-ABCDEFGHIJKLMNOP1234567890"
	masked := maskSecret(full)
	if strings.Contains(masked, "ABCDEFGHIJKLMNOP") {
		t.Fatalf("脱敏后仍含密钥主体: %s", masked)
	}
	if !strings.HasPrefix(masked, "Bearer ") {
		t.Errorf("应保留 Bearer 前缀: %s", masked)
	}
	if !strings.HasSuffix(masked, "7890") {
		t.Errorf("应保留尾部指纹: %s", masked)
	}
}

func TestRedactHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer sk-verysecretkey-1234")
	h.Set("Content-Type", "application/json")
	h.Set("X-Api-Key", "anthropic-secret-key-9999")
	out := redactHeaders(h)
	if strings.Contains(out["Authorization"], "verysecretkey") {
		t.Errorf("Authorization 未脱敏: %s", out["Authorization"])
	}
	if strings.Contains(out["X-Api-Key"], "anthropic-secret-key") {
		t.Errorf("X-Api-Key 未脱敏: %s", out["X-Api-Key"])
	}
	if out["Content-Type"] != "application/json" {
		t.Errorf("普通头不应改动: %s", out["Content-Type"])
	}
}

func TestMergeSSE(t *testing.T) {
	openai := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n" +
		"data: [DONE]\n\n"
	if got := mergeSSE(openai); got != "Hello world" {
		t.Errorf("OpenAI 合并=%q,want %q", got, "Hello world")
	}
	anthropic := "event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"你\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"好\"}}\n\n"
	if got := mergeSSE(anthropic); got != "你好" {
		t.Errorf("Anthropic 合并=%q,want %q", got, "你好")
	}
	// OpenAI Responses API:delta 为字符串,且要跳过 reasoning / 工具入参增量
	responses := "event: response.output_text.delta\n" +
		"data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"思考\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hi\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\" there\"}\n\n" +
		"data: {\"type\":\"response.function_call_arguments.delta\",\"delta\":\"{\\\"a\\\":1}\"}\n\n"
	if got := mergeSSE(responses); got != "Hi there" {
		t.Errorf("Responses 合并=%q,want %q", got, "Hi there")
	}
}

func TestExtractUsageAndModel(t *testing.T) {
	resp := `{"model":"gpt-4o","usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
	if m := extractModel("", resp); m != "gpt-4o" {
		t.Errorf("model=%q", m)
	}
	p, c, total := extractUsage(resp, false)
	if p != 10 || c != 5 || total != 15 {
		t.Errorf("usage=(%d,%d,%d)", p, c, total)
	}
	// Anthropic 风格 + total 缺省自动相加
	ant := `{"usage":{"input_tokens":20,"output_tokens":8}}`
	p, c, total = extractUsage(ant, false)
	if p != 20 || c != 8 || total != 28 {
		t.Errorf("anthropic usage=(%d,%d,%d)", p, c, total)
	}
	// OpenAI Responses API:usage 在流式 response.completed 的 response 里
	respStream := "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":100,\"output_tokens\":40,\"total_tokens\":140}}}\n\n"
	p, c, total = extractUsage(respStream, true)
	if p != 100 || c != 40 || total != 140 {
		t.Errorf("responses usage=(%d,%d,%d)", p, c, total)
	}
}

// 两段式写入:到达即插入(status=0 进行中),响应结束再回填。
func TestStore_InsertThenUpdate(t *testing.T) {
	db := filepath.Join(t.TempDir(), "logs.db")
	st, err := openStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	id, err := st.Insert(&capture{
		ts: 1000, upstream: "grok", method: "POST", path: "/v1/chat/completions",
		reqBody: `{"model":"grok-4"}`, reqBytes: 18,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 到达时:status=0,列表里应可见(进行中)
	d, _ := st.Detail(id)
	if d.Entry.Status != 0 || d.ReqBody == "" {
		t.Fatalf("到达态错: status=%d reqBody=%q", d.Entry.Status, d.ReqBody)
	}

	// 回填响应
	if err := st.Update(id, &capture{
		status: 200, durationMs: 850, stream: true,
		respBody: "data: x\n\n", respMerged: "你好", respBytes: 9,
		model: "grok-4", promptTok: 12, completeTok: 30, totalTok: 42,
		respHeaders: map[string]string{"Content-Type": "text/event-stream"},
	}); err != nil {
		t.Fatal(err)
	}
	d, _ = st.Detail(id)
	if d.Entry.Status != 200 || d.Entry.TotalTokens != 42 || !d.Entry.Stream {
		t.Fatalf("回填后 entry 错: %+v", d.Entry)
	}
	if d.RespBody != "你好" || d.RespRaw != "data: x\n\n" {
		t.Fatalf("回填后响应错: body=%q raw=%q", d.RespBody, d.RespRaw)
	}
	// 仍只有 1 条(是更新不是新增)
	page, _ := st.Query(LogQuery{})
	if page.Total != 1 {
		t.Fatalf("应仍只有 1 条,实际 %d", page.Total)
	}
}

func TestStore_Roundtrip(t *testing.T) {
	db := filepath.Join(t.TempDir(), "logs.db")
	st, err := openStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// 非流
	id1, err := st.Insert(&capture{
		ts: 1000, upstream: "openai", method: "POST", path: "/v1/chat",
		status: 200, durationMs: 120, reqBody: `{"model":"gpt-4o"}`,
		respBody: `{"ok":true}`, model: "gpt-4o", totalTok: 15,
		reqHeaders: map[string]string{"Content-Type": "application/json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 流
	id2, err := st.Insert(&capture{
		ts: 2000, upstream: "anthropic", method: "POST", path: "/v1/messages",
		status: 200, stream: true, respBody: "data: raw\n\n", respMerged: "merged text",
	})
	if err != nil {
		t.Fatal(err)
	}

	page, err := st.Query(LogQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("应有 2 条,实际 total=%d items=%d", page.Total, len(page.Items))
	}
	// 倒序:最新(id2)在前
	if page.Items[0].ID != id2 {
		t.Errorf("应按 id 倒序,首条=%d", page.Items[0].ID)
	}

	// 过滤 upstream
	page, _ = st.Query(LogQuery{Upstream: "openai"})
	if page.Total != 1 || page.Items[0].ID != id1 {
		t.Errorf("upstream 过滤失败: %+v", page)
	}

	// 详情:非流 RespBody=原始
	d1, err := st.Detail(id1)
	if err != nil {
		t.Fatal(err)
	}
	if d1.RespBody != `{"ok":true}` || d1.RespRaw != "" {
		t.Errorf("非流详情错: body=%q raw=%q", d1.RespBody, d1.RespRaw)
	}
	if d1.ReqHeaders["Content-Type"] != "application/json" {
		t.Errorf("头未还原: %+v", d1.ReqHeaders)
	}
	// 详情:流 RespBody=合并, RespRaw=原始
	d2, _ := st.Detail(id2)
	if d2.RespBody != "merged text" || d2.RespRaw != "data: raw\n\n" {
		t.Errorf("流详情错: body=%q raw=%q", d2.RespBody, d2.RespRaw)
	}

	// 删除 + 清理
	if err := st.Delete(id1); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.Purge(1500); n != 0 { // id2 ts=2000 > 1500,不删
		t.Errorf("purge 误删: %d", n)
	}
	page, _ = st.Query(LogQuery{})
	if page.Total != 1 {
		t.Errorf("删除后应剩 1 条,实际 %d", page.Total)
	}
}
