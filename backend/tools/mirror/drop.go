package mirror

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/electricbubble/gadb"

	"tool_forge/backend/tools/adbx"
)

// 往投屏画面上拖文件:安装包装到手机上,别的文件推到手机的「下载」文件夹(Download)。
//
// 这两样是做测试数据时最常用的。它们都是往手机里写东西,所以只在用户真把文件拖进来时才做,
// 推文件也绝不覆盖手机上已有的同名文件

const (
	// pushDir 推上去的文件放这儿:手机自带的文件管理里一眼就找得到
	pushDir = "/sdcard/Download"
	// maxFolderFiles 一个文件夹最多推多少个文件。拖错了(比如整个盘)不至于推到天荒地老
	maxFolderFiles = 5000
	// installTimeout 装一个应用最多等多久:有的手机要在屏幕上点确认,甚至输账号密码
	installTimeout = 3 * time.Minute
	// progressEvery 推大文件时多久报一次进度
	progressEvery = 300 * time.Millisecond
)

// DropItem 拖进来的一项处理得怎样
type DropItem struct {
	Name string `json:"name"`
	// Kind install = 装安装包;push = 推文件;folder = 推整个文件夹
	Kind string `json:"kind"`
	OK   bool   `json:"ok"`
	// Remote 推到了手机上的哪里
	Remote string `json:"remote,omitempty"`
	// Files 文件夹里推了几个文件
	Files int    `json:"files,omitempty"`
	Error string `json:"error,omitempty"`
}

// Drop 处理拖进投屏画面的文件,一个一个来。进度从投屏的那条连接上报给界面
func (s *Service) Drop(id string, paths []string) ([]DropItem, error) {
	sess, err := s.live(id)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}
	client, err := adbx.Dial(sess.adbPath)
	if err != nil {
		return nil, err
	}
	dev, _, err := adbx.PickDevice(client, sess.serial)
	if err != nil {
		return nil, err
	}
	t := &transfer{sess: sess, dev: dev, total: len(paths)}
	items := make([]DropItem, 0, len(paths))
	for i, p := range paths {
		t.index = i + 1
		items = append(items, t.one(p))
	}
	// 进度条收起来
	sess.notify(notice{Type: "transfer"})
	return items, nil
}

// transfer 一次拖放
type transfer struct {
	sess         *session
	dev          gadb.Device
	index, total int
	// said 上一次报进度的时间。一个文件夹几千个小文件,每个都报一遍界面会被刷爆
	said time.Time
}

func (t *transfer) one(local string) DropItem {
	name := filepath.Base(local)
	st, err := os.Stat(local)
	if err != nil {
		return DropItem{Name: name, Kind: "push", Error: "读不了这个文件：" + err.Error()}
	}
	switch {
	case st.IsDir():
		item := DropItem{Name: name, Kind: "folder"}
		item.Remote, item.Files, err = t.pushFolder(local, name)
		item.OK = err == nil
		if err != nil {
			item.Error = err.Error()
		}
		return item
	case strings.EqualFold(filepath.Ext(name), ".apk"):
		item := DropItem{Name: name, Kind: "install"}
		if err := t.install(local, st); err != nil {
			item.Error = err.Error()
		} else {
			item.OK = true
		}
		return item
	default:
		item := DropItem{Name: name, Kind: "push"}
		remote, err := t.freeRemoteName(pushDir, name)
		if err == nil {
			err = t.push(local, remote, st, "正在推送 "+name)
		}
		if err != nil {
			item.Error = err.Error()
			return item
		}
		t.scan(remote)
		item.Remote, item.OK = remote, true
		return item
	}
}

// pushFolder 整个文件夹推到 Download 底下,保留里面的目录结构
func (t *transfer) pushFolder(local, name string) (string, int, error) {
	// 先数一遍再动手:拖错了整个盘的话,在往手机写任何东西之前就拦下
	var files []string
	errTooMany := errors.New("too many")
	err := filepath.WalkDir(local, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			files = append(files, p)
			if len(files) > maxFolderFiles {
				return errTooMany
			}
		}
		return nil
	})
	if errors.Is(err, errTooMany) {
		return "", 0, fmt.Errorf("文件夹里超过 %d 个文件，太多了，挑一部分再拖", maxFolderFiles)
	}
	if err != nil {
		return "", 0, fmt.Errorf("读文件夹失败：%w", err)
	}
	if len(files) == 0 {
		return "", 0, errors.New("文件夹是空的")
	}
	root, err := t.freeRemoteName(pushDir, name)
	if err != nil {
		return "", 0, err
	}
	for i, p := range files {
		rel, _ := filepath.Rel(local, p)
		st, err := os.Stat(p)
		if err != nil {
			return root, i, fmt.Errorf("%s：%w", rel, err)
		}
		remote := root + "/" + filepath.ToSlash(rel)
		if err := t.push(p, remote, st, fmt.Sprintf("正在推送 %s：第 %d/%d 个文件", name, i+1, len(files))); err != nil {
			return root, i, fmt.Errorf("%s：%w", rel, err)
		}
		t.scan(remote)
	}
	return root, len(files), nil
}

