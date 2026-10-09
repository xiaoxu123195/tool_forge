package mirror

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"

	"tool_forge/backend/tools/iosmux"
)

// 往 iOS 投屏画面上拖文件:经 SFTP 推到「文件」App 的「我的 iPhone › Downloads」里。
//
// 照片、视频也放这儿,不往相册目录(DCIM)里塞:iOS 的「照片」靠自己的数据库,直接放进目录的文件
// 它不认,要认只能重建照片库 —— 那是动手机上的数据,不做。在「文件」里点开 → 共享 → 存储图像/视频
// 就进相册了。IPA 也只是推进去,装要在手机上用 TrollStore 一类打开。
//
// 和安卓一样:只在用户真把文件拖进来时才往手机写,绝不覆盖手机上已有的同名文件

const (
	// localStorageGroup 「我的 iPhone」那块存储所在的共享容器。容器目录名是 UUID,只能看元信息认
	localStorageGroup = "group.com.apple.FileProvider.LocalStorage"
	appGroupRoot      = "/var/mobile/Containers/Shared/AppGroup"
	// iosPushFolder 「我的 iPhone」底下的这个文件夹。没开 iCloud 的手机上,Safari 下载的东西也放这儿
	iosPushFolder = "Downloads"
	// mobileID 手机上普通用户 mobile 的 uid、gid。「文件」App 以它的身份读写,推上去的得归它
	mobileID = 501
)

// EventIOSTransfer iOS 拖放的进度经这个 Wails 事件报给界面:那条 WebSocket 上跑的是 VNC,没处说话
const EventIOSTransfer = "mirror:ios-transfer"

// remoteFS 往手机上写文件要用的几样。真机上是 SFTP,测试里换成本地目录
type remoteFS interface {
	ReadDir(p string) ([]fs.FileInfo, error)
	ReadFile(p string) ([]byte, error)
	Stat(p string) (fs.FileInfo, error)
	Mkdir(p string) error
	// Create 已经有同名的就失败:绝不覆盖
	Create(p string) (io.WriteCloser, error)
	Remove(p string) error
	Chown(p string, uid, gid int) error
	Chmod(p string, mode fs.FileMode) error
	Chtimes(p string, mtime time.Time) error
}

// DropIOS 处理拖进 iOS 投屏画面的文件,一个一个来。progress 报进度,空字符串表示处理完了
func (s *Service) DropIOS(t IOSTarget, paths []string, progress func(string)) ([]DropItem, error) {
	if t.SSH == nil {
		return nil, errors.New("真机浏览没连着这台 iPhone")
	}
	if len(paths) == 0 {
		return nil, nil
	}
	c, err := sftp.NewClient(t.SSH)
	if err != nil {
		return nil, fmt.Errorf("SFTP 子系统起不来(手机上的 sshd 可能没开 sftp): %w", err)
	}
	defer c.Close()
	return dropIOS(sftpFS{c}, paths, progress)
}

func dropIOS(rfs remoteFS, paths []string, progress func(string)) ([]DropItem, error) {
	root, err := findLocalStorage(rfs)
	if err != nil {
		return nil, err
	}
	t := &iosTransfer{fs: rfs, dir: root + "/" + iosPushFolder, made: map[string]bool{}}
	t.say = progressLine{total: len(paths), send: progress}
	if err := t.ensureDir(t.dir); err != nil {
		return nil, err
	}
	items := make([]DropItem, 0, len(paths))
	for i, p := range paths {
		t.say.index = i + 1
		items = append(items, t.one(p))
	}
	if progress != nil {
		progress("")
	}
	return items, nil
}

// findLocalStorage 「我的 iPhone」那块存储在哪:挨个打开共享容器的元信息,找 FileProvider 的本机存储
func findLocalStorage(rfs remoteFS) (string, error) {
	groups, err := rfs.ReadDir(appGroupRoot)
	if err != nil {
		return "", fmt.Errorf("列不了手机上的共享容器:%w", err)
	}
	for _, g := range groups {
		if !g.IsDir() {
			continue
		}
		dir := appGroupRoot + "/" + g.Name()
		data, err := rfs.ReadFile(dir + "/" + iosmux.ContainerMeta)
		if err != nil {
			continue
		}
		if id, err := iosmux.BundleIDFromMeta(data); err != nil || id != localStorageGroup {
			continue
		}
		storage := dir + "/File Provider Storage"
		if _, err := rfs.Stat(storage); err != nil {
			return "", errors.New("「文件」App 的「我的 iPhone」还没建起来:在手机上打开一次「文件」App,点开「我的 iPhone」再拖")
		}
		return storage, nil
	}
	return "", errors.New("在手机上找不到「文件」App 的「我的 iPhone」那块存储")
}

// iosTransfer 一次拖放
type iosTransfer struct {
	fs  remoteFS
	dir string
	say progressLine
	// made 这次已经确认过(或者建好)的文件夹:一个文件夹几千个文件,不用每个都去问一遍
	made map[string]bool
}

func (t *iosTransfer) one(local string) DropItem {
	name := filepath.Base(local)
	st, err := os.Stat(local)
	if err != nil {
		return DropItem{Name: name, Kind: "push", Error: "读不了这个文件：" + err.Error()}
	}
	if st.IsDir() {
		item := DropItem{Name: name, Kind: "folder"}
		item.Remote, item.Files, err = t.pushFolder(local, name)
		item.OK = err == nil
		if err != nil {
			item.Error = err.Error()
		}
		return item
	}
	item := DropItem{Name: name, Kind: "push"}
	remote, err := t.freeName(t.dir, name)
	if err == nil {
		err = t.push(local, remote, st, "正在推送 "+name)
	}
	if err != nil {
		item.Error = err.Error()
		return item
	}
	item.Remote, item.OK = remote, true
	return item
}

