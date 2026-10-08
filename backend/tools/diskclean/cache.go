package diskclean

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// CacheRule 一条缓存规则。
//
// 规则是写死在程序里的,不从文件或网络加载:规则的内容就是"删掉这些路径",
// 能改规则的人就能删用户的任何东西。新增一条要过 rules_test 里那几道检查
type CacheRule struct {
	ID    string
	Group string
	Name  string
	// Desc 清掉会怎样、什么时候别清。前端原样显示
	Desc string
	// Paths 要清的目录,%VAR% 形式的环境变量;某一段可以带 * (浏览器的多个用户配置)。
	// 清的是目录里面的东西,目录本身留着 —— 有些程序发现缓存目录没了会直接报错
	Paths []string
	// Match 非空时只删目录第一层里名字匹配的文件,不往下走
	Match []string
	// Files 具体某几个文件
	Files []string
	// MinAge 只删修改时间早于这么久之前的。临时目录要用:正在跑的安装程序还在往里写
	MinAge time.Duration
	// Admin 要管理员权限才清得动
	Admin bool
	// Procs 这些进程开着时,缓存文件多半被占着
	Procs   []string
	Default bool
	// RecycleBin 不按路径删,走系统接口清空回收站
	RecycleBin bool
}

// CacheItem 一条规则在这台机器上的情况
type CacheItem struct {
	ID    string `json:"id"`
	Group string `json:"group"`
	Name  string `json:"name"`
	Desc  string `json:"desc"`
	// Paths 实际找到的目录(展开环境变量和 * 之后)
	Paths []string `json:"paths"`
	Size  int64    `json:"size"`
	Files int64    `json:"files"`
	// Found 这台机器上有没有。没装 Chrome 就没有 Chrome 那条
	Found bool `json:"found"`
	Admin bool `json:"admin"`
	// NeedAdmin 要管理员权限而现在不是:能看到大概多大,但清不了
	NeedAdmin bool `json:"needAdmin"`
	// Running 正开着的相关程序。它们占着的文件会跳过
	Running []string `json:"running"`
	Default bool     `json:"default"`
	// RecycleBin 清的是回收站:那是用户自己留的后悔药,确认时要单独提一句
	RecycleBin bool `json:"recycleBin"`
	// Note 补充说明,比如某个目录是链接、被跳过了
	Note string `json:"note,omitempty"`
}

// CacheScanResult 缓存扫描的结果
type CacheScanResult struct {
	Items []CacheItem `json:"items"`
	// Supported 这个系统上有没有规则。目前只有 Windows 的
	Supported bool  `json:"supported"`
	Elevated  bool  `json:"elevated"`
	Cancelled bool  `json:"cancelled"`
	ElapsedMs int64 `json:"elapsedMs"`
}

// CacheCleanItem 一条规则清下来的结果
type CacheCleanItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Freed   int64  `json:"freed"`
	Deleted int64  `json:"deleted"`
	// InUse 被占用删不掉的。程序开着时很正常,关掉再清就能清掉
	InUse int64 `json:"inUse"`
	// Denied 没权限删的
	Denied int64 `json:"denied"`
	// Recent 太新、按规则留着的(临时目录里一天以内的)
	Recent int64  `json:"recent"`
	Error  string `json:"error,omitempty"`
}

// CacheCleanResult 一次缓存清理的结果
type CacheCleanResult struct {
	Items     []CacheCleanItem `json:"items"`
	Freed     int64            `json:"freed"`
	Deleted   int64            `json:"deleted"`
	Cancelled bool             `json:"cancelled"`
	ElapsedMs int64            `json:"elapsedMs"`
}

// ScanCache 看看每条规则在这台机器上有多少可清
func (s *Service) ScanCache(jobID string) (*CacheScanResult, error) {
	ctx, end := s.begin(jobID)
	defer end()
	rep := s.report(jobID)
	defer rep.close()
	start := time.Now()

	elevated := isElevated()
	procs := runningProcs()
	res := &CacheScanResult{Items: []CacheItem{}, Supported: len(s.rules) > 0, Elevated: elevated}
	rep.setPhase("统计缓存", int64(len(s.rules)))
	for _, r := range s.rules {
		if ctx.Err() != nil {
			break
		}
		rep.setCurrent(r.Name)
		item := CacheItem{
			ID: r.ID, Group: r.Group, Name: r.Name, Desc: r.Desc,
			Paths: []string{}, Running: []string{},
			Admin: r.Admin, NeedAdmin: r.Admin && !elevated, Default: r.Default,
			RecycleBin: r.RecycleBin,
		}
		for _, p := range r.Procs {
			if procs[strings.ToLower(p)] {
				item.Running = append(item.Running, p)
			}
		}
		if r.RecycleBin {
			size, n, err := recycleBinInfo()
			item.Found = err == nil
			item.Size, item.Files = size, n
		} else {
			t := s.resolve(r)
			item.Paths, item.Note = t.shown(), strings.Join(t.notes, ";")
			item.Found = len(t.dirs)+len(t.files) > 0
			item.Size, item.Files = s.measure(ctx, r, t, rep)
		}
		res.Items = append(res.Items, item)
		rep.done.Add(1)
	}
	res.Cancelled = ctx.Err() != nil
	res.ElapsedMs = time.Since(start).Milliseconds()
	return res, nil
}

