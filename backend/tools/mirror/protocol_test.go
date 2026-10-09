package mirror

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

// 下面四个用例的期望字节抄自 scrcpy 5.0.1 客户端自己的单元测试
// (app/tests/test_control_msg_serialize.c):两边编出来一模一样,手机端才认

func TestKeycodeMessageMatchesUpstream(t *testing.T) {
	got := keycodeMessage(actionUp, 0x42, 5, 0x41)
	want := []byte{
		msgInjectKeycode,
		0x01,
		0x00, 0x00, 0x00, 0x42,
		0x00, 0x00, 0x00, 0x05,
		0x00, 0x00, 0x00, 0x41,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("\n got  % x\n want % x", got, want)
	}
}

func TestTouchMessageMatchesUpstream(t *testing.T) {
	got := touchMessage(actionDown, 0x1234567887654321, position{100, 200, 1080, 1920}, 1, buttonPrimary, buttonPrimary)
	want := []byte{
		msgInjectTouch,
		0x00,
		0x12, 0x34, 0x56, 0x78, 0x87, 0x65, 0x43, 0x21,
		0x00, 0x00, 0x00, 0x64, 0x00, 0x00, 0x00, 0xc8,
		0x04, 0x38, 0x07, 0x80,
		0xff, 0xff,
		0x00, 0x00, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x01,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("\n got  % x\n want % x", got, want)
	}
}

func TestScrollMessageMatchesUpstream(t *testing.T) {
	got := scrollMessage(position{260, 1026, 1080, 1920}, 16, -16, 1)
	want := []byte{
		msgInjectScroll,
		0x00, 0x00, 0x01, 0x04, 0x00, 0x00, 0x04, 0x02,
		0x04, 0x38, 0x07, 0x80,
		0x7f, 0xff,
		0x80, 0x00,
		0x00, 0x00, 0x00, 0x01,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("\n got  % x\n want % x", got, want)
	}
}

func TestBackOrScreenOnMatchesUpstream(t *testing.T) {
	if got := backOrScreenOnMessage(actionUp); !bytes.Equal(got, []byte{msgBackOrScreenOn, 0x01}) {
		t.Fatalf("got % x", got)
	}
}

// 下面几个同样抄自上游的 test_control_msg_serialize.c

func TestTextMessageMatchesUpstream(t *testing.T) {
	got := textMessages("hello, world!")
	want := append([]byte{msgInjectText, 0x00, 0x00, 0x00, 0x0d}, "hello, world!"...)
	if !bytes.Equal(got, want) {
		t.Fatalf("\n got  % x\n want % x", got, want)
	}
}

func TestSetClipboardMatchesUpstream(t *testing.T) {
	got := setClipboardMessage(0x0102030405060708, "hello, world!", true)
	want := append([]byte{
		msgSetClipboard,
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		1,
		0x00, 0x00, 0x00, 0x0d,
	}, "hello, world!"...)
	if !bytes.Equal(got, want) {
		t.Fatalf("\n got  % x\n want % x", got, want)
	}
	// 超长的截到上限,整条消息正好 256K
	long := setClipboardMessage(1, strings.Repeat("a", clipboardTextMaxLength+10), false)
	if len(long) != 1<<18 || long[9] != 0 {
		t.Fatalf("超长剪贴板没截对: 长度 %d", len(long))
	}
}

func TestGetClipboardMatchesUpstream(t *testing.T) {
	if got := getClipboardMessage(copyKeyCopy); !bytes.Equal(got, []byte{msgGetClipboard, 0x01}) {
		t.Fatalf("got % x", got)
	}
}

func TestScanFileMatchesUpstream(t *testing.T) {
	got, ok := scanFileMessage("/sdcard/Download")
	want := append([]byte{msgScanFile, 0x00, 0x00, 0x00, 0x10}, "/sdcard/Download"...)
	if !ok || !bytes.Equal(got, want) {
		t.Fatalf("\n got  % x\n want % x", got, want)
	}
	// 太长的路径手机端会截断,截断了扫的就是别的路径,干脆不发
	if _, ok := scanFileMessage("/sdcard/" + strings.Repeat("长", 100)); ok {
		t.Fatal("超长路径不该发")
	}
}

