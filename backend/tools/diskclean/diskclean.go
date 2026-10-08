// Package diskclean 磁盘清理:大文件、重复文件、缓存。
//
// 三件事共用一条底线:删什么由这个包说了算,不由调用方说了算。
// 前端传来的每个路径都要重新过一遍 Guard —— 系统目录、程序目录、驱动器根、
// 注册表文件这些,不管请求从哪儿来都删不掉。
//
// 删除分两种,区别在于删完空间腾没腾出来:
//
//	用户文件(大文件、重复文件)  默认进回收站,删错了能捡回来
//	缓存                         直接删。回收站和缓存在同一块盘上,
//	                             进回收站等于一个字节都没腾出来
//
// 所以缓存那边的范围收得最紧:只删规则里写明的目录里面的东西,
// 目录本身留着,不跟链接,不碰比规定时间新的文件。
//
// 这个包没有挂到本地 API / MCP 上:这里的每个动作都可能删文件,
// 删哪些、什么时候删,该是人在界面上看过、确认过的。
package diskclean

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// EventProgressPrefix 进度事件名前缀,前端按 jobID 拼后缀订阅
const EventProgressPrefix = "diskclean:progress:"

// Progress 长任务的进度。节流推送,不是每个文件一条
type Progress struct {
	JobID string `json:"jobId"`
	// Phase 现在在干什么:扫描文件 / 比对内容 / 删除……
	Phase string `json:"phase"`
	// Files / Bytes 一共看过多少个文件、多少字节,扫描阶段一直往上涨
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
	// Done / Total 当前阶段做到哪儿了;Total 为 0 表示总数事先不知道
	Done  int64 `json:"done"`
	Total int64 `json:"total"`
	// Current 正在看的位置。几十秒没动静的进度条会让人以为卡死了
	Current   string `json:"current"`
	ElapsedMs int64  `json:"elapsedMs"`
}

// Service 磁盘清理的入口
type Service struct {
	ctx   context.Context
	guard *Guard
	rules []CacheRule
	// customPath 自定义缓存规则存在哪儿
	customPath string
	customMu   sync.Mutex

	mu   sync.Mutex
	jobs map[string]context.CancelFunc

	// usage 最近一次大文件扫描顺带建的目录树
	usageMu sync.Mutex
	usage   *usageTree
}

// New 新建服务
func New() *Service {
	return &Service{
		guard:      NewGuard(),
		rules:      builtinRules(),
		customPath: defaultCustomPath(),
		jobs:       map[string]context.CancelFunc{},
	}
}

// SetContext 保存 Wails 上下文(推进度事件用;应用退出时它被取消,进行中的任务跟着停)
func (s *Service) SetContext(ctx context.Context) { s.ctx = ctx }

// Cancel 取消一个进行中的任务。任务会尽快停下,把已经做完的部分交回去
func (s *Service) Cancel(jobID string) {
	s.mu.Lock()
	cancel := s.jobs[jobID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// begin 登记一个任务,拿到它的 ctx。
//
// jobID 是前端生成的:调用本身会一直阻塞到做完,前端得在发起调用之前
// 就知道 id,才能先订阅进度、中途才能取消
func (s *Service) begin(jobID string) (context.Context, func()) {
	base := s.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	if jobID != "" {
		s.mu.Lock()
		s.jobs[jobID] = cancel
		s.mu.Unlock()
	}
	return ctx, func() {
		if jobID != "" {
			s.mu.Lock()
			delete(s.jobs, jobID)
			s.mu.Unlock()
		}
		cancel()
	}
}

// reporter 汇总进度并节流推送。
//
// 扫描是好几个 goroutine 一起跑的。让每个 goroutine 自己推事件,要么把前端推爆,
// 要么各自节流还得互相加锁;改成大家只往计数器里加,由一个定时器统一往外推
type reporter struct {
	s     *Service
	jobID string
	start time.Time

	files, bytes, done, total atomic.Int64

	mu      sync.Mutex
	phase   string
	current string

	stop chan struct{}
	wg   sync.WaitGroup
}

func (s *Service) report(jobID string) *reporter {
	r := &reporter{s: s, jobID: jobID, start: time.Now(), stop: make(chan struct{})}
	if !r.live() {
		return r
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		t := time.NewTicker(150 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-r.stop:
				return
			case <-t.C:
				r.emit()
			}
		}
	}()
	return r
}

// live 有没有地方可推:测试里和没有界面时只计数不推
func (r *reporter) live() bool { return r.jobID != "" && r.s.ctx != nil }

func (r *reporter) setPhase(phase string, total int64) {
	r.mu.Lock()
	r.phase = phase
	r.mu.Unlock()
	r.done.Store(0)
	r.total.Store(total)
}

func (r *reporter) setCurrent(p string) {
	r.mu.Lock()
	r.current = p
	r.mu.Unlock()
}

func (r *reporter) emit() {
	if !r.live() {
		return
	}
	r.mu.Lock()
	phase, cur := r.phase, r.current
	r.mu.Unlock()
	wailsruntime.EventsEmit(r.s.ctx, EventProgressPrefix+r.jobID, Progress{
		JobID: r.jobID, Phase: phase,
		Files: r.files.Load(), Bytes: r.bytes.Load(),
		Done: r.done.Load(), Total: r.total.Load(),
		Current: cur, ElapsedMs: time.Since(r.start).Milliseconds(),
	})
}

// close 停掉定时器,再推最后一帧 —— 不然前端停在九十几趴上
func (r *reporter) close() {
	close(r.stop)
	r.wg.Wait()
	r.emit()
}

// ---------------- 删除用户挑中的文件(大文件、重复文件共用)----------------

// FileRef 前端要删的一个文件,带着扫描时看到的样子。
// 删之前要对一遍:从扫描完到点删除可能过了好一阵,文件可能已经变了
type FileRef struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
}

