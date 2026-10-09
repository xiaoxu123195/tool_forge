package mirror

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"howett.net/plist"
)

// 不接真手机:fakePhone 扮演越狱 iPhone —— SSH 上跑的脚本记下来、按内容回话,
// VNC 端口由一个假服务端应答。把「开服务 → 界面接上 → 两头对拷 → 关服务」走一遍

const rootlessProbe = `prefix=/var/jb
arch=iphoneos-arm64
model=iPhone10,2
daemon=/var/jb/Library/LaunchDaemons/com.82flex.trollvnc.plist
version=3.2-272
loader=install ok installed
`

type fakePhone struct {
	mu      sync.Mutex
	probe   string
	scripts []string
	stdins  [][]byte
	running bool // 最近一次写设置后把服务起来了
	noAuth  bool // 服务端不要密码:设置没生效的样子
	// installed 跑过 dpkg -i 之后,probe 里带上 daemon
	installed bool
	got       chan []byte // VNC 握手之后服务端收到的
}

func newFakePhone(probe string) *fakePhone {
	return &fakePhone{probe: probe, got: make(chan []byte, 16)}
}

func (p *fakePhone) run(script string, stdin io.Reader, _ time.Duration) (string, error) {
	var in []byte
	if stdin != nil {
		in, _ = io.ReadAll(stdin)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scripts = append(p.scripts, script)
	p.stdins = append(p.stdins, in)
	switch {
	case script == probeScript:
		if p.installed && !strings.Contains(p.probe, "daemon=") {
			return p.probe + "daemon=/var/jb/Library/LaunchDaemons/com.82flex.trollvnc.plist\nversion=3.2-272\n", nil
		}
		return p.probe, nil
	case strings.Contains(script, "dpkg -i"):
		p.installed = true
	case strings.Contains(script, "launchctl load"):
		p.running = true
	case strings.Contains(script, "launchctl unload"):
		p.running = false
	}
	return "", nil
}

func (p *fakePhone) dial(_ string, port int) (net.Conn, error) {
	p.mu.Lock()
	running, noAuth := p.running, p.noAuth
	p.mu.Unlock()
	if !running || port != trollPort {
		return nil, errors.New("端口拒绝连接")
	}
	a, b := net.Pipe()
	go p.serve(b, noAuth)
	return a, nil
}

// serve 假的 VNC 服务端:报版本、列认证方式,之后收到的都交给测试
func (p *fakePhone) serve(c net.Conn, noAuth bool) {
	defer c.Close()
	if _, err := c.Write([]byte("RFB 003.008\n")); err != nil {
		return
	}
	ver := make([]byte, 12)
	if _, err := io.ReadFull(c, ver); err != nil {
		return
	}
	types := []byte{1, 2}
	if noAuth {
		types = []byte{1, 1}
	}
	if _, err := c.Write(types); err != nil {
		return
	}
	buf := make([]byte, 256)
	for {
		n, err := c.Read(buf)
		if n > 0 {
			p.got <- append([]byte(nil), buf[:n]...)
		}
		if err != nil {
			return
		}
	}
}

func (p *fakePhone) isRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// last 最近一次跑的脚本和它的标准输入
func (p *fakePhone) last() (string, []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := len(p.scripts)
	return p.scripts[n-1], p.stdins[n-1]
}

func (p *fakePhone) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.scripts)
}

func newIOSTestService(t *testing.T, p *fakePhone) *Service {
	t.Helper()
	oldAttach, oldGrace := iosAttachWait, iosDetachGrace
	iosAttachWait, iosDetachGrace = 2*time.Second, 300*time.Millisecond
	svc := New()
	svc.iosDial = p.dial
	t.Cleanup(func() {
		svc.CloseAll()
		iosAttachWait, iosDetachGrace = oldAttach, oldGrace
	})
	return svc
}

func decodePrefs(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if _, err := plist.Unmarshal(data, &m); err != nil {
		t.Fatalf("设置文件解不开: %v\n%s", err, data)
	}
	return m
}

