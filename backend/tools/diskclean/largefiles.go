package diskclean

import (
	"container/heap"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Volume 一块盘
type Volume struct {
	Path      string `json:"path"`
	Label     string `json:"label"`
	Total     int64  `json:"total"`
	Free      int64  `json:"free"`
	System    bool   `json:"system"`
	Removable bool   `json:"removable"`
}

// Places 扫描起点的候选:本机的盘,加上个人目录
type Places struct {
	Home    string   `json:"home"`
	Volumes []Volume `json:"volumes"`
}

// GetPlaces 列出可以扫的地方
func GetPlaces() *Places {
	home, _ := os.UserHomeDir()
	return &Places{Home: home, Volumes: volumes()}
}

// LargeOptions 大文件扫描的参数
type LargeOptions struct {
	JobID string   `json:"jobId"`
	Roots []string `json:"roots"`
	// MinSize 多大算大(字节),默认 100 MB
	MinSize int64 `json:"minSize"`
	// Limit 最多交回多少个(最大的那些),默认 500
	Limit int `json:"limit"`
}

// LargeFile 一个大文件
type LargeFile struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	Ext     string `json:"ext"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"` // unix 秒
	// Blocked 系统文件之类,不让删;Reason 说为什么、该去哪儿处理
	Blocked bool   `json:"blocked"`
	Reason  string `json:"reason,omitempty"`
	// Warn 能删,但删之前要知道的事
	Warn string `json:"warn,omitempty"`
}

// LargeResult 大文件扫描的结果
type LargeResult struct {
	Files []LargeFile `json:"files"`
	// UsageID 这一次扫描顺带建的目录树,「按目录看」拿它去取每一层
	UsageID string `json:"usageId"`
	// Matched 超过阈值的一共多少个。可能比 Files 多 —— 被 Limit 截掉的那些
	Matched      int64 `json:"matched"`
	MatchedBytes int64 `json:"matchedBytes"`
	Scanned      int64 `json:"scanned"`
	ScannedBytes int64 `json:"scannedBytes"`
	// SkippedLinks 跳过的链接、目录联接、网盘同步的文件
	SkippedLinks int64 `json:"skippedLinks"`
	// Denied 没权限进的目录。不为零时结果不完整,界面上要说
	Denied    DeniedInfo `json:"denied"`
	Cancelled bool       `json:"cancelled"`
	ElapsedMs int64      `json:"elapsedMs"`
}

// DeniedInfo 没权限进的目录。
//
// 同样是"进不去",该怎么跟人说取决于两件事:
//   - 当时是不是管理员。不是,就该劝人提权;已经是了还这么劝,就是瞎指挥
//   - 进不去的在哪儿。系统保护的位置里,东西本来也删不了,不影响能清理的结果;
//     别处的进不去,才是真漏掉了
type DeniedInfo struct {
	Count int64 `json:"count"`
	// Protected 其中在系统保护位置里的
	Protected int64 `json:"protected"`
	// Dirs 是哪些目录,最多列 50 个
	Dirs []string `json:"dirs"`
	// Elevated 扫描时是不是管理员身份
	Elevated bool `json:"elevated"`
}

func deniedInfo(w *walker) DeniedInfo {
	return DeniedInfo{
		Count:     w.denied.Load(),
		Protected: w.deniedProtected.Load(),
		Dirs:      w.deniedList(),
		Elevated:  isElevated(),
	}
}

// protectedDir 没权限进的目录在不在系统保护的位置里
func (s *Service) protectedDir(p string) bool { return s.guard.Check(p).Blocked }

// ScanLarge 找出最大的那些文件
func (s *Service) ScanLarge(opt LargeOptions) (*LargeResult, error) {
	roots, err := cleanRoots(opt.Roots)
	if err != nil {
		return nil, err
	}
	if opt.MinSize <= 0 {
		opt.MinSize = 100 << 20
	}
	if opt.Limit <= 0 || opt.Limit > 2000 {
		opt.Limit = 500
	}
	ctx, end := s.begin(opt.JobID)
	defer end()
	rep := s.report(opt.JobID)
	defer rep.close()
	defer withBackupPrivilege()()
	rep.setPhase("扫描文件", 0)
	start := time.Now()

	var mu sync.Mutex
	top := &bySize{}
	var matched, matchedBytes int64
	tree := &usageBuilder{}
	w := newWalker(ctx, rep)
	w.isProtected = s.protectedDir
	// 每个目录这一层多大顺手记下来,扫完拼成目录树 —— 「按目录看」不用再扫一遍
	w.onDir = tree.add
	w.onFile = func(p string, fi fs.FileInfo) {
		size := fi.Size()
		if size < opt.MinSize {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		matched++
		matchedBytes += size
		f := sized{path: p, size: size, mod: fi.ModTime().Unix()}
		// 小顶堆只留最大的 Limit 个:全盘几千个大文件全攒着再排序,内存和时间都白花
		if top.Len() < opt.Limit {
			heap.Push(top, f)
		} else if size > (*top)[0].size {
			(*top)[0] = f
			heap.Fix(top, 0)
		}
	}
	w.run(roots)

	usageID := opt.JobID
	if usageID == "" {
		usageID = time.Now().Format("20060102150405.000000000")
	}
	t := tree.build(usageID, roots)
	s.usageMu.Lock()
	s.usage = t
	s.usageMu.Unlock()

	found := []sized(*top)
	sort.Slice(found, func(i, j int) bool { return found[i].size > found[j].size })
	res := &LargeResult{
		UsageID:      usageID,
		Files:        make([]LargeFile, 0, len(found)),
		Matched:      matched,
		MatchedBytes: matchedBytes,
		Scanned:      rep.files.Load(),
		ScannedBytes: rep.bytes.Load(),
		SkippedLinks: w.links.Load(),
		Denied:       deniedInfo(w),
		Cancelled:    ctx.Err() != nil,
		ElapsedMs:    time.Since(start).Milliseconds(),
	}
	for _, f := range found {
		v := s.guard.Check(f.path)
		res.Files = append(res.Files, LargeFile{
			Path:    f.path,
			Name:    filepath.Base(f.path),
			Dir:     filepath.Dir(f.path),
			Ext:     strings.ToLower(strings.TrimPrefix(filepath.Ext(f.path), ".")),
			Size:    f.size,
			ModTime: f.mod,
			Blocked: v.Blocked,
			Reason:  v.Reason,
			Warn:    v.Warn,
		})
	}
	return res, nil
}

type sized struct {
	path string
	size int64
	mod  int64
}

// bySize 按大小的小顶堆
type bySize []sized

func (h bySize) Len() int           { return len(h) }
func (h bySize) Less(i, j int) bool { return h[i].size < h[j].size }
func (h bySize) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *bySize) Push(x any)        { *h = append(*h, x.(sized)) }
func (h *bySize) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}
