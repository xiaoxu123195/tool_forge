package mirror

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"

	"tool_forge/backend/tools/iosmux"
)

// iOS 投屏:越狱 iPhone 上跑 TrollVNC(见 trollvnc.go),电脑这头经 usbmuxd 连它的 VNC 端口。
//
// 画面和操作走的是标准的 VNC 协议,界面那头用现成的 VNC 客户端(noVNC)解。
// 这里只做两件事:经 SSH 把手机上的服务按这一次的设置开起来、用完关掉;
// 把界面的 WebSocket 和手机上的 VNC 端口两头对拷 —— 本机不开任何转发端口,
// 经 usbmuxd 拿到的连接本身就通到手机那个端口

// IOSOptions 界面开投屏时给的选项
type IOSOptions struct {
	// KeepAwake 投屏期间不让手机自动锁屏:TrollVNC 每 30 秒按一下「唤醒」,不改手机的自动锁定设置
	KeepAwake bool `json:"keepAwake"`
}

// IOSTarget 真机浏览里连着的那台 iPhone
type IOSTarget struct {
	// SSH 真机浏览那条连接。开关手机上的服务都经它
	SSH  *ssh.Client
	UDID string
	// Owner 真机浏览那条连接的 id:那条连接断开之前,先把它名下的投屏收掉 —— 关服务还得用它
	Owner string
}

// IOSSession 开起来的一路 iOS 投屏
type IOSSession struct {
	ID string `json:"id"`
	// URL 界面的 VNC 客户端连这条本机 WebSocket
	URL string `json:"url"`
	// Password 这一次的 VNC 密码,每次投屏都换
	Password   string `json:"password"`
	DeviceName string `json:"deviceName"`
}

// iosDialWait 界面接上来时手机端口还没开(服务刚重启)的话,再等这么久
const iosDialWait = 3 * time.Second

var (
	// iosAttachWait 开好之后界面多久内得接上来。VNC 客户端是用到时才加载的,第一次慢一点
	iosAttachWait = 30 * time.Second
	// iosDetachGrace 界面断开后再留多久。React 开发模式挂两次、界面刷新,都是先断后连
	iosDetachGrace = 5 * time.Second
	// iosPauseKeep 界面说了「暂停」(窗口藏起来)之后服务留多久。没客户端连着时 TrollVNC 不截屏,
	// 留着几乎不费电;再久没回来就收掉,回来时重开一路
	iosPauseKeep = 15 * time.Minute
)

// dialPhone 经 usbmuxd 连手机上的一个端口。按 UDID 找设备:拔插一次,usbmuxd 给的编号就变了
func dialPhone(udid string, port int) (net.Conn, error) {
	dev, err := iosmux.PickDevice(udid)
	if err != nil {
		return nil, err
	}
	return iosmux.Dial(dev.ID, port)
}

// prefsCooldown cfprefsd 被杀之后,新起来的那个得活够这么久再杀,launchd 才会马上重启它。
// 不到时间就杀,launchd 要推迟十秒,这期间读设置的程序(包括 TrollVNC)全卡着 —— 实测过:
// 停了马上再开,服务要二十秒才开始听端口
var prefsCooldown = 11 * time.Second

// iosPhone 一台手机。开、关它上面的服务排着队来:
// 换选项重开时,旧的那一路关服务不能插到新的一路开服务后面
type iosPhone struct {
	mu      sync.Mutex
	current *iosSession // 手机上的服务现在归谁
	// refreshed 上一次让 cfprefsd 重读设置的时候
	refreshed time.Time
}

// waitPrefs 离上一次让 cfprefsd 重读不够久的话先等着,再让它重读。调用方拿着 p.mu
func (p *iosPhone) waitPrefs() {
	if wait := prefsCooldown - time.Since(p.refreshed); wait > 0 {
		time.Sleep(wait)
	}
}

func (s *Service) phone(udid string) *iosPhone {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.phones[udid]
	if p == nil {
		p = &iosPhone{}
		s.phones[udid] = p
	}
	return p
}

// TrollStatus 查手机上的 TrollVNC:装没装、该装哪个包
func (s *Service) TrollStatus(t IOSTarget) (*TrollStatus, error) {
	if t.SSH == nil {
		return nil, errors.New("真机浏览没连着这台 iPhone")
	}
	l, err := probeTroll(sshShell{t.SSH})
	if err != nil {
		return nil, err
	}
	return l.status(), nil
}

// InstallTroll 把 TrollVNC 装到手机上(没装过的装上,装过的换成选的这个包)。
// pkgPath 是 .deb,或者 GitHub Actions 下载下来的 zip
func (s *Service) InstallTroll(t IOSTarget, pkgPath string) (*TrollInstall, error) {
	if t.SSH == nil {
		return nil, errors.New("真机浏览没连着这台 iPhone")
	}
	return s.installIOS(sshShell{t.SSH}, t.UDID, pkgPath)
}

