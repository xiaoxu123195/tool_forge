package mirror

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// 不接真手机,用一个假的 adb 服务端扮演手机,把整条链路走一遍:
// 启动 → 界面接上 → 收画面 → 发点击 → 断开

func TestMirrorEndToEnd(t *testing.T) {
	dev := newFakeDevice(t)
	svc := newTestService(t)

	info, err := svc.Start(StartRequest{Options: Options{MaxSize: 1280, BitRate: 8_000_000, MaxFps: 60}})
	if err != nil {
		t.Fatal(err)
	}
	if info.DeviceName != "FakePhone" {
		t.Errorf("设备名 %q", info.DeviceName)
	}
	cmd := dev.shellCommand()
	for _, want := range []string{"com.genymobile.scrcpy.Server 5.0.1", "tunnel_forward=true", "video_codec=h264", "max_size=1280"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("启动命令里没有 %q: %s", want, cmd)
		}
	}

	// 口令不对的连不上
	bad := strings.Replace(info.URL, "t=", "t=x", 1)
	if _, resp, err := websocket.DefaultDialer.Dial(bad, nil); err == nil || resp == nil || resp.StatusCode != 403 {
		t.Fatalf("错的口令应该被拒绝: %v", err)
	}

	ws := dial(t, info.URL)
	// 界面一接上,就要一段新的编码:不然解码器拿不到配置包和关键帧
	if got := dev.readControl(t, 1); got[0] != msgResetVideo {
		t.Fatalf("接上后应该先发重置视频,得到 % x", got)
	}
	dev.sendVideo(t, sessionPacket(540, 1200))
	dev.sendVideo(t, mediaPacket(1<<62, 0, []byte{0, 0, 0, 1, 0x67, 0x42}))
	dev.sendVideo(t, mediaPacket(1<<61, 1000, []byte{0, 0, 0, 1, 0x65}))
	for i, check := range []func([]byte) bool{
		func(p []byte) bool { return len(p) == 12 && p[0] == 0x80 && binary.BigEndian.Uint32(p[4:]) == 540 },
		func(p []byte) bool { return len(p) == 18 && p[0]&0x40 != 0 },
		func(p []byte) bool { return len(p) == 17 && p[0]&0x20 != 0 },
	} {
		if p := readBinary(t, ws); !check(p) {
			t.Fatalf("第 %d 个包不对: % x", i+1, p)
		}
	}

	// 界面上点一下 → 手机收到一条触摸
	send(t, ws, event{T: "touch", A: actionDown, X: 10, Y: 20, W: 540, H: 1200})
	got := dev.readControl(t, 32)
	if got[0] != msgInjectTouch || binary.BigEndian.Uint32(got[10:]) != 10 || binary.BigEndian.Uint16(got[18:]) != 540 {
		t.Fatalf("触摸消息不对: % x", got)
	}
	// 坏消息丢掉,连接不断:后面的操作照样能到
	if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"t":"shell","x":"rm"}`)); err != nil {
		t.Fatal(err)
	}
	send(t, ws, event{T: "back", A: actionDown})
	if got := dev.readControl(t, 2); got[0] != msgBackOrScreenOn {
		t.Fatalf("坏消息之后的返回键没到: % x", got)
	}

	svc.Stop(info.ID)
	dev.waitClosed(t, "shell", "video", "control")
	if svc.get(info.ID) != nil {
		t.Fatal("停掉之后会话还在")
	}
}

// 手机拔线:界面要收到一句为什么断了,会话收掉
func TestMirrorDeviceGone(t *testing.T) {
	dev := newFakeDevice(t)
	svc := newTestService(t)
	info, err := svc.Start(StartRequest{Options: Options{BitRate: 8_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	ws := dial(t, info.URL)
	dev.readControl(t, 1)

	dev.closeVideo()
	n := readNotice(t, ws)
	if n.Type != "ended" || !strings.Contains(n.Text, "断开") {
		t.Fatalf("应该收到断开的说明,得到 %+v", n)
	}
	waitFor(t, func() bool { return svc.get(info.ID) == nil }, "会话没收掉")
}

// 小米一类机型不让模拟点击:手机端打出那句日志时,界面要收到提醒。
// 认得出品牌就直接说是哪个开关,认不出的给通用说法
func TestMirrorInjectDenied(t *testing.T) {
	for _, c := range []struct{ brand, want string }{
		{"", "USB 调试（安全设置）"},
		{"oneplus oneplus", "禁止权限监控"},
	} {
		dev := newFakeDevice(t)
		svc := newTestService(t)
		svc.prepare = func(string, string) (deviceInfo, error) {
			return deviceInfo{serial: "fake", sdk: 30, brand: c.brand}, nil
		}
		info, err := svc.Start(StartRequest{Options: Options{BitRate: 8_000_000}})
		if err != nil {
			t.Fatal(err)
		}
		ws := dial(t, info.URL)
		dev.readControl(t, 1)

		dev.log("[server] ERROR: Injecting input events requires the caller (or the source of the instrumentation, if any) to have the INJECT_EVENTS permission.")
		n := readNotice(t, ws)
		if n.Type != "notice" || n.Code != "inject-denied" || !strings.Contains(n.Text, c.want) {
			t.Fatalf("品牌 %q 应该提醒去开「%s」,得到 %+v", c.brand, c.want, n)
		}
		svc.Stop(info.ID)
	}
}

// 读剪贴板:界面要,手机回,界面收到;界面按的是 Ctrl+C 就先让手机按复制
func TestMirrorClipboardRead(t *testing.T) {
	dev := newFakeDevice(t)
	svc := newTestService(t)
	info, err := svc.Start(StartRequest{Options: Options{BitRate: 8_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	if info.SDK != 33 {
		t.Errorf("安卓版本没交给界面: %d", info.SDK)
	}
	ws := dial(t, info.URL)
	dev.readControl(t, 1)

	send(t, ws, event{T: "getclip", A: copyKeyCopy})
	if got := dev.readControl(t, 2); !bytes.Equal(got, []byte{msgGetClipboard, copyKeyCopy}) {
		t.Fatalf("读剪贴板的消息不对: % x", got)
	}
	dev.writeControl(t, clipboardReply("手机上复制的"))
	if n := readNotice(t, ws); n.Type != "clipboard" || n.Code != "" || n.Text != "手机上复制的" {
		t.Fatalf("剪贴板内容没交给界面: %+v", n)
	}
}

// 中文靠剪贴板粘贴:第一次写剪贴板之前,先把手机上原来的内容读出来交给界面 ——
// 那可能正是要找的东西。第二次就不再读了(剪贴板里已经是我们自己粘的字)
func TestMirrorPasteKeepsClipboard(t *testing.T) {
	dev := newFakeDevice(t)
	svc := newTestService(t)
	info, err := svc.Start(StartRequest{Options: Options{BitRate: 8_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	ws := dial(t, info.URL)
	dev.readControl(t, 1)

	send(t, ws, event{T: "paste", S: "你好"})
	if got := dev.readControl(t, 2); !bytes.Equal(got, []byte{msgGetClipboard, copyKeyNone}) {
		t.Fatalf("粘贴前该先读一次原来的剪贴板: % x", got)
	}
	dev.writeControl(t, clipboardReply("原来复制着的"))
	if n := readNotice(t, ws); n.Type != "clipboard" || n.Code != "replaced" || n.Text != "原来复制着的" {
		t.Fatalf("原来的剪贴板内容没交给界面: %+v", n)
	}
	want := setClipboardMessage(0, "你好", true)
	if got := dev.readControl(t, len(want)); !bytes.Equal(got, want) {
		t.Fatalf("粘贴的消息不对: % x", got)
	}

	send(t, ws, event{T: "paste", S: "再来"})
	want = setClipboardMessage(0, "再来", true)
	if got := dev.readControl(t, len(want)); !bytes.Equal(got, want) {
		t.Fatalf("第二次粘贴不该再读剪贴板: % x", got)
	}
}

// 剪贴板原来是空的:手机端不回话,等一小会儿照样粘贴
func TestMirrorPasteEmptyClipboard(t *testing.T) {
	old := clipboardWait
	clipboardWait = 100 * time.Millisecond
	t.Cleanup(func() { clipboardWait = old })
	dev := newFakeDevice(t)
	svc := newTestService(t)
	info, err := svc.Start(StartRequest{Options: Options{BitRate: 8_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	ws := dial(t, info.URL)
	dev.readControl(t, 1)

	send(t, ws, event{T: "paste", S: "abc中"})
	dev.readControl(t, 2)
	if n := readNotice(t, ws); n.Type != "clipboard" || n.Code != "replaced" || n.Text != "" {
		t.Fatalf("空剪贴板也该告诉界面一声: %+v", n)
	}
	want := setClipboardMessage(0, "abc中", true)
	if got := dev.readControl(t, len(want)); !bytes.Equal(got, want) {
		t.Fatalf("等完没粘贴: % x", got)
	}
}

// 录屏:开录马上要一个关键帧;手机发来的包原样存成 MP4,停下时把最后一段写完
func TestMirrorRecording(t *testing.T) {
	dev := newFakeDevice(t)
	svc := newTestService(t)
	info, err := svc.Start(StartRequest{Options: Options{BitRate: 8_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	ws := dial(t, info.URL)
	dev.readControl(t, 1)

	dir := t.TempDir()
	path, err := svc.StartRecording(info.ID, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dev.readControl(t, 1); got[0] != msgResetVideo {
		t.Fatalf("开录没要关键帧: % x", got)
	}
	if !strings.HasPrefix(filepath.Base(path), "FakePhone_录屏_") || filepath.Dir(path) != dir {
		t.Errorf("文件名不对: %s", path)
	}
	if _, err := svc.StartRecording(info.ID, dir); err == nil {
		t.Error("正在录的时候不该再开一路")
	}

	sent := sendTestStream(t, dev, 540, 1200, 0, 30)
	for i := 0; i < sent; i++ {
		// 画面转到了界面,说明录像那边也处理过了:同一个协程里先写文件再转发
		readBinary(t, ws)
	}
	rec, err := svc.StopRecording(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Files) != 1 || rec.Files[0] != path {
		t.Fatalf("录出来的文件不对: %+v", rec)
	}
	// 30 帧,每帧 1/30 秒,最后一帧按前一帧的时长算
	if rec.DurationMs < 990 || rec.DurationMs > 1100 {
		t.Errorf("时长 %d 毫秒不对", rec.DurationMs)
	}
	checkMP4(t, path, 540, 1200, 31)
	if _, err := svc.StopRecording(info.ID); err == nil {
		t.Error("停了之后再停该报没在录")
	}
}

// 录着屏手机拔了:文件照样收尾,界面先收到录好的文件,再收到断开
func TestMirrorRecordingSavedOnDisconnect(t *testing.T) {
	dev := newFakeDevice(t)
	svc := newTestService(t)
	info, err := svc.Start(StartRequest{Options: Options{BitRate: 8_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	ws := dial(t, info.URL)
	dev.readControl(t, 1)
	path, err := svc.StartRecording(info.ID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dev.readControl(t, 1)
	sent := sendTestStream(t, dev, 540, 1200, 0, 10)
	for i := 0; i < sent; i++ {
		readBinary(t, ws)
	}

	dev.closeVideo()
	if n := readNotice(t, ws); n.Type != "recorded" || len(n.Files) != 1 || n.Files[0] != path || n.Ms <= 0 {
		t.Fatalf("断开时该先交代录好的文件: %+v", n)
	}
	if n := readNotice(t, ws); n.Type != "ended" {
		t.Fatalf("然后才是断开: %+v", n)
	}
	checkMP4(t, path, 540, 1200, 11)
}

// 截图:手机自己截的原图。前面混着的警告要去掉;同一秒连截两张不能互相覆盖
func TestMirrorScreenshot(t *testing.T) {
	dev := newFakeDevice(t)
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 3, 5))); err != nil {
		t.Fatal(err)
	}
	dev.screencap = append([]byte("[Warning] Multiple displays were found, but no display id was specified!\n"), img.Bytes()...)
	svc := newTestService(t)
	info, err := svc.Start(StartRequest{Options: Options{BitRate: 8_000_000}})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	shot, err := svc.Screenshot(info.ID, dir)
	if err != nil {
		t.Fatal(err)
	}
	if shot.Width != 3 || shot.Height != 5 || !strings.HasPrefix(filepath.Base(shot.Path), "FakePhone_截图_") {
		t.Fatalf("截图信息不对: %+v", shot)
	}
	if data, _ := os.ReadFile(shot.Path); !bytes.Equal(data, img.Bytes()) {
		t.Fatal("存下来的不是那张图")
	}
	again, err := svc.Screenshot(info.ID, dir)
	if err != nil || again.Path == shot.Path {
		t.Fatalf("连截两张把前一张覆盖了: %v %v", again, err)
	}
	if _, err := svc.Screenshot(info.ID, filepath.Join(dir, "没有这个")); err == nil || !strings.Contains(err.Error(), "保存的文件夹不在了") {
		t.Fatalf("文件夹不在了要说清楚: %v", err)
	}
	svc.Stop(info.ID)
	if _, err := svc.Screenshot(info.ID, dir); err == nil {
		t.Fatal("断开之后不该还能截")
	}
}

// 手机端程序起不来:报给界面的应该是它自己日志里的那句错误
func TestMirrorStartFailure(t *testing.T) {
	dev := newFakeDevice(t)
	dev.failStart = "[server] ERROR: Could not open video stream: encoder error"
	svc := newTestService(t)
	start := time.Now()
	_, err := svc.Start(StartRequest{Options: Options{BitRate: 8_000_000}})
	if err == nil || !strings.Contains(err.Error(), "Could not open video stream") {
		t.Fatalf("应该带上手机端的错误,得到 %v", err)
	}
	// 程序已经退出了,不该傻等满 10 秒
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("花了 %v 才报错", d)
	}
}

// 界面断开后留一会儿:接回来的那一条照样能用,并且重新要一个关键帧
func TestMirrorReattach(t *testing.T) {
	dev := newFakeDevice(t)
	svc := newTestService(t)
	info, err := svc.Start(StartRequest{Options: Options{BitRate: 8_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	first := dial(t, info.URL)
	dev.readControl(t, 1)
	first.Close()

	second := dial(t, info.URL)
	if got := dev.readControl(t, 1); got[0] != msgResetVideo {
		t.Fatalf("接回来后应该再要一次关键帧: % x", got)
	}
	dev.sendVideo(t, sessionPacket(540, 1200))
	if p := readBinary(t, second); len(p) != 12 {
		t.Fatalf("接回来的那条收不到画面: % x", p)
	}

	// 断开且没人接回来:过了保留时间就收掉
	second.Close()
	waitFor(t, func() bool { return svc.get(info.ID) == nil }, "没人接回来,会话却一直没收")
	dev.waitClosed(t, "video", "control")
}

// ---- 假手机 ----

type fakeDevice struct {
	ln        net.Listener
	failStart string // 非空:shell 一启动就打出这行日志然后退出,本地套接字一律拒绝
	screencap []byte // exec:screencap -p 的输出

	mu        sync.Mutex
	cmd       string
	shell     net.Conn
	video     net.Conn
	control   net.Conn
	abstracts int
	closed    chan string
}

func newFakeDevice(t *testing.T) *fakeDevice {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	old := adbAddr
	adbAddr = ln.Addr().String()
	d := &fakeDevice{ln: ln, closed: make(chan string, 16)}
	t.Cleanup(func() {
		adbAddr = old
		ln.Close()
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go d.handle(c)
		}
	}()
	return d
}

func (d *fakeDevice) handle(c net.Conn) {
	if req, err := readRequest(c); err != nil || req != "host:transport:fake" {
		fmt.Fprintf(c, "FAIL%04x%s", len("no device"), "no device")
		c.Close()
		return
	}
	c.Write([]byte("OKAY"))
	svc, err := readRequest(c)
	if err != nil {
		c.Close()
		return
	}
	switch {
	case strings.HasPrefix(svc, "shell:"):
		c.Write([]byte("OKAY"))
		d.mu.Lock()
		d.cmd, d.shell = strings.TrimPrefix(svc, "shell:"), c
		d.mu.Unlock()
		if d.failStart != "" {
			c.Write([]byte(d.failStart + "\n"))
			c.Close()
			return
		}
		d.watch(c, "shell")
	case strings.HasPrefix(svc, "localabstract:scrcpy_"):
		if d.failStart != "" {
			fmt.Fprintf(c, "FAIL%04x%s", len("closed"), "closed")
			c.Close()
			return
		}
		c.Write([]byte("OKAY"))
		d.mu.Lock()
		d.abstracts++
		first := d.abstracts == 1
		if first {
			d.video = c
		} else {
			d.control = c
		}
		video := d.video
		d.mu.Unlock()
		if first {
			// 第一条连接先只给一个探测字节
			c.Write([]byte{0})
			d.watch(c, "video")
			return
		}
		// 和真的手机端一样:两条都接上之后,才在第一条上发 64 字节的设备名,
		// 编码器起来后再报编码。控制通道上的数据由用例自己读(见 readControl)
		name := make([]byte, deviceNameSize)
		copy(name, "FakePhone")
		var codec [4]byte
		binary.BigEndian.PutUint32(codec[:], codecH264)
		video.Write(append(name, codec[:]...))
	case svc == "exec:screencap -p":
		c.Write([]byte("OKAY"))
		c.Write(d.screencap)
		c.Close()
	default:
		fmt.Fprintf(c, "FAIL%04x%s", len("unknown service"), "unknown service")
		c.Close()
	}
}

// watch 等对方把连接关掉,记下来
func (d *fakeDevice) watch(c net.Conn, name string) {
	_, _ = io.Copy(io.Discard, c)
	d.closed <- name
}

func (d *fakeDevice) shellCommand() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cmd
}

func (d *fakeDevice) readControl(t *testing.T, n int) []byte {
	t.Helper()
	d.mu.Lock()
	c := d.control
	d.mu.Unlock()
	if c == nil {
		t.Fatal("控制通道还没连上")
	}
	buf := make([]byte, n)
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("没收到控制消息: %v", err)
	}
	return buf
}

// writeControl 手机经控制通道发回一条消息
func (d *fakeDevice) writeControl(t *testing.T, p []byte) {
	t.Helper()
	d.mu.Lock()
	c := d.control
	d.mu.Unlock()
	if _, err := c.Write(p); err != nil {
		t.Fatal(err)
	}
}

func clipboardReply(text string) []byte {
	b := []byte{deviceMsgClipboard, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(b[1:], uint32(len(text)))
	return append(b, text...)
}

// sendTestStream 发一段画面:会话包、配置包、一个关键帧,再跟 n 个普通帧(每帧 1/30 秒)。
// 返回一共发了几个包
func sendTestStream(t *testing.T, d *fakeDevice, w, h uint32, pts0 uint64, n int) int {
	t.Helper()
	d.sendVideo(t, sessionPacket(w, h))
	d.sendVideo(t, mediaPacket(1<<62, 0, testConfig))
	d.sendVideo(t, mediaPacket(1<<61, pts0, testKeyFrame))
	for i := 1; i <= n; i++ {
		d.sendVideo(t, mediaPacket(0, pts0+uint64(i)*33_333, testDeltaFrame))
	}
	return 3 + n
}

func (d *fakeDevice) sendVideo(t *testing.T, p []byte) {
	t.Helper()
	d.mu.Lock()
	c := d.video
	d.mu.Unlock()
	if _, err := c.Write(p); err != nil {
		t.Fatal(err)
	}
}

func (d *fakeDevice) closeVideo() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.video.Close()
}

func (d *fakeDevice) log(line string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.shell.Write([]byte(line + "\n"))
}

// waitClosed 等这几条连接都被我们这边关掉。控制通道不在 watch 里(用例要读它),
// 所以用一次读来判断
func (d *fakeDevice) waitClosed(t *testing.T, names ...string) {
	t.Helper()
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	if want["control"] {
		delete(want, "control")
		d.mu.Lock()
		c := d.control
		d.mu.Unlock()
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, err := c.Read(make([]byte, 1))
		// 读到数据或者读超时,都说明我们这边还开着
		var ne net.Error
		if err == nil || (errors.As(err, &ne) && ne.Timeout()) {
			t.Fatalf("控制通道没被关掉: %v", err)
		}
	}
	deadline := time.After(3 * time.Second)
	for len(want) > 0 {
		select {
		case n := <-d.closed:
			delete(want, n)
		case <-deadline:
			t.Fatalf("这些连接没被关掉: %v", want)
		}
	}
}

func readRequest(c net.Conn) (string, error) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(c, head); err != nil {
		return "", err
	}
	n, err := strconv.ParseUint(string(head), 16, 16)
	if err != nil {
		return "", err
	}
	buf := make([]byte, n)
	_, err = io.ReadFull(c, buf)
	return string(buf), err
}

// ---- 界面这一侧 ----

func newTestService(t *testing.T) *Service {
	t.Helper()
	old := detachGrace
	detachGrace = 300 * time.Millisecond
	svc := New()
	svc.prepare = func(string, string) (deviceInfo, error) { return deviceInfo{serial: "fake", sdk: 33}, nil }
	t.Cleanup(func() {
		svc.CloseAll()
		detachGrace = old
	})
	return svc
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	return ws
}

func readBinary(t *testing.T, ws *websocket.Conn) []byte {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	kind, data, err := ws.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage {
		t.Fatalf("没收到画面: kind=%d err=%v", kind, err)
	}
	return data
}

func readNotice(t *testing.T, ws *websocket.Conn) notice {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		kind, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("没收到文字消息: %v", err)
		}
		if kind != websocket.TextMessage {
			continue
		}
		var n notice
		if err := json.Unmarshal(data, &n); err != nil {
			t.Fatal(err)
		}
		return n
	}
}

func send(t *testing.T, ws *websocket.Conn, e event) {
	t.Helper()
	if err := ws.WriteJSON(e); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, ok func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