// 长文字切成几条,每条不超过 300 字节,而且不能从一个汉字中间切开
func TestTextMessagesSplit(t *testing.T) {
	text := strings.Repeat("a", 299) + "中文" + strings.Repeat("b", 400)
	b := textMessages(text)
	var parts []string
	for len(b) > 0 {
		if b[0] != msgInjectText {
			t.Fatalf("不是文字消息: % x", b[:1])
		}
		n := int(binary.BigEndian.Uint32(b[1:]))
		if n > injectTextMaxLength {
			t.Fatalf("一条 %d 字节,超过上限", n)
		}
		part := string(b[5 : 5+n])
		if !utf8.ValidString(part) {
			t.Fatalf("切出了半个字: %q", part)
		}
		parts = append(parts, part)
		b = b[5+n:]
	}
	if strings.Join(parts, "") != text || len(parts) != 3 || parts[0] != strings.Repeat("a", 299) {
		t.Fatalf("切法不对: %d 段,第一段 %d 字节", len(parts), len(parts[0]))
	}
}

func TestReadDeviceMessage(t *testing.T) {
	var stream bytes.Buffer
	// 上游 test_device_msg_deserialize.c 里的三种消息
	stream.Write([]byte{deviceMsgClipboard, 0x00, 0x00, 0x00, 0x03, 0x41, 0x42, 0x43})
	stream.Write([]byte{deviceMsgAckClipboard, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08})
	stream.Write([]byte{deviceMsgUhidOutput, 0, 42, 0, 5, 0x01, 0x02, 0x03, 0x04, 0x05})
	stream.Write(append([]byte{deviceMsgClipboard, 0, 0, 0, 6}, "中文"...))

	for i, want := range []deviceMessage{{deviceMsgClipboard, "ABC"}, {deviceMsgAckClipboard, ""}, {deviceMsgUhidOutput, ""}, {deviceMsgClipboard, "中文"}} {
		got, err := readDeviceMessage(&stream)
		if err != nil || got != want {
			t.Fatalf("第 %d 条: %+v %v,应为 %+v", i+1, got, err, want)
		}
	}
	if _, err := readDeviceMessage(&stream); err != io.EOF {
		t.Fatalf("读完应该是 EOF,得到 %v", err)
	}
	if _, err := readDeviceMessage(bytes.NewReader([]byte{9, 1, 2})); !errors.Is(err, errUnknownDeviceMessage) {
		t.Fatalf("认不出的类型要报出来,得到 %v", err)
	}
	huge := []byte{deviceMsgClipboard, 0xff, 0xff, 0xff, 0xff}
	if _, err := readDeviceMessage(bytes.NewReader(huge)); err == nil {
		t.Fatal("长度离谱的剪贴板应该报错")
	}
}

func TestFixedPoint(t *testing.T) {
	cases := []struct {
		f    float64
		u16  uint16
		i16  uint16
		name string
	}{
		{0, 0, 0, "零"},
		{1, 0xffff, 0x7fff, "满格"},
		{0.5, 0x8000, 0x4000, "一半"},
		{-1, 0, 0x8000, "负满格"},
		{2, 0xffff, 0x7fff, "超出上限要钳住"},
		{-3, 0, 0x8000, "超出下限要钳住"},
	}
	for _, c := range cases {
		if got := u16FixedPoint(c.f); got != c.u16 {
			t.Errorf("%s: u16FixedPoint(%v) = %#x,应为 %#x", c.name, c.f, got, c.u16)
		}
		if got := i16FixedPoint(c.f); got != c.i16 {
			t.Errorf("%s: i16FixedPoint(%v) = %#x,应为 %#x", c.name, c.f, got, c.i16)
		}
	}
}

