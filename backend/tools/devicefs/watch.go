package devicefs

import (
	"fmt"
	"path"
	"sort"
	"sync"
	"time"
)

// 设备上的文件变化只能靠轮询对比,没有别的办法。
//
// 实测越狱 iOS 和安卓上都没有 inotifywait / fswatch,iOS 的 FSEvents 也不是
// 能通过 SSH 用的东西。所以这里做的是"拍快照、再拍一张、比差异",
// 而不是任何形式的实时推送 —— 叫它监控会让人误以为变化会自己冒出来。
//
// 对取证来说这反而是最常用的手法:在手机上做一个动作之前拍一次,做完再拍一次,
// 差异就是那个动作落到了哪些文件上。想知道某个功能的数据存在哪儿,这是最直接的路。

// stamp 一个条目在某一刻的样子。
// 只看大小和修改时间:内容哈希要把文件整个拉下来,一个目录几百个文件根本跑不动
type stamp struct {
	size    int64
	modTime int64
	isDir   bool
}

// Change 一处变化
type Change struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// Kind added | removed | modified
	Kind  string `json:"kind"`
	IsDir bool   `json:"isDir"`
	// Size 现在多大;removed 时是消失前的大小
	Size int64 `json:"size"`
	// ModTime 现在的修改时间(秒)
	ModTime int64 `json:"modTime"`
	// SizeDelta modified 时大小变了多少,可正可负
	SizeDelta int64 `json:"sizeDelta,omitempty"`
}

// DiffResult 两次快照之间的差异
type DiffResult struct {
	Dir string `json:"dir"`
	// Baseline 为真表示这次只是记了个基线,还没有可比的东西
	Baseline bool `json:"baseline"`
	// Since 上一张快照是什么时候拍的(秒)
	Since   int64    `json:"since,omitempty"`
	Changes []Change `json:"changes"`
	// Total 这个目录现在有多少个条目
	Total int `json:"total"`
	// Truncated 目录条目太多被截断了 —— 截断之后的对比是不完整的,必须说
	Truncated bool `json:"truncated"`
}

// snapshots 每个会话+目录记一张快照。
//
// 存在服务端而不是让调用方带着走:快照是几百个条目的表,
// 塞进 MCP 的一问一答里既占上下文又容易被改坏
type snapshots struct {
	mu sync.Mutex
	m  map[string]snapEntry
}

type snapEntry struct {
	at    int64
	files map[string]stamp
}

func newSnapshots() *snapshots {
	return &snapshots{m: map[string]snapEntry{}}
}

func (s *snapshots) key(sessionID, dir string) string { return sessionID + "\x00" + dir }

// Diff 拍一张新快照,和上一张比。只看这一层。
//
// 每次调用都会把基线换成这一张 —— 所以连着调两次拿到的是"这两次之间"的变化,
// 而不是"从第一次到现在"。想要后者就别中间多调
func (m *Manager) Diff(sessionID, dir string) (*DiffResult, error) {
	lst, err := m.List(sessionID, dir)
	if err != nil {
		return nil, err
	}
	now := map[string]stamp{}
	for _, e := range lst.Entries {
		now[joinRemote(lst.Path, e.Name)] = stamp{size: e.Size, modTime: e.ModTime, isDir: e.IsDir}
	}
	return m.snaps.compare(m.snaps.key(sessionID, lst.Path), lst.Path, now, lst.Total, lst.Truncated), nil
}

// treeLimit 递归快照最多记多少条。一个 App 的数据目录几千个文件是常态,
// 缓存目录上万也不稀奇;超过就截断并标出来 —— 截断之后的对比是不完整的,必须让人知道
const treeLimit = 20000

// DiffTree 和 Diff 一样拍快照比差异,但看的是整棵子树。
//
// 只看一层的话,深处文件的改动看不见:目录的修改时间只在直接子项增删时变,
// databases/msg.db 被写了一笔,上层目录纹丝不动。想知道"这个动作落到了哪些文件上",
// 非递归不可。递归交给设备上的 find —— 和 search 走的是同一条路,模式给 * 就是全部。
// 基线和 Diff 的分开记,两种看法互不干扰
func (m *Manager) DiffTree(sessionID, dir string) (*DiffResult, error) {
	s, err := m.get(sessionID)
	if err != nil {
		return nil, err
	}
	dir = cleanRemote(dir)
	if dir == "" {
		dir = s.t.startPath()
	}
	res, err := s.t.search(dir, "*", treeLimit)
	if err != nil {
		return nil, err
	}
	now := map[string]stamp{}
	for _, h := range res.Hits {
		// find 会把起点自己也列出来;它的修改时间跟着直接子项变,不算一处变化
		if h.Path == dir {
			continue
		}
		now[h.Path] = stamp{size: h.Size, modTime: h.ModTime, isDir: h.IsDir}
	}
	return m.snaps.compare(m.snaps.treeKey(sessionID, dir), dir, now, len(now), res.Truncated), nil
}