// CleanCache 按选中的规则清缓存
func (s *Service) CleanCache(jobID string, ids []string) (*CacheCleanResult, error) {
	ctx, end := s.begin(jobID)
	defer end()
	rep := s.report(jobID)
	defer rep.close()
	start := time.Now()

	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	elevated := isElevated()
	res := &CacheCleanResult{Items: []CacheCleanItem{}}
	rep.setPhase("清理", int64(len(want)))
	for _, r := range s.rules {
		if !want[r.ID] {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		rep.setCurrent(r.Name)
		item := CacheCleanItem{ID: r.ID, Name: r.Name}
		switch {
		case r.Admin && !elevated:
			item.Error = "要以管理员身份运行工具箱才清得动"
		case r.RecycleBin:
			if size, n, err := recycleBinInfo(); err != nil {
				item.Error = err.Error()
			} else if n > 0 {
				if err := emptyRecycleBin(); err != nil {
					item.Error = err.Error()
				} else {
					item.Freed, item.Deleted = size, n
				}
			}
		default:
			t := s.resolve(r)
			for _, dir := range t.dirs {
				s.cleanDir(ctx, dir, r, &item, rep)
			}
			for _, f := range t.files {
				s.cleanFile(f, r, &item, rep)
			}
		}
		res.Freed += item.Freed
		res.Deleted += item.Deleted
		res.Items = append(res.Items, item)
	}
	res.Cancelled = ctx.Err() != nil
	res.ElapsedMs = time.Since(start).Milliseconds()
	return res, nil
}

// targets 一条规则在这台机器上展开出来的东西
type targets struct {
	dirs  []string
	files []string
	notes []string
}

func (t targets) shown() []string {
	out := append([]string{}, t.dirs...)
	return append(out, t.files...)
}

// resolve 展开规则里的环境变量和 *,只留真实存在、过得了守卫的
func (s *Service) resolve(r CacheRule) targets {
	var t targets
	for _, raw := range r.Paths {
		p, ok := expandEnv(raw, os.Getenv)
		if !ok || !filepath.IsAbs(p) {
			continue
		}
		for _, dir := range expandGlob(p) {
			if v := s.guard.CheckContentsFinal(dir); v.Blocked {
				t.notes = append(t.notes, dir+" 跳过了:"+v.Reason)
				continue
			}
			t.dirs = append(t.dirs, dir)
		}
	}
	for _, raw := range r.Files {
		p, ok := expandEnv(raw, os.Getenv)
		if !ok || !filepath.IsAbs(p) {
			continue
		}
		fi, err := os.Lstat(p)
		if err != nil || kindOf(fi) != kindFile {
			continue
		}
		if v := s.guard.CheckFinal(p); v.Blocked {
			t.notes = append(t.notes, p+" 跳过了:"+v.Reason)
			continue
		}
		t.files = append(t.files, p)
	}
	return t
}

// measure 量一条规则能清多少。和 cleanDir 用同一套筛选,量出来的就是会删的
func (s *Service) measure(ctx context.Context, r CacheRule, t targets, rep *reporter) (size, files int64) {
	var mu sync.Mutex
	cutoff := time.Now().Add(-r.MinAge)
	add := func(fi fs.FileInfo) {
		if r.MinAge > 0 && fi.ModTime().After(cutoff) {
			return
		}
		mu.Lock()
		size += fi.Size()
		files++
		mu.Unlock()
	}
	for _, f := range t.files {
		if fi, err := os.Lstat(f); err == nil {
			add(fi)
		}
	}
	for _, dir := range t.dirs {
		if len(r.Match) > 0 {
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if fi, err := e.Info(); err == nil && kindOf(fi) == kindFile && matchAny(r.Match, e.Name()) {
					add(fi)
				}
			}
			continue
		}
		w := newWalker(ctx, rep)
		w.onFile = func(_ string, fi fs.FileInfo) { add(fi) }
		w.run([]string{dir})
	}
	return size, files
}