// 界面上的鼠标:按下、拖动、抬起要编成和官方客户端一样的触摸消息
func TestEncodeEventTouch(t *testing.T) {
	type fields struct {
		action               byte
		pointer              uint64
		x, y                 uint32
		w, h                 uint16
		pressure             uint16
		actionButton, button uint32
	}
	decode := func(b []byte) fields {
		be := binary.BigEndian
		return fields{b[1], be.Uint64(b[2:]), be.Uint32(b[10:]), be.Uint32(b[14:]), be.Uint16(b[18:]), be.Uint16(b[20:]),
			be.Uint16(b[22:]), be.Uint32(b[24:]), be.Uint32(b[28:])}
	}
	cases := []struct {
		a    int
		want fields
	}{
		{actionDown, fields{0, pointerMouse, 10, 20, 540, 1200, 0xffff, 1, 1}},
		{actionMove, fields{2, pointerMouse, 10, 20, 540, 1200, 0xffff, 0, 1}},
		{actionUp, fields{1, pointerMouse, 10, 20, 540, 1200, 0, 1, 0}},
	}
	for _, c := range cases {
		b, err := encodeEvent(event{T: "touch", A: c.a, X: 10, Y: 20, W: 540, H: 1200})
		if err != nil {
			t.Fatal(err)
		}
		if len(b) != 32 || b[0] != msgInjectTouch {
			t.Fatalf("动作 %d: 消息不对 % x", c.a, b)
		}
		if got := decode(b); got != c.want {
			t.Errorf("动作 %d:\n got  %+v\n want %+v", c.a, got, c.want)
		}
	}
}

func TestEncodeEventGuards(t *testing.T) {
	// 坐标拖出画面外:钳到边上,而不是把一个画面外的点发给手机
	b, err := encodeEvent(event{T: "touch", A: actionMove, X: -5, Y: 99999, W: 540, H: 1200})
	if err != nil {
		t.Fatal(err)
	}
	if x, y := binary.BigEndian.Uint32(b[10:]), binary.BigEndian.Uint32(b[14:]); x != 0 || y != 1199 {
		t.Errorf("坐标没钳住: %d,%d", x, y)
	}

	bad := []event{
		{T: "touch", A: actionDown, X: 1, Y: 1, W: 0, H: 100},
		{T: "touch", A: actionDown, X: 1, Y: 1, W: 70000, H: 100},
		{T: "touch", A: 7, X: 1, Y: 1, W: 100, H: 100},
		{T: "scroll", X: 1, Y: 1, W: 100, H: 100, VS: math.NaN()},
		{T: "scroll", X: 1, Y: 1, W: 100, H: 100, HS: math.Inf(1)},
		{T: "key", A: actionDown, K: 0},
		{T: "key", A: 2, K: 4},
		{T: "key", A: actionDown, K: 29, M: 0x80000000},
		{T: "key", A: actionDown, K: 29, M: -1},
		{T: "back", A: 3},
		{T: "touch", A: actionDown, P: 3, X: 1, Y: 1, W: 100, H: 100},
		{T: "touch", A: 5, P: 1, X: 1, Y: 1, W: 100, H: 100},
		{T: "text"},
		{T: "text", S: strings.Repeat("a", maxTypedText+1)},
		{T: "text", S: "\xff\xfe"},
		{T: "paste"},
		{T: "getclip", A: 3},
		{T: "panel", A: 3},
		{T: "shell"},
	}
	for _, e := range bad {
		if _, err := encodeEvent(e); err == nil {
			t.Errorf("%+v 应该被拒绝", e)
		}
	}

	if b, _ := encodeEvent(event{T: "key", A: actionDown, K: 3}); !bytes.Equal(b, keycodeMessage(actionDown, 3, 0, 0)) {
		t.Errorf("主页键编错了: % x", b)
	}
	// Ctrl+A:修饰键原样带上
	if b, _ := encodeEvent(event{T: "key", A: actionDown, K: 29, M: 0x3000}); !bytes.Equal(b, keycodeMessage(actionDown, 29, 0, 0x3000)) {
		t.Errorf("Ctrl+A 编错了: % x", b)
	}
	if b, _ := encodeEvent(event{T: "reset"}); !bytes.Equal(b, []byte{msgResetVideo}) {
		t.Errorf("重置视频编错了: % x", b)
	}
	// 滚轮往下一格:安卓里向下是负的;超过 16 格钳住
	b, _ = encodeEvent(event{T: "scroll", X: 1, Y: 1, W: 100, H: 100, HS: 40, VS: -1})
	if hs, vs := binary.BigEndian.Uint16(b[13:]), binary.BigEndian.Uint16(b[15:]); hs != 0x7fff || vs != 0xf800 {
		t.Errorf("滚轮编错了: hs=%#x vs=%#x", hs, vs)
	}

	for _, c := range []struct {
		e    event
		want []byte
	}{
		{event{T: "text", S: "abc"}, textMessages("abc")},
		{event{T: "paste", S: "你好"}, setClipboardMessage(0, "你好", true)},
		{event{T: "getclip", A: copyKeyNone}, []byte{msgGetClipboard, 0}},
		{event{T: "getclip", A: copyKeyCut}, []byte{msgGetClipboard, 2}},
		{event{T: "panel", A: 0}, []byte{msgExpandNotificationPanel}},
		{event{T: "panel", A: 1}, []byte{msgExpandSettingsPanel}},
		{event{T: "panel", A: 2}, []byte{msgCollapsePanels}},
	} {
		if b, err := encodeEvent(c.e); err != nil || !bytes.Equal(b, c.want) {
			t.Errorf("%+v 编成了 % x(%v),应为 % x", c.e, b, err, c.want)
		}
	}
}

