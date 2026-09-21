package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// MCP 工作台:连上任意一个服务器,看清它提供什么,手动调一次,看到原始报文。
//
// 和 Service 分开是有意的。Service 管的是"给 AI 问答用的那批常驻连接",
// 它的 CallTool 把结果压成一段文本喂给模型 —— 那正是调试时最不想要的:
// 报错的结构、非文本内容、isError 标记全在压扁的过程中没了。
// 这里反过来,原始 JSON 一个字节不动地端上来。
//
// 连接是按配置指纹缓存的,不写进 servers.json:工作台上试一个服务器,
// 不该在配置列表里留下一条。改了命令或地址,指纹变了,自然是一条新连接。

// PromptArgument 提示词模板的一个参数。
//
// 单独成类型而不是内联的匿名结构体:Wails 生成前端绑定时给匿名结构体
// 造出来的是一个没有名字的 class,整个 models.ts 会编不过
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// PromptInfo 一个提示词模板
type PromptInfo struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Arguments   []PromptArgument `json:"arguments"`
}

// ResourceInfo 一个资源
type ResourceInfo struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType,omitempty"`
}

// InspectResult 连上之后看到的全部
type InspectResult struct {
	// Fingerprint 这条连接的标识,后续调用带上它就能复用
	Fingerprint     string         `json:"fingerprint"`
	ServerInfo      string         `json:"serverInfo"`
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    []string       `json:"capabilities"`
	Tools           []ToolInfo     `json:"tools"`
	Prompts         []PromptInfo   `json:"prompts"`
	Resources       []ResourceInfo `json:"resources"`
	// Warnings 不致命的问题:服务器说支持 prompts 却拉不出来之类。
	// 这些要显示但不能当成失败 —— 大半服务器只实现 tools,那很正常
	Warnings   []string `json:"warnings"`
	DurationMs int64    `json:"durationMs"`
}

// CallResult 一次调用的完整记录。
//
// 原始请求和原始响应都留着:MCP 调试里一大半时间花在"我到底发出去了什么"上,
// 而客户端库通常只让你看到它替你解析完的结果
type CallResult struct {
	Method string `json:"method"`
	// Request 发出去的 params(已格式化的 JSON)
	Request string `json:"request"`
	// Response 收回来的 result 原文(已格式化);出错时为空
	Response string `json:"response"`
	// Text 从 content 块里提取出来的文本,方便直接读
	Text string `json:"text,omitempty"`
	// IsError 工具自己报告的失败(协议层是成功的)
	IsError bool `json:"isError"`
	// Error 协议层 / 传输层的错误
	Error string `json:"error,omitempty"`
	// RPCCode JSON-RPC 错误码,没有就是 0
	RPCCode    int   `json:"rpcCode,omitempty"`
	DurationMs int64 `json:"durationMs"`
	At         int64 `json:"at"`
}

// Workbench 工作台的连接池
type Workbench struct {
	mu    sync.Mutex
	conns map[string]*wbConn
}

type wbConn struct {
	c    *client
	srv  Server
	last time.Time
}

func NewWorkbench() *Workbench {
	return &Workbench{conns: map[string]*wbConn{}}
}

// idleTimeout 工作台连接闲置多久后回收。
// stdio 的服务器是本地进程,一直挂着既占内存也可能占着设备/端口
const idleTimeout = 10 * time.Minute

