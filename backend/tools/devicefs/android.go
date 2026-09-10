package devicefs

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/electricbubble/gadb"
)

// Android 这条路的形状:
//
//	adb server(:5037)→ adb 协议 → 设备上的 shell → su(Magisk)→ toybox
//
// 走的是 adb 协议本身,不再 fork adb.exe 去跑命令。换掉的不只是"快一点":
//
//   - 命令行那三层引用没有了(Go 的 exec、Windows 命令行、adb 把参数拼回字符串),
//     现在只剩设备上 sh 这一层
//   - adb 第一次被调用会 fork 一个常驻 server 并继承 stdout 管道,导致 Wait
//     永远回不来、超时形同虚设 —— 这条路整个不存在了
//   - 非 root 的读现在是干净的二进制,不用再绕 base64
//
// 唯一还需要 adb 可执行文件的地方是"server 没起来时把它拉起来" ——
// adb 协议本身没有"启动服务端"这回事,只能由 adb.exe 自己 fork。
//
// 有两件事换了传输方式也躲不掉,因为根源在设备上:
//
//  1. su 会分配一个 pty,pty 把 LF 换成 CRLF。502056 字节的文件经 su 读出来
//     变成 506112 字节,而且不报任何错。所以走 root 的读仍然要过 base64。
//  2. adb 的 sync 协议(Pull / List)以 shell 用户身份跑,进不去 /data。
//     List 在那儿返回的是"零条且无错误",比报错更坑 —— 会被显示成"空目录"。
//     所以列目录统一走 shell + stat,不用 sync 那条。

const (
	// androidPreviewLimit 预览最多拉多少字节。
	// 比 iOS 小一个数量级,因为走 root 时这边是 base64 文本通道(约 1.4 MB/s),
	// 给 8MB 的话点一下要等六秒
	androidPreviewLimit = 2 << 20
	// androidCmdTimeout 普通命令(列目录、stat)的上限
	androidCmdTimeout = 30 * time.Second
	// androidPullTimeout 拉文件的上限
	androidPullTimeout = 5 * time.Minute
	// androidSearchTimeout 全盘 find 是分钟级的
	androidSearchTimeout = 120 * time.Second
	// androidStageDir 导出时在设备上暂存的位置。
	// /sdcard 是 FUSE 挂载,不认 Unix 属主,shell 用户读得到 root 写的文件 ——
	// 换成 /data/local/tmp 的话 root 写出来的文件 shell 反而读不了
	androidStageDir = "/sdcard"
)

type androidTransport struct {
	dev    gadb.Device
	serial string
	// root su 能不能用。用不了的话只看得到 /sdcard,
	// 而有价值的数据全在 /data/data 下面
	root bool
}

func connectAndroid(opt ConnectOptions) (transport, *Session, error) {
	client, err := dialAdb(opt.AdbPath)
	if err != nil {
		return nil, nil, err
	}
	dev, model, err := pickAndroidDevice(client, opt.DeviceID)
	if err != nil {
		return nil, nil, err
	}
	t := &androidTransport{dev: dev, serial: dev.Serial()}
	t.root = t.probeRoot()

	return t, &Session{
		Platform: "android",
		DeviceID: t.serial,
		Addr:     t.serial,
		Rooted:   t.root,
		Model:    model,
	}, nil
}

// minUsableAdb adb 能认出现代设备的最低末位版本号。
//
// 40 之前的版本没有 RSA 授权握手(那是 Android 4.2.2 引入的),对着一台现代手机
// 的表现是"设备列表为空",不会有任何报错 —— 这正是最难查的那种失败。
// Windows 上一堆手机助手会把老 adb 塞进系统目录并抢占 5037,撞上的概率不低。
const minUsableAdb = 40

