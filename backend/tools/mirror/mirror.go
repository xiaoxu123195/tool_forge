// Package mirror 手机投屏:把安卓手机的屏幕实时显示在工具里,并能用鼠标直接操作。
//
// 手机那头跑的是 scrcpy 的服务端(见 scrcpy.go):它用手机自己的硬件编码器把屏幕
// 编成 H.264,经 adb 传回来;这头把鼠标操作编成触摸事件发回去。
// 手机上不用装 App,也不需要 root。
//
// 视频不走 Wails 的事件通道 —— 那是 JSON 文本,一秒几十帧的二进制塞进去太浪费 ——
// 而是本机一个只给本程序用的 WebSocket(见 hub.go):一帧一条二进制消息,
// 界面那头用 WebCodecs 解码画到画布上,鼠标操作也从这条连接回来。
package mirror

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Service 管着所有正在投屏的会话
type Service struct {
	mu       sync.Mutex
	sessions map[string]*session
	hub      *hub
	// prepare 选中设备、读几项属性、推好手机端程序。测试里换成假的
	prepare func(adbPath, serial string) (deviceInfo, error)
}

func New() *Service {
	return &Service{sessions: map[string]*session{}, prepare: prepareDevice}
}

// StartRequest 界面发起一路投屏
type StartRequest struct {
	// Serial 设备序列号:真机浏览里连着的那台
	Serial string `json:"serial"`
	// AdbPath adb 路径,空 = 优先用自带的
	AdbPath string  `json:"adbPath"`
	Options Options `json:"options"`
}

// Session 开起来的一路投屏
type Session struct {
	ID string `json:"id"`
	// DeviceName 手机自己报的型号
	DeviceName string `json:"deviceName"`
	// URL 视频和鼠标操作都走这条 WebSocket
	URL string `json:"url"`
	// SDK 安卓 API 级别,读不到时是 0。复制、粘贴键要 24(安卓 7)起才有
	SDK int `json:"sdk"`
}

// Start 推送、启动手机端程序,连好视频和控制两条通道。
// 返回时还没开始收画面:界面接上 WebSocket 那一刻才要第一帧
func (s *Service) Start(req StartRequest) (*Session, error) {
	if err := req.Options.validate(); err != nil {
		return nil, err
	}
	info, err := s.prepare(req.AdbPath, req.Serial)
	if err != nil {
		return nil, err
	}
	h, err := s.ensureHub()
	if err != nil {
		return nil, err
	}
	sess, err := launch(info, req.AdbPath, req.Options, s.forget)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// 刚连好就断了(比如线松了):这时不该把一个死会话交出去
	if sess.isClosed() {
		return nil, fmt.Errorf("投屏刚开始就断了:%s", sess.endReason())
	}
	s.sessions[sess.id] = sess
	return &Session{ID: sess.id, DeviceName: sess.deviceName, URL: h.url(sess), SDK: info.sdk}, nil
}

// Stop 结束一路投屏。手机端程序在通道关掉后自己退出
func (s *Service) Stop(id string) {
	if sess := s.get(id); sess != nil {
		sess.close("")
	}
}

// CloseAll 应用退出时调用。不收的话手机端程序会一直跑到手机拔线
func (s *Service) CloseAll() {
	s.mu.Lock()
	all := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		all = append(all, sess)
	}
	h := s.hub
	s.hub = nil
	s.mu.Unlock()
	for _, sess := range all {
		sess.close("")
	}
	if h != nil {
		h.close()
	}
}

