// Package devicefs 直接浏览连着的手机上的文件系统。
//
// 为什么不是"浏览导出结果":先导出再看,意味着你得先猜对要导哪个目录。
// 而取证现场恰恰是反过来的 —— 先翻,翻到有价值的再取。
// go-forensic 只有 export,没有列目录的命令,所以这一层是自己实现的。
//
// iOS 这条路的形状:
//
//	go-forensic ios proxy   把设备的 22 端口经 USB 转到本机一个端口
//	        ↓
//	SSH(root/密码)         越狱设备上的 OpenSSH
//	        ↓
//	SFTP 子系统             列目录、读文件
//
// 用 SFTP 而不是 exec "ls":文件名里出现空格、换行、引号在取证场景里是常态,
// 靠解析 ls 的输出迟早会读错;SFTP 给的是带类型的结构,没有 shell 引用这回事。
package devicefs

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Session 一个连上的设备。
//
// proxy 是 go-forensic 起的转发进程,它必须活到会话结束 —— 它一死,
// SSH 连接下面的管子就断了
type Session struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	// DeviceID 设备 UDID;空表示用检测到的第一台
	DeviceID string `json:"deviceId"`
	// Addr 实际连上的本机地址,排查问题时有用
	Addr string `json:"addr"`
	// StartPath 建议的起始目录
	StartPath string `json:"startPath"`

	proxy *exec.Cmd
	ssh   *ssh.Client
	sftp  *sftp.Client
}

// Manager 管着所有活着的会话。
//
// 前端拿到的是 session id,后续每次调用都带着它回来 —— 连接是有状态的
// (一个转发进程 + 一条 SSH),不可能每次调用重建一遍
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	seq      int
}

func NewManager() *Manager {
	return &Manager{sessions: map[string]*Session{}}
}

// ConnectOptions 连接参数
type ConnectOptions struct {
	// Platform 目前只支持 ios
	Platform string `json:"platform"`
	// DeviceID 设备 UDID;空 = 第一台
	DeviceID string `json:"deviceId"`
	// User SSH 用户名,越狱设备一般是 root
	User string `json:"user"`
	// Password SSH 密码
	Password string `json:"password"`
	// BinaryPath go-forensic 路径;空 = 走 PATH
	BinaryPath string `json:"binaryPath"`
	// RemotePort 设备上的 SSH 端口,默认 22
	RemotePort int `json:"remotePort"`
}

// sshTimeout 握手超时。设备没开 SSH 时会一直连不上,
// 不设超时的话界面就是一直转圈,什么都不说
const sshTimeout = 20 * time.Second

// Connect 连上一台设备
func (m *Manager) Connect(opt ConnectOptions) (*Session, error) {
	if opt.Platform != "" && opt.Platform != "ios" {
		return nil, fmt.Errorf("暂时只支持 ios,不支持 %q", opt.Platform)
	}
	if opt.User == "" {
		opt.User = "root"
	}
	if opt.Password == "" {
		return nil, errors.New("需要 SSH 密码 —— 越狱设备默认是 alpine,改过就填改后的")
	}
	if opt.RemotePort == 0 {
		opt.RemotePort = 22
	}

	// 端口让系统分配,不写死 2222:导出功能默认也用 2222,
	// 两边同时开着的话后起的那个直接绑不上
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	proxy, err := startProxy(opt, port)
	if err != nil {
		return nil, err
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	client, err := dialSSH(addr, opt.User, opt.Password)
	if err != nil {
		stopProxy(proxy)
		return nil, err
	}
	sf, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		stopProxy(proxy)
		return nil, fmt.Errorf("SFTP 子系统起不来(设备上的 sshd 可能没开 sftp): %w", err)
	}

	m.mu.Lock()
	m.seq++
	s := &Session{
		ID:        fmt.Sprintf("dev-%d", m.seq),
		Platform:  "ios",
		DeviceID:  opt.DeviceID,
		Addr:      addr,
		StartPath: iosStartPath,
		proxy:     proxy,
		ssh:       client,
		sftp:      sf,
	}
	m.sessions[s.ID] = s
	m.mu.Unlock()
	return s, nil
}

// get 取一个会话;找不到就明说,别让调用方对着 nil 猜
func (m *Manager) get(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("会话 %q 已经不在了(可能已断开),重新连接一次", id)
	}
	return s, nil
}

// Disconnect 断开并清理。SFTP、SSH、转发进程一个都不能漏 ——
// 转发进程漏了会一直占着 USB 通道,下次连接直接失败
func (m *Manager) Disconnect(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if !ok {
		return nil // already gone
	}
	return s.close()
}

// CloseAll 应用退出时调,把所有转发进程收干净
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.sessions = map[string]*Session{}
	m.mu.Unlock()
	for _, s := range all {
		_ = s.close()
	}
}

func (s *Session) close() error {
	if s.sftp != nil {
		_ = s.sftp.Close()
	}
	if s.ssh != nil {
		_ = s.ssh.Close()
	}
	stopProxy(s.proxy)
	return nil
}

// freePort 让系统分配一个空闲端口。
//
// 拿到之后立刻关掉再交给子进程去绑,中间有一段谁都能抢走的窗口。
// 这是端口分配的老问题,没有干净解法;好在这里的竞争者只有本机的其他程序,
// 撞上了表现是"连接失败"而不是连到别的东西上
func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("找不到空闲端口: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}

func dialSSH(addr, user, password string) (*ssh.Client, error) {
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
		// 设备的主机密钥不校验:连的是自己刚用 USB 转发起来的本机端口,
		// 中间人要能插进来的话它早就在这台机器上了。校验反而会因为
		// 每次换设备主机密钥都变而一直报错
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         sshTimeout,
	}
	conn, err := net.DialTimeout("tcp", addr, sshTimeout)
	if err != nil {
		return nil, fmt.Errorf("连不上转发端口 %s —— 设备可能没插好,或者 go-forensic 的代理没起来: %w", addr, err)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("SSH 认证失败(用户 %s):%w —— 检查密码,以及设备上装没装 OpenSSH", user, err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// run 在设备上跑一条命令,分开拿 stdout / stderr。
//
// 超时不是可选项:find 扫全盘时可能几分钟不返回,而 SSH 会话本身没有超时 ——
// 不设的话界面就永远转圈。超时后关掉会话把命令打断。
func (s *Session) run(cmd string, timeout time.Duration) (stdout, stderr string, err error) {
	sess, err := s.ssh.NewSession()
	if err != nil {
		return "", "", err
	}
	var outBuf, errBuf bytes.Buffer
	sess.Stdout = &outBuf
	sess.Stderr = &errBuf

	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()

	select {
	case err = <-done:
	case <-time.After(timeout):
		_ = sess.Close()
		return outBuf.String(), errBuf.String(), fmt.Errorf("命令超过 %s 还没结束", timeout)
	}
	_ = sess.Close()
	return outBuf.String(), errBuf.String(), err
}
