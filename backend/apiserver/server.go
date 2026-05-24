package apiserver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Server 管理 HTTP 监听 + 路由 + 鉴权,生命周期受 Config 控制。
//
// 用法:
//
//	srv := apiserver.New()
//	srv.Register(handler1); srv.Register(handler2)
//	srv.ApplyConfig(cfg)   // 根据配置决定启停
type Server struct {
	mu        sync.RWMutex
	cfg       Config
	handlers  map[string]ToolHandler
	listener  net.Listener
	httpSrv   *http.Server
	lastError string
}

func New() *Server {
	return &Server{
		cfg:      DefaultConfig(),
		handlers: map[string]ToolHandler{},
	}
}

// Register 注册一个工具 handler。可多次调用,Name() 重复时后注册覆盖前者。
// 必须在 Start 之前调用。
func (s *Server) Register(h ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[h.Name()] = h
}

// ListTools 列出所有已注册的工具(给 UI 显示用)
func (s *Server) ListTools() []ToolInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ToolInfo, 0, len(s.handlers))
	for _, h := range s.handlers {
		out = append(out, ToolInfo{
			Name:        h.Name(),
			Title:       h.Title(),
			Description: h.Description(),
			Path:        "/api/v1/tools/" + h.Name(),
			Enabled:     s.cfg.EnabledTools[h.Name()],
		})
	}
	return out
}

// Status 返回当前 server 状态(运行中? 监听在哪? 上次启动错误?)
func (s *Server) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := Status{Error: s.lastError}
	if s.listener != nil {
		st.Running = true
		st.Addr = s.listener.Addr().String()
	}
	return st
}

// Config 拿当前配置(只读快照)
func (s *Server) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg := s.cfg
	// 拷贝 map 避免外部修改
	tools := make(map[string]bool, len(cfg.EnabledTools))
	for k, v := range cfg.EnabledTools {
		tools[k] = v
	}
	cfg.EnabledTools = tools
	return cfg
}

// ApplyConfig 根据新配置决定启停 / 重启 server。
//   - Enabled=false                 → 停止
//   - Enabled=true 且端口变化       → 重启
//   - Enabled=true 且端口未变       → 仅热更其它字段(鉴权 token 等)
func (s *Server) ApplyConfig(cfg Config) error {
	s.mu.Lock()
	oldCfg := s.cfg
	s.cfg = normalizeConfig(cfg)
	needRestart := s.listener != nil && oldCfg.Port != s.cfg.Port
	needStop := !s.cfg.Enabled && s.listener != nil
	needStart := s.cfg.Enabled && s.listener == nil
	s.mu.Unlock()

	if needStop {
		return s.stopLocked()
	}
	if needRestart {
		if err := s.stopLocked(); err != nil {
			return err
		}
		return s.startLocked()
	}
	if needStart {
		return s.startLocked()
	}
	return nil
}

func normalizeConfig(c Config) Config {
	if c.Port <= 0 || c.Port > 65535 {
		c.Port = 11435
	}
	if c.EnabledTools == nil {
		c.EnabledTools = map[string]bool{}
	}
	return c
}

func (s *Server) startLocked() error {
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

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/healthz", s.handleHealth)
	mux.HandleFunc("/api/v1/tools", s.handleListTools)
	mux.HandleFunc("/api/v1/tools/", s.handleToolCall) // 注意尾斜杠 = 前缀路由

	httpSrv := &http.Server{
		Handler:      s.withMiddleware(mux),
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 5 * time.Minute, // 给较长的工具一些余地(包名搜索本身 ~10s)
	}

	s.mu.Lock()
	s.listener = ln
	s.httpSrv = httpSrv
	s.lastError = ""
	s.mu.Unlock()

	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[apiserver] serve error: %v", err)
			s.mu.Lock()
			s.lastError = err.Error()
			s.mu.Unlock()
		}
	}()

	log.Printf("[apiserver] listening on %s", addr)
	return nil
}

func (s *Server) stopLocked() error {
	s.mu.Lock()
	httpSrv := s.httpSrv
	s.listener = nil
	s.httpSrv = nil
	s.mu.Unlock()

	if httpSrv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return httpSrv.Shutdown(ctx)
}

// Shutdown 应用关闭时优雅停止
func (s *Server) Shutdown() error {
	return s.stopLocked()
}