// Fingerprint 一个服务器配置的标识。
// 只认"连到哪儿"的字段:改了显示名不该断开重连
func Fingerprint(s Server) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00", s.Kind, s.Command)
	for _, a := range s.Args {
		fmt.Fprintf(h, "%s\x1f", a)
	}
	fmt.Fprintf(h, "\x00%s\x00", s.URL)
	// map 遍历无序,排完再算,否则同一份配置每次指纹都不同
	for _, kv := range sortedKV(s.Env) {
		fmt.Fprintf(h, "%s=%s\x1f", kv[0], kv[1])
	}
	h.Write([]byte{0})
	for _, kv := range sortedKV(s.Headers) {
		fmt.Fprintf(h, "%s=%s\x1f", kv[0], kv[1])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func sortedKV(m map[string]string) [][2]string {
	out := make([][2]string, 0, len(m))
	for k, v := range m {
		out = append(out, [2]string{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

// Inspect 连上并把服务器提供的一切拉一遍。
//
// tools 拉不出来才算失败;prompts / resources 拉不出来只记一条提示 ——
// 这两个是可选能力,大半服务器没实现,报成失败会让人以为自己配错了
func (w *Workbench) Inspect(ctx context.Context, srv Server) (*InspectResult, error) {
	start := time.Now()
	c, fp, err := w.dial(ctx, srv)
	if err != nil {
		return nil, err
	}

	callCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	res := &InspectResult{
		Fingerprint:     fp,
		ServerInfo:      c.serverInfo,
		ProtocolVersion: c.protocolVersion,
		Capabilities:    c.capabilities,
		Tools:           []ToolInfo{},
		Prompts:         []PromptInfo{},
		Resources:       []ResourceInfo{},
		Warnings:        []string{},
	}

	tools, err := c.listTools(callCtx, srv)
	if err != nil {
		w.drop(fp)
		return nil, fmt.Errorf("拉工具列表失败: %w", err)
	}
	res.Tools = tools

	if prompts, err := c.listPrompts(callCtx); err != nil {
		if w := optionalWarning("prompts", err); w != "" {
			res.Warnings = append(res.Warnings, w)
		}
	} else {
		res.Prompts = prompts
	}
	if resources, err := c.listResources(callCtx); err != nil {
		if w := optionalWarning("resources", err); w != "" {
			res.Warnings = append(res.Warnings, w)
		}
	} else {
		res.Resources = resources
	}

	res.DurationMs = time.Since(start).Milliseconds()
	return res, nil
}

// optionalWarning 可选能力拉取失败时该不该说话。
// "方法不存在"是没实现,属于正常,不吭声;别的错要说 —— 那可能是真出了问题
func optionalWarning(what string, err error) string {
	var re *rpcError
	if errorsAs(err, &re) && (re.Code == -32601 || re.Code == -32602) {
		return ""
	}
	msg := err.Error()
	if strings.Contains(msg, "-32601") || strings.Contains(strings.ToLower(msg), "method not found") {
		return ""
	}
	return fmt.Sprintf("%s 列表拉取失败: %s", what, msg)
}

// errorsAs 避免为一个类型断言引 errors 包的别名(这里只有一层包装)
func errorsAs(err error, target **rpcError) bool {
	for err != nil {
		if re, ok := err.(*rpcError); ok {
			*target = re
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// CallTool 调一次工具,原始报文原样返回
func (w *Workbench) CallTool(ctx context.Context, srv Server, name string, args map[string]any) (*CallResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	return w.rawCall(ctx, srv, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
}

// GetPrompt 取一个提示词模板渲染后的内容
func (w *Workbench) GetPrompt(ctx context.Context, srv Server, name string, args map[string]any) (*CallResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	return w.rawCall(ctx, srv, "prompts/get", map[string]any{
		"name":      name,
		"arguments": args,
	})
}

// ReadResource 读一个资源
func (w *Workbench) ReadResource(ctx context.Context, srv Server, uri string) (*CallResult, error) {
	return w.rawCall(ctx, srv, "resources/read", map[string]any{"uri": uri})
}

// rawCall 发一条请求,把来回两边的原文都留下。
//
// 协议层失败不返回 Go error,而是记进 CallResult.Error —— 调试界面要的是
// "这次调用发生了什么",把它变成一个抛出去的异常,界面上就只剩一行红字,
// 发出去的报文反而看不到了
func (w *Workbench) rawCall(ctx context.Context, srv Server, method string, params map[string]any) (*CallResult, error) {
	c, fp, err := w.dial(ctx, srv)
	if err != nil {
		return nil, err
	}
	out := &CallResult{
		Method:  method,
		Request: mustPretty(params),
		At:      time.Now().UnixMilli(),
	}
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	start := time.Now()
	raw, err := c.tr.call(callCtx, method, params)
	out.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		out.Error = err.Error()
		var re *rpcError
		if errorsAs(err, &re) {
			out.RPCCode = re.Code
		} else {
			// 传输层坏了,连接多半已经不可用,断掉让下次重连
			w.drop(fp)
		}
		return out, nil
	}
	out.Response = prettyRaw(raw)
	out.Text, out.IsError = extractContent(raw)
	return out, nil
}

// extractContent 从响应里尽力取出文本和 isError。
// 三种方法的响应形状不同(tools/call 是 content,prompts/get 是 messages,
// resources/read 是 contents),都尽力试一遍,取不到就算了 —— 原文始终都在
func extractContent(raw json.RawMessage) (string, bool) {
	var shape struct {
		IsError  bool           `json:"isError"`
		Content  []contentBlock `json:"content"`
		Contents []contentBlock `json:"contents"`
		Messages []struct {
			Role    string       `json:"role"`
			Content contentBlock `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		return "", false
	}
	var parts []string
	for _, b := range append(shape.Content, shape.Contents...) {
		if s := b.text(); s != "" {
			parts = append(parts, s)
		}
	}
	for _, m := range shape.Messages {
		if s := m.Content.text(); s != "" {
			parts = append(parts, m.Role+": "+s)
		}
	}
	return joinNonEmpty(parts, "\n"), shape.IsError
}

type contentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	MimeType string `json:"mimeType"`
	URI      string `json:"uri"`
	Blob     string `json:"blob"`
}

func (b contentBlock) text() string {
	if b.Text != "" {
		return b.Text
	}
	switch {
	case b.Blob != "":
		return fmt.Sprintf("(%s 二进制内容,%d 字节 base64)", orDefault(b.MimeType, "未知类型"), len(b.Blob))
	case b.URI != "":
		return "(资源引用: " + b.URI + ")"
	case b.Type != "" && b.Type != "text":
		return "(" + b.Type + " 类型的内容)"
	}
	return ""
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// dial 拿一条可用的连接,必要时新建
func (w *Workbench) dial(ctx context.Context, srv Server) (*client, string, error) {
	fp := Fingerprint(srv)

	w.mu.Lock()
	w.reapLocked()
	if wc, ok := w.conns[fp]; ok {
		wc.last = time.Now()
		c := wc.c
		w.mu.Unlock()
		return c, fp, nil
	}
	w.mu.Unlock()

	connCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	c, err := connect(connCtx, srv)
	if err != nil {
		return nil, fp, err
	}

	w.mu.Lock()
	// 两次点击同时触发建连时,后到的那条关掉,复用先到的
	if existing, ok := w.conns[fp]; ok {
		w.mu.Unlock()
		_ = c.close()
		return existing.c, fp, nil
	}
	w.conns[fp] = &wbConn{c: c, srv: srv, last: time.Now()}
	w.mu.Unlock()
	return c, fp, nil
}

// reapLocked 回收闲置连接;调用方须持锁
func (w *Workbench) reapLocked() {
	cutoff := time.Now().Add(-idleTimeout)
	for fp, wc := range w.conns {
		if wc.last.Before(cutoff) {
			_ = wc.c.close()
			delete(w.conns, fp)
		}
	}
}

func (w *Workbench) drop(fp string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if wc, ok := w.conns[fp]; ok {
		_ = wc.c.close()
		delete(w.conns, fp)
	}
}

// Disconnect 主动断开一条工作台连接
func (w *Workbench) Disconnect(srv Server) { w.drop(Fingerprint(srv)) }

// Shutdown 应用退出时把起的进程收掉
func (w *Workbench) Shutdown() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for fp, wc := range w.conns {
		_ = wc.c.close()
		delete(w.conns, fp)
	}
}

func mustPretty(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func prettyRaw(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return mustPretty(v)
}