// 双指缩放的两根手指:各用各的指针号,当成真的触摸注入,不带鼠标按键
func TestEncodeEventFingers(t *testing.T) {
	for _, c := range []struct {
		p, a     int
		pointer  uint64
		pressure uint16
	}{
		{1, actionDown, pointerFinger, 0xffff},
		{2, actionDown, pointerVirtualFinger, 0xffff},
		{2, actionMove, pointerVirtualFinger, 0xffff},
		{1, actionUp, pointerFinger, 0},
	} {
		b, err := encodeEvent(event{T: "touch", A: c.a, P: c.p, X: 10, Y: 20, W: 540, H: 1200})
		if err != nil {
			t.Fatal(err)
		}
		be := binary.BigEndian
		if b[1] != byte(c.a) || be.Uint64(b[2:]) != c.pointer || be.Uint16(b[22:]) != c.pressure || be.Uint32(b[24:]) != 0 || be.Uint32(b[28:]) != 0 {
			t.Errorf("手指 %d 动作 %d 编错了: % x", c.p, c.a, b)
		}
	}
	if pointerFinger != 0xfffffffffffffffe || pointerVirtualFinger != 0xfffffffffffffffd {
		t.Fatal("手指的指针号和上游的 -2、-3 对不上")
	}
}

func TestReadPacket(t *testing.T) {
	var stream bytes.Buffer
	stream.Write(sessionPacket(1080, 2400))
	stream.Write(mediaPacket(1<<62, 0, []byte{0, 0, 0, 1, 0x67}))
	stream.Write(mediaPacket(1<<61, 33_000, []byte{0, 0, 0, 1, 0x65, 0xaa}))

	hdr := make([]byte, headerSize)
	p, err := readPacket(&stream, hdr)
	if err != nil || len(p) != 12 || p[0] != 0x80 || binary.BigEndian.Uint32(p[4:]) != 1080 || binary.BigEndian.Uint32(p[8:]) != 2400 {
		t.Fatalf("会话包不对: % x %v", p, err)
	}
	p, err = readPacket(&stream, hdr)
	if err != nil || len(p) != 17 || p[0]&0x40 == 0 {
		t.Fatalf("配置包不对: % x %v", p, err)
	}
	p, err = readPacket(&stream, hdr)
	if err != nil || len(p) != 18 || p[0]&0x20 == 0 || binary.BigEndian.Uint64(p)&(1<<61-1) != 33_000 {
		t.Fatalf("关键帧不对: % x %v", p, err)
	}
	if _, err := readPacket(&stream, hdr); err != io.EOF {
		t.Fatalf("读完应该是 EOF,得到 %v", err)
	}

	// 长度大得离谱 = 流错位了,不能照着它去分配内存
	huge := make([]byte, headerSize)
	binary.BigEndian.PutUint32(huge[8:], maxPacketSize+1)
	if _, err := readPacket(bytes.NewReader(huge), hdr); err == nil {
		t.Fatal("超长的包应该报错")
	}
	// 负载没读全
	short := mediaPacket(0, 1, []byte{1, 2, 3})
	if _, err := readPacket(bytes.NewReader(short[:len(short)-1]), hdr); err == nil {
		t.Fatal("负载不全应该报错")
	}
}

