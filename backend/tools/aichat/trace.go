package aichat

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// 请求留档的容量。都是内存里的环形缓冲,不落盘 ——
// 里面有完整的请求体(可能含用户上传的文件文本)和响应,写进磁盘等于凭空多一份副本。
const (
	// traceKeepCount 保留最近多少次**请求**(不是多少次提问)。
	//
	// 一次提问不止一条:模型每要求调一次工具就是一轮新请求,密钥失效换一把重试
	// 也是一条。带工具的对话问三句就能把二十条挤满 —— 所以这个数要比直觉大。
	traceKeepCount = 40
	// traceBodyLimit / traceFrameLimit 单条记录的两个上限
	traceBodyLimit  = 64 << 10  // 请求体(附件文本会很大)
	traceFrameLimit = 256 << 10 // 响应帧合计
	// traceTotalLimit 整个环加起来最多占多少内存。
	//
	// 光靠"条数 × 单条上限"算出来的是最坏情况(40×320KB≈12MB),而绝大多数请求
	// 只有几 KB —— 按条数留会让常见情况白白留得太少,按字节封顶才是真的把内存兜住。
	// 两个限制同时生效,先撞上哪个就按哪个淘汰。
	traceTotalLimit = 8 << 20
)

// RequestTrace 一次对上游的请求现场:发了什么、回了什么。
//
// 存在的理由:接自建中转时,"请求失败"四个字提供的信息量约等于零 ——
// 分不清是我们发的参数它不认,还是它回的格式我们没解出来。
// 有了这份留档,两边都能当场看到。
//
// 密钥永远不进这里(见 redactHeaders / redactURL):这个面板是用来截图发给别人问的。
type RequestTrace struct {
	ID   string `json:"id"`
	Ts   int64  `json:"ts"`
	Kind string `json:"kind"` // "chat" / 以后可能有别的来源
	// ConvID 发起这次请求的会话;空表示不是聊天发起的
	ConvID       string `json:"convId,omitempty"`
	ProviderID   string `json:"providerId"`
	ProviderName string `json:"providerName"`
	Model        string `json:"model"`
	Endpoint     string `json:"endpoint"`
	Method       string `json:"method"`
	URL          string `json:"url"`
	// Headers 形如 "Content-Type: application/json";已脱敏
	Headers []string `json:"headers,omitempty"`
	Body    string   `json:"body,omitempty"`
	// BodyTruncated 请求体太长被截了(附件文本会很大)。截的是尾部 ——
	// 模型名、参数、工具声明都在 JSON 前面,那才是排查时要看的
	BodyTruncated bool `json:"bodyTruncated,omitempty"`
	Status        int  `json:"status,omitempty"`
	// Frames 原始 SSE data 行,按到达顺序
	Frames          []string `json:"frames,omitempty"`
	FramesTruncated bool     `json:"framesTruncated,omitempty"`
	Error           string   `json:"error,omitempty"`
	DurationMs      int      `json:"durationMs,omitempty"`
	// Done 流已经结束(正常或出错)。没结束的那条在面板上标成"进行中" ——
	// 卡住不动的请求恰恰是最需要看的一种
	Done bool `json:"done,omitempty"`
}

// TraceSummary 列表用的精简版:不含 body 和 frames,免得开个面板就把几 MB 塞进前端
type TraceSummary struct {
	ID string `json:"id"`
	Ts int64  `json:"ts"`
	// Kind "chat" = 聊天发的,"test" = 检测模型发的。
	// 检测没有会话,面板上"只看当前会话"会把它滤掉,得让用户看得出少了什么
	Kind         string `json:"kind"`
	ProviderName string `json:"providerName"`
	Model        string `json:"model"`
	Endpoint     string `json:"endpoint"`
	ConvID       string `json:"convId,omitempty"`
	Status       int    `json:"status,omitempty"`
	Error        string `json:"error,omitempty"`
	DurationMs   int    `json:"durationMs,omitempty"`
	FrameCount   int    `json:"frameCount"`
	Done         bool   `json:"done,omitempty"`
}

