// Package adbx 是连安卓设备的公共底座:连上 adb 服务端、挑一台设备、
// 在设备上跑命令。
//
// 单独成包是因为有两个地方要用 —— 真机浏览要翻文件系统,移动取证要批量导出。
// 而这一层踩过的坑不轻,任何一处复制粘贴出去都会慢慢跑偏:
//
//   - adb 可执行文件在 Windows 上经常被手机助手换成十几年前的版本,
//     那种版本认不出现代设备,而且**不报错**,只给一个空的设备列表
//   - 拉起 adb 服务端时它会 fork 一个常驻进程并继承 stdout 管道,
//     不设 WaitDelay 的话 Wait 永远回不来
//   - su 会分配 pty,pty 把 LF 换成 CRLF,读二进制必须绕开
package adbx

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/electricbubble/gadb"
)

// MinUsableVersion adb 能认出现代设备的最低末位版本号。
//
// 40 之前的版本没有 RSA 授权握手(那是 Android 4.2.2 引入的),对着一台现代手机
// 的表现是"设备列表为空",不会有任何报错 —— 这正是最难查的那种失败。
const MinUsableVersion = 40

// CmdWaitDelay 等本地进程的 io 拷贝最多再拖多久。
//
// 这一行是必须的,不是保险:adb 拉起来的服务端是常驻进程,而且继承了我们这条
// stdout 管道的写端。Go 的 Wait 要等 io 拷贝 goroutine 结束,管道又要等所有
// 持有写端的进程退出才关闭 —— 服务端永远不退,于是 context 到期把 adb 杀了
// 也没用,Wait 照样卡死,界面上就是一直转圈。
const CmdWaitDelay = 3 * time.Second

// Dial 连上 adb 服务端;没起来就先把它拉起来。
//
// adb 协议里没有"启动服务端"这回事,只能靠 adb 可执行文件自己 fork 一个 ——
// 这是整条路上唯一还需要外部可执行文件的地方,而且只在服务端没跑时用一次。
func Dial(adbPath string) (gadb.Client, error) {
	client, err := gadb.NewClient()
	if err == nil {
		if v, verr := client.ServerVersion(); verr == nil && v > 0 && v < MinUsableVersion {
			return gadb.Client{}, fmt.Errorf(
				"正在跑的 adb 服务端版本过老(协议版本 %d)—— 它没有 RSA 授权握手,"+
					"认不出 2013 年以后的设备,表现就是设备列表一直是空的。"+
					"多半是别的软件把老版本的 adb 抢先起在了 5037 端口上;"+
					"用新版 platform-tools 执行一次 adb kill-server 再重试", v)
		}
		return client, nil
	}

	if serr := startServer(adbPath); serr != nil {
		return gadb.Client{}, serr
	}
	client, err = gadb.NewClient()
	if err != nil {
		return gadb.Client{}, fmt.Errorf("adb 服务端起来了但连不上: %w", err)
	}
	return client, nil
}

func startServer(adbPath string) error {
	adb, err := ResolveBinary(adbPath)
	if err != nil {
		return err
	}
	if _, stderr, err := RunLocal(context.Background(), 30*time.Second, adb, "start-server"); err != nil {
		if s := strings.TrimSpace(stderr); s != "" {
			return fmt.Errorf("启动 adb 服务端失败:%s", s)
		}
		return fmt.Errorf("启动 adb 服务端失败: %w", err)
	}
	return nil
}

// BundledPath 应用自己那份 adb 放在哪。
//
// 没显式指定路径时优先用它,而不是直接奔 PATH:Windows 上一堆手机助手
// 会把自带的 adb 塞进系统目录,那些版本往往老到认不出现代设备,
// 而它们又排在 PATH 前面 —— 用户根本不会想到问题出在这里。
func BundledPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	name := "adb"
	if runtime.GOOS == "windows" {
		name = "adb.exe"
	}
	p := filepath.Join(home, ".toolforge", "platform-tools", name)
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return ""
}

// ResolveBinary 决定用哪个 adb 可执行文件,并在版本明显过老时直接拦下来。
//
// 拦下来比让它继续跑好:老版本不会报错,只会给一个空的设备列表,
// 而空列表看起来就是"线没插好",人会去查线、换口、重启手机 —— 全是白费功夫。
func ResolveBinary(explicit string) (string, error) {
	adb := strings.TrimSpace(explicit)
	if adb == "" {
		if b := BundledPath(); b != "" {
			adb = b
		} else {
			adb = "adb"
		}
	}
	out, _, err := RunLocal(context.Background(), 15*time.Second, adb, "version")
	if err != nil {
		return "", fmt.Errorf("跑不起来 %s:%w —— 确认 adb 装了、路径对", adb, err)
	}
	full, minor := ParseVersion(out)
	if minor > 0 && minor < MinUsableVersion {
		where := adb
		if resolved, err := exec.LookPath(adb); err == nil {
			where = resolved
		}
		return "", fmt.Errorf(
			"adb 版本过老(%s,在 %s)—— 这个版本没有 RSA 授权握手,认不出 2013 年以后的设备,"+
				"表现就是设备列表一直是空的。装一份新的 platform-tools,在「adb 路径」里指过去",
			full, where)
	}
	return adb, nil
}

