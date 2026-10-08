package diskclean

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// 空文件夹:连同下面所有子目录,一个文件都没有的目录。
//
// 删它腾不出空间,图的是整洁,所以范围收得很保守:
//   - 有任何东西就不算空 —— 链接、网盘占位文件、读不全的目录、没进去的子目录都算"有东西"
//   - 个人目录下直接那一层(文档、图片、联系人……)不报:那是系统和程序认定该在的位置
//   - 程序的数据目录不进:AppData、以 . 开头的目录、微信 QQ 放在文档里的数据目录。
//     真机上试过,不跳过的话个人目录里能扫出上千个:六成多是微信数据目录里的结构,
//     剩下的大多在 .cache 这类工具目录里
//   - 一天以内建的或动过的不报:刚建的空文件夹多半是马上要往里放东西
//
// 删的时候不进回收站:里面什么都没有,进回收站没有意义。而且用的是只能删空目录的
// 系统调用 —— 里面要是多了东西,它自己就会失败,不可能连带删掉文件

// EmptyOptions 空文件夹扫描的参数
type EmptyOptions struct {
	JobID string   `json:"jobId"`
	Roots []string `json:"roots"`
	// SkipDevDirs 跳过 node_modules、.git 这类目录:那里面的空目录是工具链要的
	SkipDevDirs bool `json:"skipDevDirs"`
}

// EmptyDir 一个空文件夹(只报最外层,里面套着的空子目录一起删)
type EmptyDir struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Parent  string `json:"parent"`
	ModTime int64  `json:"modTime"`
	// Nested 里面还套着几个空子目录
	Nested int64 `json:"nested"`
}

// EmptyResult 空文件夹扫描的结果
type EmptyResult struct {
	Dirs []EmptyDir `json:"dirs"`
	// Recent 一天以内建的或动过的空目录,没列出来
	Recent int64 `json:"recent"`
	// Scanned 看了多少个目录
	Scanned   int64      `json:"scanned"`
	Denied    DeniedInfo `json:"denied"`
	Truncated bool       `json:"truncated"`
	Cancelled bool       `json:"cancelled"`
	ElapsedMs int64      `json:"elapsedMs"`
}

const (
	// emptyMinAge 比这新的空目录不报
	emptyMinAge = 24 * time.Hour
	// maxEmptyListed 最多列多少个
	maxEmptyListed = 5000
)

// ScanEmptyDirs 找空文件夹
func (s *Service) ScanEmptyDirs(opt EmptyOptions) (*EmptyResult, error) {
	roots, err := cleanRoots(opt.Roots)
	if err != nil {
		return nil, err
	}
	ctx, end := s.begin(opt.JobID)
	defer end()
	rep := s.report(opt.JobID)
	defer rep.close()
	defer withBackupPrivilege()()
	rep.setPhase("扫描目录", 0)
	start := time.Now()

	var mu sync.Mutex
	stats := map[string]dirStat{}
	w := newWalker(ctx, rep)
	w.isProtected = s.protectedDir
	w.skipDir = func(p, name string) bool {
		lower := strings.ToLower(name)
		if opt.SkipDevDirs && devDirs[lower] {
			return true
		}
		// 程序和工具的数据:以 . 开头的目录(.cache、.vscode 之类)、微信 QQ 这类放在文档里的
		// 数据目录、网盘的同步目录(在里面删会同步到云端)、Go 的模块缓存。
		// 里面的空目录是程序自己的结构,不是人要整理的东西
		if _, prog := programDirOf(name); prog || strings.HasPrefix(name, ".") ||
			(lower == "pkg" && strings.EqualFold(filepath.Base(filepath.Dir(p)), "go")) {
			return true
		}
		return s.guard.IsAppData(p) || s.guard.Check(p).Blocked
	}
	w.onDir = func(dir string, st dirStat) {
		mu.Lock()
		stats[dir] = st
		mu.Unlock()
	}
	w.run(roots)

	res := &EmptyResult{
		Dirs:      []EmptyDir{},
		Scanned:   int64(len(stats)),
		Denied:    deniedInfo(w),
		Cancelled: ctx.Err() != nil,
	}
	// 取消了的扫描不报:没记录的目录会被当成"有东西",结果不会错,
	// 但会漏很多 —— 与其给一份残缺的,不如让人重扫
	if res.Cancelled {
		res.ElapsedMs = time.Since(start).Milliseconds()
		return res, nil
	}

	type agg struct {
		st        dirStat
		emptyKids int64
		nested    int64
		newest    time.Time
		empty     bool
	}
	all := make(map[string]*agg, len(stats))
	order := make([]string, 0, len(stats))
	for p, st := range stats {
		all[p] = &agg{st: st, newest: st.mod}
		order = append(order, p)
	}
	// 从深到浅:子目录的路径一定比父目录长,先把子目录判完再判父目录
	sort.Slice(order, func(i, j int) bool { return len(order[i]) > len(order[j]) })
	for _, p := range order {
		a := all[p]
		st := a.st
		a.empty = st.files == 0 && st.others == 0 && st.skipped == 0 && !st.incomplete &&
			a.emptyKids == st.subdirs
		if !a.empty {
			continue
		}
		if parent := all[filepath.Dir(p)]; parent != nil && filepath.Dir(p) != p {
			parent.emptyKids++
			parent.nested += 1 + a.nested
			if a.newest.After(parent.newest) {
				parent.newest = a.newest
			}
		}
	}

	isRoot := map[string]bool{}
	for _, r := range roots {
		isRoot[r] = true
	}
	cutoff := time.Now().Add(-emptyMinAge)
	for _, p := range order {
		a := all[p]
		if !a.empty || isRoot[p] {
			continue
		}
		// 只报最外层:父目录也是空的(而且父目录不是扫描起点)就由父目录代表
		if parent := all[filepath.Dir(p)]; parent != nil && parent.empty && !isRoot[filepath.Dir(p)] {
			continue
		}
		if s.guard.Structural(p) || s.guard.Check(p).Blocked {
			continue
		}
		if a.newest.After(cutoff) {
			res.Recent++
			continue
		}
		res.Dirs = append(res.Dirs, EmptyDir{
			Path: p, Name: filepath.Base(p), Parent: filepath.Dir(p),
			ModTime: a.newest.Unix(), Nested: a.nested,
		})
	}
	sort.Slice(res.Dirs, func(i, j int) bool { return res.Dirs[i].Path < res.Dirs[j].Path })
	if len(res.Dirs) > maxEmptyListed {
		res.Dirs = res.Dirs[:maxEmptyListed]
		res.Truncated = true
	}
	res.ElapsedMs = time.Since(start).Milliseconds()
	return res, nil
}

