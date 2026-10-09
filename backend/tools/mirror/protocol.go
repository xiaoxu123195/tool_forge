package mirror

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf8"
)

// 和手机端程序之间的二进制协议。字节布局照着 5.0.1 服务端的
// DesktopConnection、Streamer、ControlMessageReader、DeviceMessageWriter;多字节整数一律大端。

// ---- 视频流 ----

const (
	// codecH264 视频流开头的编码 ID,就是 "h264" 四个字母
	codecH264 = 0x68323634
	// deviceNameSize 设备名字段定长 64 字节,UTF-8,0 结尾
	deviceNameSize = 64
	// headerSize 每个包都以 12 字节开头。最高位是 1 的是会话包(只有这 12 字节:标志、宽、高),
	// 否则是帧包:8 字节时间戳和标志、4 字节负载长度,后面跟着负载
	headerSize = 12
	// maxPacketSize 一帧 H.264 再大也到不了这个数,超过就是流已经错位了
	maxPacketSize = 32 << 20
)

// readDeviceName 读连接开头的设备名(手机报的型号)
func readDeviceName(r io.Reader) (string, error) {
	buf := make([]byte, deviceNameSize)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		buf = buf[:i]
	}
	return string(buf), nil
}

// readCodecID 视频流开头的编码 ID。0 和 1 是手机端在说「这一路不发了」:
// 0 是它主动关掉,1 是配置出错
func readCodecID(r io.Reader) (uint32, error) {
	var b [4]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b[:]), nil
}

// readPacket 读一个包,原样返回(头加负载)—— 界面那头自己解析,这里只管切对边界。
// hdr 是调用方给的 12 字节缓冲,省得每帧分配一次
func readPacket(r io.Reader, hdr []byte) ([]byte, error) {
	if _, err := io.ReadFull(r, hdr[:headerSize]); err != nil {
		return nil, err
	}
	if hdr[0]&0x80 != 0 {
		return append([]byte(nil), hdr[:headerSize]...), nil
	}
	size := binary.BigEndian.Uint32(hdr[8:headerSize])
	if size > maxPacketSize {
		return nil, fmt.Errorf("视频包长度 %d 不对,流已经错位了", size)
	}
	pkt := make([]byte, headerSize+int(size))
	copy(pkt, hdr[:headerSize])
	if _, err := io.ReadFull(r, pkt[headerSize:]); err != nil {
		return nil, err
	}
	return pkt, nil
}

// ---- 控制消息 ----

const (
	msgInjectKeycode           = 0
	msgInjectText              = 1
	msgInjectTouch             = 2
	msgInjectScroll            = 3
	msgBackOrScreenOn          = 4
	msgExpandNotificationPanel = 5
	msgExpandSettingsPanel     = 6
	msgCollapsePanels          = 7
	msgGetClipboard            = 8
	msgSetClipboard            = 9
	msgResetVideo              = 17
	msgScanFile                = 22
)

const (
	// injectTextMaxLength 一条文字消息最多多少字节,多了手机端只收前面的。长的切成几条发
	injectTextMaxLength = 300
	// clipboardTextMaxLength 写剪贴板的文字最多多少字节:整条消息 256K,
	// 减去类型、序号、粘贴标志、长度这 14 个字节
	clipboardTextMaxLength = 1<<18 - 14
	// scanFilePathMaxLength 让手机扫描的路径最长多少字节
	scanFilePathMaxLength = 256
)

// 读剪贴板之前要不要先按一下复制、剪切
const (
	copyKeyNone = 0
	copyKeyCopy = 1
	copyKeyCut  = 2
)

const (
	actionDown = 0
	actionUp   = 1
	actionMove = 2

	buttonPrimary = 1

	// pointerMouse 鼠标指针。只按着左键时手机端把它当成一根手指,和真的触摸没有区别
	pointerMouse = ^uint64(0)
	// pointerFinger、pointerVirtualFinger 双指缩放模拟出来的两根手指,
	// 和鼠标那根各算各的,缩放时鼠标的按下抬起不会把它们打断
	pointerFinger        = ^uint64(1)
	pointerVirtualFinger = ^uint64(2)

	// metaMask 安卓 KeyEvent.META_* 里有意义的位:Shift、Alt、Sym、Fn、Ctrl、Meta 和三个锁定键
	metaMask = 0x7770ff
)

