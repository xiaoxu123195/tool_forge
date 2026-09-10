// Package iosmux 直接和本机的 usbmuxd 说话:列出插着的 iOS 设备,
// 并把一条 TCP 连接接到设备上的某个端口。
//
// usbmuxd 是苹果的 USB 复用守护进程(Windows 上叫 Apple Mobile Device Service,
// 随 iTunes / Apple Devices 装上;macOS 自带)。它在本机监听一个端口,
// 客户端用 plist 跟它说"我要连 3 号设备的 22 端口",它答应之后,
// **这条 socket 本身就变成了通往设备那个端口的隧道**。
//
// 这一点是整个包成立的前提:拿到的是一个普通的 net.Conn,
// 可以直接交给 SSH、HTTP 或者任何认 net.Conn 的东西,
// 不需要在本机开转发端口,也就不需要为此跑一个转发进程。
package iosmux

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"strconv"
	"strings"
	"time"

	"howett.net/plist"
)

const (
	// headerLen 每条消息前面固定 16 字节:长度、协议版本、消息类型、标签
	headerLen = 16
	// protoPlist 协议版本号,1 表示负载是 plist
	protoPlist uint32 = 1
	// msgPlistPayload 消息类型,8 表示"负载自己说自己是什么"
	msgPlistPayload uint32 = 8
	// maxPayload 一条响应的上限。设备列表也就几百字节,
	// 给到 4MB 纯粹是防着一个坏掉的长度字段让我们去分配几个 G
	maxPayload = 4 << 20
	// handshakeTimeout 握手阶段的超时。
	// 只管到"连上设备端口"为止 —— 之后那条连接是要长期用的,不能带着超时
	handshakeTimeout = 10 * time.Second
)

// Device 一台连着的设备
type Device struct {
	// ID usbmuxd 给的编号,拔插一次就会变,只在本次会话里有意义
	ID int `json:"id"`
	// UDID 设备的序列号,这才是稳定的身份
	UDID string `json:"udid"`
	// Type USB 还是 Network(无线调试)
	Type string `json:"type"`
}

// ErrNoDevice 一台设备都没有
var ErrNoDevice = errors.New("没有找到 iOS 设备")

// Available 本机的 usbmuxd 在不在。
// 不在的话后面所有操作都没意义,可以提前给一句人话
func Available() bool {
	conn, err := dialMux()
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// UnavailableHint usbmuxd 连不上时说给人听的
const UnavailableHint = "连不上本机的 usbmuxd —— iOS 设备是靠它走 USB 的。" +
	"Windows 上它随 iTunes / Apple Devices 一起装,叫 Apple Mobile Device Service;" +
	"装了的话确认这个服务正在运行"

// ListDevices 列出当前插着的设备
func ListDevices() ([]Device, error) {
	conn, err := dialMux()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", UnavailableHint, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))

	if err := send(conn, 1, newRequest("ListDevices")); err != nil {
		return nil, err
	}
	body, err := recv(conn)
	if err != nil {
		return nil, err
	}

	var resp struct {
		DeviceList []struct {
			DeviceID   int `plist:"DeviceID"`
			Properties struct {
				SerialNumber   string `plist:"SerialNumber"`
				ConnectionType string `plist:"ConnectionType"`
			} `plist:"Properties"`
		} `plist:"DeviceList"`
	}
	if _, err := plist.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("设备列表读不懂: %w", err)
	}

	out := make([]Device, 0, len(resp.DeviceList))
	for _, d := range resp.DeviceList {
		out = append(out, Device{
			ID:   d.DeviceID,
			UDID: d.Properties.SerialNumber,
			Type: d.Properties.ConnectionType,
		})
	}
	return out, nil
}

// PickDevice 挑一台。want 是 UDID(可以只给前几位);空 = 第一台 USB 设备。
//
// 优先 USB:无线连着的那台也会出现在列表里,但取证要的是插在这儿的这一台,
// 而且无线连接随时会断
func PickDevice(want string) (Device, error) {
	devs, err := ListDevices()
	if err != nil {
		return Device{}, err
	}
	if len(devs) == 0 {
		return Device{}, ErrNoDevice
	}

	want = strings.TrimSpace(want)
	if want != "" {
		for _, d := range devs {
			if strings.EqualFold(d.UDID, want) || strings.HasPrefix(strings.ToLower(d.UDID), strings.ToLower(want)) {
				return d, nil
			}
		}
		var got []string
		for _, d := range devs {
			got = append(got, d.UDID)
		}
		return Device{}, fmt.Errorf("没有 UDID 是 %s 的设备(在线的:%s)", want, strings.Join(got, ", "))
	}

	for _, d := range devs {
		if d.Type == "USB" {
			return d, nil
		}
	}
	return devs[0], nil
}