// EmptyDeleteRequest 删一批空文件夹
type EmptyDeleteRequest struct {
	JobID string   `json:"jobId"`
	Paths []string `json:"paths"`
}

// DeleteEmptyDirs 删空文件夹。每个都重新核一遍还是不是空的
func (s *Service) DeleteEmptyDirs(req EmptyDeleteRequest) (*DeleteResult, error) {
	ctx, end := s.begin(req.JobID)
	defer end()
	rep := s.report(req.JobID)
	defer rep.close()
	rep.setPhase("删除", int64(len(req.Paths)))

	res := &DeleteResult{Items: []DeleteItem{}}
	for _, p := range req.Paths {
		if ctx.Err() != nil {
			res.Cancelled = true
			break
		}
		rep.setCurrent(p)
		item := DeleteItem{Path: p}
		if err := s.removeEmptyTree(filepath.Clean(p)); err != nil {
			item.Reason = err.Error()
			res.Failed++
		} else {
			item.OK = true
			res.Deleted++
		}
		res.Items = append(res.Items, item)
		rep.done.Add(1)
	}
	return res, nil
}

// removeEmptyTree 确认 dir 连同子目录都是空的,再从深到浅一个个删
func (s *Service) removeEmptyTree(dir string) error {
	if !filepath.IsAbs(dir) {
		return errors.New("不是完整路径")
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return describe(err)
	}
	if kindOf(fi) != kindDir {
		return errors.New("不是普通目录")
	}
	if s.guard.Structural(dir) {
		return errors.New("这是个人目录下的系统文件夹,空着也不删")
	}
	if v := s.guard.CheckFinal(dir); v.Blocked {
		return errors.New(v.Reason)
	}
	var dirs []string
	var walk func(d string) error
	walk = func(d string) error {
		entries, err := os.ReadDir(d)
		if err != nil {
			return describe(err)
		}
		for _, e := range entries {
			fi, err := e.Info()
			if err != nil {
				return describe(err)
			}
			if kindOf(fi) != kindDir {
				return errors.New("扫描之后里面多了东西,没删")
			}
			if err := walk(filepath.Join(d, e.Name())); err != nil {
				return err
			}
		}
		dirs = append(dirs, d) // 后序:子目录先进来
		return nil
	}
	if err := walk(dir); err != nil {
		return err
	}
	for _, d := range dirs {
		// os.Remove 删目录用的是只删得掉空目录的系统调用:
		// 核完之后这一瞬间要是有人往里放了东西,这里会失败,不会连带删掉
		if err := os.Remove(d); err != nil && !errors.Is(err, os.ErrNotExist) {
			return describe(err)
		}
	}
	return nil
}