// dialAdb 连上 adb 服务端;没起来就先把它拉起来。
//
// adb 协议里没有"启动服务端"这回事,只能靠 adb 可执行文件自己 fork 一个。
// 这是整条路上唯一还需要外部可执行文件的地方,而且只在服务端没跑时用一次。
func dialAdb(adbPath string) (gadb.Client, error) {
	client, err := gadb.NewClient()
	if err == nil {
		if v, verr := client.ServerVersion(); verr == nil && v > 0 && v < minUsableAdb {
			return gadb.Client{}, fmt.Errorf(
				"正在跑的 adb 服务端版本过老(协议版本 %d)—— 它没有 RSA 授权握手,"+
					"认不出 2013 年以后的设备,表现就是设备列表一直是空的。"+
					"多半是别的软件把老版本的 adb 抢先起在了 5037 端口上;"+
					"用新版 platform-tools 执行一次 adb kill-server 再重试", v)
		}
		return client, nil
	}

	// 服务端没跑,拉起来再连一次
	if serr := startAdbServer(adbPath); serr != nil {
		return gadb.Client{}, serr
	}
	client, err = gadb.NewClient()
	if err != nil {
		return gadb.Client{}, fmt.Errorf("adb 服务端起来了但连不上: %w", err)
	}
	return client, nil
}

func startAdbServer(adbPath string) error {
	adb, err := resolveAdb(adbPath)
	if err != nil {
		return err
	}
	if _, stderr, err := runCmd(context.Background(), 30*time.Second, adb, "start-server"); err != nil {
		if s := strings.TrimSpace(stderr); s != "" {
			return fmt.Errorf("启动 adb 服务端失败:%s", s)
		}
		return fmt.Errorf("启动 adb 服务端失败: %w", err)
	}
	return nil
}