// keycodeMessage 按键:动作、键码、重复次数、修饰键
func keycodeMessage(action uint8, keycode int32, repeat, metaState uint32) []byte {
	b := make([]byte, 14)
	b[0] = msgInjectKeycode
	b[1] = action
	binary.BigEndian.PutUint32(b[2:], uint32(keycode))
	binary.BigEndian.PutUint32(b[6:], repeat)
	binary.BigEndian.PutUint32(b[10:], metaState)
	return b
}

// touchMessage 触摸:动作、指针、位置、压力(0~1)、触发的按键、当前按着的键
func touchMessage(action uint8, pointer uint64, p position, pressure float64, actionButton, buttons uint32) []byte {
	b := make([]byte, 32)
	b[0] = msgInjectTouch
	b[1] = action
	binary.BigEndian.PutUint64(b[2:], pointer)
	p.put(b[10:])
	binary.BigEndian.PutUint16(b[22:], u16FixedPoint(pressure))
	binary.BigEndian.PutUint32(b[24:], actionButton)
	binary.BigEndian.PutUint32(b[28:], buttons)
	return b
}

// scrollMessage 滚轮。单位是「格」,线上编成 [-16, 16] 映射到 16 位定点数
func scrollMessage(p position, hscroll, vscroll float64, buttons uint32) []byte {
	b := make([]byte, 21)
	b[0] = msgInjectScroll
	p.put(b[1:])
	binary.BigEndian.PutUint16(b[13:], i16FixedPoint(hscroll/16))
	binary.BigEndian.PutUint16(b[15:], i16FixedPoint(vscroll/16))
	binary.BigEndian.PutUint32(b[17:], buttons)
	return b
}

// backOrScreenOnMessage 返回键;屏幕灭着的时候改成点亮屏幕
func backOrScreenOnMessage(action uint8) []byte {
	return []byte{msgBackOrScreenOn, action}
}

// textMessages 输入一段文字。手机端一条最多收 300 字节,长的切成几条,
// 切口落在字符边界上 —— 从一个字中间断开,两半都会变成乱码
func textMessages(text string) []byte {
	var out []byte
	for text != "" {
		n := utf8Prefix(text, injectTextMaxLength)
		b := make([]byte, 5+n)
		b[0] = msgInjectText
		binary.BigEndian.PutUint32(b[1:], uint32(n))
		copy(b[5:], text[:n])
		out = append(out, b...)
		text = text[n:]
	}
	return out
}

// setClipboardMessage 写手机剪贴板;paste 为真时写完再按一下粘贴(安卓 7 起才有这个键)。
// sequence 非 0 时手机端写完会回一条确认
func setClipboardMessage(sequence uint64, text string, paste bool) []byte {
	text = text[:utf8Prefix(text, clipboardTextMaxLength)]
	b := make([]byte, 14+len(text))
	b[0] = msgSetClipboard
	binary.BigEndian.PutUint64(b[1:], sequence)
	if paste {
		b[9] = 1
	}
	binary.BigEndian.PutUint32(b[10:], uint32(len(text)))
	copy(b[14:], text)
	return b
}

// getClipboardMessage 读手机剪贴板,内容从控制通道回来。copyKey 是读之前先按复制还是剪切
func getClipboardMessage(copyKey uint8) []byte {
	return []byte{msgGetClipboard, copyKey}
}

// scanFileMessage 让手机把一个文件登记进媒体库:推上去的照片、视频要这样才会出现在相册里。
// 路径太长手机端会截断,截断的路径扫了也白扫,所以干脆不发
func scanFileMessage(path string) ([]byte, bool) {
	if len(path) > scanFilePathMaxLength {
		return nil, false
	}
	b := make([]byte, 5+len(path))
	b[0] = msgScanFile
	binary.BigEndian.PutUint32(b[1:], uint32(len(path)))
	copy(b[5:], path)
	return b, true
}

// utf8Prefix s 里不超过 max 字节、又不切断字符的最长前缀有多长
func utf8Prefix(s string, max int) int {
	if len(s) <= max {
		return len(s)
	}
	n := max
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return n
}

