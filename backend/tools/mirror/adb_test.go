package mirror

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

// adb 拒绝时要把它给的原因带出来:「设备没找到」和「连不上 adb」是两种处理办法
func TestOpenServiceReportsFail(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	old := adbAddr
	adbAddr = ln.Addr().String()
	defer func() { adbAddr = old }()

	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		req, _ := readRequest(c)
		got <- req
		msg := "device 'nope' not found"
		fmt.Fprintf(c, "FAIL%04x%s", len(msg), msg)
	}()

	_, err = openService("nope", "shell:id")
	var ae *adbError
	if !errors.As(err, &ae) || !strings.Contains(ae.msg, "not found") {
		t.Fatalf("应该带上 adb 的原因,得到 %v", err)
	}
	if req := <-got; req != "host:transport:nope" {
		t.Fatalf("请求不对: %q", req)
	}
}
