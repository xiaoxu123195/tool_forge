package devicefs

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"tool_forge/backend/tools/iosmux"
)

// iOS 这条路的形状:
//
//	usbmuxd                 直接把一条连接接到设备的 22 端口
//	        ↓
//	SSH(root/密码)         越狱设备上的 OpenSSH
//	        ↓
//	SFTP 子系统             列目录、读文件
//
// 第一层以前是 fork 一个转发进程、让它在本机开个端口、再连过去。
// 现在直接跟 usbmuxd 说话,拿到的就是通往设备端口的 net.Conn ——
// 少了一个进程、一个本机端口,也少了"转发起没起来"这一整类看不懂的失败。
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
	// iosPullTimeout 单个文件拉取的上限
	iosPullTimeout = 3 * time.Minute
	// iosSearchTimeout 全盘 find 在 USB 上是分钟级的,给足但不能无限等
	iosSearchTimeout = 90 * time.Second
)

type iosTransport struct {
	ssh  *ssh.Client
	sftp *sftp.Client
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

	dev, err := iosmux.PickDevice(opt.DeviceID)
	if err != nil {
		return nil, nil, err
	}
	client, _, err := iosmux.DialSSH(dev, opt.RemotePort, opt.User, opt.Password)
	if err != nil {
		return nil, nil, err
	}
	// addr 只是显示用的,连接本身已经建好了
	addr := fmt.Sprintf("usb:%s:%d", shortUDID(dev.UDID), opt.RemotePort)
	sf, err := sftp.NewClient(client)
	if err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("SFTP 子系统起不来(设备上的 sshd 可能没开 sftp): %w", err)
	}

	t := &iosTransport{ssh: client, sftp: sf}
	return t, &Session{
		Platform: "ios",
		DeviceID: dev.UDID,
		Addr:     addr,
		Model:    dev.Type,
		// 越狱设备上 root 是前提,连得上就说明有
		Rooted: true,
	}, nil
}

// shortUDID 界面上显示用。完整 UDID 四十位,一屏都放不下,
// 而认一台设备看前后各几位就够了
func shortUDID(udid string) string {
	if len(udid) <= 12 {
		return udid
	}
	return udid[:8] + "…" + udid[len(udid)-4:]
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