func (s *Service) get(id string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

// live 取一路还开着的投屏。截图、录屏、拖文件都要它
func (s *Service) live(id string) (*session, error) {
	sess := s.get(id)
	if sess == nil || sess.isClosed() {
		return nil, errors.New("投屏已经断开了,重新连上再试")
	}
	return sess, nil
}

func (s *Service) forget(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions[sess.id] == sess {
		delete(s.sessions, sess.id)
	}
}

// ---- 一路投屏 ----

const (
	// connectTimeout 手机端程序起来、开始监听要多久。慢的机器一两秒
	connectTimeout = 10 * time.Second
	// encoderTimeout 编码器起来、发出编码 ID 要多久
	encoderTimeout = 15 * time.Second
)

// detachGrace 界面断开后会话再留多久。React 开发模式会把组件挂两次,
// 切到别的页面再切回来也是先断后连 —— 一断就收掉的话这两种情况都会白白重连一次
var detachGrace = 3 * time.Second

// clipboardWait 读手机剪贴板最多等多久。剪贴板是空的时候手机端不回话,只能等到超时
var clipboardWait = time.Second

type session struct {
	id, token  string
	serial     string
	adbPath    string
	info       deviceInfo
	deviceName string

	// shell 手机端程序的输出(日志)。这条连接一关,程序也就没人管了
	shell   net.Conn
	video   net.Conn
	control net.Conn

	logs logTail
	// injectDenied 手机拒绝了模拟点击。小米一类机型要另开一个开关
	injectDenied atomic.Bool

	// clipKept 第一次粘贴之前已经把手机剪贴板原来的内容读出来交给界面了
	clipKept atomic.Bool
	clipMu   sync.Mutex
	// clipWait 有人在等手机剪贴板的回信:下一条剪贴板消息给它,不转给界面
	clipWait chan string

	recMu sync.Mutex
	rec   *recorder // 正在录屏,可能没有

	mu      sync.Mutex
	ws      *websocket.Conn // 当前接着的界面,可能没有
	grace   *time.Timer
	closed  chan struct{}
	once    sync.Once
	reason  string
	onClose func(*session)

	wsMu   sync.Mutex // WebSocket 同一时刻只能有一个人写
	ctrlMu sync.Mutex // 控制消息同理
}

// launch 在手机上启动手机端程序并连好两条通道
func launch(info deviceInfo, adbPath string, o Options, onClose func(*session)) (*session, error) {
	scid := randomScid()
	shell, err := openService(info.serial, "shell:"+serverCommand(scid, o))
	if err != nil {
		return nil, fmt.Errorf("在手机上启动投屏程序失败: %w", err)
	}
	s := &session{
		id:      randomHex(8),
		token:   randomHex(16),
		serial:  info.serial,
		adbPath: adbPath,
		info:    info,
		shell:   shell,
		closed:  make(chan struct{}),
		onClose: onClose,
	}
	go s.readLogs()
	if err := s.connect(scid); err != nil {
		s.close("")
		return nil, err
	}
	go s.pumpVideo()
	go s.readDeviceMessages()
	return s, nil
}

// connect 依次连上视频、控制两条通道,读完开头的设备名和编码 ID
func (s *session) connect(scid uint32) error {
	service := "localabstract:" + socketName(scid)
	video, err := s.dialFirst(service)
	if err != nil {
		return err
	}
	ok := false
	var control net.Conn
	defer func() {
		if !ok {
			video.Close()
			if control != nil {
				control.Close()
			}
		}
	}()

	// 顺序是死的:手机端先等视频、再等控制,两条都接上之后才发设备名、开始编码。
	// 先读设备名再连控制通道的话,两头会互相等到超时
	control, err = openService(s.serial, service)
	if err != nil {
		return s.startError("连控制通道失败", err)
	}
	_ = video.SetReadDeadline(time.Now().Add(5 * time.Second))
	name, err := readDeviceName(video)
	if err != nil {
		return s.startError("读设备信息失败", err)
	}
	_ = video.SetReadDeadline(time.Now().Add(encoderTimeout))
	codec, err := readCodecID(video)
	if err != nil {
		return s.startError("等视频编码器超时", err)
	}
	switch codec {
	case codecH264:
	case 0, 1:
		// 编码器起不来:分辨率太高、编码器有毛病都会走到这里
		return s.startError("手机上的视频编码器起不来,换低一档画质试试", nil)
	default:
		return fmt.Errorf("手机端发来的视频格式不对(%#x)", codec)
	}
	_ = video.SetReadDeadline(time.Time{})

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isClosed() {
		return s.startError("投屏程序在手机上退出了", nil)
	}
	s.video, s.control, s.deviceName = video, control, name
	ok = true
	return nil
}

// dialFirst 连第一条通道。手机端程序刚启动时还没开始监听,得反复试;
// 而且 adb 那头可能接了就断 —— 读到它发来的那一个字节才算真的连上
func (s *session) dialFirst(service string) (net.Conn, error) {
	deadline := time.Now().Add(connectTimeout)
	var last error
	for {
		if s.isClosed() {
			return nil, s.startError("投屏程序在手机上没起来", nil)
		}
		c, err := openService(s.serial, service)
		if err == nil {
			_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
			var one [1]byte
			if _, err = io.ReadFull(c, one[:]); err == nil {
				return c, nil
			}
			c.Close()
		}
		last = err
		if time.Now().After(deadline) {
			return nil, s.startError(fmt.Sprintf("等了 %d 秒,手机端投屏程序还是连不上", int(connectTimeout.Seconds())), last)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// startError 启动失败时,手机端日志里最后一条错误往往才是真正的原因
func (s *session) startError(what string, err error) error {
	if e := s.logs.lastError(); e != "" {
		return fmt.Errorf("%s:%s", what, e)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return fmt.Errorf("%s", what)
}

// readLogs 收手机端程序的输出。它一结束,就是程序退出了
func (s *session) readLogs() {
	sc := bufio.NewScanner(s.shell)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		s.logs.add(line)
		if strings.Contains(line, "INJECT_EVENTS permission") && s.injectDenied.CompareAndSwap(false, true) {
			s.notify(injectDeniedNotice(s.info.brand))
		}
	}
	s.close(s.endReason())
}

// pumpVideo 把手机发来的包原样转给界面;录着屏的话同时写进文件
func (s *session) pumpVideo() {
	hdr := make([]byte, headerSize)
	for {
		pkt, err := readPacket(s.video, hdr)
		if err != nil {
			s.close(s.endReason())
			return
		}
		s.record(pkt)
		s.forward(pkt)
	}
}

// readDeviceMessages 收手机经控制通道发回来的消息:剪贴板内容转给界面。
// 别的消息用不上,但也得读掉 —— 不读的话缓冲区一满,手机端就卡住了
func (s *session) readDeviceMessages() {
	r := bufio.NewReader(s.control)
	for {
		m, err := readDeviceMessage(r)
		if errors.Is(err, errUnknownDeviceMessage) {
			// 对不齐了:剩下的只能全丢掉,好歹不让手机端卡住
			_, _ = io.Copy(io.Discard, r)
		}
		if err != nil {
			s.close(s.endReason())
			return
		}
		if m.kind == deviceMsgClipboard {
			s.gotClipboard(m.text)
		}
	}
}

// gotClipboard 手机剪贴板的内容到了:有人在等就给它,否则交给界面
func (s *session) gotClipboard(text string) {
	s.clipMu.Lock()
	w := s.clipWait
	s.clipWait = nil
	s.clipMu.Unlock()
	if w != nil {
		w <- text
		return
	}
	s.notify(notice{Type: "clipboard", Text: text})
}

// keepClipboard 第一次往手机剪贴板里写东西之前,先把原来的内容读出来交给界面。
//
// 中文是经剪贴板粘贴过去的,写进去就把原来的顶掉了 —— 而手机上复制着的那段话,
// 可能正是要找的东西。一次投屏只读这一回:之后剪贴板里是我们自己粘的字
func (s *session) keepClipboard() {
	if !s.clipKept.CompareAndSwap(false, true) {
		return
	}
	w := make(chan string, 1)
	s.clipMu.Lock()
	s.clipWait = w
	s.clipMu.Unlock()
	if s.sendControl(getClipboardMessage(copyKeyNone)) != nil {
		return
	}
	select {
	case text := <-w:
		s.notify(notice{Type: "clipboard", Code: "replaced", Text: text})
	case <-time.After(clipboardWait):
		// 剪贴板原来是空的,手机端不回话
		s.clipMu.Lock()
		if s.clipWait == w {
			s.clipWait = nil
		}
		s.clipMu.Unlock()
		s.notify(notice{Type: "clipboard", Code: "replaced"})
	}
}

// record 正在录屏的话,把这个包也写进文件。写盘出错只停录屏,投屏照常
func (s *session) record(pkt []byte) {
	s.recMu.Lock()
	defer s.recMu.Unlock()
	if s.rec == nil {
		return
	}
	if err := s.rec.feed(pkt); err != nil {
		res := s.rec.finish()
		s.rec = nil
		go s.notify(recordedNotice(res, "录屏写文件出错，已经停了："+err.Error()))
	}
}

// takeRecording 停掉录屏、把文件收尾。没在录时第二个返回值是 false
func (s *session) takeRecording() (Recording, bool) {
	s.recMu.Lock()
	defer s.recMu.Unlock()
	if s.rec == nil {
		return Recording{}, false
	}
	res := s.rec.finish()
	s.rec = nil
	return res, true
}

func (s *session) endReason() string {
	if e := s.logs.lastError(); e != "" {
		return "投屏程序在手机上退出了:" + e
	}
	return "投屏断开了:手机拔掉了,或者投屏程序在手机上退出了"
}

// forward 发给当前接着的界面;没有界面就丢掉 —— 接上的那一刻会要一个新的关键帧
func (s *session) forward(pkt []byte) {
	s.mu.Lock()
	ws := s.ws
	s.mu.Unlock()
	if ws == nil {
		return
	}
	if err := s.write(ws, websocket.BinaryMessage, pkt); err != nil {
		s.detach(ws)
	}
}

func (s *session) write(ws *websocket.Conn, kind int, data []byte) error {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return ws.WriteMessage(kind, data)
}

// attach 界面接上来了。新接的会顶掉旧的
func (s *session) attach(ws *websocket.Conn) {
	s.mu.Lock()
	if s.isClosed() {
		s.mu.Unlock()
		_ = s.write(ws, websocket.TextMessage, mustJSON(notice{Type: "ended", Text: s.reason}))
		ws.Close()
		return
	}
	old := s.ws
	s.ws = ws
	if s.grace != nil {
		s.grace.Stop()
		s.grace = nil
	}
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}

	// 新接上的解码器得从配置包和关键帧开始解,让手机端重起一段编码
	if err := s.sendControl([]byte{msgResetVideo}); err != nil {
		s.close(s.endReason())
		return
	}
	if s.injectDenied.Load() {
		s.notify(injectDeniedNotice(s.info.brand))
	}
	s.readEvents(ws)
}

// readEvents 收界面发来的操作,编好发给手机。坏消息直接丢掉,不至于断开
func (s *session) readEvents(ws *websocket.Conn) {
	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			s.detach(ws)
			return
		}
		var e event
		if json.Unmarshal(data, &e) != nil {
			continue
		}
		msg, err := encodeEvent(e)
		if err != nil {
			continue
		}
		if e.T == "paste" {
			s.keepClipboard()
		}
		if err := s.sendControl(msg); err != nil {
			s.close(s.endReason())
			return
		}
	}
}

// detach 界面断开了。会话再留一会儿,等它接回来
func (s *session) detach(ws *websocket.Conn) {
	s.mu.Lock()
	if s.ws != ws {
		s.mu.Unlock()
		return
	}
	s.ws = nil
	if s.grace == nil && !s.isClosed() {
		s.grace = time.AfterFunc(detachGrace, func() {
			s.mu.Lock()
			idle := s.ws == nil
			s.mu.Unlock()
			if idle {
				s.close("")
			}
		})
	}
	s.mu.Unlock()
	ws.Close()
}

func (s *session) sendControl(msg []byte) error {
	s.ctrlMu.Lock()
	defer s.ctrlMu.Unlock()
	_ = s.control.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, err := s.control.Write(msg)
	return err
}

// notice 发给界面的文字消息
type notice struct {
	// Type notice = 提醒一句,会话照常;ended = 会话结束了;clipboard = 手机剪贴板的内容;
	// recorded = 录屏停了(Files、Ms 是结果);transfer = 拖进来的文件处理到哪了
	Type string `json:"type"`
	// Code 细分:inject-denied = 不让模拟点击;replaced = 第一次粘贴前剪贴板里原来的内容
	Code string `json:"code,omitempty"`
	Text string `json:"text"`
	// Files 录屏存成的文件,转过屏会有好几段
	Files []string `json:"files,omitempty"`
	// Ms 录了多久
	Ms int64 `json:"ms,omitempty"`
}

func (s *session) notify(n notice) {
	s.mu.Lock()
	ws := s.ws
	s.mu.Unlock()
	if ws != nil {
		_ = s.write(ws, websocket.TextMessage, mustJSON(n))
	}
}

func (s *session) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// close 收掉这一路。reason 非空时先告诉界面为什么断了
func (s *session) close(reason string) {
	s.once.Do(func() {
		s.mu.Lock()
		s.reason = reason
		close(s.closed)
		ws := s.ws
		s.ws = nil
		if s.grace != nil {
			s.grace.Stop()
		}
		video, control := s.video, s.control
		s.mu.Unlock()

		// 录着屏就断了(拔线、关面板):把文件收尾,不然最后一段没有时长
		res, recording := s.takeRecording()
		if ws != nil {
			if recording {
				_ = s.write(ws, websocket.TextMessage, mustJSON(recordedNotice(res, "")))
			}
			if reason != "" {
				_ = s.write(ws, websocket.TextMessage, mustJSON(notice{Type: "ended", Text: reason}))
			}
			ws.Close()
		}
		for _, c := range []net.Conn{video, control, s.shell} {
			if c != nil {
				c.Close()
			}
		}
		if s.onClose != nil {
			s.onClose(s)
		}
	})
}

// ---- 小工具 ----

// logTail 手机端程序最近的输出
type logTail struct {
	mu    sync.Mutex
	lines []string
}

const maxLogLines = 40

func (l *logTail) add(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, line)
	if len(l.lines) > maxLogLines {
		l.lines = l.lines[len(l.lines)-maxLogLines:]
	}
}

// lastError 最后一条 ERROR 的内容,去掉前缀
func (l *logTail) lastError() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.lines) - 1; i >= 0; i-- {
		if _, msg, ok := strings.Cut(l.lines[i], "ERROR: "); ok {
			return strings.TrimSpace(msg)
		}
	}
	return ""
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// randomScid 31 位随机数,手机端用它区分同一台手机上的几路投屏
func randomScid() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint32(b[:]) & 0x7fffffff
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