func (s *Service) installIOS(sh shell, udid, pkgPath string) (*TrollInstall, error) {
	p := s.phone(udid)
	p.mu.Lock()
	cur := p.current
	p.mu.Unlock()
	// 正在投屏(换新版本的包):先收掉这一路,装完界面会重新开
	if cur != nil {
		s.closeIOS(cur)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.waitPrefs()
	defer func() { p.refreshed = time.Now() }()
	return installTroll(sh, pkgPath)
}

// StartIOS 按这一次的设置(新密码)启动手机上的 TrollVNC,等它开始服务
func (s *Service) StartIOS(t IOSTarget, o IOSOptions) (*IOSSession, error) {
	if t.SSH == nil {
		return nil, errors.New("真机浏览没连着这台 iPhone")
	}
	return s.startIOS(sshShell{t.SSH}, t.UDID, t.Owner, o)
}

func (s *Service) startIOS(sh shell, udid, owner string, o IOSOptions) (*IOSSession, error) {
	p := s.phone(udid)
	p.mu.Lock()
	defer p.mu.Unlock()
	// 同一台手机上一路还开着(换了选项重开):先把它断开,服务马上要按新设置重启
	if old := p.current; old != nil {
		p.current = nil
		s.dropIOS(old)
	}
	l, err := probeTroll(sh)
	if err != nil {
		return nil, err
	}
	if why := l.unsupported(); why != "" {
		return nil, errors.New(why)
	}
	if !l.installed() {
		return nil, errTrollMissing
	}
	h, err := s.ensureHub()
	if err != nil {
		return nil, err
	}
	sess := &iosSession{
		id:       randomHex(8),
		token:    randomHex(16),
		password: newVNCPassword(),
		name:     l.name(),
		udid:     udid,
		owner:    owner,
		sh:       sh,
		layout:   l,
		dial:     func() (net.Conn, error) { return s.iosDial(udid, trollPort) },
		closed:   make(chan struct{}),
	}
	p.waitPrefs()
	err = startTroll(sh, l, sess.password, o.KeepAwake)
	p.refreshed = time.Now()
	if err != nil {
		_ = stopTroll(sh, l, false)
		return nil, err
	}
	if err := waitVNC(sess.dial, trollReadyTimeout); err != nil {
		// 没起来,或者起来了却不要密码:都不能让它开着
		_ = stopTroll(sh, l, false)
		if log := trollLog(sh, l); log != "" && !errors.Is(err, errVNCNoAuth) {
			return nil, fmt.Errorf("%w\n手机上的日志:\n%s", err, log)
		}
		return nil, err
	}
	p.current = sess
	s.mu.Lock()
	s.ios[sess.id] = sess
	s.mu.Unlock()
	sess.idle = func() { s.StopIOS(sess.id) }
	sess.mu.Lock()
	sess.armGrace(iosAttachWait)
	sess.mu.Unlock()
	return &IOSSession{ID: sess.id, URL: h.vncURL(sess), Password: sess.password, DeviceName: sess.name}, nil
}

// StopIOS 结束一路 iOS 投屏,并停掉手机上的服务
func (s *Service) StopIOS(id string) {
	if sess := s.getIOS(id); sess != nil {
		s.closeIOS(sess)
	}
}

// PauseIOS 窗口藏起来、切到别的工具:界面断开画面,手机上的服务留着 —— 回来直接接上,
// 不用重启服务。留太久没回来(iosPauseKeep)才收掉
func (s *Service) PauseIOS(id string) {
	sess := s.getIOS(id)
	if sess == nil {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.paused = true
	if sess.ws == nil && !sess.isClosed() {
		sess.armGrace(iosPauseKeep)
	}
}

// ResumeIOS 界面回来了:这一路还在的话照旧接上,不在了(暂停太久被收掉、手机拔过)就得重开
func (s *Service) ResumeIOS(id string) bool {
	sess := s.getIOS(id)
	return sess != nil && !sess.isClosed()
}

// StopIOSOwnedBy 真机浏览那条连接要断开了:先把它名下的投屏收掉,关服务还要用这条连接
func (s *Service) StopIOSOwnedBy(owner string) {
	for _, sess := range s.iosSessions() {
		if sess.owner == owner {
			s.closeIOS(sess)
		}
	}
}

func (s *Service) getIOS(id string) *iosSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ios[id]
}

func (s *Service) iosSessions() []*iosSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := make([]*iosSession, 0, len(s.ios))
	for _, sess := range s.ios {
		all = append(all, sess)
	}
	return all
}