// ParseVersion 从 "Android Debug Bridge version 1.0.41" 里取出版本。
// 第二个返回值是末位版本号(41 / 26),取不到时为 0
func ParseVersion(out string) (full string, minor int) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		const marker = "version "
		i := strings.Index(line, marker)
		if !strings.Contains(line, "Android Debug Bridge") || i < 0 {
			continue
		}
		full = strings.TrimSpace(line[i+len(marker):])
		parts := strings.Split(full, ".")
		if len(parts) >= 3 {
			minor, _ = strconv.Atoi(parts[2])
		}
		return full, minor
	}
	return "", 0
}

// PickDevice 选一台设备。
//
// 状态必须是 online。gadb 把 adb 报的 unauthorized 归进 StateUnknown ——
// 而那恰好是最常见的一种,解法就在手机屏幕上,必须说出来,
// 不然人只会看到一句"没找到设备"然后去查线。
func PickDevice(client gadb.Client, want string) (dev gadb.Device, model string, err error) {
	devices, err := client.DeviceList()
	if err != nil {
		return gadb.Device{}, "", fmt.Errorf("取设备列表失败: %w", err)
	}
	if len(devices) == 0 {
		return gadb.Device{}, "", errors.New("adb 没看到任何设备 —— 确认线插好、" +
			"手机上开了 USB 调试并允许了这台电脑")
	}
	for _, d := range devices {
		if want != "" && d.Serial() != want {
			continue
		}
		state, err := d.State()
		if err != nil {
			return gadb.Device{}, "", fmt.Errorf("读设备 %s 的状态失败: %w", d.Serial(), err)
		}
		switch state {
		case gadb.StateOnline:
			m, _ := d.Model()
			return d, m, nil
		case gadb.StateUnknown:
			return gadb.Device{}, "", fmt.Errorf(
				"设备 %s 的状态不是「已连接」,最常见的原因是还没授权 —— "+
					"手机屏幕上会弹「允许 USB 调试」,点一下允许", d.Serial())
		default:
			return gadb.Device{}, "", fmt.Errorf("设备 %s 当前状态是 %s,连不上", d.Serial(), state)
		}
	}
	return gadb.Device{}, "", fmt.Errorf("没找到序列号为 %s 的设备", want)
}

// HasRoot 试一下 su 能不能用
func HasRoot(dev gadb.Device, timeout time.Duration) bool {
	out, err := Shell(dev, "id", true, timeout)
	return err == nil && strings.Contains(string(out), "uid=0")
}

// Shell 在设备上跑一段脚本,拿原始字节。
//
// asRoot 时用 base64 把脚本包起来再交给 su:su -c 自己还要再起一个 sh,
// 命令替换让解出来的整段成为它的一个参数,不会被二次切分 ——
// 文件名里有空格或引号时这是唯一稳的写法。
func Shell(dev gadb.Device, script string, asRoot bool, timeout time.Duration) ([]byte, error) {
	cmd := script
	if asRoot {
		payload := base64.StdEncoding.EncodeToString([]byte(script))
		cmd = fmt.Sprintf(`su -c "$(echo %s | base64 -d)"`, payload)
	}

	type result struct {
		out []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := dev.RunShellCommandWithBytes(cmd)
		ch <- result{out, err}
	}()
	select {
	case r := <-ch:
		return r.out, r.err
	case <-time.After(timeout):
		// adb 协议这条路没有"杀掉远端命令"的手段,只能不再等它;
		// 那个 goroutine 会随着连接自己结束
		return nil, fmt.Errorf("设备上的命令超过 %s 还没结束", timeout)
	}
}

// Text 跑一段脚本并拿到文本输出。
// 走 su 时设备那头的 pty 会把 LF 变成 CRLF,统一去掉 \r
func Text(dev gadb.Device, script string, asRoot bool, timeout time.Duration) (string, error) {
	out, err := Shell(dev, script, asRoot, timeout)
	s := strings.ReplaceAll(string(out), "\r\n", "\n")
	if err != nil && strings.TrimSpace(s) == "" {
		return "", err
	}
	return s, nil
}

// Quote 把一个值包成单引号字符串。
//
// 路径和关键词都是用户输入的,直接拼进命令行等于把设备的 shell 交给对方 ——
// 一个 `; rm -rf /` 就能在别人的手机上执行。单引号里除了单引号本身
// 什么都不特殊,所以只需要处理单引号:闭合、转义、再开。
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// RunLocal 跑一个本地进程,带超时。见 CmdWaitDelay 上面那段说明
func RunLocal(ctx context.Context, timeout time.Duration, name string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = CmdWaitDelay
	hideWindow(cmd)
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return out.String(), errBuf.String(),
			fmt.Errorf("命令超过 %s 还没结束(%s)", timeout, name)
	}
	return out.String(), errBuf.String(), err
}