func TestIOSMirrorEndToEnd(t *testing.T) {
	phone := newFakePhone(rootlessProbe)
	svc := newIOSTestService(t, phone)

	info, err := svc.startIOS(phone, "udid-1", "dev-1", IOSOptions{KeepAwake: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Password) != 8 || info.DeviceName != "iPhone 8 Plus" || !strings.Contains(info.URL, "/vnc/") {
		t.Fatalf("会话信息不对: %+v", info)
	}
	script, stdin := phone.scripts[1], phone.stdins[1]
	for _, want := range []string{
		"'/var/jb/var/mobile/Library/Preferences/com.82flex.trollvnc.plist'",
		"'/var/mobile/Library/Preferences/com.82flex.trollvnc.plist'",
		"killall -9 cfprefsd",
		"launchctl load -w '/var/jb/Library/LaunchDaemons/com.82flex.trollvnc.plist'",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("启动脚本里没有 %q:\n%s", want, script)
		}
	}
	prefs := decodePrefs(t, stdin)
	for k, want := range map[string]any{
		"Enabled": true, "FullPassword": info.Password, "BindHost": "127.0.0.1",
		"BonjourEnabled": false, "HttpPort": uint64(0), "Port": uint64(trollPort), "KeepAliveSec": uint64(30),
	} {
		if prefs[k] != want {
			t.Errorf("设置 %s = %#v,应该是 %#v", k, prefs[k], want)
		}
	}

	// 口令不对的连不上
	bad := strings.Replace(info.URL, "t=", "t=x", 1)
	if _, resp, err := websocket.DefaultDialer.Dial(bad, nil); err == nil || resp == nil || resp.StatusCode != 403 {
		t.Fatalf("错的口令应该被拒绝: %v", err)
	}

	// 两头对拷:手机的版本号到界面,界面的回话到手机
	ws := dial(t, info.URL)
	if got := readBinary(t, ws); string(got) != "RFB 003.008\n" {
		t.Fatalf("界面收到的不是版本号: %q", got)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, []byte("RFB 003.008\n")); err != nil {
		t.Fatal(err)
	}
	if got := readBinary(t, ws); !bytes.Equal(got, []byte{1, 2}) {
		t.Fatalf("认证方式没转过来: % x", got)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-phone.got:
		if string(got) != "hello" {
			t.Fatalf("手机收到 %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("界面发的没到手机")
	}

	svc.StopIOS(info.ID)
	script, stdin = phone.last()
	if strings.Contains(script, "launchctl load") || !strings.Contains(script, "launchctl unload") {
		t.Fatalf("停的时候应该只卸载服务:\n%s", script)
	}
	prefs = decodePrefs(t, stdin)
	if prefs["Enabled"] != false || prefs["FullPassword"] != nil {
		t.Fatalf("停了以后设置应该是关着、没有密码: %#v", prefs)
	}
	if svc.getIOS(info.ID) != nil || phone.isRunning() {
		t.Fatal("停了以后会话或服务还在")
	}
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Fatal("停了以后界面的连接应该断开")
	}
}

func TestIOSRefusesServerWithoutPassword(t *testing.T) {
	phone := newFakePhone(rootlessProbe)
	phone.noAuth = true
	svc := newIOSTestService(t, phone)
	_, err := svc.startIOS(phone, "udid-1", "dev-1", IOSOptions{})
	if !errors.Is(err, errVNCNoAuth) {
		t.Fatalf("不要密码的服务应该当失败: %v", err)
	}
	if phone.isRunning() || len(svc.iosSessions()) != 0 {
		t.Fatal("不要密码的服务必须马上停掉")
	}
}

func TestIOSNotInstalled(t *testing.T) {
	phone := newFakePhone("prefix=/var/jb\narch=iphoneos-arm64\nmodel=iPhone10,2\nloader=install ok installed\n")
	svc := newIOSTestService(t, phone)
	if _, err := svc.startIOS(phone, "udid-1", "dev-1", IOSOptions{}); !errors.Is(err, errTrollMissing) {
		t.Fatalf("没装应该明说: %v", err)
	}
	if phone.count() != 1 {
		t.Fatalf("没装就不该往手机写东西,跑了 %d 段脚本", phone.count())
	}
}

func TestIOSRestartTakesOver(t *testing.T) {
	phone := newFakePhone(rootlessProbe)
	svc := newIOSTestService(t, phone)
	a, err := svc.startIOS(phone, "udid-1", "dev-1", IOSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.startIOS(phone, "udid-1", "dev-1", IOSOptions{KeepAwake: true})
	if err != nil {
		t.Fatal(err)
	}
	if svc.getIOS(a.ID) != nil || svc.getIOS(b.ID) == nil || a.Password == b.Password {
		t.Fatal("重开应该顶掉旧的一路,并换一个新密码")
	}
	// 旧的那一路再收一次(界面的清理晚到了)不能把新开的服务停掉
	n := phone.count()
	svc.StopIOS(a.ID)
	if phone.count() != n || !phone.isRunning() {
		t.Fatal("收旧的一路把新开的服务停了")
	}
	svc.StopIOS(b.ID)
	if phone.isRunning() {
		t.Fatal("新的一路收掉后服务应该停")
	}
}

func TestIOSStopsWhenNobodyAttaches(t *testing.T) {
	phone := newFakePhone(rootlessProbe)
	svc := newIOSTestService(t, phone)
	iosAttachWait = 200 * time.Millisecond
	info, err := svc.startIOS(phone, "udid-1", "dev-1", IOSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return svc.getIOS(info.ID) == nil && !phone.isRunning() }, "界面一直没接上来,服务应该自己停")
}

func TestIOSStopsAfterDetachGrace(t *testing.T) {
	phone := newFakePhone(rootlessProbe)
	svc := newIOSTestService(t, phone)
	info, err := svc.startIOS(phone, "udid-1", "dev-1", IOSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ws := dial(t, info.URL)
	readBinary(t, ws)
	ws.Close()
	waitFor(t, func() bool { return svc.getIOS(info.ID) == nil && !phone.isRunning() }, "界面断开不回来,服务应该停")
}

func TestIOSReinstallWhileMirroring(t *testing.T) {
	phone := newFakePhone(rootlessProbe)
	svc := newIOSTestService(t, phone)
	info, err := svc.startIOS(phone, "udid-1", "dev-1", IOSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pkg := writeZip(t, map[string]string{"com.82flex.trollvnc_3.2-273_iphoneos-arm64.deb": "new"})
	if _, err := svc.installIOS(phone, "udid-1", pkg); err != nil {
		t.Fatal(err)
	}
	if svc.getIOS(info.ID) != nil || phone.isRunning() {
		t.Fatal("换包之前要先收掉正在投的那一路,装完服务是停着的")
	}
	// 最后两段:dpkg 装新包(标准输入就是包),再查一遍装没装上
	n := phone.count()
	if !strings.Contains(phone.scripts[n-2], "dpkg -i") || string(phone.stdins[n-2]) != "new" || phone.scripts[n-1] != probeScript {
		t.Fatal("新包没推上去")
	}
}

func TestStopIOSOwnedBy(t *testing.T) {
	p1, p2 := newFakePhone(rootlessProbe), newFakePhone(rootlessProbe)
	svc := newIOSTestService(t, p1)
	svc.iosDial = func(udid string, port int) (net.Conn, error) {
		if udid == "udid-2" {
			return p2.dial(udid, port)
		}
		return p1.dial(udid, port)
	}
	a, err := svc.startIOS(p1, "udid-1", "dev-1", IOSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.startIOS(p2, "udid-2", "dev-2", IOSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	svc.StopIOSOwnedBy("dev-1")
	if svc.getIOS(a.ID) != nil || p1.isRunning() {
		t.Fatal("dev-1 名下的投屏应该收掉")
	}
	if svc.getIOS(b.ID) == nil || !p2.isRunning() {
		t.Fatal("别的连接名下的投屏不该动")
	}
}
