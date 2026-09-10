package devicefs

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

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
// 只有递归查找例外 —— SFTP 协议里没有这回事,客户端自己递归的话每层一次往返,
// USB 上慢到没法用,所以那一个走 SSH 上的 find。

// iosStartPath 打开浏览器时落在哪里。
// mobile 用户的 Library 是取证上最有价值的那一支:账号、邮件、通讯录、
// 各家 App 的沙盒数据都挂在这下面
const iosStartPath = "/private/var/mobile/Library"

// iosPreviewLimit SFTP 能跑满 USB,预览可以给得宽一些
const iosPreviewLimit = 8 << 20

const (
	// sshTimeout 握手超时。设备没开 SSH 时会一直连不上,
	// 不设超时的话界面就是一直转圈,什么都不说
	sshTimeout = 20 * time.Second
	// proxyReadyTimeout 等转发进程说"我起来了"最多等多久
	proxyReadyTimeout = 15 * time.Second
	// iosPullTimeout 单个文件拉取的上限
	iosPullTimeout = 3 * time.Minute
	// iosSearchTimeout 全盘 find 在 USB 上是分钟级的,给足但不能无限等
	iosSearchTimeout = 90 * time.Second
)

type iosTransport struct {
	proxy *exec.Cmd
	ssh   *ssh.Client
	sftp  *sftp.Client
}

func connectIOS(opt ConnectOptions) (transport, *Session, error) {
	if opt.User == "" {
		opt.User = "root"
	}
	if opt.Password == "" {
		return nil, nil, errNoPassword
	}
	if opt.RemotePort == 0 {
		opt.RemotePort = 22
	}

	// 端口让系统分配,不写死 2222:导出功能默认也用 2222,
	// 两边同时开着的话后起的那个直接绑不上
	port, err := freePort()
	if err != nil {
		return nil, nil, err
	}
	proxy, err := startProxy(opt, port)
	if err != nil {
		return nil, nil, err
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	client, err := dialSSH(addr, opt.User, opt.Password)
	if err != nil {
		stopProxy(proxy)
		return nil, nil, err
	}
	sf, err := sftp.NewClient(client)
	if err != nil {
		_ = client.Close()
		stopProxy(proxy)
		return nil, nil, fmt.Errorf("SFTP 子系统起不来(设备上的 sshd 可能没开 sftp): %w", err)
	}

	t := &iosTransport{proxy: proxy, ssh: client, sftp: sf}
	return t, &Session{
		Platform: "ios",
		DeviceID: opt.DeviceID,
		Addr:     addr,
		// 越狱设备上 root 是前提,连得上就说明有
		Rooted: true,
	}, nil
}

func (t *iosTransport) startPath() string   { return iosStartPath }
func (t *iosTransport) previewLimit() int64 { return iosPreviewLimit }

func (t *iosTransport) close() error {
	if t.sftp != nil {
		_ = t.sftp.Close()
	}
	if t.ssh != nil {
		_ = t.ssh.Close()
	}
	stopProxy(t.proxy)
	return nil
}

// list 列一个目录。
//
// 单条读不出来(权限、断链)不算失败:把错误挂在那一条上,其余照常返回。
// 取证时最想看的目录往往恰好是有几条读不了的那个,整个报错等于什么都看不到。
func (t *iosTransport) list(dir string) (*Listing, error) {
	infos, err := t.sftp.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("列不了 %s:%w", dir, err)
	}
	out := newListing(dir, len(infos))
	// 目录在前、同类按名字排。SFTP 给的顺序是服务端的,不保证稳定,
	// 每次刷新顺序都变的话人根本没法在长列表里定位
	sort.Slice(infos, func(i, j int) bool {
		di, dj := infos[i].IsDir(), infos[j].IsDir()
		if di != dj {
			return di
		}
		return strings.ToLower(infos[i].Name()) < strings.ToLower(infos[j].Name())
	})

	for _, fi := range infos {
		if len(out.Entries) >= maxEntries {
			out.Truncated = true
			break
		}
		full := path.Join(dir, fi.Name())
		e := Entry{
			Name:    fi.Name(),
			Path:    full,
			IsDir:   fi.IsDir(),
			Size:    fi.Size(),
			ModTime: fi.ModTime().Unix(),
			Mode:    fi.Mode().String(),
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			if target, err := t.sftp.ReadLink(full); err == nil {
				e.Symlink = target
				// 软链指向目录时也要能点进去。ReadDir 给的是链本身的信息,
				// IsDir 永远是假 —— 不跟一步的话 iOS 上一堆入口都点不开
				if st, err := t.sftp.Stat(full); err == nil {
					e.IsDir = st.IsDir()
					e.Size = st.Size()
				}
			} else {
				e.Err = "断链或读不到目标"
			}
		}
		out.Entries = append(out.Entries, e)
	}
	return out, nil
}

func (t *iosTransport) stat(p string) (*Entry, error) {
	fi, err := t.sftp.Stat(p)
	if err != nil {
		return nil, err
	}
	return &Entry{
		Name:    path.Base(p),
		Path:    p,
		IsDir:   fi.IsDir(),
		Size:    fi.Size(),
		ModTime: fi.ModTime().Unix(),
		Mode:    fi.Mode().String(),
	}, nil
}

