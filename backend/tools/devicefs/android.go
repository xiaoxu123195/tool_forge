package devicefs

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Android 这条路的形状:
//
//	adb shell → su(Magisk)→ 设备自带的 toybox
//
// 没有 SFTP 这种结构化通道,所有信息都得从命令输出里解析出来。
// 三件事是实测定下来的,不是拍脑袋:
//
//  1. 命令用 base64 包一层再送。原因是引用层数太多:Go 的 exec 一层、
//     Windows 命令行一层、adb 把参数拼回字符串一层、设备的 sh 一层、su 再起的 sh 一层。
//     文件名里有个空格或引号就会在某一层碎掉。base64 之后命令行上只剩
//     [A-Za-z0-9+/=],没有任何一层能改动它。
//
//  2. 读文件必须走 base64,不能直接 cat。su 会分配一个 pty,pty 把 LF 换成 CRLF ——
//     502056 字节的文件直接 cat 出来变成 506112 字节,而且不会报任何错。
//     这个坑不看字节数根本发现不了。
//
//  3. 整文件导出改走"设备内先复制到 /sdcard,再 adb pull"。base64 通道约 1.4 MB/s,
//     而 adb 自己的同步协议是 26 MB/s。代价是会往设备里写一个临时文件,
//     所以只在明确要导出时才这么干;浏览和预览一个字节都不往设备写。

const (
	// androidPreviewLimit 预览最多拉多少字节。
	// 比 iOS 小一个数量级,因为这边预览走的是 base64 文本通道(约 1.4 MB/s),
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
	adb    string
	serial string
	// root su 能不能用。用不了的话只看得到 /sdcard,
	// 而有价值的数据全在 /data/data 下面
	root bool
}

func connectAndroid(opt ConnectOptions) (transport, *Session, error) {
	adb := strings.TrimSpace(opt.AdbPath)
	if adb == "" {
		adb = "adb"
	}
	serial, model, err := pickAndroidDevice(adb, opt.DeviceID)
	if err != nil {
		return nil, nil, err
	}
	t := &androidTransport{adb: adb, serial: serial}
	t.root = t.probeRoot()

	return t, &Session{
		Platform: "android",
		DeviceID: serial,
		Addr:     serial,
		Rooted:   t.root,
		Model:    model,
	}, nil
}

// pickAndroidDevice 选一台设备。
//
// 状态必须是 device:unauthorized 说明手机上那个"允许 USB 调试"的框还没点,
// offline 说明连接是坏的 —— 这两种都得说清楚,不然人只会看到一句"没找到设备"
func pickAndroidDevice(adb, want string) (serial, model string, err error) {
	out, _, err := runCmd(context.Background(), 20*time.Second, adb, "devices", "-l")
	if err != nil {
		return "", "", fmt.Errorf("跑不起来 %s:%w —— 确认 adb 装了、路径对", adb, err)
	}
	type dev struct{ serial, state, model string }
	var devices []dev
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "*") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		d := dev{serial: fields[0], state: fields[1]}
		for _, f := range fields[2:] {
			if strings.HasPrefix(f, "model:") {
				d.model = strings.TrimPrefix(f, "model:")
			}
		}
		devices = append(devices, d)
	}
	if len(devices) == 0 {
		return "", "", errors.New("adb 没看到任何设备 —— 确认线插好、手机上开了 USB 调试;" +
			"如果别的工具占着 adb 服务端口,它的版本过老也会导致设备认不出来")
	}
	for _, d := range devices {
		if want != "" && d.serial != want {
			continue
		}
		switch d.state {
		case "device":
			return d.serial, d.model, nil
		case "unauthorized":
			return "", "", fmt.Errorf("设备 %s 还没授权 —— 手机屏幕上会弹「允许 USB 调试」,点一下允许", d.serial)
		default:
			return "", "", fmt.Errorf("设备 %s 当前状态是 %s,连不上", d.serial, d.state)
		}
	}
	return "", "", fmt.Errorf("没找到序列号为 %s 的设备", want)
}

