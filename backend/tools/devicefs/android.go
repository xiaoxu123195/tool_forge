package devicefs

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/electricbubble/gadb"

	"tool_forge/backend/tools/adbx"
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
	client, err := adbx.Dial(opt.AdbPath)
	if err != nil {
		return nil, nil, err
	}
	dev, model, err := adbx.PickDevice(client, opt.DeviceID)
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

// probeRoot 试一下 su 能不能用
func (t *androidTransport) probeRoot() bool {
	return adbx.HasRoot(t.dev, androidCmdTimeout)
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

func (t *androidTransport) raw(script string, asRoot bool, timeout time.Duration) ([]byte, error) {
	return adbx.Shell(t.dev, script, asRoot, timeout)
}

// text 跑一段脚本并拿到文本输出。走 su 时设备那头的 pty 会把 LF 变成 CRLF,
// adbx 那边已经统一去掉了
func (t *androidTransport) text(script string, timeout time.Duration) (string, error) {
	return adbx.Text(t.dev, script, t.root, timeout)
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
