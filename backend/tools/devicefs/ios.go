package devicefs

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// iosStartPath 打开浏览器时落在哪里。
// mobile 用户的 Library 是取证上最有价值的那一支:账号、邮件、通讯录、
// 各家 App 的沙盒数据都挂在这下面
const iosStartPath = "/private/var/mobile/Library"

// proxyReadyTimeout 等转发进程说"我起来了"最多等多久
const proxyReadyTimeout = 15 * time.Second

// startProxy 起 go-forensic 的 USB 端口转发。
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
		"-l", fmt.Sprint(localPort),
		"-r", fmt.Sprint(opt.RemotePort),
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
	// Wait 是必须的:不回收的话进程留在僵尸状态,
	// USB 通道也跟着不放
	go func() { _ = cmd.Wait() }()
}
