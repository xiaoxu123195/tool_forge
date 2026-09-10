package devicefs

import (
	"fmt"
	"os"
	"path"
	"strings"

	"tool_forge/backend/tools/adbx"
)

// Entry 设备上的一个条目
type Entry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
	// ModTime Unix 秒。取证里"最后修改时间"经常就是结论本身
	ModTime int64  `json:"modTime"`
	Mode    string `json:"mode"`
	// Symlink 是软链时,指向哪儿。两个平台上到处都是软链
	// (iOS 的 /var、Android 的 /sdcard),不标出来的话
	// 人会以为自己在两个不同的地方看到了同一份数据
	Symlink string `json:"symlink,omitempty"`
	// Err 这一条读不出来时的原因(权限之类)。整个目录不该因为一条报错就全废
	Err string `json:"err,omitempty"`
}

// Listing 一次列目录的结果
type Listing struct {
	Path string `json:"path"`
	// Parent 上一级路径;已经在根上时为空
	Parent  string  `json:"parent"`
	Entries []Entry `json:"entries"`
	// Truncated 条目太多被截断了
	Truncated bool `json:"truncated"`
	// Total 截断前一共有多少条
	Total int `json:"total"`
}

// maxEntries 一个目录最多返回多少条。
// iOS 的 Containers/Data/Application 下面动辄几百个 UUID 目录,
// Android 的缓存目录里上万个文件也不稀奇 —— 全丢给前端表格会直接卡死
const maxEntries = 2000

func newListing(dir string, total int) *Listing {
	return &Listing{
		Path:    dir,
		Parent:  parentOf(dir),
		Entries: []Entry{},
		Total:   total,
	}
}

// List 列一个目录
func (m *Manager) List(sessionID, dir string) (*Listing, error) {
	s, err := m.get(sessionID)
	if err != nil {
		return nil, err
	}
	dir = cleanRemote(dir)
	if dir == "" {
		dir = s.t.startPath()
	}
	return s.t.list(dir)
}

// Stat 单个条目的信息
func (m *Manager) Stat(sessionID, p string) (*Entry, error) {
	s, err := m.get(sessionID)
	if err != nil {
		return nil, err
	}
	return s.t.stat(cleanRemote(p))
}

// Search 在设备上按文件名查找(不分大小写,支持 * 通配)
func (m *Manager) Search(sessionID, root, pattern string, limit int) (*SearchResult, error) {
	s, err := m.get(sessionID)
	if err != nil {
		return nil, err
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, errNeedPattern
	}
	root = cleanRemote(root)
	if root == "" {
		root = s.t.startPath()
	}
	if limit <= 0 || limit > searchLimit {
		limit = searchLimit
	}
	// 人敲的是"想找什么",不是 shell 通配符。两头自动补 * ——
	// 不补的话搜 "mmkv" 一条都出不来,而那正是最常见的用法
	if !strings.ContainsAny(pattern, "*?") {
		pattern = "*" + pattern + "*"
	}
	return s.t.search(root, pattern, limit)
}

// Pull 把设备上的文件拉到本地。
// limit > 0 时只拉前 limit 字节 —— 预览一个 500MB 的数据库时,
// 前几十 KB 就够判断它是什么了,没必要整个搬过来
func (m *Manager) Pull(sessionID, remote, localPath string, limit int64) (int64, bool, error) {
	s, err := m.get(sessionID)
	if err != nil {
		return 0, false, err
	}
	return s.t.pull(cleanRemote(remote), localPath, limit)
}

// createLocal 建本地落地文件,顺便把父目录补出来
func createLocal(localPath string) (*os.File, error) {
	if err := os.MkdirAll(path.Dir(toSlash(localPath)), 0o755); err != nil {
		return nil, err
	}
	return os.Create(localPath)
}

// cleanRemote 规范化设备上的路径。
//
// 只做斜杠折叠和去尾斜杠,不做 filepath.Clean —— 那玩意在 Windows 上会把
// 正斜杠换成反斜杠,而这里是发给手机的路径
func cleanRemote(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "\\", "/")
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func parentOf(p string) string {
	if p == "/" || p == "" {
		return ""
	}
	return path.Dir(p)
}

// toSlash 本地路径统一成正斜杠再取目录。
// Windows 上两种斜杠混着用时 path.Dir 会切错
func toSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

// shellQuote 把一个值包成单引号字符串,防止用户输入的路径把设备的 shell 接管过去。
// 实现在 adbx 里,两个平台共用一份 —— 转义规则各写一遍迟早会有一处写漏
func shellQuote(s string) string { return adbx.Quote(s) }

var errNeedPattern = fmt.Errorf("要找什么?给个文件名或片段")