// pushFolder 整个文件夹推到 Downloads 底下,保留里面的目录结构
func (t *iosTransfer) pushFolder(local, name string) (string, int, error) {
	files, err := folderFiles(local)
	if err != nil {
		return "", 0, err
	}
	root, err := t.freeName(t.dir, name)
	if err != nil {
		return "", 0, err
	}
	for i, p := range files {
		rel, _ := filepath.Rel(local, p)
		rel = filepath.ToSlash(rel)
		st, err := os.Stat(p)
		if err == nil {
			err = t.ensureDirs(root, path.Dir(rel))
		}
		if err == nil {
			err = t.push(p, root+"/"+rel, st, fmt.Sprintf("正在推送 %s：第 %d/%d 个文件", name, i+1, len(files)))
		}
		if err != nil {
			return root, i, fmt.Errorf("%s：%w", rel, err)
		}
	}
	return root, len(files), nil
}

// push 推一个文件:归 mobile,修改时间照搬本地的。推到一半断了就删掉半截的
func (t *iosTransfer) push(local, remote string, st os.FileInfo, label string) error {
	f, err := os.Open(local)
	if err != nil {
		return fmt.Errorf("读不了：%w", err)
	}
	defer f.Close()
	t.say.progress(label)
	w, err := t.fs.Create(remote)
	if err != nil {
		return fmt.Errorf("推送失败：%w", err)
	}
	r := &progressReader{r: f, total: st.Size(), report: func(pct int) {
		t.say.progress(fmt.Sprintf("%s %d%%", label, pct))
	}}
	_, err = io.Copy(w, r)
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = t.fs.Remove(remote)
		return fmt.Errorf("推送失败：%w", err)
	}
	if err := t.own(remote, 0o644); err != nil {
		return err
	}
	_ = t.fs.Chtimes(remote, st.ModTime())
	return nil
}

// ensureDirs root 底下的 rel 一层层建上,已经有的不动
func (t *iosTransfer) ensureDirs(root, rel string) error {
	if err := t.ensureDir(root); err != nil {
		return err
	}
	if rel == "." || rel == "" {
		return nil
	}
	cur := root
	for _, seg := range strings.Split(rel, "/") {
		cur += "/" + seg
		if err := t.ensureDir(cur); err != nil {
			return err
		}
	}
	return nil
}

// ensureDir 文件夹不在就建上,归 mobile
func (t *iosTransfer) ensureDir(dir string) error {
	if t.made[dir] {
		return nil
	}
	st, err := t.fs.Stat(dir)
	switch {
	case err == nil && !st.IsDir():
		return fmt.Errorf("手机上 %s 是个文件，不是文件夹", path.Base(dir))
	case err == nil:
	case errors.Is(err, fs.ErrNotExist):
		if err := t.fs.Mkdir(dir); err != nil {
			return fmt.Errorf("建不了文件夹 %s：%w", path.Base(dir), err)
		}
		if err := t.own(dir, 0o755); err != nil {
			return err
		}
	default:
		return err
	}
	t.made[dir] = true
	return nil
}

// own 推上去的东西归 mobile:SFTP 是以 root 登录的,不改的话「文件」App 打不开
func (t *iosTransfer) own(p string, mode fs.FileMode) error {
	if err := t.fs.Chown(p, mobileID, mobileID); err != nil {
		return fmt.Errorf("改不了 %s 的归属：%w", path.Base(p), err)
	}
	if err := t.fs.Chmod(p, mode); err != nil {
		return fmt.Errorf("改不了 %s 的权限：%w", path.Base(p), err)
	}
	return nil
}

// freeName 手机上已经有同名的就换成「名字 (1).扩展名」,不覆盖原有的文件
func (t *iosTransfer) freeName(dir, name string) (string, error) {
	return freeName(dir, name, func(p string) (bool, error) {
		_, err := t.fs.Stat(p)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, fs.ErrNotExist):
			return false, nil
		default:
			return false, fmt.Errorf("查手机上有没有同名文件失败：%w", err)
		}
	})
}

// ---- SFTP ----

type sftpFS struct{ c *sftp.Client }

func (s sftpFS) ReadDir(p string) ([]fs.FileInfo, error) { return s.c.ReadDir(p) }
func (s sftpFS) Stat(p string) (fs.FileInfo, error)      { return s.c.Stat(p) }
func (s sftpFS) Mkdir(p string) error                    { return s.c.Mkdir(p) }
func (s sftpFS) Remove(p string) error                   { return s.c.Remove(p) }
func (s sftpFS) Chown(p string, uid, gid int) error      { return s.c.Chown(p, uid, gid) }
func (s sftpFS) Chmod(p string, mode fs.FileMode) error  { return s.c.Chmod(p, mode) }
func (s sftpFS) Chtimes(p string, t time.Time) error     { return s.c.Chtimes(p, t, t) }

func (s sftpFS) ReadFile(p string) ([]byte, error) {
	f, err := s.c.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// 元信息 plist 也就几 KB,给到 1 MB 纯粹防着读到个不对劲的大文件
	return io.ReadAll(io.LimitReader(f, 1<<20))
}

func (s sftpFS) Create(p string) (io.WriteCloser, error) {
	f, err := s.c.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return nil, err
	}
	return sftpFile{f}, nil
}

// sftpFile 写文件时多个包一起发:一个包一个包等回话的话,USB 上推大视频要慢好几倍
type sftpFile struct{ *sftp.File }

func (f sftpFile) ReadFrom(r io.Reader) (int64, error) { return f.File.ReadFromWithConcurrency(r, 0) }
