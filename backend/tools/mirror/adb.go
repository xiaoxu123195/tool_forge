package mirror

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// adbAddr adb 服务端的地址。gadb 连的也是这里(它不认 ANDROID_ADB_SERVER_PORT),两边保持一致
var adbAddr = "127.0.0.1:5037"

// openService 请 adb 服务端把这条连接接到某台设备的某个服务上,之后它就是一根直通的管子。
//
// gadb 的端口转发只认 tcp:端口,而手机端程序监听的是本地抽象套接字,
// 所以这里直接说 adb 的协议:先 host:transport:序列号 选中设备,再报要连的服务。
// 不用在电脑上占端口,也不用另起 adb 进程
func openService(serial, service string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", adbAddr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("连不上 adb 服务端: %w", err)
	}
	if err := adbRequest(conn, "host:transport:"+serial); err != nil {
		conn.Close()
		return nil, err
	}
	if err := adbRequest(conn, service); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// adbRequest 发一条请求:4 位十六进制长度加内容,再读 OKAY 或 FAIL
func adbRequest(conn net.Conn, req string) error {
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetDeadline(time.Time{})
	if _, err := fmt.Fprintf(conn, "%04x%s", len(req), req); err != nil {
		return fmt.Errorf("给 adb 发请求失败: %w", err)
	}
	status := make([]byte, 4)
	if _, err := io.ReadFull(conn, status); err != nil {
		return fmt.Errorf("adb 没有回应: %w", err)
	}
	switch string(status) {
	case "OKAY":
		return nil
	case "FAIL":
		return &adbError{msg: readADBString(conn)}
	default:
		return fmt.Errorf("adb 回了看不懂的东西: %q", status)
	}
}

// readADBString FAIL 后面跟着的原因,同样是长度前缀加内容
func readADBString(conn net.Conn) string {
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return ""
	}
	n, err := strconv.ParseUint(string(head), 16, 16)
	if err != nil {
		return ""
	}
	msg := make([]byte, n)
	if _, err := io.ReadFull(conn, msg); err != nil {
		return ""
	}
	return string(msg)
}

type adbError struct{ msg string }

func (e *adbError) Error() string { return "adb 拒绝了: " + e.msg }