func (t *iosTransport) exists(p string) bool {
	_, err := t.sftp.Stat(p)
	return err == nil
}

func (t *iosTransport) pull(remote, local string, limit int64) (int64, bool, error) {
	src, err := t.sftp.Open(remote)
	if err != nil {
		return 0, false, fmt.Errorf("打不开 %s:%w", remote, err)
	}
	defer src.Close()

	dst, err := createLocal(local)
	if err != nil {
		return 0, false, err
	}
	defer dst.Close()

	done := make(chan struct{})
	var n int64
	var copyErr error
	go func() {
		defer close(done)
		if limit > 0 {
			n, copyErr = io.Copy(dst, io.LimitReader(src, limit))
			return
		}
		n, copyErr = io.Copy(dst, src)
	}()

	select {
	case <-done:
	case <-time.After(iosPullTimeout):
		// 超时就把连接关掉打断拷贝 —— 不然这个 goroutine 会一直挂着
		_ = src.Close()
		return 0, false, fmt.Errorf("拉取 %s 超过 %s 还没完成", remote, iosPullTimeout)
	}
	if copyErr != nil {
		return n, false, copyErr
	}
	return n, limit > 0 && n == limit, nil
}

// search 按文件名递归查找。
//
// 这条走 SSH exec 而不是 SFTP —— SFTP 协议里没有"递归查找"这回事,
// 自己在客户端递归的话每一层都是一次往返,USB 上慢到没法用。
func (t *iosTransport) search(root, pattern string, limit int) (*SearchResult, error) {
	// stat 的格式串里用 | 分隔:文件名可能带空格,放在最后一段才切得开
	cmd := fmt.Sprintf(
		"find %s -iname %s 2>/dev/null | head -n %d | xargs -d '\\n' -r stat -c '%%F|%%s|%%Y|%%n' 2>/dev/null",
		shellQuote(root), shellQuote(pattern), limit+1)

	out, stderr, err := t.run(cmd, iosSearchTimeout)
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("在设备上执行查找失败: %w", err)
	}
	res := parseStatLines(out, root, pattern, limit)
	res.Stderr = strings.TrimSpace(stderr)
	return res, nil
}

// run 在设备上跑一条命令,分开拿 stdout / stderr。
//
// 超时不是可选项:find 扫全盘时可能几分钟不返回,而 SSH 会话本身没有超时 ——
// 不设的话界面就永远转圈。超时后关掉会话把命令打断。
func (t *iosTransport) run(cmd string, timeout time.Duration) (stdout, stderr string, err error) {
	sess, err := t.ssh.NewSession()
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

// ---------- USB 端口转发 ----------

// startProxy 起 USB 端口转发。
//
// 这个进程要活到会话结束 —— 它就是设备 22 端口通到本机的那根管子。
// 起完不能马上去连:进程刚 fork 出来时端口还没绑上,立刻拨号会吃一个
// connection refused。所以这里读它的输出,等它自己说 "the proxy is running"。
func startProxy(opt ConnectOptions, localPort int) (*exec.Cmd, error) {
	bin := strings.TrimSpace(opt.BinaryPath)
	if bin == "" {
		bin = "go-forensic"
	}
	args := []string{"ios", "proxy",
		"-l", strconv.Itoa(localPort),
		"-r", strconv.Itoa(opt.RemotePort),
		"-p", "tcp",
	}
	if opt.DeviceID != "" {
		args = append(args, "-d", opt.DeviceID)
	}
	cmd := exec.Command(bin, args...)
	hideWindow(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("起不来 %s:%w —— 在设置里确认 go-forensic 的路径", bin, err)
	}

	ready := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		var first string
		for sc.Scan() {
			line := sc.Text()
			if first == "" {
				first = line
			}
			if strings.Contains(line, "proxy is running") {
				select {
				case ready <- "":
				default:
				}
			}
		}
		// 进程自己结束了(多半是报错退出),把第一行原话带出去 ——
		// 只说"连接失败"的话,人不知道是没插线还是路径不对
		select {
		case ready <- first:
		default:
		}
		_, _ = io.Copy(io.Discard, stdout)
	}()

	select {
	case msg := <-ready:
		if msg != "" {
			stopProxy(cmd)
			return nil, fmt.Errorf("USB 转发没起来:%s", msg)
		}
		return cmd, nil
	case <-time.After(proxyReadyTimeout):
		stopProxy(cmd)
		return nil, fmt.Errorf("等 USB 转发就绪超过 %s —— 设备可能没插好,或者没信任这台电脑", proxyReadyTimeout)
	}
}

func stopProxy(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	// Wait 是必须的:不回收的话进程留在僵尸状态,USB 通道也跟着不放
	go func() { _ = cmd.Wait() }()
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
		return nil, fmt.Errorf("连不上转发端口 %s —— 设备可能没插好,或者转发没起来: %w", addr, err)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("SSH 认证失败(用户 %s):%w —— 检查密码,以及设备上装没装 OpenSSH", user, err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}
