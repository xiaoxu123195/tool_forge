package diskclean

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DupOptions 重复文件扫描的参数
type DupOptions struct {
	JobID string   `json:"jobId"`
	Roots []string `json:"roots"`
	// MinSize 小于这个的不看(字节),默认 1 MB。几 KB 的重复删了也腾不出空间,只会把列表撑爆
	MinSize int64 `json:"minSize"`
	// SkipDevDirs 跳过 node_modules、.git 这类目录。
	// 里面的重复是工具链故意的,删了项目就坏
	SkipDevDirs bool `json:"skipDevDirs"`
}

// DupFile 一组重复里的一个文件
type DupFile struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	ModTime int64  `json:"modTime"`
	Blocked bool   `json:"blocked"`
	Reason  string `json:"reason,omitempty"`
	Warn    string `json:"warn,omitempty"`
}

// DupGroup 内容完全一样的一组文件
type DupGroup struct {
	// ID 内容的 SHA-256。删除时拿它核对:要删的和要留的都必须还是这个内容
	ID    string    `json:"id"`
	Size  int64     `json:"size"`
	Files []DupFile `json:"files"`
	// Wasted 这一组只留一份能腾出多少
	Wasted int64 `json:"wasted"`
}

// DupResult 重复文件扫描的结果
type DupResult struct {
	Groups []DupGroup `json:"groups"`
	// TotalGroups 一共找到多少组。组太多时 Groups 只给最占地方的那些
	TotalGroups int   `json:"totalGroups"`
	Wasted      int64 `json:"wasted"`

	Scanned      int64 `json:"scanned"`
	ScannedBytes int64 `json:"scannedBytes"`
	// HashedBytes 真正读了多少字节去比内容。分级筛的意义就在于它远小于 ScannedBytes
	HashedBytes int64 `json:"hashedBytes"`
	// Hardlinks 同一个文件的多个名字,不算重复:删掉一个名字腾不出空间
	Hardlinks    int64 `json:"hardlinks"`
	SkippedLinks int64 `json:"skippedLinks"`
	// Denied 没权限进的目录
	Denied DeniedInfo `json:"denied"`
	// Unreadable 读不了内容的(被占用、没权限、扫的时候在变)
	Unreadable int64 `json:"unreadable"`
	Cancelled  bool  `json:"cancelled"`
	ElapsedMs  int64 `json:"elapsedMs"`
}

const (
	// sampleSize 初筛时头尾各读这么多。大小相同的文件,绝大多数在头尾就分得出来
	sampleSize = 64 << 10
	// maxGroups 最多交回多少组,按能腾出的空间排
	maxGroups = 2000
	// hashWorkers 同时读几个文件。读文件的瓶颈在盘,开太多反而互相抢
	hashWorkers = 4
)

var errChanged = errors.New("扫描之后内容变了,没删")

type cand struct {
	path   string
	size   int64
	mod    int64
	id     string // 文件身份,认硬链接用
	sample string
	full   string
	bad    bool
}