// bundledAdbPath 应用自己那份 adb 放在哪。
//
// 没显式指定路径时优先用它,而不是直接奔 PATH:Windows 上一堆手机助手
// 会把自带的 adb 塞进系统目录,那些版本往往老到认不出现代设备,
// 而它们又排在 PATH 前面 —— 用户根本不会想到问题出在这里。
func bundledAdbPath() string {
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

// resolveAdb 决定用哪个 adb 可执行文件,并在版本明显过老时直接拦下来
func resolveAdb(explicit string) (string, error) {
	adb := strings.TrimSpace(explicit)
	if adb == "" {
		if b := bundledAdbPath(); b != "" {
			adb = b
		} else {
			adb = "adb"
		}
	}
	out, _, err := runCmd(context.Background(), 15*time.Second, adb, "version")
	if err != nil {
		return "", fmt.Errorf("跑不起来 %s:%w —— 确认 adb 装了、路径对", adb, err)
	}
	full, minor := parseAdbVersion(out)
	if minor > 0 && minor < minUsableAdb {
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

// parseAdbVersion 从 "Android Debug Bridge version 1.0.41" 里取出版本。
// 第二个返回值是末位版本号(41 / 26),取不到时为 0
func parseAdbVersion(out string) (full string, minor int) {
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

// pickAndroidDevice 选一台设备。
//
// 状态必须是 online:unauthorized 说明手机上那个"允许 USB 调试"的框还没点,
// offline 说明连接是坏的 —— 这两种都得说清楚,不然人只会看到一句"没找到设备"
func pickAndroidDevice(client gadb.Client, want string) (gadb.Device, string, error) {
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
			model, _ := d.Model()
			return d, model, nil
		case gadb.StateUnknown:
			// adb 报的原始状态里 unauthorized 也落在这里 —— 这是最常见的一种,
			// 而它的解法就在手机屏幕上,必须说出来
			return gadb.Device{}, "", fmt.Errorf(
				"设备 %s 的状态不是"+"「已连接」,最常见的原因是还没授权 —— "+
					"手机屏幕上会弹「允许 USB 调试」,点一下允许", d.Serial())
		default:
			return gadb.Device{}, "", fmt.Errorf("设备 %s 当前状态是 %s,连不上", d.Serial(), state)
		}
	}
	return gadb.Device{}, "", fmt.Errorf("没找到序列号为 %s 的设备", want)
}

// probeRoot 试一下 su 能不能用
func (t *androidTransport) probeRoot() bool {
	out, err := t.raw("id", true, androidCmdTimeout)
	return err == nil && strings.Contains(string(out), "uid=0")
}

func (t *androidTransport) startPath() string {
	// 有 root 就直接落在 /data/data —— 各家 App 的数据全在那儿,这才是要看的东西。
	// 没 root 只能看 /sdcard,落在 /data/data 上只会得到一个"权限不足"
	if t.root {
		return "/data/data"
	}
	return "/sdcard"
}

func (t *androidTransport) previewLimit() int64 { return androidPreviewLimit }

// close adb 服务端是系统级的,别的工具可能也在用,不该由我们关掉
func (t *androidTransport) close() error { return nil }

// raw 在设备上跑一段脚本,拿原始字节。
//
// asRoot 时用 base64 把脚本包起来交给 su。包这一层不是为了躲命令行引用
// (走 adb 协议之后只剩设备 sh 一层),而是因为 su -c 自己还要再起一个 sh:
// 命令替换让解出来的整段成为 su 的一个参数,不会被二次切分。
func (t *androidTransport) raw(script string, asRoot bool, timeout time.Duration) ([]byte, error) {
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
		out, err := t.dev.RunShellCommandWithBytes(cmd)
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

// text 跑一段脚本并拿到文本输出。
// 走 su 时设备那头的 pty 会把 LF 变成 CRLF,统一去掉 \r
func (t *androidTransport) text(script string, timeout time.Duration) (string, error) {
	out, err := t.raw(script, t.root, timeout)
	s := strings.ReplaceAll(string(out), "\r\n", "\n")
	if err != nil && strings.TrimSpace(s) == "" {
		return "", err
	}
	return s, nil
}

func (t *androidTransport) list(dir string) (*Listing, error) {
	// find 会把 dir 自己也列出来,后面按路径过滤掉
	script := fmt.Sprintf("find %s -maxdepth 1 -exec stat -c '%%F|%%s|%%Y|%%N' {} + 2>/dev/null",
		shellQuote(dir))
	out, err := t.text(script, androidCmdTimeout)
	if err != nil {
		return nil, fmt.Errorf("列不了 %s:%w", dir, err)
	}
	if strings.TrimSpace(out) == "" {
		// 什么都没回:目录不存在,或者没权限进去。
		// 后者在没 root 的机器上是常态,得说清楚而不是显示成"空目录"
		if !t.exists(dir) {
			return nil, fmt.Errorf("%s 不存在或读不了%s", dir, t.rootHint())
		}
		return newListing(dir, 0), nil
	}

	var entries []Entry
	var links []string
	for _, line := range strings.Split(out, "\n") {
		e, ok := parseStatLine(line)
		if !ok || e.Path == dir {
			continue
		}
		if e.Symlink != "" {
			links = append(links, e.Path)
		}
		entries = append(entries, e)
	}

	// 软链指向目录时也要能点进去。跟一步要额外一次往返,
	// 所以攒到一起问一次,而不是一条一条来
	if len(links) > 0 {
		t.resolveLinks(entries, links)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	out2 := newListing(dir, len(entries))
	if len(entries) > maxEntries {
		out2.Truncated = true
		entries = entries[:maxEntries]
	}
	out2.Entries = append(out2.Entries, entries...)
	return out2, nil
}

// resolveLinks 批量跟一步软链,把指向目录的那些标成目录
func (t *androidTransport) resolveLinks(entries []Entry, links []string) {
	quoted := make([]string, len(links))
	for i, l := range links {
		quoted[i] = shellQuote(l)
	}
	script := "stat -L -c '%F|%s|%Y|%n' " + strings.Join(quoted, " ") + " 2>/dev/null"
	out, err := t.text(script, androidCmdTimeout)
	if err != nil {
		return // 跟不动就算了,链本身的信息已经有了
	}
	byPath := map[string]Entry{}
	for _, line := range strings.Split(out, "\n") {
		if e, ok := parseStatLine(line); ok {
			byPath[e.Path] = e
		}
	}
	for i := range entries {
		if entries[i].Symlink == "" {
			continue
		}
		if target, ok := byPath[entries[i].Path]; ok {
			entries[i].IsDir = target.IsDir
			entries[i].Size = target.Size
		} else {
			entries[i].Err = "断链或读不到目标"
		}
	}
}

func (t *androidTransport) stat(p string) (*Entry, error) {
	out, err := t.text(fmt.Sprintf("stat -c '%%F|%%s|%%Y|%%N' %s 2>/dev/null", shellQuote(p)),
		androidCmdTimeout)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(out, "\n") {
		if e, ok := parseStatLine(line); ok {
			return &e, nil
		}
	}
	return nil, fmt.Errorf("读不到 %s%s", p, t.rootHint())
}

func (t *androidTransport) exists(p string) bool {
	out, err := t.text(fmt.Sprintf("stat -c '%%F' %s >/dev/null 2>&1 && echo Y", shellQuote(p)),
		androidCmdTimeout)
	return err == nil && strings.Contains(out, "Y")
}

func (t *androidTransport) search(root, pattern string, limit int) (*SearchResult, error) {
	script := fmt.Sprintf(
		"find %s -iname %s -exec stat -c '%%F|%%s|%%Y|%%n' {} + 2>/dev/null | head -n %d",
		shellQuote(root), shellQuote(pattern), limit+1)
	out, err := t.text(script, androidSearchTimeout)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, fmt.Errorf("在设备上执行查找失败: %w", err)
	}
	return parseStatLines(out, root, pattern, limit), nil
}

// pull 把设备上的文件拉到本地。
//
// 三条路,按"要不要 root"和"要多少"分:
//   - 不用 root:直接读 shell 通道。走 adb 协议之后这条是干净的二进制,
//     不用绕 base64,也不往设备里写东西
//   - 要 root 且只取开头(预览):base64 文本通道。慢,但同样一个字节都不往设备写 ——
//     翻看不该改动被取证的设备
//   - 要 root 且要整个(导出):设备内先复制到 /sdcard 再走 sync 协议拉。
//     快二十倍,代价是会在设备上留一个临时文件,拉完就删
func (t *androidTransport) pull(remote, local string, limit int64) (int64, bool, error) {
	if !t.root {
		return t.pullPlain(remote, local, limit)
	}
	if limit > 0 {
		return t.pullInline(remote, local, limit)
	}
	return t.pullStaged(remote, local)
}

// pullPlain 非 root:shell 通道本身就是干净的二进制
func (t *androidTransport) pullPlain(remote, local string, limit int64) (int64, bool, error) {
	script := "cat " + shellQuote(remote)
	if limit > 0 {
		script = fmt.Sprintf("head -c %d %s", limit, shellQuote(remote))
	}
	data, err := t.raw(script, false, androidPullTimeout)
	if err != nil {
		return 0, false, fmt.Errorf("读不了 %s%s: %w", remote, t.rootHint(), err)
	}
	return writeLocal(local, data, limit)
}

// pullInline root + 只取开头:base64 中转。
// su 的 pty 会把 LF 换成 CRLF,直接读二进制一定是坏的
func (t *androidTransport) pullInline(remote, local string, limit int64) (int64, bool, error) {
	script := fmt.Sprintf("head -c %d %s 2>/dev/null | toybox base64", limit, shellQuote(remote))
	out, err := t.raw(script, true, androidPullTimeout)
	if err != nil && len(bytes.TrimSpace(out)) == 0 {
		return 0, false, fmt.Errorf("读不了 %s%s", remote, t.rootHint())
	}
	// base64 的输出被 pty 插了 CRLF,而且 toybox 会按行折行,全去掉再解
	cleaned := strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, string(out))
	data, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return 0, false, fmt.Errorf("设备返回的内容解不开(可能这个文件读不了): %w", err)
	}
	return writeLocal(local, data, limit)
}

// pullStaged root + 要整个:设备内复制到 /sdcard,再走 sync 协议拉
func (t *androidTransport) pullStaged(remote, local string) (int64, bool, error) {
	stage := fmt.Sprintf("%s/.toolforge-pull-%d", androidStageDir, time.Now().UnixNano())
	script := fmt.Sprintf("cp %s %s && chmod 666 %s",
		shellQuote(remote), shellQuote(stage), shellQuote(stage))
	if _, err := t.text(script, androidPullTimeout); err != nil {
		return 0, false, fmt.Errorf("在设备上复制 %s 失败%s: %w", remote, t.rootHint(), err)
	}
	// 无论后面成不成,设备上那份临时文件都得删掉
	defer func() {
		_, _ = t.text("rm -f "+shellQuote(stage), androidCmdTimeout)
	}()

	dst, err := createLocal(local)
	if err != nil {
		return 0, false, err
	}
	defer dst.Close()
	if err := t.dev.Pull(stage, dst); err != nil {
		return 0, false, fmt.Errorf("从设备拉取失败: %w", err)
	}
	st, err := os.Stat(local)
	if err != nil {
		return 0, false, err
	}
	return st.Size(), false, nil
}

func writeLocal(local string, data []byte, limit int64) (int64, bool, error) {
	dst, err := createLocal(local)
	if err != nil {
		return 0, false, err
	}
	defer dst.Close()
	n, err := dst.Write(data)
	if err != nil {
		return int64(n), false, err
	}
	return int64(n), limit > 0 && int64(n) == limit, nil
}

// rootHint 没 root 时补一句为什么。
// /data/data 下面没 root 就是读不了,不说的话人会以为是路径打错了
func (t *androidTransport) rootHint() string {
	if t.root {
		return ""
	}
	return "(当前没有 root,/data 下面读不了)"
}

// runCmd 跑一个本地进程,带超时。
//
// 现在只剩"启动 adb 服务端"一处在用它,但 WaitDelay 这一行仍然是必须的:
// adb 拉起来的那个服务端是常驻进程,而且继承了我们这条 stdout 管道的写端。
// Go 的 Wait 要等 io 拷贝 goroutine 结束,管道又要等所有持有写端的进程退出 ——
// 服务端永远不退,于是 context 到期把 adb 杀了也没用,Wait 照样卡死。
// 界面上的表现就是"连接中"三个字一直转下去。
const cmdWaitDelay = 3 * time.Second

func runCmd(ctx context.Context, timeout time.Duration, name string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = cmdWaitDelay
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

// parseStatLine 解一行 stat -c '%F|%s|%Y|%N' 的输出。
//
// 只切前三个 | ——  文件名里出现 | 是合法的,路径必须放最后一段。
// %N 对普通文件给纯路径,对软链给 "路径 -> '目标'",按这个区分
func parseStatLine(line string) (Entry, bool) {
	line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
	if line == "" {
		return Entry{}, false
	}
	parts := strings.SplitN(line, "|", 4)
	if len(parts) != 4 {
		return Entry{}, false
	}
	kind, name := parts[0], parts[3]
	e := Entry{
		IsDir: kind == "directory",
		Mode:  kind,
	}
	e.Size, _ = strconv.ParseInt(parts[1], 10, 64)
	e.ModTime, _ = strconv.ParseInt(parts[2], 10, 64)

	if kind == "symbolic link" {
		if i := strings.Index(name, " -> "); i >= 0 {
			e.Symlink = strings.Trim(name[i+4:], "'")
			name = name[:i]
		}
	}
	e.Path = strings.Trim(name, "'")
	e.Name = path.Base(e.Path)
	return e, true
}

// parseStatLines 把一堆 stat 输出解成查找结果。iOS 和 Android 用的是同一套格式
func parseStatLines(out, root, pattern string, limit int) *SearchResult {
	res := &SearchResult{Root: root, Pattern: pattern, Hits: []SearchHit{}}
	for _, line := range strings.Split(out, "\n") {
		e, ok := parseStatLine(line)
		if !ok {
			continue
		}
		if len(res.Hits) >= limit {
			res.Truncated = true
			break
		}
		res.Hits = append(res.Hits, SearchHit{
			Path:    e.Path,
			IsDir:   e.IsDir,
			Size:    e.Size,
			ModTime: e.ModTime,
		})
	}
	return res
}
