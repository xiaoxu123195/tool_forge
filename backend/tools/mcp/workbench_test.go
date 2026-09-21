package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeTransport 一个可编排响应的假服务器
type fakeTransport struct {
	// replies method → 原始 result
	replies map[string]string
	// fails method → 要返回的错误
	fails  map[string]error
	calls  []string
	closed bool
}

func (f *fakeTransport) call(_ context.Context, method string, _ any) (json.RawMessage, error) {
	f.calls = append(f.calls, method)
	if err, ok := f.fails[method]; ok {
		return nil, err
	}
	if s, ok := f.replies[method]; ok {
		return json.RawMessage(s), nil
	}
	return json.RawMessage(`{}`), nil
}
func (f *fakeTransport) notify(context.Context, string, any) error { return nil }
func (f *fakeTransport) close() error                              { f.closed = true; return nil }

// wbWith 把一个假连接直接塞进工作台,跳过真实建连
func wbWith(t *testing.T, ft *fakeTransport) (*Workbench, Server) {
	t.Helper()
	srv := Server{Name: "fake", Kind: TransportStdio, Command: "fake"}
	w := NewWorkbench()
	// last 必须给,否则零值时间会被闲置回收当场清掉,然后真的去执行 "fake"
	w.conns[Fingerprint(srv)] = &wbConn{c: &client{tr: ft}, srv: srv, last: time.Now()}
	return w, srv
}

// 指纹只认"连到哪儿":改显示名不该断开重连,改命令必须换一条连接
func TestFingerprintIgnoresDisplayNameOnly(t *testing.T) {
	a := Server{Name: "甲", Kind: TransportStdio, Command: "npx", Args: []string{"-y", "x"}}
	b := a
	b.Name = "乙"
	if Fingerprint(a) != Fingerprint(b) {
		t.Error("只改了显示名,不该换一条连接")
	}
	c := a
	c.Args = []string{"-y", "y"}
	if Fingerprint(a) == Fingerprint(c) {
		t.Error("参数变了却还是同一条连接")
	}
	// map 无序,同一份配置每次算出来必须一样
	d := Server{Name: "甲", Kind: TransportStdio, Command: "npx", Args: []string{"-y", "x"},
		Env: map[string]string{"A": "1", "B": "2", "C": "3"}}
	e := Server{Name: "甲", Kind: TransportStdio, Command: "npx", Args: []string{"-y", "x"},
		Env: map[string]string{"C": "3", "B": "2", "A": "1"}}
	for i := 0; i < 20; i++ {
		if Fingerprint(d) != Fingerprint(e) {
			t.Fatal("同一份 env 因为 map 顺序算出了不同的指纹")
		}
	}
}

