package mirror

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"howett.net/plist"

	"tool_forge/backend/tools/iosmux"
)

// localFS 拿本地临时目录当手机:手机上的路径照搬到 root 底下。
// 改归属、权限只记下来 —— Windows 上做不了,要验的是「有没有改」
type localFS struct {
	root  string
	owned map[string]bool        // 改成归 mobile 的
	modes map[string]fs.FileMode // 改过的权限
}

func (l *localFS) real(p string) string { return filepath.Join(l.root, filepath.FromSlash(p)) }

func (l *localFS) ReadDir(p string) ([]fs.FileInfo, error) {
	entries, err := os.ReadDir(l.real(p))
	if err != nil {
		return nil, err
	}
	out := make([]fs.FileInfo, 0, len(entries))
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			out = append(out, info)
		}
	}
	return out, nil
}
func (l *localFS) ReadFile(p string) ([]byte, error)  { return os.ReadFile(l.real(p)) }
func (l *localFS) Stat(p string) (fs.FileInfo, error) { return os.Stat(l.real(p)) }
func (l *localFS) Mkdir(p string) error               { return os.Mkdir(l.real(p), 0o755) }
func (l *localFS) Remove(p string) error              { return os.Remove(l.real(p)) }
func (l *localFS) Chtimes(p string, t time.Time) error {
	return os.Chtimes(l.real(p), t, t)
}
func (l *localFS) Create(p string) (io.WriteCloser, error) {
	return os.OpenFile(l.real(p), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
}
func (l *localFS) Chown(p string, uid, gid int) error {
	if uid != mobileID || gid != mobileID {
		return errors.New("该归 mobile")
	}
	l.owned[p] = true
	return nil
}
func (l *localFS) Chmod(p string, mode fs.FileMode) error {
	l.modes[p] = mode
	return nil
}

func newLocalFS(t *testing.T) *localFS {
	return &localFS{root: t.TempDir(), owned: map[string]bool{}, modes: map[string]fs.FileMode{}}
}

// newFakeIPhone 造一个带共享容器的「手机」:一个是「我的 iPhone」的存储,一个是别的 App 组
func newFakeIPhone(t *testing.T, withStorage bool) *localFS {
	t.Helper()
	l := newLocalFS(t)
	group := func(uuid, id string) string {
		dir := appGroupRoot + "/" + uuid
		if err := os.MkdirAll(l.real(dir), 0o755); err != nil {
			t.Fatal(err)
		}
		meta, err := plist.Marshal(map[string]any{"MCMMetadataIdentifier": id}, plist.BinaryFormat)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(l.real(dir+"/"+iosmux.ContainerMeta), meta, 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	group("11111111-AAAA", "group.com.tencent.xin")
	local := group("22222222-BBBB", localStorageGroup)
	if withStorage {
		if err := os.MkdirAll(l.real(local+"/File Provider Storage"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func writeLocal(t *testing.T, path, body string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestDropIOS(t *testing.T) {
	phone := newFakeIPhone(t, true)
	src := t.TempDir()
	mtime := time.Date(2024, 5, 1, 8, 30, 0, 0, time.UTC)
	photo := filepath.Join(src, "照片.jpg")
	writeLocal(t, photo, "jpeg", mtime)
	writeLocal(t, filepath.Join(src, "资料", "a.txt"), "a", mtime)
	writeLocal(t, filepath.Join(src, "资料", "子目录", "b.txt"), "b", mtime)

	var said []string
	items, err := dropIOS(phone, []string{photo, photo, filepath.Join(src, "资料"), filepath.Join(src, "不存在.txt")},
		func(s string) { said = append(said, s) })
	if err != nil {
		t.Fatal(err)
	}
	downloads := appGroupRoot + "/22222222-BBBB/File Provider Storage/Downloads"
	want := []DropItem{
		{Name: "照片.jpg", Kind: "push", OK: true, Remote: downloads + "/照片.jpg"},
		// 第二次拖同一个文件:不覆盖,换个名字
		{Name: "照片.jpg", Kind: "push", OK: true, Remote: downloads + "/照片 (1).jpg"},
		{Name: "资料", Kind: "folder", OK: true, Remote: downloads + "/资料", Files: 2},
	}
	for i, w := range want {
		if items[i] != w {
			t.Fatalf("第 %d 项:\n得到 %+v\n应该 %+v", i+1, items[i], w)
		}
	}
	if items[3].OK || !strings.Contains(items[3].Error, "读不了") {
		t.Fatalf("不存在的文件应该报读不了: %+v", items[3])
	}

	if body, _ := os.ReadFile(phone.real(downloads + "/资料/子目录/b.txt")); string(body) != "b" {
		t.Fatalf("文件夹里的子目录没推全: %q", body)
	}
	st, err := os.Stat(phone.real(downloads + "/照片.jpg"))
	if err != nil || !st.ModTime().Equal(mtime) {
		t.Fatalf("修改时间要照搬本地的: %v %v", st.ModTime(), err)
	}
	// 推上去的文件和建的文件夹都要归 mobile:SFTP 是 root 登录的,不改「文件」App 打不开
	for p, mode := range map[string]fs.FileMode{
		downloads:                   0o755,
		downloads + "/照片.jpg":       0o644,
		downloads + "/资料":           0o755,
		downloads + "/资料/子目录":       0o755,
		downloads + "/资料/子目录/b.txt": 0o644,
	} {
		if !phone.owned[p] || phone.modes[p] != mode {
			t.Errorf("%s 没改归属或者权限不对: 归属 %v 权限 %o", p, phone.owned[p], phone.modes[p])
		}
	}
	if len(said) == 0 || said[len(said)-1] != "" || !strings.Contains(strings.Join(said, "\n"), "正在推送 照片.jpg") {
		t.Fatalf("进度不对: %q", said)
	}
}

func TestDropIOSWithoutLocalStorage(t *testing.T) {
	// 「文件」App 从没打开过:容器有了,存储目录还没建
	phone := newFakeIPhone(t, false)
	src := filepath.Join(t.TempDir(), "a.txt")
	writeLocal(t, src, "a", time.Now())
	if _, err := dropIOS(phone, []string{src}, nil); err == nil || !strings.Contains(err.Error(), "打开一次「文件」App") {
		t.Fatalf("要说清楚怎么办: %v", err)
	}

	// 连容器都找不到
	empty := newLocalFS(t)
	_ = os.MkdirAll(empty.real(appGroupRoot), 0o755)
	if _, err := dropIOS(empty, []string{src}, nil); err == nil || !strings.Contains(err.Error(), "找不到") {
		t.Fatalf("找不到存储要明说: %v", err)
	}
}

func TestUploadSplitsOnRotation(t *testing.T) {
	src, _ := writeBrowserMP4(t, false, 0x020000)
	data, _ := os.ReadFile(src)
	dir := t.TempDir()
	svc := New()

	id, first, err := svc.BeginUpload(dir, "iPhone 8 Plus", ".mp4")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AppendUpload(id, data); err != nil {
		t.Fatal(err)
	}
	// 转屏:前一个文件收尾,接着往 _2 里录
	second, err := svc.NextUpload(id)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.TrimSuffix(first, ".mp4") + "_2.mp4"; second != want {
		t.Fatalf("第二个文件 %s,应该是 %s", second, want)
	}
	if fixed, _ := os.ReadFile(first); len(fixed) == len(data) {
		t.Fatal("换文件时前一个没补时长")
	}
	if err := svc.AppendUpload(id, data); err != nil {
		t.Fatal(err)
	}
	// 又转了一次,但没等到画面就停了:最后那个空文件不该留下
	third, err := svc.NextUpload(id)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := svc.EndUpload(id, 4000)
	if err != nil || rec.Error != "" {
		t.Fatalf("结束出错: %+v %v", rec, err)
	}
	if len(rec.Files) != 2 || rec.Files[0] != first || rec.Files[1] != second || rec.Bytes != int64(2*len(data)) {
		t.Fatalf("应该是两个文件: %+v", rec)
	}
	if _, err := os.Stat(third); !os.IsNotExist(err) {
		t.Fatal("空的那个文件应该删掉")
	}
	if fixed, _ := os.ReadFile(second); len(fixed) == len(data) {
		t.Fatal("最后一个文件没补时长")
	}
}