// traceRing 最近若干次请求的环形缓冲。
//
// 做成包级变量而不是挂在 Service 上:协议层(streamOpenAI 等)是一组不带接收者的
// 函数,为了留档给它们全都加一个参数,会让本来跟业务无关的东西渗进每个签名。
// 包里已经有 streamClient 这样的包级共享物,风格一致。
var traceRing = struct {
	mu   sync.Mutex
	list []*RequestTrace
}{}

// requestTrace 一次请求的记录句柄。所有方法都容忍 nil 接收者,
// 这样调用点不用写 if —— 留档是旁路,不该让主流程为它长出分支。
type requestTrace struct {
	t     *RequestTrace
	start time.Time
	bytes int
}

// startTrace 开一条记录并立刻放进环里。
//
// 立刻放进去(而不是等结束时再放)是有意的:请求卡住不返回时,
// 那条"进行中"的记录本身就是最重要的线索。
func startTrace(kind string, p Provider, model, endpoint, convID, method, rawURL string) *requestTrace {
	t := &RequestTrace{
		ID:           uuid.NewString(),
		Ts:           time.Now().UnixMilli(),
		Kind:         kind,
		ConvID:       convID,
		ProviderID:   p.ID,
		ProviderName: p.Name,
		Model:        model,
		Endpoint:     endpoint,
		Method:       method,
		URL:          redactURL(rawURL),
	}
	traceRing.mu.Lock()
	traceRing.list = append(traceRing.list, t)
	evictLocked()
	traceRing.mu.Unlock()
	return &requestTrace{t: t, start: time.Now()}
}

// evictLocked 从头上丢掉最老的,直到条数和总字节都在限内。
// 调用方必须已经持有 traceRing.mu。
//
// 只保底留一条:哪怕这一条自己就超了总量上限,也不能把它也丢掉 ——
// 刚出问题的那次请求正是要看的,面板空着比留一条超标的更糟。
func evictLocked() {
	for len(traceRing.list) > traceKeepCount {
		traceRing.list = traceRing.list[1:]
	}
	for len(traceRing.list) > 1 && traceBytesLocked() > traceTotalLimit {
		traceRing.list = traceRing.list[1:]
	}
}

func traceBytesLocked() int {
	n := 0
	for _, t := range traceRing.list {
		n += len(t.Body)
		for _, f := range t.Frames {
			n += len(f)
		}
	}
	return n
}

// request 记下真正发出去的请求头和请求体
func (r *requestTrace) request(h http.Header, body []byte) {
	if r == nil {
		return
	}
	traceRing.mu.Lock()
	defer traceRing.mu.Unlock()
	r.t.Headers = redactHeaders(h)
	s := string(body)
	if len(s) > traceBodyLimit {
		s = s[:traceBodyLimit]
		r.t.BodyTruncated = true
	}
	r.t.Body = s
}

func (r *requestTrace) status(code int) {
	if r == nil {
		return
	}
	traceRing.mu.Lock()
	defer traceRing.mu.Unlock()
	r.t.Status = code
}

// frame 记一帧响应。超过总量上限后只计数不再存 ——
// 一次生图能推几 MB base64,全存下来就把这个环撑爆了
func (r *requestTrace) frame(payload string) {
	if r == nil {
		return
	}
	traceRing.mu.Lock()
	defer traceRing.mu.Unlock()
	if r.bytes+len(payload) > traceFrameLimit {
		r.t.FramesTruncated = true
		return
	}
	r.bytes += len(payload)
	r.t.Frames = append(r.t.Frames, payload)
	// 长流是一边跑一边把环撑大的。只在新建记录时淘汰的话,
	// 总量上限对"一次几百帧的长回答"完全不起作用
	evictLocked()
}

func (r *requestTrace) fail(err error) {
	if r == nil || err == nil {
		return
	}
	traceRing.mu.Lock()
	defer traceRing.mu.Unlock()
	r.t.Error = err.Error()
	r.t.DurationMs = int(time.Since(r.start).Milliseconds())
	r.t.Done = true
}

