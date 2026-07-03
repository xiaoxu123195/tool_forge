package llmproxy

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

const (
	// hardReadMax 请求体读入内存的安全上限(必须读全才能转发);LLM 请求极少超过。
	hardReadMax = 64 << 20 // 64MB
	tagHeader   = "X-PrismCat-Tag"
)

var doneMarker = []byte("[DONE]")

// teeCapture 包住上游响应体:转发给客户端的同时把前 limit 字节留存,
// 并在每次读到数据后回调 onData,让上层"边流边回填"日志——这样即使上游流式连接
// 迟迟不 EOF(中转在 [DONE] 后不及时关闭),内容也已经入库,而不是卡在"进行中"。
type teeCapture struct {
	rc      io.ReadCloser
	buf     bytes.Buffer
	limit   int
	total   int
	trunc   bool
	firstAt time.Time        // 首次读到数据的时刻,用于算 TTFT
	onData  func(force bool) // force=true 表示遇到 [DONE] 或读结束,应立即回填(不受节流限制)
}

func (t *teeCapture) Read(p []byte) (int, error) {
	n, err := t.rc.Read(p)
	if n > 0 {
		if t.firstAt.IsZero() {
			t.firstAt = time.Now()
		}
		t.total += n
		if remain := t.limit - t.buf.Len(); remain > 0 {
			if remain >= n {
				t.buf.Write(p[:n])
			} else {
				t.buf.Write(p[:remain])
				t.trunc = true
			}
		} else {
			t.trunc = true
		}
	}
	if t.onData != nil {
		force := err != nil || (n > 0 && bytes.Contains(p[:n], doneMarker))
		t.onData(force)
	}
	return n, err
}

func (t *teeCapture) Close() error { return t.rc.Close() }

// forward 执行一次转发并记录。name 为上游名,rest 为转发到上游的路径(以 / 开头)。
func (s *Server) forward(w http.ResponseWriter, r *http.Request, up Upstream, name, rest string) {
	maxBytes := s.maxBodyBytes()

	full, _ := io.ReadAll(io.LimitReader(r.Body, hardReadMax))
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(full))
	storedReq, reqTrunc := capBytes(full, maxBytes)

	target, err := url.Parse(up.Target)
	if err != nil || target.Host == "" {
		http.Error(w, "llmproxy: 上游地址无效: "+up.Target, http.StatusBadGateway)
		return
	}

	cap := &capture{
		ts:         time.Now().UnixMilli(),
		upstream:   name,
		method:     r.Method,
		path:       rest,
		reqHeaders: redactHeaders(r.Header),
		reqBody:    string(storedReq),
		reqBytes:   len(full),
		reqTrunc:   reqTrunc,
		tag:        r.Header.Get(tagHeader),
	}

	// 请求一到就先落一条"进行中"(status=0),这样流式响应在转发期间也立刻可见;
	// 响应结束后再 Update 回填。两段式写入避免"拿到结果了日志还没出现"。
	id, ierr := s.store.Insert(cap)
	if ierr != nil {
		s.logf("写日志(到达)失败: %v", ierr)
		s.setLogErr(ierr)
	} else {
		s.setLogErr(nil)
	}

	tr, terr := buildTransport(up.OutboundProxy, up.TimeoutSec)
	if terr != nil {
		http.Error(w, "llmproxy: 出站代理配置无效: "+terr.Error(), http.StatusBadGateway)
		return
	}

	var (
		capTee    *teeCapture
		start     = time.Now()
		lastFlush time.Time
	)

	// flush 把 cap 的当前状态回填到到达时插入的那行(id<=0 表示到达插入失败,补插一条完整的)。
	flush := func() {
		cap.durationMs = int(time.Since(start).Milliseconds())
		if capTee != nil {
			if !capTee.firstAt.IsZero() {
				cap.ttftMs = int(capTee.firstAt.Sub(start).Milliseconds())
			}
			cap.respBytes = capTee.total
			cap.respTrunc = capTee.trunc
			// 上游若压缩了(极少,因为我们已请求 identity),这里解压成可读文本入库
			cap.respBody = string(decodeBody(capTee.buf.Bytes(), cap.respEncoding))
			if cap.stream {
				cap.respMerged = mergeSSE(cap.respBody)
			}
			cap.model = extractModel(cap.reqBody, cap.respBody)
			cap.promptTok, cap.completeTok, cap.totalTok = extractUsage(cap.respBody, cap.stream)
		}
		var werr error
		if id > 0 {
			werr = s.store.Update(id, cap)
		} else {
			id, werr = s.store.Insert(cap)
		}
		if werr != nil {
			s.logf("写日志(回填)失败: %v", werr)
			s.setLogErr(werr)
		} else {
			s.setLogErr(nil)
		}
	}

	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = target.Scheme
			pr.Out.URL.Host = target.Host
			pr.Out.URL.Path = singleJoinPath(target.Path, rest)
			pr.Out.URL.RawQuery = r.URL.RawQuery
			pr.Out.Host = target.Host
			pr.Out.Header.Del(tagHeader) // 控制头不外泄给上游
			// 要求上游不压缩,日志才可读;identity 任何客户端都接受,不影响转发。
			pr.Out.Header.Set("Accept-Encoding", "identity")
		},
		Transport:     tr,
		FlushInterval: -1, // SSE 直通,逐块刷新
		ModifyResponse: func(resp *http.Response) error {
			cap.status = resp.StatusCode
			cap.respEncoding = resp.Header.Get("Content-Encoding")
			cap.respHeaders = redactHeaders(resp.Header)
			cap.stream = isSSE(resp.Header)
			tee := &teeCapture{rc: resp.Body, limit: maxBytes}
			// 边流边回填:遇到 [DONE]/读结束立即回填,否则最多每 300ms 一次,避免高频写库。
			tee.onData = func(force bool) {
				if force || time.Since(lastFlush) >= 300*time.Millisecond {
					lastFlush = time.Now()
					flush()
				}
			}
			resp.Body = tee
			capTee = tee
			flush() // 响应头一到就回填:状态立刻从"进行中"变成真实状态码
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, e error) {
			cap.errMsg = e.Error()
			if cap.status == 0 {
				cap.status = http.StatusBadGateway
			}
			flush()
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("llmproxy upstream error: " + e.Error()))
		},
	}

	rp.ServeHTTP(w, r)
	flush() // 非流式 / 干净 EOF:做最终回填
}

