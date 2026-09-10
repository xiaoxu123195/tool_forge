// Package filehash 提供文件哈希计算:单遍流式读取、多算法并行、进度推送、批量与文件信息。
//
// 设计要点:
//   - 大文件不一次性读入内存:固定 1MB 缓冲循环读,io.MultiWriter 喂给所有选中的 hasher
//   - 进度/速度/耗时:用一个计数 writer 混进 MultiWriter,按 ~100ms 节流推送事件
//   - 取消:每次读循环检查 ctx,取消即静默结束
//   - 算法全部走 Go 标准库(MD5/SHA-1/SHA-256/SHA-512/CRC32),无第三方依赖
package filehash

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/mimetype"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// 事件名前缀(前端按 jobID 拼后缀订阅)
const (
	EventProgressPrefix = "filehash:progress:"  // 单文件进度(节流)
	EventFileDonePrefix = "filehash:file-done:" // 单文件算完
	EventDonePrefix     = "filehash:done:"      // 整个任务结束
	EventErrorPrefix    = "filehash:error:"     // 任务级错误
)

// SupportedAlgos 受支持的算法 id(与前端勾选项一致)
var SupportedAlgos = []string{"MD5", "SHA-1", "SHA-256", "SHA-512", "CRC32"}

func newHasher(algo string) hash.Hash {
	switch algo {
	case "MD5":
		return md5.New()
	case "SHA-1":
		return sha1.New()
	case "SHA-256":
		return sha256.New()
	case "SHA-512":
		return sha512.New()
	case "CRC32":
		return crc32.NewIEEE()
	}
	return nil
}

// filterAlgos 去掉不认识/重复的算法,保持 SupportedAlgos 的顺序
func filterAlgos(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, a := range SupportedAlgos {
		for _, want := range in {
			if a == want && !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	return out
}

// Progress 单文件进度(节流推送)
type Progress struct {
	JobID      string  `json:"jobId"`
	Index      int     `json:"index"` // 第几个文件(0 基)
	Total      int     `json:"total"` // 文件总数
	Path       string  `json:"path"`
	Name       string  `json:"name"`
	BytesDone  int64   `json:"bytesDone"`
	BytesTotal int64   `json:"bytesTotal"`
	SpeedBps   float64 `json:"speedBps"` // 字节/秒
	ElapsedMs  int64   `json:"elapsedMs"`
}

// FileResult 单文件最终结果
type FileResult struct {
	JobID      string            `json:"jobId"`
	Index      int               `json:"index"`
	Total      int               `json:"total"`
	Path       string            `json:"path"`
	Name       string            `json:"name"`
	Size       int64             `json:"size"`
	Hashes     map[string]string `json:"hashes"` // algo -> hex(小写)
	DurationMs int64             `json:"durationMs"`
	Error      string            `json:"error,omitempty"`
}

// FileInfo InspectFile 的返回(文件信息查看)
type FileInfo struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Ext        string `json:"ext"`
	Size       int64  `json:"size"`
	ModifiedAt int64  `json:"modifiedAt"` // unix milli
	MimeType   string `json:"mimeType"`
	MimeExt    string `json:"mimeExt"`  // mimetype 推断出的扩展(.png 等)
	Category   string `json:"category"` // 图片/视频/音频/文本/PDF/压缩包/二进制...
	MagicHex   string `json:"magicHex"` // 头部前 16 字节 hex(大写)
	IsDir      bool   `json:"isDir"`
}

// Service 管理哈希任务(job → cancel)
type Service struct {
	ctx  context.Context
	mu   sync.Mutex
	jobs map[string]context.CancelFunc
}

// New 新建服务
func New() *Service {
	return &Service{jobs: make(map[string]context.CancelFunc)}
}

// SetContext 保存 Wails 上下文(用于事件推送)
func (s *Service) SetContext(ctx context.Context) { s.ctx = ctx }

func (s *Service) emit(name string, data any) {
	if s.ctx != nil {
		wailsruntime.EventsEmit(s.ctx, name, data)
	}
}

// StartHashJob 顺序流式计算多个文件的多个哈希,返回 jobID。
// 进度/结果通过 filehash:progress|file-done|done|error 事件推送。
func (s *Service) StartHashJob(paths []string, algos []string) (string, error) {
	if s.ctx == nil {
		return "", fmt.Errorf("service 未初始化")
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("未选择文件")
	}
	algos = filterAlgos(algos)
	if len(algos) == 0 {
		return "", fmt.Errorf("未选择算法")
	}

	jobID := uuid.NewString()
	ctx, cancel := context.WithCancel(s.ctx)
	s.mu.Lock()
	s.jobs[jobID] = cancel
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.jobs, jobID)
			s.mu.Unlock()
			cancel()
		}()
		total := len(paths)
		for i, p := range paths {
			select {
			case <-ctx.Done():
				return // 取消:静默结束(已发的 file-done 前端保留)
			default:
			}
			res := s.hashOne(ctx, jobID, i, total, p, algos)
			s.emit(EventFileDonePrefix+jobID, res)
		}
		s.emit(EventDonePrefix+jobID, jobID)
	}()
	return jobID, nil
}