func TestReadDeviceName(t *testing.T) {
	for _, name := range []string{"GM1910", "小米 14 Pro"} {
		buf := make([]byte, deviceNameSize)
		copy(buf, name)
		if got, err := readDeviceName(bytes.NewReader(buf)); err != nil || got != name {
			t.Errorf("读出 %q %v,应为 %q", got, err, name)
		}
	}
}

func TestServerCommand(t *testing.T) {
	got := serverCommand(0x1234abcd, Options{MaxSize: 1280, BitRate: 8_000_000, MaxFps: 60})
	want := "CLASSPATH=/data/local/tmp/scrcpy-server.jar app_process / com.genymobile.scrcpy.Server 5.0.1 " +
		"scid=1234abcd log_level=info tunnel_forward=true audio=false video_codec=h264 " +
		"max_size=1280 video_bit_rate=8000000 clipboard_autosync=false max_fps=60"
	if got != want {
		t.Fatalf("\n got  %s\n want %s", got, want)
	}
	if got := serverCommand(1, Options{BitRate: 8_000_000}); strings.Contains(got, "max_fps") || strings.Contains(got, "keep_active") {
		t.Errorf("不限帧率时不该带 max_fps,没要保持亮屏时不该带 keep_active: %s", got)
	}
	// 保持亮屏用的是定时报「有人在用」,不是改手机设置的 stay_awake
	got = serverCommand(1, Options{BitRate: 8_000_000, KeepAwake: true})
	if !strings.HasSuffix(got, " keep_active=true") || strings.Contains(got, "stay_awake") {
		t.Errorf("保持亮屏的参数不对: %s", got)
	}
	if socketName(0xab) != "scrcpy_000000ab" {
		t.Errorf("套接字名不对: %s", socketName(0xab))
	}
}

func TestOptionsValidate(t *testing.T) {
	o := func(size, rate, fps int) Options { return Options{MaxSize: size, BitRate: rate, MaxFps: fps} }
	good := []Options{o(0, 8_000_000, 0), o(1280, 4_000_000, 60), o(320, 500_000, 120)}
	for _, o := range good {
		if err := o.validate(); err != nil {
			t.Errorf("%+v 应该通过: %v", o, err)
		}
	}
	bad := []Options{o(100, 8_000_000, 60), o(5000, 8_000_000, 60), o(1280, 100, 60), o(1280, 8_000_000, -1), o(1280, 8_000_000, 500)}
	for _, o := range bad {
		if err := o.validate(); err == nil {
			t.Errorf("%+v 应该被拒绝", o)
		}
	}
}

// 内置的手机端程序必须是官方发布的那一份
func TestBundledServer(t *testing.T) {
	sum := sha256.Sum256(serverJar)
	if got := hex.EncodeToString(sum[:]); got != serverSHA256 {
		t.Fatalf("内置的手机端程序校验值是 %s,和记录的 %s 不一致", got, serverSHA256)
	}
}

// ---- 构造测试用的包 ----

func sessionPacket(w, h uint32) []byte {
	b := make([]byte, headerSize)
	binary.BigEndian.PutUint32(b, 0x80000000)
	binary.BigEndian.PutUint32(b[4:], w)
	binary.BigEndian.PutUint32(b[8:], h)
	return b
}

func mediaPacket(flags, pts uint64, payload []byte) []byte {
	b := make([]byte, headerSize+len(payload))
	binary.BigEndian.PutUint64(b, flags|pts)
	binary.BigEndian.PutUint32(b[8:], uint32(len(payload)))
	copy(b[headerSize:], payload)
	return b
}