func (r *requestTrace) finish() {
	if r == nil {
		return
	}
	traceRing.mu.Lock()
	defer traceRing.mu.Unlock()
	r.t.DurationMs = int(time.Since(r.start).Milliseconds())
	r.t.Done = true
}

// ListRequestTraces 最近的请求留档(新的在前),不含 body / frames
func (s *Service) ListRequestTraces() []TraceSummary {
	traceRing.mu.Lock()
	defer traceRing.mu.Unlock()
	out := make([]TraceSummary, 0, len(traceRing.list))
	for i := len(traceRing.list) - 1; i >= 0; i-- {
		t := traceRing.list[i]
		out = append(out, TraceSummary{
			ID: t.ID, Ts: t.Ts, Kind: t.Kind, ProviderName: t.ProviderName, Model: t.Model,
			Endpoint: t.Endpoint, ConvID: t.ConvID, Status: t.Status,
			Error: t.Error, DurationMs: t.DurationMs, FrameCount: len(t.Frames),
			Done: t.Done,
		})
	}
	return out
}

// GetRequestTrace 取一条完整留档(含请求体和全部响应帧)
func (s *Service) GetRequestTrace(id string) (*RequestTrace, error) {
	traceRing.mu.Lock()
	defer traceRing.mu.Unlock()
	for _, t := range traceRing.list {
		if t.ID == id {
			cp := *t
			cp.Frames = append([]string(nil), t.Frames...)
			cp.Headers = append([]string(nil), t.Headers...)
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("这条记录已经被更新的请求挤掉了(只保留最近 %d 条)", traceKeepCount)
}

// ClearRequestTraces 清空留档
func (s *Service) ClearRequestTraces() {
	traceRing.mu.Lock()
	defer traceRing.mu.Unlock()
	traceRing.list = nil
}

// secretHeaders 值必须打码的请求头(小写比较)。
// 少列一个就是把密钥明文放进一个专门用来截图分享的面板里,所以宁可多列。
var secretHeaders = map[string]bool{
	"authorization":       true,
	"x-api-key":           true,
	"api-key":             true,
	"x-goog-api-key":      true,
	"x-auth-token":        true,
	"proxy-authorization": true,
	"cookie":              true,
}

// redactHeaders 把请求头拍平成 "Name: value",敏感项换成掩码。
//
// 保留前 4 位:多把密钥轮换时,"失败的是哪一把"是个常见问题,
// 光看一排星号答不了。4 位不足以还原密钥,但够区分。
func redactHeaders(h http.Header) []string {
	out := make([]string, 0, len(h))
	for name, vals := range h {
		v := strings.Join(vals, ", ")
		if secretHeaders[strings.ToLower(name)] {
			v = maskSecret(v)
		}
		out = append(out, name+": "+v)
	}
	sort.Strings(out)
	return out
}

// redactURL 摘掉查询串里的密钥。Gemini 是 ?key=xxx 这种把密钥放 URL 上的,
// 只脱请求头不脱 URL 等于没脱。
//
// 做法是在原始串上直接替换那一段值,而不是改完 url.Values 再 Encode 回去:
// Encode 会把掩码里的星号转义成 %2A、还会按字母序重排参数 —— 一个用来
// "看看到底发了什么"的面板,不该把发出去的东西改个样子再给人看。
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	out := raw
	for _, k := range []string{"key", "api_key", "apikey", "access_token", "token"} {
		v := q.Get(k)
		if v == "" {
			continue
		}
		masked := maskSecret(v)
		// 原始串里可能是转义过的形态,两种都换一遍
		out = strings.ReplaceAll(out, v, masked)
		if esc := url.QueryEscape(v); esc != v {
			out = strings.ReplaceAll(out, esc, masked)
		}
	}
	return out
}

func maskSecret(v string) string {
	// "Bearer sk-abcd..." 这种前面有方案名,掩码要打在实际的密钥上
	prefix := ""
	if i := strings.IndexByte(v, ' '); i >= 0 && i < 12 {
		prefix = v[:i+1]
		v = v[i+1:]
	}
	if len(v) <= 4 {
		return prefix + "****"
	}
	return prefix + v[:4] + "****"
}