func (s *snapshots) treeKey(sessionID, dir string) string {
	return sessionID + "\x00tree\x00" + dir
}

// compare 用这张快照换掉基线,算出和基线的差异。now 的键是完整路径
func (s *snapshots) compare(key, dir string, now map[string]stamp, total int, truncated bool) *DiffResult {
	res := &DiffResult{
		Dir:       dir,
		Changes:   []Change{},
		Total:     total,
		Truncated: truncated,
	}

	s.mu.Lock()
	prev, had := s.m[key]
	s.m[key] = snapEntry{at: time.Now().Unix(), files: now}
	s.mu.Unlock()

	if !had {
		res.Baseline = true
		return res
	}
	res.Since = prev.at

	for p, cur := range now {
		old, existed := prev.files[p]
		switch {
		case !existed:
			res.Changes = append(res.Changes, Change{
				Name: path.Base(p), Path: p, Kind: "added",
				IsDir: cur.isDir, Size: cur.size, ModTime: cur.modTime,
			})
		case old.size != cur.size || old.modTime != cur.modTime:
			res.Changes = append(res.Changes, Change{
				Name: path.Base(p), Path: p, Kind: "modified",
				IsDir: cur.isDir, Size: cur.size, ModTime: cur.modTime,
				SizeDelta: cur.size - old.size,
			})
		}
	}
	for p, old := range prev.files {
		if _, still := now[p]; !still {
			res.Changes = append(res.Changes, Change{
				Name: path.Base(p), Path: p, Kind: "removed",
				IsDir: old.isDir, Size: old.size, ModTime: old.modTime,
			})
		}
	}

	// map 的遍历顺序是随机的,不排的话同样的变化每次给出的顺序都不一样,
	// 看的人会以为结果不稳
	sort.Slice(res.Changes, func(i, j int) bool {
		if res.Changes[i].Kind != res.Changes[j].Kind {
			return res.Changes[i].Kind < res.Changes[j].Kind
		}
		return res.Changes[i].Path < res.Changes[j].Path
	})
	return res
}

// ResetSnapshot 丢掉某个目录的基线,下次 Diff 重新开始
func (m *Manager) ResetSnapshot(sessionID, dir string) {
	dir = cleanRemote(dir)
	m.snaps.mu.Lock()
	defer m.snaps.mu.Unlock()
	delete(m.snaps.m, m.snaps.key(sessionID, dir))
	delete(m.snaps.m, m.snaps.treeKey(sessionID, dir))
}

// EnsureSession 找一个现成的会话用;没有就连一个。
//
// 界面上连的和这里连的是同一批 —— 共用一个 Manager 是有意的:
// 用户在界面上开着的那条,agent 直接接着用,不用再连一次,
// 也不会两条会话同时抢一台设备
func (m *Manager) EnsureSession(opt ConnectOptions) (*Session, error) {
	platform := opt.Platform
	if platform == "" {
		platform = "ios"
	}
	m.mu.Lock()
	for _, s := range m.sessions {
		if s.Platform == platform {
			m.mu.Unlock()
			return s, nil
		}
	}
	m.mu.Unlock()

	opt.Platform = platform
	return m.Connect(opt)
}

// Sessions 现在连着哪些设备
func (m *Manager) Sessions() []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		// 拷一份出去,别把内部那个连着 transport 的结构体交出去
		out = append(out, Session{
			ID: s.ID, Platform: s.Platform, DeviceID: s.DeviceID,
			Addr: s.Addr, StartPath: s.StartPath, Rooted: s.Rooted, Model: s.Model,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// joinRemote 设备上的路径一律用正斜杠
func joinRemote(dir, name string) string {
	if dir == "/" {
		return "/" + name
	}
	return fmt.Sprintf("%s/%s", dir, name)
}