// ScanDuplicates 找内容完全一样的文件。分三级,能不读的尽量不读:
//
//	大小  大小不一样的不可能内容一样,这一级一个字节都不读
//	头尾  大小一样的,各读头尾 64KB 比一比,绝大多数在这里分开
//	全文  头尾都一样的才读全文算 SHA-256
func (s *Service) ScanDuplicates(opt DupOptions) (*DupResult, error) {
	roots, err := cleanRoots(opt.Roots)
	if err != nil {
		return nil, err
	}
	if opt.MinSize <= 0 {
		opt.MinSize = 1 << 20
	}
	ctx, end := s.begin(opt.JobID)
	defer end()
	rep := s.report(opt.JobID)
	defer rep.close()
	defer withBackupPrivilege()()
	start := time.Now()

	res := &DupResult{Groups: []DupGroup{}, Denied: DeniedInfo{Dirs: []string{}}}
	var hashed, unreadable, hardlinks atomic.Int64
	finish := func() *DupResult {
		res.HashedBytes = hashed.Load()
		res.Unreadable = unreadable.Load()
		res.Hardlinks = hardlinks.Load()
		res.Cancelled = ctx.Err() != nil
		res.ElapsedMs = time.Since(start).Milliseconds()
		return res
	}

	// ---- 第一级:大小 ----
	rep.setPhase("扫描文件", 0)
	var mu sync.Mutex
	bySize := map[int64][]*cand{}
	w := newWalker(ctx, rep)
	w.isProtected = s.protectedDir
	w.skipDir = func(p, name string) bool {
		if opt.SkipDevDirs && devDirs[strings.ToLower(name)] {
			return true
		}
		// 系统目录里的"重复"大多是同一个文件的多个硬链接,而且删不得,扫了全是噪音
		return s.guard.Check(p).Blocked
	}
	w.onFile = func(p string, fi fs.FileInfo) {
		size := fi.Size()
		if size < opt.MinSize {
			return
		}
		c := &cand{path: p, size: size, mod: fi.ModTime().Unix()}
		mu.Lock()
		bySize[size] = append(bySize[size], c)
		mu.Unlock()
	}
	w.run(roots)
	res.Scanned, res.ScannedBytes = rep.files.Load(), rep.bytes.Load()
	res.SkippedLinks, res.Denied = w.links.Load(), deniedInfo(w)
	if ctx.Err() != nil {
		return finish(), nil
	}

	var pool []*cand
	for _, cs := range bySize {
		if len(cs) > 1 {
			pool = append(pool, cs...)
		}
	}

	// ---- 第二级:头尾 ----
	rep.setPhase("初筛内容", int64(len(pool)))
	parallel(ctx, len(pool), hashWorkers, func(i int) {
		c := pool[i]
		rep.setCurrent(c.path)
		c.id, _ = fileID(c.path)
		sum, n, err := sampleHash(c.path, c.size)
		hashed.Add(n)
		rep.done.Add(1)
		if err != nil {
			c.bad = true
			unreadable.Add(1)
			return
		}
		c.sample = sum
	})
	if ctx.Err() != nil {
		return finish(), nil
	}

	type key struct {
		size int64
		sum  string
	}
	bySample := map[key][]*cand{}
	for _, c := range pool {
		if !c.bad {
			k := key{c.size, c.sample}
			bySample[k] = append(bySample[k], c)
		}
	}
	var settled, needFull []*cand
	for _, g := range bySample {
		g = dropHardlinks(g, &hardlinks)
		if len(g) < 2 {
			continue
		}
		if g[0].size <= 2*sampleSize {
			// 小文件的"头尾"就是全文,初筛算的已经是全文哈希
			for _, c := range g {
				c.full = c.sample
			}
			settled = append(settled, g...)
		} else {
			needFull = append(needFull, g...)
		}
	}

	// ---- 第三级:全文 ----
	var total int64
	for _, c := range needFull {
		total += c.size
	}
	rep.setPhase("比对全文", total)
	parallel(ctx, len(needFull), hashWorkers, func(i int) {
		c := needFull[i]
		rep.setCurrent(c.path)
		sum, err := fullHash(ctx, c.path, func(n int64) {
			hashed.Add(n)
			rep.done.Add(n)
		})
		if err != nil {
			c.bad = true
			if ctx.Err() == nil {
				unreadable.Add(1)
			}
			return
		}
		c.full = sum
	})
	if ctx.Err() != nil {
		return finish(), nil
	}

	byFull := map[key][]*cand{}
	for _, c := range append(settled, needFull...) {
		if !c.bad && c.full != "" {
			k := key{c.size, c.full}
			byFull[k] = append(byFull[k], c)
		}
	}
	for k, g := range byFull {
		if len(g) < 2 {
			continue
		}
		sort.Slice(g, func(i, j int) bool { return g[i].path < g[j].path })
		grp := DupGroup{ID: k.sum, Size: k.size, Wasted: k.size * int64(len(g)-1), Files: make([]DupFile, 0, len(g))}
		for _, c := range g {
			v := s.guard.Check(c.path)
			grp.Files = append(grp.Files, DupFile{
				Path: c.path, Name: filepath.Base(c.path), Dir: filepath.Dir(c.path), ModTime: c.mod,
				Blocked: v.Blocked, Reason: v.Reason, Warn: v.Warn,
			})
		}
		res.Wasted += grp.Wasted
		res.Groups = append(res.Groups, grp)
	}
	sort.Slice(res.Groups, func(i, j int) bool {
		a, b := res.Groups[i], res.Groups[j]
		if a.Wasted != b.Wasted {
			return a.Wasted > b.Wasted
		}
		return a.ID < b.ID
	})
	res.TotalGroups = len(res.Groups)
	if len(res.Groups) > maxGroups {
		res.Groups = res.Groups[:maxGroups]
	}
	return finish(), nil
}

// dropHardlinks 同一个文件的多个名字只留一个
func dropHardlinks(g []*cand, counter *atomic.Int64) []*cand {
	seen := map[string]bool{}
	out := g[:0:0]
	for _, c := range g {
		if c.id != "" {
			if seen[c.id] {
				counter.Add(1)
				continue
			}
			seen[c.id] = true
		}
		out = append(out, c)
	}
	return out
}

// sampleHash 头尾各 64KB 的哈希。文件不超过 128KB 时读的就是全文,得到的是全文的 SHA-256
func sampleHash(p string, size int64) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	if size <= 2*sampleSize {
		n, err := io.Copy(h, f)
		if err != nil {
			return "", n, err
		}
		if n != size {
			return "", n, errChanged
		}
		return hex.EncodeToString(h.Sum(nil)), n, nil
	}
	n1, err := io.CopyN(h, f, sampleSize)
	if err != nil {
		return "", n1, err
	}
	if _, err := f.Seek(size-sampleSize, io.SeekStart); err != nil {
		return "", n1, err
	}
	n2, err := io.CopyN(h, f, sampleSize)
	if err != nil {
		return "", n1 + n2, err
	}
	return hex.EncodeToString(h.Sum(nil)), n1 + n2, nil
}

var bufPool = sync.Pool{New: func() any { b := make([]byte, 1<<20); return &b }}