// position 坐标连同它所在画面的尺寸一起发:手机一转屏尺寸就变了,
// 对不上的手机端会直接丢掉,免得点到错的地方
type position struct {
	x, y int32
	w, h uint16
}

func (p position) put(b []byte) {
	binary.BigEndian.PutUint32(b[0:], uint32(p.x))
	binary.BigEndian.PutUint32(b[4:], uint32(p.y))
	binary.BigEndian.PutUint16(b[8:], p.w)
	binary.BigEndian.PutUint16(b[10:], p.h)
}

// u16FixedPoint [0, 1] → 无符号 16 位定点,1 记作 0xffff
func u16FixedPoint(f float64) uint16 {
	switch {
	case f <= 0:
		return 0
	case f >= 1:
		return 0xffff
	}
	return uint16(f * 0x10000)
}

// i16FixedPoint [-1, 1] → 有符号 16 位定点,1 记作 0x7fff
func i16FixedPoint(f float64) uint16 {
	f = math.Max(-1, math.Min(1, f))
	i := int32(f * 0x8000)
	if i > 0x7fff {
		i = 0x7fff
	}
	return uint16(int16(i))
}

// ---- 手机发回来的消息(控制通道的另一个方向) ----

const (
	deviceMsgClipboard    = 0
	deviceMsgAckClipboard = 1
	deviceMsgUhidOutput   = 2
	// deviceClipboardMaxLength 手机发来的剪贴板最多这么多字节:整条 256K 减去类型和长度
	deviceClipboardMaxLength = 1<<18 - 5
)

// deviceMessage 手机发回来的一条消息。这里只用得上剪贴板,别的读掉就行
type deviceMessage struct {
	kind byte
	text string
}

// errUnknownDeviceMessage 认不出的消息类型:不知道它多长,后面的字节就再也对不齐了
var errUnknownDeviceMessage = errors.New("手机发来了认不出的消息")

// readDeviceMessage 读一条手机发回来的消息
func readDeviceMessage(r io.Reader) (deviceMessage, error) {
	var head [1]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return deviceMessage{}, err
	}
	m := deviceMessage{kind: head[0]}
	switch m.kind {
	case deviceMsgClipboard:
		var n [4]byte
		if _, err := io.ReadFull(r, n[:]); err != nil {
			return m, err
		}
		size := binary.BigEndian.Uint32(n[:])
		if size > deviceClipboardMaxLength {
			return m, fmt.Errorf("剪贴板长度 %d 不对,消息已经错位了", size)
		}
		buf := make([]byte, size)
		if _, err := io.ReadFull(r, buf); err != nil {
			return m, err
		}
		m.text = string(buf)
	case deviceMsgAckClipboard:
		// 8 字节序号:我们写剪贴板时不要确认,收到了也不用管
		if _, err := io.CopyN(io.Discard, r, 8); err != nil {
			return m, err
		}
	case deviceMsgUhidOutput:
		// 2 字节设备号、2 字节长度、数据。我们不模拟实体键盘,不会有这种消息,读掉以防万一
		var h [4]byte
		if _, err := io.ReadFull(r, h[:]); err != nil {
			return m, err
		}
		if _, err := io.CopyN(io.Discard, r, int64(binary.BigEndian.Uint16(h[2:]))); err != nil {
			return m, err
		}
	default:
		return m, errUnknownDeviceMessage
	}
	return m, nil
}

// ---- 界面发来的操作 ----

// event 界面上的一次操作,一条 WebSocket 文本消息
type event struct {
	// T 是哪种操作:touch(鼠标左键、双指)、scroll(滚轮)、key(按键)、back(返回)、
	// text(输入文字)、paste(经剪贴板粘贴)、getclip(读剪贴板)、panel(通知栏)、reset(要一个新的关键帧)
	T string `json:"t"`
	// A 动作:0 按下,1 抬起,2 移动(只有 touch 有)。
	// getclip 里是读之前先按复制(1)还是剪切(2);panel 里 0 拉通知栏、1 拉快捷设置、2 收起来
	A int `json:"a"`
	// X、Y 是相对 W×H 这个画面的坐标 —— 就是会话包里报的视频尺寸
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
	// HS、VS 滚了几格
	HS float64 `json:"hs"`
	VS float64 `json:"vs"`
	// K 安卓键码
	K int `json:"k"`
	// M 按键时按着的修饰键(安卓的 META_* 位),Ctrl+A 全选要靠它
	M int `json:"m"`
	// P 哪根手指:0 是鼠标,1、2 是双指缩放模拟出来的那两根
	P int `json:"p"`
	// S 要输入或粘贴的文字
	S string `json:"s"`
}

