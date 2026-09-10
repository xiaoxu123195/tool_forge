package devicefs

import (
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"
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
	// Symlink 是软链时,指向哪儿。iOS 上 /var 就是 /private/var 的软链,
	// 不标出来的话人会以为自己在两个不同的地方看到了同一份数据
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
// iOS 上 /var/mobile/Containers/Data/Application 下面动辄几百个 UUID 目录,
// 而某些缓存目录有上万个文件 —— 全丢给前端表格会直接卡死
const maxEntries = 2000

// List 列一个目录。
//
// 单条读不出来(权限、断链)不算失败:把错误挂在那一条上,其余照常返回。
// 取证时最想看的目录往往恰好是有几条读不了的那个,整个报错等于什么都看不到。
func (m *Manager) List(sessionID, dir string) (*Listing, error) {
	s, err := m.get(sessionID)
	if err != nil {
		return nil, err
	}
	dir = cleanRemote(dir)
	if dir == "" {
		dir = s.StartPath
	}

	infos, err := s.sftp.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("列不了 %s:%w", dir, err)
	}

	out := &Listing{
		Path:    dir,
		Parent:  parentOf(dir),
		Entries: []Entry{},
		Total:   len(infos),
	}
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
			if target, err := s.sftp.ReadLink(full); err == nil {
				e.Symlink = target
				// 软链指向目录时也要能点进去。ReadDir 给的是链本身的信息,
				// IsDir 永远是假 —— 不跟一步的话 iOS 上一堆入口都点不开
				if st, err := s.sftp.Stat(full); err == nil {
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

// Stat 单个条目的信息
func (m *Manager) Stat(sessionID, p string) (*Entry, error) {
	s, err := m.get(sessionID)
	if err != nil {
		return nil, err
	}
	p = cleanRemote(p)
	fi, err := s.sftp.Stat(p)
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

// pullTimeout 单个文件拉取的上限。
// 设备上有几 GB 的媒体库,USB 上拉一个就是好几分钟 —— 预览不该等这么久
const pullTimeout = 3 * time.Minute

// Pull 把设备上的文件拉到本地。
//
// limit > 0 时只拉前 limit 字节 —— 预览一个 500MB 的数据库时,
// 前几十 KB 就够判断它是什么了,没必要整个搬过来。
// 返回实际写了多少字节,以及是不是截断了。
func (m *Manager) Pull(sessionID, remote, localPath string, limit int64) (int64, bool, error) {
	s, err := m.get(sessionID)
	if err != nil {
		return 0, false, err
	}
	remote = cleanRemote(remote)

	src, err := s.sftp.Open(remote)
	if err != nil {
		return 0, false, fmt.Errorf("打不开 %s:%w", remote, err)
	}
	defer src.Close()

	if err := os.MkdirAll(path.Dir(filepathToSlash(localPath)), 0o755); err != nil {
		return 0, false, err
	}
	dst, err := os.Create(localPath)
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
	case <-time.After(pullTimeout):
		// 超时就把连接关掉打断拷贝 —— 不然这个 goroutine 会一直挂着
		_ = src.Close()
		return 0, false, fmt.Errorf("拉取 %s 超过 %s 还没完成", remote, pullTimeout)
	}
	if copyErr != nil {
		return n, false, copyErr
	}
	return n, limit > 0 && n == limit, nil
}

// cleanRemote 规范化设备上的路径。
//
// 只做斜杠折叠和去尾斜杠,不做 filepath.Clean —— 那玩意在 Windows 上会把
// 正斜杠换成反斜杠,而这里是发给 iOS 的路径
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

// filepathToSlash 本地路径统一成正斜杠再取目录。
// Windows 上两种斜杠混着用时 path.Dir 会切错
func filepathToSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}