// DeleteRequest 删一批文件
type DeleteRequest struct {
	JobID string    `json:"jobId"`
	Files []FileRef `json:"files"`
	// Permanent 不进回收站,直接删
	Permanent bool `json:"permanent"`
}

// DeleteItem 一个文件删没删、没删的话为什么
type DeleteItem struct {
	Path   string `json:"path"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// DeleteResult 一批删除的结果
type DeleteResult struct {
	Items   []DeleteItem `json:"items"`
	Deleted int          `json:"deleted"`
	Failed  int          `json:"failed"`
	// Bytes 删掉的文件一共多大
	Bytes int64 `json:"bytes"`
	// Recycled 进的是回收站:空间要等清空回收站才真正腾出来,界面上必须说清楚
	Recycled  bool `json:"recycled"`
	Cancelled bool `json:"cancelled"`
}

// DeleteFiles 删一批用户挑中的文件(大文件页来的)
func (s *Service) DeleteFiles(req DeleteRequest) (*DeleteResult, error) {
	ctx, end := s.begin(req.JobID)
	defer end()
	rep := s.report(req.JobID)
	defer rep.close()
	rep.setPhase("删除", int64(len(req.Files)))

	res := &DeleteResult{Items: []DeleteItem{}, Recycled: !req.Permanent}
	seen := map[string]bool{}
	for _, f := range req.Files {
		if ctx.Err() != nil {
			res.Cancelled = true
			break
		}
		key := norm(f.Path)
		if seen[key] {
			continue
		}
		seen[key] = true
		rep.setCurrent(f.Path)

		item := DeleteItem{Path: f.Path}
		if err := s.removeUserFile(f, req.Permanent); err != nil {
			item.Reason = err.Error()
			res.Failed++
		} else {
			item.OK = true
			res.Deleted++
			res.Bytes += f.Size
		}
		res.Items = append(res.Items, item)
		rep.done.Add(1)
	}
	return res, nil
}

// removeUserFile 删一个用户挑中的文件。
// 每一步失败都给人话原因,前端原样显示在那一行上
func (s *Service) removeUserFile(f FileRef, permanent bool) error {
	p := filepath.Clean(f.Path)
	fi, err := checkRegular(p)
	if err != nil {
		return err
	}
	// 先确认是普通文件再去解真实路径:解真实路径要打开文件,
	// 而网盘那种没下载到本地的文件,打开一下就会触发下载
	if v := s.guard.CheckFinal(p); v.Blocked {
		return errors.New(v.Reason)
	}
	if fi.Size() != f.Size || fi.ModTime().Unix() != f.ModTime {
		return errors.New("扫描之后这个文件变了,没删——重新扫一遍再决定")
	}
	return removeFile(p, permanent)
}

// checkRegular 确认路径还在、是个普通文件
func checkRegular(p string) (fs.FileInfo, error) {
	if !filepath.IsAbs(p) {
		return nil, errors.New("不是完整路径")
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return nil, describe(err)
	}
	switch kindOf(fi) {
	case kindFile:
		return fi, nil
	case kindLink:
		return nil, errors.New("这是链接或网盘同步的文件,不删")
	default:
		return nil, errors.New("不是普通文件")
	}
}

// removeFile 删一个文件:进回收站,或者直接删
func removeFile(p string, permanent bool) error {
	var err error
	if permanent {
		err = os.Remove(p)
	} else {
		err = moveToTrash(p)
	}
	if err != nil {
		return describe(err)
	}
	return nil
}

// errTrashAborted 放不进回收站时系统弹窗问过一次,用户选了不删
var errTrashAborted = errors.New("取消了:文件放不进回收站,系统问过是否直接删除")

// describe 把系统错误翻成人话。认不出来的原样给出去 —— 那至少是条线索
func describe(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errTrashAborted):
		return err
	case errors.Is(err, fs.ErrNotExist):
		return errors.New("文件已经不在了")
	case isInUse(err):
		return errors.New("文件正被别的程序占用")
	case errors.Is(err, fs.ErrPermission):
		return errors.New("没有权限(可能要以管理员身份运行工具箱)")
	}
	return err
}