// Dial 把一条连接接到设备的某个端口上。
//
// 返回的 net.Conn 就是通往设备那个端口的隧道,由调用方负责关闭。
// 上面**没有留超时** —— 握手时设的 deadline 在成功后会被清掉,
// 不然一条准备长期用的连接会在十秒后突然开始报超时
func Dial(deviceID, port int) (net.Conn, error) {
	conn, err := dialMux()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", UnavailableHint, err)
	}
	if err := connectPort(conn, deviceID, port); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func connectPort(conn net.Conn, deviceID, port int) error {
	if err := conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return err
	}
	req := connectRequest{
		request:  newRequest("Connect"),
		DeviceID: deviceID,
		// 端口号在这里是大端的,而 plist 里存的是个普通整数 ——
		// 所以要自己把两个字节调个个儿。写 22 而不调,到那头就成了 5632
		Port: swap16(port),
	}
	if err := send(conn, 1, req.flat()); err != nil {
		return err
	}
	body, err := recv(conn)
	if err != nil {
		return err
	}
	var resp struct {
		Number uint32 `plist:"Number"`
	}
	if _, err := plist.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("连接响应读不懂: %w", err)
	}
	if resp.Number != 0 {
		return fmt.Errorf("usbmuxd 拒绝连接设备 %d 的 %d 端口:%s", deviceID, port, replyText(resp.Number))
	}
	// 握手完了就把超时摘掉,后面这条连接归调用方长期使用
	return conn.SetDeadline(time.Time{})
}

// replyText 把 usbmuxd 的返回码翻成能照着做点什么的话
func replyText(code uint32) string {
	switch code {
	case 0:
		return "ok"
	case 1:
		return "命令不对"
	case 2:
		return "设备不对(多半是刚被拔掉了)"
	case 3:
		return "端口拒绝连接 —— 设备上那个端口没人监听(要连 22 的话,先确认设备上装了并启动了 SSH)"
	case 6:
		return "协议版本不对"
	default:
		return "未知返回码 " + strconv.Itoa(int(code))
	}
}

// request 每条请求都要带的三个字段。
// usbmuxd 会拿 ProgName 记日志,给个能认出来的名字
type request struct {
	MessageType         string `plist:"MessageType"`
	ClientVersionString string `plist:"ClientVersionString"`
	ProgName            string `plist:"ProgName"`
}

func newRequest(msgType string) request {
	return request{
		MessageType:         msgType,
		ClientVersionString: "tool_forge",
		ProgName:            "tool_forge",
	}
}

// connectRequest 连设备某个端口的请求
type connectRequest struct {
	request
	DeviceID int `plist:"DeviceID"`
	Port     int `plist:"PortNumber"`
}

// flatConnect 是 connectRequest 真正发出去的样子:所有字段摊平在一层。
//
// 这里必须摊平,不能直接把嵌了 request 的结构体丢给编码器。
// plist 的编码器按反射走,而 request 是个未导出的类型 —— 嵌进去之后
// 它整个被当成未导出字段跳过了,MessageType / ProgName 三个字段凭空消失,
// 而且不报任何错。发出去的 dict 里没有 MessageType,usbmuxd 只回一个错误码,
// 每个端口都连不上,连 lockdownd 都连不上。
//
// 别把它改回嵌入结构 —— 那样"看起来更干净",但错误是静默的
type flatConnect struct {
	MessageType         string `plist:"MessageType"`
	ClientVersionString string `plist:"ClientVersionString"`
	ProgName            string `plist:"ProgName"`
	DeviceID            int    `plist:"DeviceID"`
	Port                int    `plist:"PortNumber"`
}

func (r connectRequest) flat() flatConnect {
	return flatConnect{
		MessageType:         r.MessageType,
		ClientVersionString: r.ClientVersionString,
		ProgName:            r.ProgName,
		DeviceID:            r.DeviceID,
		Port:                r.Port,
	}
}

func dialMux() (net.Conn, error) {
	if runtime.GOOS == "windows" {
		return net.DialTimeout("tcp", "127.0.0.1:27015", handshakeTimeout)
	}
	return net.DialTimeout("unix", "/var/run/usbmuxd", handshakeTimeout)
}

// send 发一条 plist 消息:16 字节头 + XML 负载
func send(conn net.Conn, tag uint32, payload any) error {
	var body bytes.Buffer
	if err := plist.NewEncoder(&body).Encode(payload); err != nil {
		return err
	}
	var buf bytes.Buffer
	// 长度算的是"连头在内"的总长
	_ = binary.Write(&buf, binary.LittleEndian, uint32(body.Len()+headerLen))
	_ = binary.Write(&buf, binary.LittleEndian, protoPlist)
	_ = binary.Write(&buf, binary.LittleEndian, msgPlistPayload)
	_ = binary.Write(&buf, binary.LittleEndian, tag)
	buf.Write(body.Bytes())

	_, err := conn.Write(buf.Bytes())
	return err
}

// recv 收一条消息,返回 plist 负载
func recv(conn net.Conn) ([]byte, error) {
	var head [headerLen]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return nil, fmt.Errorf("读不到响应头: %w", err)
	}
	total := binary.LittleEndian.Uint32(head[0:4])
	if total < headerLen {
		return nil, fmt.Errorf("响应头里的长度不合法: %d", total)
	}
	n := total - headerLen
	if n > maxPayload {
		return nil, fmt.Errorf("响应过大(%d 字节),不像是正常的 usbmuxd 消息", n)
	}
	body := make([]byte, n)
	// 必须读满。TCP 一次 Read 给多少是不定的,
	// 少读的那几个字节会变成下一条消息的头,之后全乱套
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, fmt.Errorf("响应体没读全: %w", err)
	}
	return body, nil
}

// swap16 把低 16 位的两个字节对调
func swap16(v int) int {
	return ((v << 8) & 0xFF00) | ((v >> 8) & 0xFF)
}