// buildTransport 按出站代理设置构造 transport。timeoutSec 仅作"等待首字节"超时,不限制流式总时长。
func buildTransport(proxyStr string, timeoutSec int) (*http.Transport, error) {
	tr := &http.Transport{
		MaxIdleConns:        50,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	if timeoutSec > 0 {
		tr.ResponseHeaderTimeout = time.Duration(timeoutSec) * time.Second
	}
	switch strings.TrimSpace(proxyStr) {
	case "", "direct":
		tr.Proxy = nil
	case "env":
		tr.Proxy = http.ProxyFromEnvironment
	default:
		u, err := url.Parse(strings.TrimSpace(proxyStr))
		if err != nil {
			return nil, err
		}
		switch u.Scheme {
		case "http", "https":
			tr.Proxy = http.ProxyURL(u)
		case "socks5", "socks5h":
			var auth *proxy.Auth
			if u.User != nil {
				pw, _ := u.User.Password()
				auth = &proxy.Auth{User: u.User.Username(), Password: pw}
			}
			d, derr := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
			if derr != nil {
				return nil, derr
			}
			tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				if cd, ok := d.(proxy.ContextDialer); ok {
					return cd.DialContext(ctx, network, addr)
				}
				return d.Dial(network, addr)
			}
		default:
			return nil, fmt.Errorf("不支持的出站代理协议: %s", u.Scheme)
		}
	}
	return tr, nil
}

// splitUpstream 把请求路径拆成上游名 + 转发路径。"/openai/v1/x" → ("openai","/v1/x")。
func splitUpstream(p string) (name, rest string) {
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "", "/"
	}
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i], "/" + p[i+1:]
	}
	return p, "/"
}

func singleJoinPath(a, b string) string {
	switch {
	case a == "":
		return b
	case strings.HasSuffix(a, "/") && strings.HasPrefix(b, "/"):
		return a + b[1:]
	case !strings.HasSuffix(a, "/") && !strings.HasPrefix(b, "/"):
		return a + "/" + b
	default:
		return a + b
	}
}

func isSSE(h http.Header) bool {
	return strings.Contains(strings.ToLower(h.Get("Content-Type")), "text/event-stream")
}

// decodeBody 按 Content-Encoding 解压(gzip/deflate);解压失败或未压缩则原样返回。
// 加了膨胀上限,避免 gzip bomb。
func decodeBody(raw []byte, encoding string) []byte {
	if len(raw) == 0 {
		return raw
	}
	const maxOut = 64 << 20 // 64MB 解压上限
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "gzip", "x-gzip":
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return raw
		}
		defer zr.Close()
		if out, err := io.ReadAll(io.LimitReader(zr, maxOut)); err == nil && len(out) > 0 {
			return out
		}
		return raw
	case "deflate":
		// 标准是 zlib 包装,但不少实现是裸 flate,两种都试
		if zr, err := zlib.NewReader(bytes.NewReader(raw)); err == nil {
			defer zr.Close()
			if out, err := io.ReadAll(io.LimitReader(zr, maxOut)); err == nil && len(out) > 0 {
				return out
			}
		}
		fr := flate.NewReader(bytes.NewReader(raw))
		defer fr.Close()
		if out, err := io.ReadAll(io.LimitReader(fr, maxOut)); err == nil && len(out) > 0 {
			return out
		}
		return raw
	default:
		return raw
	}
}

func capBytes(b []byte, max int) ([]byte, bool) {
	if max <= 0 || len(b) <= max {
		return b, false
	}
	return b[:max], true
}