// probeRoot 试一下 su 能不能用
func (t *androidTransport) probeRoot() bool {
	out, _, err := t.shell(context.Background(), 15*time.Second, "id", true)
	return err == nil && strings.Contains(out, "uid=0")
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

// close Android 这边没有需要收的长驻进程 —— adb 服务是系统级的,
// 不该由我们关掉(别的工具可能也在用)
func (t *androidTransport) close() error { return nil }

// shell 在设备上跑一段脚本。
//
// script 会被 base64 包起来再送,理由见文件头。root 为真时用 su 跑。
func (t *androidTransport) shell(ctx context.Context, timeout time.Duration, script string, asRoot bool) (string, string, error) {
	payload := base64.StdEncoding.EncodeToString([]byte(script))
	// 命令替换让解出来的整段脚本成为 su 的**一个**参数,不会被再切一次;
	// 用管道喂给 su 的写法在部分设备上会把脚本原样回显出来
	var wrapped string
	if asRoot {
		wrapped = fmt.Sprintf(`su -c "$(echo %s | base64 -d)"`, payload)
	} else {
		wrapped = fmt.Sprintf(`echo %s | base64 -d | sh`, payload)
	}
	return runCmd(ctx, timeout, t.adb, "-s", t.serial, "exec-out", wrapped)
}

// text 跑一段脚本并拿到文本输出。
// 设备那头的 pty 会把 LF 变成 CRLF,统一去掉 \r
func (t *androidTransport) text(script string, timeout time.Duration) (string, error) {
	out, stderr, err := t.shell(context.Background(), timeout, script, t.root)
	out = strings.ReplaceAll(out, "\r\n", "\n")
	if err != nil && strings.TrimSpace(out) == "" {
		if s := strings.TrimSpace(stderr); s != "" {
			return "", fmt.Errorf("%s", s)
		}
		return "", err
	}
	return out, nil
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
		// find 什么都没回:目录不存在,或者没权限进去。
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
// 两条路,按 limit 分:
//   - limit > 0(预览):走 base64 文本通道。慢,但一个字节都不往设备里写 ——
//     翻看不该改动被取证的设备
//   - limit <= 0(导出):设备内先复制到 /sdcard 再 adb pull。快二十倍,
//     代价是会在设备上留一个临时文件,拉完就删
func (t *androidTransport) pull(remote, local string, limit int64) (int64, bool, error) {
	if limit > 0 {
		return t.pullInline(remote, local, limit)
	}
	return t.pullStaged(remote, local)
}

func (t *androidTransport) pullInline(remote, local string, limit int64) (int64, bool, error) {
	script := fmt.Sprintf("head -c %d %s 2>/dev/null | toybox base64", limit, shellQuote(remote))
	out, _, err := t.shell(context.Background(), androidPullTimeout, script, t.root)
	if err != nil && strings.TrimSpace(out) == "" {
		return 0, false, fmt.Errorf("读不了 %s%s", remote, t.rootHint())
	}
	// base64 的输出被 pty 插了 CRLF,而且 toybox 会按行折行,全去掉再解
	cleaned := strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, out)
	data, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return 0, false, fmt.Errorf("设备返回的内容解不开(可能这个文件读不了): %w", err)
	}
	dst, err := createLocal(local)
	if err != nil {
		return 0, false, err
	}
	defer dst.Close()
	n, err := dst.Write(data)
	if err != nil {
		return int64(n), false, err
	}
	return int64(n), int64(n) == limit, nil
}

func (t *androidTransport) pullStaged(remote, local string) (int64, bool, error) {
	stage := fmt.Sprintf("%s/.toolforge-pull-%d", androidStageDir, time.Now().UnixNano())
	script := fmt.Sprintf("cp %s %s && chmod 666 %s", shellQuote(remote), shellQuote(stage), shellQuote(stage))
	if _, err := t.text(script, androidPullTimeout); err != nil {
		return 0, false, fmt.Errorf("在设备上复制 %s 失败%s: %w", remote, t.rootHint(), err)
	}
	// 无论后面成不成,设备上那份临时文件都得删掉
	defer func() {
		_, _ = t.text("rm -f "+shellQuote(stage), androidCmdTimeout)
	}()

	if _, _, err := runCmd(context.Background(), androidPullTimeout,
		t.adb, "-s", t.serial, "pull", stage, local); err != nil {
		return 0, false, fmt.Errorf("adb pull 失败: %w", err)
	}
	st, err := os.Stat(local)
	if err != nil {
		return 0, false, err
	}
	return st.Size(), false, nil
}

// rootHint 没 root 时补一句为什么。
// /data/data 下面没 root 就是读不了,不说的话人会以为是路径打错了
func (t *androidTransport) rootHint() string {
	if t.root {
		return ""
	}
	return "(当前没有 root,/data 下面读不了)"
}

// runCmd 跑一个本地进程,带超时
func runCmd(ctx context.Context, timeout time.Duration, name string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	hideWindow(cmd)
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return out.String(), errBuf.String(), fmt.Errorf("命令超过 %s 还没结束", timeout)
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