// install 先把安装包推到临时目录,再让手机装,装完删掉临时文件
func (t *transfer) install(local string, st os.FileInfo) error {
	name := filepath.Base(local)
	tmp := "/data/local/tmp/toolforge-" + randomHex(6) + ".apk"
	if err := t.push(local, tmp, st, "正在把 "+name+" 传到手机"); err != nil {
		return err
	}
	defer func() { _, _ = adbx.Text(t.dev, "rm -f "+adbx.Quote(tmp), false, 15*time.Second) }()
	// 这句不能被限流吞掉:手机上弹确认框时,人得知道要去点
	t.said = time.Time{}
	t.progress("正在安装 " + name + "，手机上要是弹出确认，在画面上点允许")
	out, err := adbx.Text(t.dev, "pm install -r -t "+adbx.Quote(tmp), false, installTimeout)
	if code, _ := installFailure(out); code == "INSTALL_FAILED_DEPRECATED_SDK_VERSION" {
		// 安卓 14 起默认不让装目标版本太老的应用,做数据又常常就是要装老版本
		out, err = adbx.Text(t.dev, "pm install -r -t --bypass-low-target-sdk-block "+adbx.Quote(tmp), false, installTimeout)
	}
	return installResult(out, err)
}

// push 推一个文件,修改时间照搬本地的(推照片时相册按它排序)
func (t *transfer) push(local, remote string, st os.FileInfo, label string) error {
	f, err := os.Open(local)
	if err != nil {
		return fmt.Errorf("读不了：%w", err)
	}
	defer f.Close()
	t.progress(label)
	r := &progressReader{r: f, total: st.Size(), report: func(pct int) {
		t.progress(fmt.Sprintf("%s %d%%", label, pct))
	}}
	if err := t.dev.Push(r, remote, st.ModTime(), 0o644); err != nil {
		return fmt.Errorf("推送失败：%w", err)
	}
	return nil
}

// scan 照片、视频、音乐推上去之后让手机登记进媒体库,相册、音乐里才看得到
func (t *transfer) scan(remote string) {
	if !isMedia(remote) {
		return
	}
	if msg, ok := scanFileMessage(remote); ok {
		_ = t.sess.sendControl(msg)
	}
}

// progress 报进度,一秒最多十次
func (t *transfer) progress(text string) {
	if time.Since(t.said) < 100*time.Millisecond {
		return
	}
	t.said = time.Now()
	if t.total > 1 {
		text += fmt.Sprintf("（共 %d 项，第 %d 项）", t.total, t.index)
	}
	t.sess.notify(notice{Type: "transfer", Text: text})
}

// freeRemoteName 手机上已经有同名的就换成「名字 (1).扩展名」,不覆盖原有的文件
func (t *transfer) freeRemoteName(dir, name string) (string, error) {
	return freeName(dir, name, func(p string) (bool, error) {
		out, err := adbx.Text(t.dev, "[ -e "+adbx.Quote(p)+" ] && echo y || echo n", false, 15*time.Second)
		if err != nil {
			return false, fmt.Errorf("查手机上有没有同名文件失败：%w", err)
		}
		return strings.TrimSpace(out) == "y", nil
	})
}

func freeName(dir, name string, exists func(string) (bool, error)) (string, error) {
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if stem == "" {
		// .nomedia 这种:整个名字就是扩展名,编号加在后面
		stem, ext = name, ""
	}
	for i := 0; i < 100; i++ {
		cand := name
		if i > 0 {
			cand = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		p := path.Join(dir, cand)
		taken, err := exists(p)
		if err != nil {
			return "", err
		}
		if !taken {
			return p, nil
		}
	}
	return "", fmt.Errorf("手机上同名的 %s 太多了", name)
}

// isMedia 相册、音乐会收录的文件
func isMedia(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic", ".heif", ".bmp", ".dng",
		".mp4", ".3gp", ".mkv", ".mov", ".webm", ".avi", ".ts", ".m4v",
		".mp3", ".m4a", ".aac", ".wav", ".flac", ".ogg", ".amr", ".opus":
		return true
	}
	return false
}

