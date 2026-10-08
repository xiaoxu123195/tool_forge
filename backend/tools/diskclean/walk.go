package diskclean

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// entryKind 一个目录项该怎么对待
type entryKind int

const (
	kindFile entryKind = iota
	kindDir
	// kindLink 链接、目录联接、网盘同步的文件。不进去、不读、不删:
	//   - 链接和联接:跟进去可能走到别的盘、走进系统目录,还可能绕成环
	//   - 网盘文件(OneDrive 之类):没下载到本地的,读一下就会触发下载;
	//     删掉会连云端一起删。两样都不是"清理本机磁盘"该有的副作用
	kindLink
	// kindOther 设备、管道、套接字
	kindOther
)

func kindOf(fi fs.FileInfo) entryKind {
	if isLinkLike(fi) {
		return kindLink
	}
	m := fi.Mode()
	switch {
	case m&(fs.ModeSymlink|fs.ModeIrregular) != 0:
		return kindLink
	case m.IsDir():
		return kindDir
	case m.IsRegular():
		return kindFile
	}
	return kindOther
}

// walker 并发遍历目录树。
//
// 全盘扫描几十万个目录,一个一个列太慢 —— 列目录的时间大半在等磁盘,
// 同时开几路能把等待叠起来
type walker struct {
	ctx     context.Context
	rep     *reporter
	sem     chan struct{}
	pending sync.WaitGroup

	// skipDir 返回 true 的目录不进去
	skipDir func(path, name string) bool
	// onFile 每个普通文件调一次。好几个 goroutine 同时调,实现方自己管并发
	onFile func(path string, fi fs.FileInfo)
	// isProtected 没权限进的目录要按它分开数:系统保护位置里的进不去无所谓
	// (那里的东西本来也删不了),别处的进不去才是真漏掉了
	isProtected func(path string) bool

	links           atomic.Int64 // 跳过的链接 / 网盘文件
	denied          atomic.Int64 // 没权限进的目录
	deniedProtected atomic.Int64 // 其中在系统保护位置里的

	deniedMu   sync.Mutex
	deniedDirs []string
}

// maxDeniedListed 没权限进的目录最多列多少个给人看
const maxDeniedListed = 50

func (w *walker) noteDenied(dir string) {
	w.denied.Add(1)
	if w.isProtected != nil && w.isProtected(dir) {
		w.deniedProtected.Add(1)
	}
	w.deniedMu.Lock()
	if len(w.deniedDirs) < maxDeniedListed {
		w.deniedDirs = append(w.deniedDirs, dir)
	}
	w.deniedMu.Unlock()
}

// deniedList 没权限进的目录,排好序。不会是 nil:到前端是要 map 的
func (w *walker) deniedList() []string {
	w.deniedMu.Lock()
	defer w.deniedMu.Unlock()
	out := append([]string{}, w.deniedDirs...)
	sort.Strings(out)
	return out
}

func newWalker(ctx context.Context, rep *reporter) *walker {
	n := runtime.NumCPU() * 2
	if n > 16 {
		n = 16
	}
	return &walker{ctx: ctx, rep: rep, sem: make(chan struct{}, n)}
}

func (w *walker) run(roots []string) {
	for _, r := range roots {
		w.dispatch(r)
	}
	w.pending.Wait()
}

func (w *walker) dispatch(dir string) {
	w.pending.Add(1)
	select {
	case w.sem <- struct{}{}:
		go func() {
			defer func() { <-w.sem }()
			w.visit(dir)
		}()
	default:
		// 没有空闲的就在当前这个 goroutine 里接着走,不排队:
		// 排队的话一旦队列满了、所有 goroutine 又都在等着往里塞,就死锁了
		w.visit(dir)
	}
}

func (w *walker) visit(dir string) {
	defer w.pending.Done()
	if w.ctx.Err() != nil {
		return
	}
	w.rep.setCurrent(dir)
	// 读到一半出错时 ReadDir 也会把已经读到的交回来,照样处理
	entries, err := os.ReadDir(dir)
	if err != nil && errors.Is(err, fs.ErrPermission) {
		w.noteDenied(dir)
	}
	for _, e := range entries {
		if w.ctx.Err() != nil {
			return
		}
		fi, err := e.Info()
		if err != nil {
			continue // 列出来之后、看它之前被删了
		}
		p := filepath.Join(dir, e.Name())
		switch kindOf(fi) {
		case kindDir:
			if defaultSkip(p, e.Name()) || (w.skipDir != nil && w.skipDir(p, e.Name())) {
				continue
			}
			w.dispatch(p)
		case kindFile:
			w.rep.files.Add(1)
			w.rep.bytes.Add(fi.Size())
			if w.onFile != nil {
				w.onFile(p, fi)
			}
		case kindLink:
			w.links.Add(1)
		}
	}
}

// defaultSkip 哪儿都不该进去的目录:回收站里是已经删掉的东西,
// 系统卷信息是还原点,两者都进不去,进得去也不该算在"占了多少空间"里
func defaultSkip(path, name string) bool {
	n := strings.ToLower(name)
	if n != "$recycle.bin" && n != "system volume information" {
		return false
	}
	return isVolumeRoot(norm(filepath.Dir(path)))
}

// devDirs 重复文件扫描默认跳过的目录。
// 里面的重复是工具链故意的(每个项目一份依赖、版本库的对象),删了项目就坏
var devDirs = map[string]bool{
	"node_modules": true, ".git": true, ".svn": true, ".hg": true,
	"__pycache__": true, ".venv": true, ".gradle": true,
}

// cleanRoots 整理前端给的扫描起点:要完整路径、要存在、去重,
// 还要去掉被别的起点包含的 —— 选了 C:\ 又选了 C:\Users,后者会被扫两遍、算两遍
func cleanRoots(in []string) ([]string, error) {
	var roots []string
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if !filepath.IsAbs(r) {
			return nil, fmt.Errorf("不是完整路径:%s", r)
		}
		r = filepath.Clean(r)
		fi, err := os.Stat(r)
		if err != nil {
			return nil, fmt.Errorf("打不开 %s:%w", r, describe(err))
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("不是目录:%s", r)
		}
		roots = append(roots, r)
	}
	if len(roots) == 0 {
		return nil, errors.New("先选要扫描的盘或目录")
	}
	sort.Slice(roots, func(i, j int) bool { return len(roots[i]) < len(roots[j]) })
	var out []string
	for _, r := range roots {
		nested := false
		for _, o := range out {
			if within(norm(r), norm(o)) {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, r)
		}
	}
	return out, nil
}

// parallel 用 workers 个 goroutine 把 0..n-1 跑一遍
func parallel(ctx context.Context, n, workers int, fn func(i int)) {
	var next atomic.Int64
	var wg sync.WaitGroup
	for k := 0; k < workers; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1) - 1)
				if i >= n || ctx.Err() != nil {
					return
				}
				fn(i)
			}
		}()
	}
	wg.Wait()
}