// dropIOS 只断开界面和转发,不碰手机上的服务
func (s *Service) dropIOS(sess *iosSession) {
	sess.shutdown()
	s.mu.Lock()
	if s.ios[sess.id] == sess {
		delete(s.ios, sess.id)
	}
	s.mu.Unlock()
}

// closeIOS 断开,并在服务还归这一路管时把它停掉
func (s *Service) closeIOS(sess *iosSession) {
	s.dropIOS(sess)
	p := s.phone(sess.udid)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.current != sess {
		return // 已经被新开的一路接手了,服务归它管
	}
	p.current = nil
	_ = stopTroll(sess.sh, sess.layout, false)
}

// ---- 一路 iOS 投屏 ----

type iosSession struct {
	id, token string
	password  string
	name      string
	udid      string
	owner     string
	sh        shell
	layout    trollLayout
	dial      func() (net.Conn, error)
	// idle 界面一直没接上来,或者断开后没再回来:收掉这一路
	idle func()

	mu    sync.Mutex
	ws    *websocket.Conn // 当前接着的界面,可能没有
	conn  net.Conn        // 和它配对的那条到手机 VNC 端口的连接
	grace *time.Timer
	// paused 界面暂停了(窗口藏起来):断开后留得久一些,等它回来
	paused bool
	closed chan struct{}
	once   sync.Once
}

// attach 界面的 VNC 客户端接上来了:另开一条到手机 VNC 端口的连接,两头对拷。
// 每接一次就是一次完整的 VNC 握手,新接的顶掉旧的
func (s *iosSession) attach(ws *websocket.Conn) {
	conn, err := s.dialWait()
	if err != nil {
		msg := "连不上手机上的 VNC 服务:" + err.Error()
		_ = ws.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, msg[:utf8Prefix(msg, 120)]),
			time.Now().Add(time.Second))
		ws.Close()
		return
	}
	s.mu.Lock()
	if s.isClosed() {
		s.mu.Unlock()
		conn.Close()
		ws.Close()
		return
	}
	oldWS, oldConn := s.ws, s.conn
	s.ws, s.conn = ws, conn
	s.paused = false
	if s.grace != nil {
		s.grace.Stop()
		s.grace = nil
	}
	s.mu.Unlock()
	if oldWS != nil {
		oldWS.Close()
	}
	if oldConn != nil {
		oldConn.Close()
	}

	go s.toUI(ws, conn)
	s.toPhone(ws, conn)
	s.detach(ws, conn)
}

// dialWait 服务刚重启时端口可能还没开,稍微等一下
func (s *iosSession) dialWait() (net.Conn, error) {
	deadline := time.Now().Add(iosDialWait)
	for {
		c, err := s.dial()
		if err == nil || time.Now().After(deadline) || s.isClosed() {
			return c, err
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// toUI 手机发来的原样转给界面。只有这一处往 WebSocket 里写
func (s *iosSession) toUI(ws *websocket.Conn, conn net.Conn) {
	buf := make([]byte, 64<<10)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
				break
			}
		}
		if err != nil {
			break
		}
	}
	// 手机那头断了:把界面这头也关掉,下面 toPhone 的读才会返回
	ws.Close()
}

// toPhone 界面发来的(按键、鼠标、剪贴板)转给手机
func (s *iosSession) toPhone(ws *websocket.Conn, conn net.Conn) {
	for {
		kind, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if kind != websocket.BinaryMessage {
			continue
		}
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := conn.Write(data); err != nil {
			return
		}
	}
}

// detach 界面断开了。这一路再留一会儿,等它接回来
func (s *iosSession) detach(ws *websocket.Conn, conn net.Conn) {
	conn.Close()
	ws.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ws != ws {
		return
	}
	s.ws, s.conn = nil, nil
	if !s.isClosed() {
		if s.paused {
			s.armGrace(iosPauseKeep)
		} else {
			s.armGrace(iosDetachGrace)
		}
	}
}

// armGrace 过了 d 还没有界面接着,就收掉这一路。调用方拿着 s.mu
func (s *iosSession) armGrace(d time.Duration) {
	if s.grace != nil {
		s.grace.Stop()
	}
	s.grace = time.AfterFunc(d, func() {
		s.mu.Lock()
		idle := s.ws == nil
		s.mu.Unlock()
		if idle && s.idle != nil {
			s.idle()
		}
	})
}

func (s *iosSession) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// shutdown 断开界面和转发
func (s *iosSession) shutdown() {
	s.once.Do(func() {
		s.mu.Lock()
		close(s.closed)
		ws, conn := s.ws, s.conn
		s.ws, s.conn = nil, nil
		if s.grace != nil {
			s.grace.Stop()
		}
		s.mu.Unlock()
		if ws != nil {
			ws.Close()
		}
		if conn != nil {
			conn.Close()
		}
	})
}
