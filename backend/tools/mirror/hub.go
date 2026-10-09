package mirror

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// hub 本机的 WebSocket 服务,第一次投屏时才起。
//
// 只听 127.0.0.1,端口由系统分配;每路投屏一个随机口令,写在地址里。
// 浏览器里的网页也能连 127.0.0.1,所以认的是口令 —— 拿不到口令的连不上
type hub struct {
	ln       net.Listener
	srv      *http.Server
	svc      *Service
	upgrader websocket.Upgrader
}

func (s *Service) ensureHub() (*hub, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hub != nil {
		return s.hub, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("开不了本机的视频通道: %w", err)
	}
	h := &hub{
		ln:  ln,
		svc: s,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4 << 10,
			WriteBufferSize: 64 << 10,
			// 界面在 Wails 自己的源下,和这个端口必然不同源;放行来源,把关靠口令
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/mirror/", h.serve)
	h.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = h.srv.Serve(ln) }()
	s.hub = h
	return h, nil
}

func (h *hub) url(sess *session) string {
	return fmt.Sprintf("ws://%s/mirror/%s?t=%s", h.ln.Addr(), sess.id, sess.token)
}

func (h *hub) serve(w http.ResponseWriter, r *http.Request) {
	sess := h.svc.get(strings.TrimPrefix(r.URL.Path, "/mirror/"))
	if sess == nil || subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("t")), []byte(sess.token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ws, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	// 界面只发鼠标、按键这种小消息
	ws.SetReadLimit(64 << 10)
	sess.attach(ws)
}

func (h *hub) close() {
	_ = h.srv.Close()
}
