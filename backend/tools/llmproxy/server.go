package llmproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Server 管理代理监听 + 转发 + 日志存储,生命周期受 Config 控制(参考 apiserver)。
type Server struct {
	mu         sync.RWMutex
	cfg        Config
	store      *Store
	listener   net.Listener
	httpSrv    *http.Server
	lastError  string
	lastLogErr string
	purgeStop  chan struct{}
}

// New 打开存储、读配置(不自动启动监听)。
func New() (*Server, error) {
	path, err := dbFilePath()
	if err != nil {
		return nil, err
	}
	st, err := openStore(path)
	if err != nil {
		return nil, err
	}
	cfg, _ := LoadConfig()
	return &Server{cfg: normalizeConfig(cfg), store: st}, nil
}

// Config 返回配置只读快照。
func (s *Server) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg := s.cfg
	cfg.Upstreams = append([]Upstream(nil), s.cfg.Upstreams...)
	return cfg
}

// Status 返回运行状态。
func (s *Server) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := Status{Error: s.lastError, LastLogError: s.lastLogErr}
	if s.listener != nil {
		st.Running = true
		st.Addr = s.listener.Addr().String()
	}
	return st
}

// setLogErr 记录/清除最近一次写日志的结果(暴露到 Status,便于发现多实例锁库等静默失败)。
func (s *Server) setLogErr(err error) {
	s.mu.Lock()
	if err != nil {
		s.lastLogErr = err.Error()
	} else {
		s.lastLogErr = ""
	}
	s.mu.Unlock()
}

// ApplyConfig 根据新配置启停/热更。端口变化才重启;上游/保留天数等热生效。
func (s *Server) ApplyConfig(cfg Config) error {
	s.mu.Lock()
	old := s.cfg
	s.cfg = normalizeConfig(cfg)
	running := s.listener != nil
	needStop := !s.cfg.Enabled && running
	needRestart := running && s.cfg.Enabled && old.Port != s.cfg.Port
	needStart := s.cfg.Enabled && !running
	s.mu.Unlock()

	switch {
	case needStop:
		return s.stop()
	case needRestart:
		if err := s.stop(); err != nil {
			return err
		}
		return s.start()
	case needStart:
		return s.start()
	}
	return nil
}

func (s *Server) start() error {
	s.mu.Lock()
	addr := fmt.Sprintf("127.0.0.1:%d", s.cfg.Port)
	s.mu.Unlock()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		s.mu.Lock()
		s.lastError = err.Error()
		s.mu.Unlock()
		return fmt.Errorf("监听 %s 失败:%w", addr, err)
	}

	httpSrv := &http.Server{
		Handler:           http.HandlerFunc(s.handle),
		ReadHeaderTimeout: 30 * time.Second,
		// WriteTimeout/ReadTimeout 留 0:SSE 流式与大上传不能被掐断
	}

	stop := make(chan struct{})
	s.mu.Lock()
	s.listener = ln
	s.httpSrv = httpSrv
	s.lastError = ""
	s.purgeStop = stop
	s.mu.Unlock()

	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logf("serve error: %v", err)
			s.mu.Lock()
			s.lastError = err.Error()
			s.mu.Unlock()
		}
	}()
	go s.purgeLoop(stop)

	s.logf("listening on %s", addr)
	return nil
}

func (s *Server) stop() error {
	s.mu.Lock()
	httpSrv := s.httpSrv
	stop := s.purgeStop
	s.listener = nil
	s.httpSrv = nil
	s.purgeStop = nil
	s.mu.Unlock()

	if stop != nil {
		close(stop)
	}
	if httpSrv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return httpSrv.Shutdown(ctx)
}

// Shutdown 应用退出时调用。
func (s *Server) Shutdown() error {
	err := s.stop()
	if s.store != nil {
		_ = s.store.Close()
	}
	return err
}

// handle 是代理入口:按路径首段路由到上游。
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	name, rest := splitUpstream(r.URL.Path)
	if name == "" || name == "favicon.ico" {
		s.writeHelp(w)
		return
	}
	up, ok := s.findUpstream(name)
	if !ok {
		// 记一条"未匹配上游"的日志:这样 base_url 写错(少了 /{上游} 段或名字不对)时
		// 用户能在列表里直接看到请求确实到了、只是没路由到,而不是一片空白。
		s.logRejected(r, name, rest)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "llmproxy: 未配置上游 %q。base_url 需形如 http://127.0.0.1:%d/{上游}/v1。已配置:%s\n",
			name, s.Config().Port, strings.Join(s.upstreamNames(), ", "))
		return
	}
	s.forward(w, r, up, name, rest)
}

// logRejected 把一条未匹配到上游、未转发的请求也记入日志,便于排查 base_url 写错。
func (s *Server) logRejected(r *http.Request, name, rest string) {
	full, _ := io.ReadAll(io.LimitReader(r.Body, hardReadMax))
	_ = r.Body.Close()
	stored, trunc := capBytes(full, s.maxBodyBytes())
	if _, err := s.store.Insert(&capture{
		ts:         time.Now().UnixMilli(),
		upstream:   name,
		method:     r.Method,
		path:       rest,
		reqHeaders: redactHeaders(r.Header),
		reqBody:    string(stored),
		reqBytes:   len(full),
		reqTrunc:   trunc,
		status:     http.StatusNotFound,
		errMsg:     fmt.Sprintf("未配置上游 %q,请求未转发(检查 base_url 是否含 /{上游} 段、名字是否一致)", name),
	}); err != nil {
		s.setLogErr(err)
	} else {
		s.setLogErr(nil)
	}
}