// fullHash 全文 SHA-256。读一段报一段进度,随时能取消 —— 几个 GB 的视频要读好一会儿
func fullHash(ctx context.Context, p string, progress func(int64)) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	bp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bp)
	buf := *bp
	h := sha256.New()
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			if progress != nil {
				progress(int64(n))
			}
		}
		if err == io.EOF {
			return hex.EncodeToString(h.Sum(nil)), nil
		}
		if err != nil {
			return "", err
		}
	}
}

// ---------------- 删除重复文件 ----------------

// DupDeleteGroup 一组里留哪些、删哪些
type DupDeleteGroup struct {
	ID     string   `json:"id"`
	Size   int64    `json:"size"`
	Keep   []string `json:"keep"`
	Delete []string `json:"delete"`
}

// DupDeleteRequest 删一批重复文件
type DupDeleteRequest struct {
	JobID     string           `json:"jobId"`
	Groups    []DupDeleteGroup `json:"groups"`
	Permanent bool             `json:"permanent"`
}

// DeleteDuplicates 删重复文件。每一组都要先确认留下的那份还在、内容没变,
// 再逐个确认要删的那些内容也没变 —— 扫描到删除之间可能隔了好一阵
func (s *Service) DeleteDuplicates(req DupDeleteRequest) (*DeleteResult, error) {
	ctx, end := s.begin(req.JobID)
	defer end()
	rep := s.report(req.JobID)
	defer rep.close()
	var total int64
	for _, g := range req.Groups {
		total += int64(len(g.Delete))
	}
	rep.setPhase("核对并删除", total)

	res := &DeleteResult{Items: []DeleteItem{}, Recycled: !req.Permanent}
	for _, g := range req.Groups {
		if ctx.Err() != nil {
			break
		}
		s.deleteDupGroup(ctx, g, req.Permanent, res, rep)
	}
	res.Cancelled = ctx.Err() != nil
	return res, nil
}

func (s *Service) deleteDupGroup(ctx context.Context, g DupDeleteGroup, permanent bool, res *DeleteResult, rep *reporter) {
	failAll := func(why string) {
		for _, p := range g.Delete {
			res.Items = append(res.Items, DeleteItem{Path: p, Reason: why})
			res.Failed++
			rep.done.Add(1)
		}
	}
	if len(g.Delete) == 0 {
		return
	}
	if len(g.Keep) == 0 {
		failAll("这一组一份都没留,不删")
		return
	}
	keep := map[string]bool{}
	for _, k := range g.Keep {
		keep[norm(k)] = true
	}
	for _, d := range g.Delete {
		if keep[norm(d)] {
			failAll("同一个文件既要留又要删,这一组不删")
			return
		}
	}

	// 留下的那份要是已经变了或者没了,删掉其余的就等于把这份内容彻底删没了。
	// 硬链接身份对所有要留的都取(便宜);内容只核到第一份对得上的为止(要读全文)
	keeperIDs := map[string]bool{}
	for _, k := range g.Keep {
		if id, ok := fileID(k); ok {
			keeperIDs[id] = true
		}
	}
	alive := false
	for _, k := range g.Keep {
		if ctx.Err() != nil {
			return
		}
		if sameContent(ctx, k, g.Size, g.ID) == nil {
			alive = true
			break
		}
	}
	if !alive {
		failAll("要保留的那份已经不在了或者内容变了,这一组不删")
		return
	}

	for _, d := range g.Delete {
		if ctx.Err() != nil {
			return
		}
		rep.setCurrent(d)
		item := DeleteItem{Path: d}
		if err := s.removeDuplicate(ctx, d, g, keeperIDs, permanent); err != nil {
			item.Reason = err.Error()
			res.Failed++
		} else {
			item.OK = true
			res.Deleted++
			res.Bytes += g.Size
		}
		res.Items = append(res.Items, item)
		rep.done.Add(1)
	}
}

func (s *Service) removeDuplicate(ctx context.Context, p string, g DupDeleteGroup, keeperIDs map[string]bool, permanent bool) error {
	p = filepath.Clean(p)
	if _, err := checkRegular(p); err != nil {
		return err
	}
	if v := s.guard.CheckFinal(p); v.Blocked {
		return errors.New(v.Reason)
	}
	if id, ok := fileID(p); ok && keeperIDs[id] {
		return errors.New("和要保留的那份是同一个文件(硬链接),删了腾不出空间")
	}
	if err := sameContent(ctx, p, g.Size, g.ID); err != nil {
		return err
	}
	return removeFile(p, permanent)
}

// sameContent 文件还是那么大、全文哈希还是那个值
func sameContent(ctx context.Context, p string, size int64, id string) error {
	fi, err := os.Lstat(p)
	if err != nil {
		return describe(err)
	}
	if kindOf(fi) != kindFile {
		return errors.New("不是普通文件")
	}
	if fi.Size() != size {
		return errChanged
	}
	sum, err := fullHash(ctx, p, nil)
	if err != nil {
		if ctx.Err() != nil {
			return errors.New("已取消")
		}
		return describe(err)
	}
	if !strings.EqualFold(sum, id) {
		return errChanged
	}
	return nil
}