var errBadEvent = errors.New("看不懂的操作")

// maxTypedText 一次「输入文字」最多多少字节。打字是一个字一个字来的,
// 输入法一次上屏也就一句话;太长的要么是误操作,要么该走粘贴
const maxTypedText = 16 << 10

// encodeEvent 把一次操作编成控制消息(可能是好几条连在一起)。界面是自己人,但这里照样把关:
// 坐标钳在画面里,动作、键码、修饰键只认合法的值
func encodeEvent(e event) ([]byte, error) {
	switch e.T {
	case "touch":
		p, ok := e.position()
		if !ok {
			return nil, errBadEvent
		}
		if e.P == 1 || e.P == 2 {
			// 模拟出来的手指:当成真的触摸,不带鼠标按键
			ptr := pointerFinger
			if e.P == 2 {
				ptr = pointerVirtualFinger
			}
			switch e.A {
			case actionDown, actionMove:
				return touchMessage(uint8(e.A), ptr, p, 1, 0, 0), nil
			case actionUp:
				return touchMessage(actionUp, ptr, p, 0, 0, 0), nil
			}
			return nil, errBadEvent
		}
		if e.P != 0 {
			return nil, errBadEvent
		}
		// 和鼠标的语义一致:按下和抬起带上「触发的是左键」,
		// 按下、移动时左键处于按住状态,抬起后什么都没按着
		switch e.A {
		case actionDown:
			return touchMessage(actionDown, pointerMouse, p, 1, buttonPrimary, buttonPrimary), nil
		case actionMove:
			return touchMessage(actionMove, pointerMouse, p, 1, 0, buttonPrimary), nil
		case actionUp:
			return touchMessage(actionUp, pointerMouse, p, 0, buttonPrimary, 0), nil
		}
	case "scroll":
		p, ok := e.position()
		if !ok || math.IsNaN(e.HS) || math.IsNaN(e.VS) || math.IsInf(e.HS, 0) || math.IsInf(e.VS, 0) {
			return nil, errBadEvent
		}
		return scrollMessage(p, e.HS, e.VS, 0), nil
	case "key":
		if (e.A == actionDown || e.A == actionUp) && e.K > 0 && e.K < 1000 && e.M >= 0 && e.M&^metaMask == 0 {
			return keycodeMessage(uint8(e.A), int32(e.K), 0, uint32(e.M)), nil
		}
	case "back":
		if e.A == actionDown || e.A == actionUp {
			return backOrScreenOnMessage(uint8(e.A)), nil
		}
	case "text":
		if e.S != "" && len(e.S) <= maxTypedText && utf8.ValidString(e.S) {
			return textMessages(e.S), nil
		}
	case "paste":
		if e.S != "" && utf8.ValidString(e.S) {
			return setClipboardMessage(0, e.S, true), nil
		}
	case "getclip":
		if e.A == copyKeyNone || e.A == copyKeyCopy || e.A == copyKeyCut {
			return getClipboardMessage(uint8(e.A)), nil
		}
	case "panel":
		switch e.A {
		case 0:
			return []byte{msgExpandNotificationPanel}, nil
		case 1:
			return []byte{msgExpandSettingsPanel}, nil
		case 2:
			return []byte{msgCollapsePanels}, nil
		}
	case "reset":
		return []byte{msgResetVideo}, nil
	}
	return nil, errBadEvent
}

func (e event) position() (position, bool) {
	if e.W <= 0 || e.H <= 0 || e.W > math.MaxUint16 || e.H > math.MaxUint16 {
		return position{}, false
	}
	clamp := func(v, n int) int32 { return int32(max(0, min(v, n-1))) }
	return position{x: clamp(e.X, e.W), y: clamp(e.Y, e.H), w: uint16(e.W), h: uint16(e.H)}, true
}