// CancelHashJob 取消进行中的任务
func (s *Service) CancelHashJob(jobID string) {
	s.mu.Lock()
	cancel := s.jobs[jobID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// hashOne 流式计算单个文件的所有选中算法
func (s *Service) hashOne(ctx context.Context, jobID string, index, total int, path string, algos []string) FileResult {
	name := filepath.Base(path)
	res := FileResult{
		JobID: jobID, Index: index, Total: total,
		Path: path, Name: name, Hashes: map[string]string{},
	}
	start := time.Now()

	f, err := os.Open(path)
	if err != nil {
		res.Error = err.Error()
		res.DurationMs = time.Since(start).Milliseconds()
		return res
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil {
		if st.IsDir() {
			res.Error = "是目录,不能计算哈希"
			return res
		}
		res.Size = st.Size()
	}

	// 算法名 ↔ hasher 一一对应,避免后面取结果时错位
	type named struct {
		name string
		h    hash.Hash
	}
	hashers := make([]named, 0, len(algos))
	writers := make([]io.Writer, 0, len(algos)+1)
	for _, a := range algos {
		h := newHasher(a)
		if h == nil {
			continue
		}
		hashers = append(hashers, named{a, h})
		writers = append(writers, h)
	}

	pw := &progressWriter{
		s: s, jobID: jobID, index: index, total: total,
		path: path, name: name, totalBytes: res.Size, start: start,
	}
	writers = append(writers, pw)
	mw := io.MultiWriter(writers...)

	pw.emit() // 起始 0% 一帧:UI 立刻显示当前文件

	buf := make([]byte, 1<<20) // 1MB
	if err := copyWithCancel(ctx, mw, f, buf); err != nil {
		if ctx.Err() != nil {
			res.Error = "已取消"
		} else {
			res.Error = err.Error()
		}
		res.DurationMs = time.Since(start).Milliseconds()
		return res
	}

	for _, n := range hashers {
		res.Hashes[n.name] = hex.EncodeToString(n.h.Sum(nil))
	}
	res.DurationMs = time.Since(start).Milliseconds()
	pw.emitFinal() // 收尾 100% 一帧
	return res
}

// copyWithCancel 带取消检查的流式拷贝(io.Copy 不感知 ctx)
func copyWithCancel(ctx context.Context, dst io.Writer, src io.Reader, buf []byte) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return nil
			}
			return rerr
		}
	}
}

// progressWriter 混进 MultiWriter,统计已读字节并节流推送进度
type progressWriter struct {
	s          *Service
	jobID      string
	index      int
	total      int
	path       string
	name       string
	totalBytes int64
	start      time.Time
	done       int64
	lastEmit   time.Time
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.done += int64(len(p))
	now := time.Now()
	if now.Sub(w.lastEmit) >= 100*time.Millisecond {
		w.lastEmit = now
		w.emit()
	}
	return len(p), nil
}

