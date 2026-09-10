package iosmux

import (
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// 越狱设备上的 SSH 走这一层。真机浏览和移动取证都要用,所以放在这儿,
// 不各写一份 —— 下面那两处 deadline 的处理是有讲究的,复制出去迟早写错一边。

// SSHTimeout 握手上限。
// 设备上的 sshd 半死不活时,不设这个就是一个永远不动的"连接中"
const SSHTimeout = 20 * time.Second

// DialSSH 经 USB 连上设备的 SSH。
//
// user 空时用 root(越狱设备上的默认账号)
func DialSSH(dev Device, port int, user, password string) (*ssh.Client, net.Conn, error) {
	if user == "" {
		user = "root"
	}
	if port == 0 {
		port = 22
	}
	conn, err := Dial(dev.ID, port)
	if err != nil {
		return nil, nil, err
	}
	client, err := sshOver(conn, dev.UDID, user, password)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return client, conn, nil
}

// sshOver 在一条已经通到设备的连接上做 SSH 握手。
//
// 收 net.Conn 而不是地址:连接是 usbmuxd 给的,本机根本没有对应的端口可拨
func sshOver(conn net.Conn, label, user, password string) (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
			// 有些 sshd 只开 keyboard-interactive,不开 password。
			// 两个都挂上,省得因为服务端配置差异连不上
			ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
				answers := make([]string, len(qs))
				for i := range answers {
					answers[i] = password
				}
				return answers, nil
			}),
		},
		// 设备的主机密钥不校验:这条连接是 usbmuxd 给的,物理上就是那根 USB 线,
		// 中间没有网络可插。校验反而会因为每换一台设备主机密钥就变而一直报错
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         SSHTimeout,
	}

	// 握手要有超时,握完必须摘掉 —— 留着的话,这条准备长期用的连接
	// 会在二十秒后毫无征兆地开始报超时
	if err := conn.SetDeadline(time.Now().Add(SSHTimeout)); err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, label, cfg)
	if err != nil {
		return nil, fmt.Errorf("SSH 认证失败(用户 %s):%w —— 检查密码,以及设备上装没装 OpenSSH", user, err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}