// 工具拉不出来才算失败;prompts/resources 没实现是常态,不能报成失败
func TestInspectToleratesMissingOptionalCapabilities(t *testing.T) {
	ft := &fakeTransport{
		replies: map[string]string{
			"tools/list": `{"tools":[{"name":"t1","description":"d","inputSchema":{"type":"object"}}]}`,
		},
		fails: map[string]error{
			"prompts/list":   &rpcError{Code: -32601, Message: "Method not found"},
			"resources/list": &rpcError{Code: -32601, Message: "Method not found"},
		},
	}
	w, srv := wbWith(t, ft)
	res, err := w.Inspect(context.Background(), srv)
	if err != nil {
		t.Fatalf("只是没实现可选能力,不该失败: %v", err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "t1" {
		t.Fatalf("工具没拉对: %+v", res.Tools)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("「没实现」不该报警,那是常态: %v", res.Warnings)
	}
	// 空数组不能是 nil,前端直接 .map
	if res.Prompts == nil || res.Resources == nil {
		t.Error("空列表该是数组而不是 null")
	}
}

// 可选能力挂在别的原因上,要说出来 —— 那可能是真出了问题
func TestInspectWarnsOnRealFailure(t *testing.T) {
	ft := &fakeTransport{
		replies: map[string]string{"tools/list": `{"tools":[]}`},
		fails:   map[string]error{"prompts/list": errors.New("连接被重置")},
	}
	w, srv := wbWith(t, ft)
	res, _ := w.Inspect(context.Background(), srv)
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "连接被重置") {
		t.Errorf("真失败该报出来: %v", res.Warnings)
	}
}

// 工具列表拉不出来就是连接不可用,要断开让下次重连
func TestInspectDropsConnectionWhenToolsFail(t *testing.T) {
	ft := &fakeTransport{fails: map[string]error{"tools/list": errors.New("boom")}}
	w, srv := wbWith(t, ft)
	if _, err := w.Inspect(context.Background(), srv); err == nil {
		t.Fatal("工具拉不出来该报错")
	}
	if !ft.closed {
		t.Error("失败后该断开连接")
	}
	if len(w.conns) != 0 {
		t.Error("坏掉的连接还留在池子里")
	}
}

// 调用要把来回两边的原文都留下 —— 这一页存在的理由就是这个
func TestCallToolKeepsBothRawSides(t *testing.T) {
	ft := &fakeTransport{
		replies: map[string]string{
			"tools/call": `{"content":[{"type":"text","text":"命中 3 个"}],"isError":false}`,
		},
	}
	w, srv := wbWith(t, ft)
	res, err := w.CallTool(context.Background(), srv, "search", map[string]any{"q": "微信"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Request, `"微信"`) {
		t.Errorf("发出去的参数没留下: %s", res.Request)
	}
	if !strings.Contains(res.Response, `"content"`) {
		t.Errorf("原始响应没留下: %s", res.Response)
	}
	if res.Text != "命中 3 个" {
		t.Errorf("提取的文本不对: %q", res.Text)
	}
	if res.IsError || res.Error != "" {
		t.Errorf("这次是成功的: %+v", res)
	}
	// 中文不能被转义成 \u
	if strings.Contains(res.Request, `\u`) {
		t.Errorf("中文被转义了,界面上会变成乱码: %s", res.Request)
	}
}

// 「工具自己说失败」和「协议层失败」是两回事,查的方向完全不同,不能混
func TestCallDistinguishesToolErrorFromProtocolError(t *testing.T) {
	ft := &fakeTransport{
		replies: map[string]string{
			"tools/call": `{"content":[{"type":"text","text":"文件不存在"}],"isError":true}`,
		},
	}
	w, srv := wbWith(t, ft)
	res, _ := w.CallTool(context.Background(), srv, "read", nil)
	if !res.IsError {
		t.Error("工具报告的失败没认出来")
	}
	if res.Error != "" {
		t.Errorf("协议层是成功的,Error 该为空: %q", res.Error)
	}
	if res.Text != "文件不存在" {
		t.Errorf("失败时的正文也要给出来: %q", res.Text)
	}

	ft2 := &fakeTransport{fails: map[string]error{"tools/call": &rpcError{Code: -32602, Message: "Invalid params"}}}
	w2, srv2 := wbWith(t, ft2)
	res2, err := w2.CallTool(context.Background(), srv2, "read", nil)
	if err != nil {
		t.Fatal("协议层失败也该返回结果对象,而不是抛出去 —— 抛了界面上就只剩一行红字")
	}
	if res2.RPCCode != -32602 || res2.Error == "" {
		t.Errorf("JSON-RPC 错误码没记下来: %+v", res2)
	}
	if res2.Request == "" {
		t.Error("失败时更要看到发出去的是什么")
	}
	// rpcError 是服务器答复的,连接本身还好,不该断
	if ft2.closed {
		t.Error("协议层报错不代表连接坏了,不该断开")
	}
}

// 传输层坏了要断开,否则后面每一次调用都失败
func TestCallDropsConnectionOnTransportError(t *testing.T) {
	ft := &fakeTransport{fails: map[string]error{"tools/call": errors.New("broken pipe")}}
	w, srv := wbWith(t, ft)
	if _, err := w.CallTool(context.Background(), srv, "x", nil); err != nil {
		t.Fatal(err)
	}
	if !ft.closed || len(w.conns) != 0 {
		t.Error("传输层坏了该断开重来")
	}
}

// 三种方法的响应形状不同,都要能取出文本
func TestExtractContentAcrossShapes(t *testing.T) {
	cases := []struct{ name, raw, want string }{
		{"tools/call", `{"content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}`, "a\nb"},
		{"resources/read", `{"contents":[{"uri":"file:///x","text":"hi"}]}`, "hi"},
		{"prompts/get", `{"messages":[{"role":"user","content":{"type":"text","text":"问题"}}]}`, "user: 问题"},
		{"非文本块", `{"content":[{"type":"image","mimeType":"image/png","blob":"AAAA"}]}`, "(image/png 二进制内容,4 字节 base64)"},
		{"资源引用", `{"content":[{"type":"resource","uri":"file:///a"}]}`, "(资源引用: file:///a)"},
		{"读不懂的形状", `{"weird":1}`, ""},
	}
	for _, c := range cases {
		got, _ := extractContent(json.RawMessage(c.raw))
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// 带游标的列表要翻到底,游标不动时要停下来
func TestPagedStopsOnRepeatedCursor(t *testing.T) {
	ft := &fakeTransport{replies: map[string]string{
		"prompts/list": `{"prompts":[{"name":"p1"}],"nextCursor":"same"}`,
	}}
	c := &client{tr: ft}
	got, err := c.listPrompts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// 游标一直是 "same":第一次请求返回 same,第二次还是 same → 停
	if len(ft.calls) > 3 {
		t.Errorf("游标不动却一直翻页,请求了 %d 次", len(ft.calls))
	}
	if len(got) == 0 {
		t.Error("该拿到第一页的内容")
	}
}