func (w *progressWriter) emit() {
	elapsed := time.Since(w.start)
	var speed float64
	if elapsed > 0 {
		speed = float64(w.done) / elapsed.Seconds()
	}
	w.s.emit(EventProgressPrefix+w.jobID, Progress{
		JobID: w.jobID, Index: w.index, Total: w.total,
		Path: w.path, Name: w.name,
		BytesDone: w.done, BytesTotal: w.totalBytes,
		SpeedBps: speed, ElapsedMs: elapsed.Milliseconds(),
	})
}

func (w *progressWriter) emitFinal() {
	w.lastEmit = time.Now()
	w.emit()
}

// InspectFile 读取文件信息:大小/修改时间/扩展/MIME/类别/魔数头
func InspectFile(path string) (*FileInfo, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	info := &FileInfo{
		Path:       path,
		Name:       filepath.Base(path),
		Ext:        strings.ToLower(filepath.Ext(path)),
		Size:       st.Size(),
		ModifiedAt: st.ModTime().UnixMilli(),
		IsDir:      st.IsDir(),
	}
	if st.IsDir() {
		info.Category = "目录"
		return info, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return info, nil // 元信息部分仍可用
	}
	defer f.Close()

	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head) // 文件短于 512 会返回 ErrUnexpectedEOF,n 仍有效
	head = head[:n]

	hexLen := n
	if hexLen > 16 {
		hexLen = 16
	}
	info.MagicHex = strings.ToUpper(hex.EncodeToString(head[:hexLen]))

	mt := mimetype.Detect(head)
	if mt != nil {
		info.MimeType = mt.String()
		info.MimeExt = mt.Extension()
		info.Category = classify(mt.String(), head)
	}
	return info, nil
}

// classify 把 MIME 归到中文大类
func classify(mime string, head []byte) string {
	base := mime
	if i := strings.IndexByte(base, ';'); i >= 0 {
		base = base[:i]
	}
	switch {
	case strings.HasPrefix(base, "image/"):
		return "图片"
	case strings.HasPrefix(base, "video/"):
		return "视频"
	case strings.HasPrefix(base, "audio/"):
		return "音频"
	case strings.HasPrefix(base, "text/"):
		return "文本"
	case base == "application/pdf":
		return "PDF"
	case isArchiveMime(base):
		return "压缩包"
	case base == "application/octet-stream":
		if looksText(head) {
			return "文本"
		}
		return "二进制"
	case strings.HasPrefix(base, "application/"):
		return "应用/二进制"
	default:
		if base == "" {
			return "未知"
		}
		return base
	}
}

func isArchiveMime(base string) bool {
	switch base {
	case "application/zip", "application/x-7z-compressed", "application/x-rar-compressed",
		"application/gzip", "application/x-tar", "application/x-bzip2", "application/x-xz",
		"application/vnd.rar", "application/x-zip-compressed":
		return true
	}
	return false
}

// looksText 粗判一段字节是否像文本(无 NUL,且多为可打印/常见空白)
func looksText(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	printable := 0
	for _, c := range b {
		if c == 0 {
			return false
		}
		if c == '\n' || c == '\r' || c == '\t' || (c >= 0x20 && c < 0x7f) || c >= 0x80 {
			printable++
		}
	}
	return printable*100/len(b) >= 90
}

// HashFiles 同步算一批文件的哈希,算完一次性返回。
//
// 和 StartHashJob 的区别只在"怎么把结果交出去":那个走 Wails 事件、
// 前端一条条收(几十个文件时能看到进度);这个是给本地 API / MCP 用的,
// 调用方要的就是一个结果数组,没有进度可推的地方。
//
// 复用同一个 hashOne,所以两条路算出来的东西一定一致 ——
// 各写一份读盘逻辑迟早会在缓冲区大小、算法顺序这种地方分叉。
func (s *Service) HashFiles(ctx context.Context, paths []string, algos []string) []FileResult {
	algos = filterAlgos(algos)
	if len(algos) == 0 {
		algos = SupportedAlgos // 没指定就全算:一次读盘的成本已经付了
	}
	out := make([]FileResult, 0, len(paths))
	total := len(paths)
	for i, p := range paths {
		select {
		case <-ctx.Done():
			return out // 调用方断了就把已经算完的交出去,不假装全跑完了
		default:
		}
		out = append(out, s.hashOne(ctx, "", i, total, p, algos))
	}
	return out
}