// cleanDir 清掉一个缓存目录里的东西,目录本身留着。
//
// 先删文件,再从深到浅收掉删空了的子目录。不用 os.RemoveAll:它碰到一个被占用的文件
// 就整个停下,而缓存目录里总有几个文件正开着 —— 要的是"能删的都删掉",
// 不是"有一个删不掉就一个都不删"。单线程走:这是写操作,出了问题要好排查
func (s *Service) cleanDir(ctx context.Context, root string, r CacheRule, item *CacheCleanItem, rep *reporter) {
	cutoff := time.Now().Add(-r.MinAge)
	normRoot := norm(root)
	// 子目录的修改时间要在动手之前记下来:删掉里面的文件会把它刷成"现在",
	// 按删完之后的时间判断的话,删空了的旧目录永远显得"太新"、永远收不掉
	type seenDir struct {
		path string
		mod  time.Time
	}
	var dirs []seenDir
	var walk func(dir string)
	walk = func(dir string) {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if ctx.Err() != nil {
				return
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			p := filepath.Join(dir, e.Name())
			switch kindOf(fi) {
			case kindDir:
				if len(r.Match) == 0 {
					dirs = append(dirs, seenDir{p, fi.ModTime()})
					walk(p)
				}
			case kindFile:
				if len(r.Match) > 0 && !matchAny(r.Match, e.Name()) {
					continue
				}
				if r.MinAge > 0 && fi.ModTime().After(cutoff) {
					item.Recent++
					continue
				}
				// 遍历不跟链接,走到这里的一定在 root 里面。再对一遍是兜底:
				// 哪天有人改了遍历方式,这两行能挡住
				if !under(norm(p), normRoot) || s.guard.Check(p).Blocked {
					item.Denied++
					continue
				}
				rep.setCurrent(p)
				s.tally(os.Remove(p), fi.Size(), item)
			}
			// 链接、联接、网盘文件:不删。删一个联接不会碰到它指向的东西,
			// 但缓存目录里出现这些本来就不寻常,不碰最稳
		}
	}
	walk(root)

	// 子目录从深到浅收:子路径一定比父路径长,按长度倒排就是先子后父。
	// os.Remove 只删得掉空目录,没删空的自然留着
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i].path) > len(dirs[j].path) })
	for _, d := range dirs {
		if ctx.Err() != nil {
			return
		}
		// 新建的空目录也留着:正在跑的程序可能刚建好、马上要往里写
		if r.MinAge > 0 && d.mod.After(cutoff) {
			continue
		}
		_ = os.Remove(d.path)
	}
}

func (s *Service) cleanFile(p string, r CacheRule, item *CacheCleanItem, rep *reporter) {
	fi, err := os.Lstat(p)
	if err != nil || kindOf(fi) != kindFile {
		return
	}
	if r.MinAge > 0 && fi.ModTime().After(time.Now().Add(-r.MinAge)) {
		item.Recent++
		return
	}
	if s.guard.Check(p).Blocked {
		item.Denied++
		return
	}
	rep.setCurrent(p)
	s.tally(os.Remove(p), fi.Size(), item)
}

// tally 记一次删除的结果
func (s *Service) tally(err error, size int64, item *CacheCleanItem) {
	switch {
	case err == nil:
		item.Freed += size
		item.Deleted++
	case errors.Is(err, fs.ErrNotExist):
		// 程序自己先删掉了,不算失败
	case isInUse(err):
		item.InUse++
	default:
		item.Denied++
	}
}

func matchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, _ := filepath.Match(strings.ToLower(p), strings.ToLower(name)); ok {
			return true
		}
	}
	return false
}

var envRef = regexp.MustCompile(`%([A-Za-z0-9_()]+)%`)

// expandEnv 展开 %VAR%。有一个变量取不到值就整条作废:
// 空串拼出来的会是 \Temp 或者 C:\ 这种完全不是本意的地方
func expandEnv(s string, getenv func(string) string) (string, bool) {
	ok := true
	out := envRef.ReplaceAllStringFunc(s, func(m string) string {
		v := getenv(m[1 : len(m)-1])
		if v == "" {
			ok = false
		}
		return v
	})
	return out, ok
}

// expandGlob 展开路径里带 * 的段,只留真实存在的目录(不是链接)。
//
// 不用 filepath.Glob:路径前半截来自环境变量,用户名里要是有 [ 这种字符,
// Glob 会把它当成通配符。这里只在规则自己写的那几段里认通配符
func expandGlob(p string) []string {
	vol := filepath.VolumeName(p)
	sep := string(filepath.Separator)
	parts := strings.Split(strings.Trim(p[len(vol):], sep), sep)
	cur := []string{vol + sep}
	for _, part := range parts {
		if part == "" {
			continue
		}
		var next []string
		for _, c := range cur {
			if !strings.ContainsAny(part, "*?") {
				next = append(next, filepath.Join(c, part))
				continue
			}
			entries, err := os.ReadDir(c)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if ok, _ := filepath.Match(part, e.Name()); !ok {
					continue
				}
				if fi, err := e.Info(); err == nil && kindOf(fi) == kindDir {
					next = append(next, filepath.Join(c, e.Name()))
				}
			}
		}
		cur = next
	}
	out := []string{}
	for _, c := range cur {
		if fi, err := os.Lstat(c); err == nil && kindOf(fi) == kindDir {
			out = append(out, c)
		}
	}
	return out
}