// =================== 中间件 ===================

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 仅允许 127.0.0.1 客户端(double-check,即使将来 listen 0.0.0.0 也安全)
		if !isLocalAddr(r.RemoteAddr) {
			writeError(w, http.StatusForbidden, "forbidden", "仅允许本机访问")
			return
		}
		// 鉴权
		if !s.checkAuth(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "缺少或错误的 Authorization Bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) checkAuth(r *http.Request) bool {
	s.mu.RLock()
	auth := s.cfg.AuthEnabled
	token := s.cfg.Token
	s.mu.RUnlock()
	if !auth {
		return true
	}
	got := r.Header.Get("Authorization")
	if got == "" {
		return false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(got, prefix) {
		return false
	}
	return strings.TrimSpace(got[len(prefix):]) == token
}

func isLocalAddr(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}

// =================== 路由 handlers ===================

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"time": time.Now().UTC().Format(time.RFC3339),
	})
}

// GET /api/v1/tools — 列已注册 + 已启用的工具元信息(未启用的不列,避免泄露)
func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	tools := s.ListTools()
	// 只列开了的(没开的可视为"用户不希望被外部知道")
	exposed := make([]ToolInfo, 0, len(tools))
	for _, t := range tools {
		if t.Enabled {
			exposed = append(exposed, t)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": exposed})
}

// POST /api/v1/tools/<name> — 调用具体工具
func (s *Server) handleToolCall(w http.ResponseWriter, r *http.Request) {
	const prefix = "/api/v1/tools/"
	name := strings.TrimPrefix(r.URL.Path, prefix)
	if name == "" || strings.Contains(name, "/") {
		writeError(w, http.StatusNotFound, "not_found", "未知的工具路径")
		return
	}

	s.mu.RLock()
	h, ok := s.handlers[name]
	enabled := s.cfg.EnabledTools[name]
	s.mu.RUnlock()

	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "工具 "+name+" 未注册")
		return
	}
	if !enabled {
		writeError(w, http.StatusForbidden, "tool_disabled", "工具 "+name+" 未在 Tool Forge 中启用对外暴露")
		return
	}

	// 方法校验
	allowed := h.Methods()
	if len(allowed) == 0 {
		allowed = []string{http.MethodPost}
	}
	methodOK := false
	for _, m := range allowed {
		if strings.EqualFold(m, r.Method) {
			methodOK = true
			break
		}
	}
	if !methodOK {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}

	// 读 body(限制 1MB,防止恶意大包)
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body_failed", err.Error())
		return
	}

	// 流式 handler 走 SSE 分支
	if sh, ok := h.(StreamHandler); ok {
		serveSSE(w, r, sh, body)
		return
	}

	resp, err := h.Handle(r.Context(), body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "tool_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(resp)
}

// serveSSE 把 StreamHandler 包装成 Server-Sent Events 响应。
//
// 协议: 每个事件一行 "data: <json>\n\n"。客户端用 curl -N 或 EventSource 接收。
// 客户端断开 (r.Context().Done()) 会触发 handler 内部 ctx 取消, handler 可借此停掉
// 后台任务(比如 kill go-forensic 进程)。
func serveSSE(w http.ResponseWriter, r *http.Request, h StreamHandler, body []byte) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "服务器不支持流式响应")
		return
	}

	// SSE 长连接可能跑几分钟到几十分钟,清掉 server 全局 WriteTimeout
	if rc := http.NewResponseController(w); rc != nil {
		_ = rc.SetWriteDeadline(time.Time{})
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// 关掉 Nginx 等反代缓冲(虽然我们目前不走反代,放着不亏)
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// emit 串行写,handler 内部多 goroutine 调用时需自己做同步(参考 forensic handler 用 channel)
	emit := func(ev StreamEvent) error {
		data, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	// handler 出错时写一条 error 事件 + 结束;不再 writeError 因为头已经发了 200
	if err := h.HandleStream(r.Context(), body, emit); err != nil && !isClientGone(r.Context()) {
		_ = emit(StreamEvent{
			Type: "error",
			Data: map[string]string{"message": err.Error()},
		})
	}
}

func isClientGone(ctx context.Context) bool {
	return ctx.Err() != nil
}

// =================== Token 生成 ===================

// GenerateToken 用 crypto/rand 出 32 字节 base64url,带 "tf_" 前缀方便识别。
func GenerateToken() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return "tf_" + base64.RawURLEncoding.EncodeToString(buf)
}
