package mirror

import (
	"crypto/des"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"tool_forge/backend/tools/iosmux"
)

// 接真的越狱 iPhone 走一遍:查 TrollVNC → 按这一次的设置开服务 → 经本机 WebSocket 握手、
// 认证到拿到画面尺寸 → 停掉服务、确认端口关了。只看不点:不往手机发任何触摸和按键。
// 默认跳过;插着手机时这样跑(SSH 密码从环境变量给,别写进代码):
//
//	IOS_LIVE=1 IOS_SSH_PASSWORD=<密码> go test ./backend/tools/mirror -run TestLiveIOS -v
func TestLiveIOS(t *testing.T) {
	if os.Getenv("IOS_LIVE") != "1" {
		t.Skip("要接真 iPhone;设 IOS_LIVE=1 才跑")
	}
	dev, err := iosmux.PickDevice(os.Getenv("IOS_UDID"))
	if err != nil {
		t.Fatal(err)
	}
	client, _, err := iosmux.DialSSH(dev, 22, "root", os.Getenv("IOS_SSH_PASSWORD"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	svc := New()
	defer svc.CloseAll()
	target := IOSTarget{SSH: client, UDID: dev.UDID, Owner: "live"}

	st, err := svc.TrollStatus(target)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("手机:%+v", *st)
	if !st.Installed {
		t.Skip("这台没装 TrollVNC")
	}

	begin := time.Now()
	info, err := svc.StartIOS(target, IOSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("服务起来用了 %v,机型 %s", time.Since(begin).Round(time.Millisecond), info.DeviceName)

	ws, _, err := websocket.DefaultDialer.Dial(info.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	w, h, name, err := vncLogin(&wsStream{ws: ws}, info.Password)
	ws.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("VNC 认证通过:画面 %dx%d,名字 %q", w, h, name)
	if w == 0 || h == 0 {
		t.Fatal("画面尺寸是 0")
	}

	svc.StopIOS(info.ID)
	if c, err := dialPhone(dev.UDID, trollPort); err == nil {
		c.Close()
		t.Fatal("停了以后手机上的 VNC 端口还开着")
	}
	t.Log("停了以后端口已关")
}

// wsStream 把 WebSocket 的二进制消息接成一条字节流,VNC 握手按字节读
type wsStream struct {
	ws  *websocket.Conn
	buf []byte
}

func (s *wsStream) Read(p []byte) (int, error) {
	for len(s.buf) == 0 {
		_ = s.ws.SetReadDeadline(time.Now().Add(10 * time.Second))
		_, data, err := s.ws.ReadMessage()
		if err != nil {
			return 0, err
		}
		s.buf = data
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

func (s *wsStream) Write(p []byte) (int, error) {
	return len(p), s.ws.WriteMessage(websocket.BinaryMessage, p)
}

// vncLogin 握手、经典 VNC 密码认证、ClientInit,读到 ServerInit 为止
func vncLogin(rw io.ReadWriter, password string) (w, h int, name string, err error) {
	ver := make([]byte, 12)
	if _, err = io.ReadFull(rw, ver); err != nil {
		return
	}
	if _, err = rw.Write([]byte("RFB 003.008\n")); err != nil {
		return
	}
	n := make([]byte, 1)
	if _, err = io.ReadFull(rw, n); err != nil {
		return
	}
	types := make([]byte, n[0])
	if _, err = io.ReadFull(rw, types); err != nil {
		return
	}
	if _, err = rw.Write([]byte{2}); err != nil {
		return
	}
	challenge := make([]byte, 16)
	if _, err = io.ReadFull(rw, challenge); err != nil {
		return
	}
	// 经典 VNC 认证:密码补到 8 字节、每个字节位序反过来当 DES 密钥,加密服务端给的 16 字节
	key := make([]byte, 8)
	copy(key, password)
	for i, b := range key {
		var r byte
		for j := 0; j < 8; j++ {
			r = r<<1 | (b>>j)&1
		}
		key[i] = r
	}
	block, err := des.NewCipher(key)
	if err != nil {
		return
	}
	resp := make([]byte, 16)
	block.Encrypt(resp[:8], challenge[:8])
	block.Encrypt(resp[8:], challenge[8:])
	if _, err = rw.Write(resp); err != nil {
		return
	}
	result := make([]byte, 4)
	if _, err = io.ReadFull(rw, result); err != nil {
		return
	}
	if binary.BigEndian.Uint32(result) != 0 {
		err = fmt.Errorf("VNC 认证没通过(%d)", binary.BigEndian.Uint32(result))
		return
	}
	if _, err = rw.Write([]byte{1}); err != nil {
		return
	}
	init := make([]byte, 24)
	if _, err = io.ReadFull(rw, init); err != nil {
		return
	}
	w, h = int(binary.BigEndian.Uint16(init)), int(binary.BigEndian.Uint16(init[2:]))
	nameBuf := make([]byte, binary.BigEndian.Uint32(init[20:]))
	if _, err = io.ReadFull(rw, nameBuf); err != nil {
		return
	}
	return w, h, string(nameBuf), nil
}