func (s *Server) writeHelp(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "Tool Forge LLM Proxy\n用法:把 base_url 指到 http://127.0.0.1:%d/{上游}/...\n已配置上游:%s\n",
		s.Config().Port, strings.Join(s.upstreamNames(), ", "))
}

func (s *Server) findUpstream(name string) (Upstream, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.cfg.Upstreams {
		if u.Name == name {
			if u.Disabled {
				return Upstream{}, false // 已禁用,按未配置处理
			}
			return u, true
		}
	}
	return Upstream{}, false
}

func (s *Server) upstreamNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.cfg.Upstreams))
	for _, u := range s.cfg.Upstreams {
		if !u.Disabled {
			out = append(out, u.Name)
		}
	}
	return out
}

func (s *Server) maxBodyBytes() int {
	s.mu.RLock()
	kb := s.cfg.MaxBodyKB
	s.mu.RUnlock()
	if kb <= 0 {
		kb = 8192
	}
	return kb * 1024
}

// ----- 日志查询(委托 store) -----

func (s *Server) ListLogs(q LogQuery) (*LogPage, error)  { return s.store.Query(q) }
func (s *Server) Stats(q StatsQuery) (*Stats, error)     { return s.store.Stats(q) }
func (s *Server) LogDetail(id int64) (*LogDetail, error) { return s.store.Detail(id) }
func (s *Server) DeleteLog(id int64) error               { return s.store.Delete(id) }
func (s *Server) ClearLogs() error                       { return s.store.Clear() }

// ----- 重放 -----

// Replay 重新发起一次请求(密钥未落盘,需调用方在 Headers 里自带 Authorization)。
func (s *Server) Replay(in ReplayInput) (*LogDetail, error) {
	up, ok := s.findUpstream(in.Upstream)
	if !ok {
		return nil, fmt.Errorf("未知上游: %s", in.Upstream)
	}
	target, err := url.Parse(up.Target)
	if err != nil || target.Host == "" {
		return nil, fmt.Errorf("上游地址无效: %s", up.Target)
	}
	rest := in.Path
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodPost
	}
	full := target.Scheme + "://" + target.Host + singleJoinPath(target.Path, rest)

	req, err := http.NewRequest(method, full, strings.NewReader(in.Body))
	if err != nil {
		return nil, err
	}
	for k, v := range in.Headers {
		req.Header.Set(k, v)
	}

	tr, err := buildTransport(up.OutboundProxy, up.TimeoutSec)
	if err != nil {
		return nil, err
	}
	maxBytes := s.maxBodyBytes()
	storedReq, reqTrunc := capBytes([]byte(in.Body), maxBytes)
	cap := &capture{
		ts:         time.Now().UnixMilli(),
		upstream:   in.Upstream,
		method:     method,
		path:       rest,
		reqHeaders: redactHeaders(req.Header),
		reqBody:    string(storedReq),
		reqBytes:   len(in.Body),
		reqTrunc:   reqTrunc,
		tag:        "replay",
	}

	start := time.Now()
	resp, err := (&http.Client{Transport: tr}).Do(req)
	cap.durationMs = int(time.Since(start).Milliseconds())
	if err != nil {
		cap.status = http.StatusBadGateway
		cap.errMsg = err.Error()
	} else {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, hardReadMax))
		_ = resp.Body.Close()
		cap.status = resp.StatusCode
		cap.respHeaders = redactHeaders(resp.Header)
		cap.stream = isSSE(resp.Header)
		stored, trunc := capBytes(body, maxBytes)
		cap.respBody = string(stored)
		cap.respBytes = len(body)
		cap.respTrunc = trunc
		if cap.stream {
			cap.respMerged = mergeSSE(string(stored))
		}
		cap.model = extractModel(cap.reqBody, cap.respBody)
		cap.promptTok, cap.completeTok, cap.totalTok = extractUsage(cap.respBody, cap.stream)
	}

	id, err := s.store.Insert(cap)
	if err != nil {
		return nil, err
	}
	return s.store.Detail(id)
}

// ----- 保留清理 -----

func (s *Server) purgeLoop(stop <-chan struct{}) {
	s.purgeOnce()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.purgeOnce()
		}
	}
}

func (s *Server) purgeOnce() {
	s.mu.RLock()
	days := s.cfg.RetentionDays
	s.mu.RUnlock()
	if days <= 0 {
		return
	}
	before := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	if n, err := s.store.Purge(before); err == nil && n > 0 {
		s.logf("清理过期日志 %d 条", n)
	}
}

func (s *Server) logf(format string, args ...any) {
	log.Printf("[llmproxy] "+format, args...)
}

func normalizeConfig(c Config) Config {
	if c.Port <= 0 || c.Port > 65535 {
		c.Port = 8788
	}
	if c.MaxBodyKB <= 0 {
		c.MaxBodyKB = 8192
	}
	if c.RetentionDays < 0 {
		c.RetentionDays = 0
	}
	// 过滤非法上游(空名/空目标);新建切片,不就地改写调用方的底层数组
	clean := make([]Upstream, 0, len(c.Upstreams))
	for _, u := range c.Upstreams {
		u.Name = strings.TrimSpace(u.Name)
		u.Target = strings.TrimRight(strings.TrimSpace(u.Target), "/")
		if u.Name == "" || u.Target == "" {
			continue
		}
		clean = append(clean, u)
	}
	c.Upstreams = clean
	return c
}
