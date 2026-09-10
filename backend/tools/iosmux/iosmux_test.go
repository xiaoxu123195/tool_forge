package iosmux

import (
	"bytes"
	"strings"
	"testing"

	"howett.net/plist"
)

// 每条请求都必须带上 MessageType,少了它 usbmuxd 只回一个错误码。
//
// 这条守的是一次真实的失败:Connect 的结构体里嵌了个未导出的类型,
// plist 的编码器按反射走,把它整个当成未导出字段跳过了 ——
// MessageType / ProgName 三个字段凭空消失,而且不报错。
// 表现是每个端口都连不上,连每台设备都在跑的 lockdownd 都连不上,
// 从错误码完全看不出来是自己发的东西不对。
func TestRequestsCarryMessageType(t *testing.T) {
	cases := map[string]any{
		"ListDevices": newRequest("ListDevices"),
		"Connect":     connectRequest{request: newRequest("Connect"), DeviceID: 7, Port: swap16(22)}.flat(),
	}
	for want, payload := range cases {
		var buf bytes.Buffer
		if err := plist.NewEncoder(&buf).Encode(payload); err != nil {
			t.Fatalf("%s 编不出来: %v", want, err)
		}
		got := buf.String()
		for _, key := range []string{"MessageType", "ClientVersionString", "ProgName"} {
			if !strings.Contains(got, "<key>"+key+"</key>") {
				t.Errorf("%s 的负载里没有 %s:\n%s", want, key, got)
			}
		}
		if !strings.Contains(got, "<string>"+want+"</string>") {
			t.Errorf("%s 的 MessageType 值不对:\n%s", want, got)
		}
	}
}

// Connect 还得带上设备和端口,而且端口是大端的
func TestConnectPayload(t *testing.T) {
	var buf bytes.Buffer
	req := connectRequest{request: newRequest("Connect"), DeviceID: 7, Port: swap16(22)}
	if err := plist.NewEncoder(&buf).Encode(req.flat()); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "<key>DeviceID</key><integer>7</integer>") {
		t.Errorf("设备号不对:\n%s", got)
	}
	// 22 换成大端就是 5632。不换的话设备那头收到的是别的端口
	if !strings.Contains(got, "<key>PortNumber</key><integer>5632</integer>") {
		t.Errorf("端口没转成大端:\n%s", got)
	}
}

func TestSwap16(t *testing.T) {
	cases := map[int]int{22: 5632, 62078: 32498, 2222: 44552, 0: 0}
	for in, want := range cases {
		if got := swap16(in); got != want {
			t.Errorf("swap16(%d) = %d,想要 %d", in, got, want)
		}
	}
	// 转两次要转回来 —— 不成立的话说明高低位有一边被丢了
	for _, p := range []int{22, 80, 443, 2222, 62078} {
		if got := swap16(swap16(p)); got != p {
			t.Errorf("swap16 来回一趟没回到原值: %d → %d", p, got)
		}
	}
}

// 响应头里的长度是不可信输入,不能拿它直接去分配内存
func TestRecvRejectsAbsurdLength(t *testing.T) {
	// 长度字段说有 4GB
	head := []byte{0xff, 0xff, 0xff, 0xff, 1, 0, 0, 0, 8, 0, 0, 0, 1, 0, 0, 0}
	if _, err := recv(fakeConn{bytes.NewReader(head)}); err == nil {
		t.Error("离谱的长度应该被拒绝,而不是照着去分配")
	}
	// 比头还短
	short := []byte{4, 0, 0, 0, 1, 0, 0, 0, 8, 0, 0, 0, 1, 0, 0, 0}
	if _, err := recv(fakeConn{bytes.NewReader(short)}); err == nil {
		t.Error("小于头长度的长度字段应该被拒绝")
	}
}