// ---- 安装结果 ----

var (
	installSuccess = regexp.MustCompile(`(?m)^\s*Success\s*$`)
	installFail    = regexp.MustCompile(`Failure \[([A-Z0-9_]+)(?::\s*([^\]]*))?\]`)
)

// installFailure 从 pm install 的输出里取出失败代码和说明
func installFailure(out string) (code, detail string) {
	if m := installFail.FindStringSubmatch(out); m != nil {
		return m[1], strings.TrimSpace(m[2])
	}
	return "", ""
}

func installResult(out string, err error) error {
	if installSuccess.MatchString(out) {
		return nil
	}
	if code, detail := installFailure(out); code != "" {
		return errors.New(installMessage(code, detail))
	}
	if err != nil {
		return fmt.Errorf("安装失败：%w", err)
	}
	msg := lastLine(out)
	if msg == "" {
		msg = "手机没说为什么"
	}
	return fmt.Errorf("安装失败：%s", msg)
}

// installMessage 常见的失败代码翻成人话,并说怎么办
func installMessage(code, detail string) string {
	switch code {
	case "INSTALL_FAILED_VERSION_DOWNGRADE":
		return "手机上装着更新的版本。要装这个旧版，得先卸载手机上的（会清掉它的数据）"
	case "INSTALL_FAILED_UPDATE_INCOMPATIBLE", "INSTALL_FAILED_SHARED_USER_INCOMPATIBLE":
		return "和手机上已经装的那个签名不一样，得先卸载手机上的（会清掉它的数据）"
	case "INSTALL_FAILED_INSUFFICIENT_STORAGE":
		return "手机空间不够"
	case "INSTALL_FAILED_NO_MATCHING_ABIS":
		return "安装包里没有适合这台手机处理器的版本"
	case "INSTALL_FAILED_OLDER_SDK":
		return "这个安装包要求的安卓版本比这台手机新"
	case "INSTALL_FAILED_DEPRECATED_SDK_VERSION":
		return "安装包太老，这台手机的系统不让装"
	case "INSTALL_FAILED_USER_RESTRICTED":
		return "手机不让通过 USB 装应用：小米、红米要在「开发者选项」里打开「USB 安装」；别的手机看看屏幕上是不是弹了确认框没点"
	case "INSTALL_FAILED_ABORTED":
		return "安装被取消了：手机上弹的确认框被拒绝了，或者等太久没点"
	case "INSTALL_FAILED_VERIFICATION_FAILURE", "INSTALL_FAILED_VERIFICATION_TIMEOUT":
		return "手机的安装验证没通过，有的手机要在屏幕上点确认"
	case "INSTALL_FAILED_INVALID_APK", "INSTALL_PARSE_FAILED_NOT_APK", "INSTALL_PARSE_FAILED_UNEXPECTED_EXCEPTION",
		"INSTALL_PARSE_FAILED_NO_CERTIFICATES", "INSTALL_PARSE_FAILED_INCONSISTENT_CERTIFICATES", "INSTALL_PARSE_FAILED_BAD_MANIFEST":
		return "安装包坏了，或者不是完整的 APK（拆成好几个的分包安装包这里装不了）"
	case "INSTALL_FAILED_DUPLICATE_PERMISSION", "INSTALL_FAILED_CONFLICTING_PROVIDER":
		return "和手机上另一个应用冲突了（" + code + "）"
	case "INSTALL_FAILED_MISSING_SHARED_LIBRARY":
		return "这台手机缺这个应用要的系统组件"
	}
	if detail != "" {
		return "安装失败：" + code + "（" + detail + "）"
	}
	return "安装失败：" + code
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// progressReader 推大文件时按百分比报进度,隔一会儿报一次
type progressReader struct {
	r      io.Reader
	total  int64
	done   int64
	last   time.Time
	report func(pct int)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.total > 0 && time.Since(p.last) >= progressEvery {
		p.last = time.Now()
		p.report(int(p.done * 100 / p.total))
	}
	return n, err
}
