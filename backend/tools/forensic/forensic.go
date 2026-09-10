// Package forensic 包装 go-forensic CLI，支持流式输出与取消。
package forensic

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// 事件名
const (
	EventLog  = "forensic:log"
	EventDone = "forensic:done"
)

// Info 可执行文件的探测结果
type Info struct {
	Found   bool   `json:"found"`
	Path    string `json:"path"`
	Version string `json:"version"`
	Error   string `json:"error,omitempty"`
}

// LogLine 推送给前端的日志行
type LogLine struct {
	JobID  string `json:"jobId"`
	Stream string `json:"stream"` // stdout / stderr
	Line   string `json:"line"`
}

// DoneEvent 执行结束事件
type DoneEvent struct {
	JobID    string `json:"jobId"`
	ExitCode int    `json:"exitCode"`
	Error    string `json:"error,omitempty"`
	Canceled bool   `json:"canceled"`
}

// Service 管理取证任务
type Service struct {
	// ctx 任务的父上下文,只用于取消;应用退出时连带把 go-forensic 进程收掉
	ctx context.Context
	// wailsCtx 仅用于给桌面工具页推事件。
	//
	// 和 ctx 分开是必须的:wailsruntime.EventsEmit 拿到一个不是 Wails 生命周期
	// 给的 context 时,不是返回错误,而是直接把整个进程结束掉。
	// 本地 API / MCP 这类调用方根本不需要事件(它们走 subscribers 那条 channel),
	// 合在一起的话,一次纯 API 调用就能因为"这个进程没开 GUI"而自杀。
	wailsCtx context.Context
	mu       sync.Mutex
	jobs     map[string]*job
	binPath  string
	// subscribers 给 HTTP SSE 等"非 Wails 前端"用的订阅者。
	// emit 时既调 wailsruntime.EventsEmit(给桌面工具页),也 fan-out 到此处的 channel。
	subscribers map[string][]chan EventEnvelope
}

type job struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

// EventEnvelope 统一封装 log / done 两类事件,Subscribe 用
type EventEnvelope struct {
	Type string     `json:"type"` // "log" | "done"
	Log  *LogLine   `json:"log,omitempty"`
	Done *DoneEvent `json:"done,omitempty"`
}

// New 新建服务
func New() *Service {
	return &Service{
		jobs:        make(map[string]*job),
		subscribers: make(map[string][]chan EventEnvelope),
	}
}

// Subscribe 订阅指定 jobID 的事件流。
// 返回的 channel 在收到 type=="done" 后会自动关闭(由 emitDone 触发)。
// 调用方应在 defer 里调 unsubscribe,避免任务还没结束就 leak。
func (s *Service) Subscribe(jobID string) (<-chan EventEnvelope, func()) {
	ch := make(chan EventEnvelope, 64)
	s.mu.Lock()
	s.subscribers[jobID] = append(s.subscribers[jobID], ch)
	s.mu.Unlock()

	unsubscribe := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		list := s.subscribers[jobID]
		for i, c := range list {
			if c == ch {
				// 删除 + 关闭(允许重复 close 无副作用,但用 recover 保险)
				s.subscribers[jobID] = append(list[:i], list[i+1:]...)
				defer func() {
					recover()
				}()
				close(ch)
				return
			}
		}
	}
	return ch, unsubscribe
}

// emitToSubscribers 给指定 jobID 的所有订阅者推一个事件;channel 满时丢弃。
func (s *Service) emitToSubscribers(jobID string, env EventEnvelope) {
	s.mu.Lock()
	chans := append([]chan EventEnvelope(nil), s.subscribers[jobID]...)
	s.mu.Unlock()
	for _, ch := range chans {
		select {
		case ch <- env:
		default: // 满了就丢,避免阻塞 service 主流程
		}
	}
}

// closeSubscribers 任务结束时主动关闭所有该 jobID 的订阅 channel,
// 让正在 range 等数据的协程能优雅退出。
func (s *Service) closeSubscribers(jobID string) {
	s.mu.Lock()
	chans := s.subscribers[jobID]
	delete(s.subscribers, jobID)
	s.mu.Unlock()
	for _, ch := range chans {
		// close 已 closed channel 会 panic,用 recover 兜底
		func() {
			defer func() { recover() }()
			close(ch)
		}()
	}
}

// SetContext 保存 Wails 上下文(事件推送 + 任务取消)。
// 桌面端在 startup 里调;纯 API / 测试场景可以改用 SetRunContext。
func (s *Service) SetContext(ctx context.Context) {
	s.ctx = ctx
	s.wailsCtx = ctx
}

// SetRunContext 只设任务上下文,不设事件上下文。
// 给没有 GUI 的场景用:任务照跑、订阅照收,只是不往 Wails 推事件。
func (s *Service) SetRunContext(ctx context.Context) {
	s.ctx = ctx
}

// emitWails 往桌面工具页推一个事件;没有 Wails 上下文时什么都不做
func (s *Service) emitWails(name string, data any) {
	if s.wailsCtx == nil {
		return
	}
	wailsruntime.EventsEmit(s.wailsCtx, name, data)
}

// SetBinaryPath 自定义 go-forensic 路径（空字符串 = 使用 PATH）
func (s *Service) SetBinaryPath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.binPath = strings.TrimSpace(path)
}

// GetBinaryPath 返回当前解析到的可执行路径
func (s *Service) resolveBinary() string {
	s.mu.Lock()
	p := s.binPath
	s.mu.Unlock()
	if p == "" {
		return "go-forensic"
	}
	return p
}

// Check 探测可执行文件，跑 `go-forensic version`
func (s *Service) Check(customPath string) Info {
	target := strings.TrimSpace(customPath)
	if target == "" {
		target = "go-forensic"
	}
	resolved, err := exec.LookPath(target)
	if err != nil {
		return Info{Found: false, Path: target, Error: "未在系统 PATH 中找到，或路径不可执行"}
	}
	cmd := exec.Command(resolved, "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Info{Found: false, Path: resolved, Error: fmt.Sprintf("已找到但执行失败：%v\n%s", err, out)}
	}
	return Info{
		Found:   true,
		Path:    resolved,
		Version: strings.TrimSpace(string(out)),
	}
}

// Run 启动一个取证任务，返回 jobID；后续通过事件推送输出。
//
// 安卓的 export 走原生实现(adb 协议),其余仍然调命令行。
// 两条路对外是一样的:同一个 jobID、同一串 forensic:log / forensic:done 事件、
// 同一个 Cancel —— 界面和 MCP 那头都感觉不到区别。
func (s *Service) Run(args []string) (string, error) {
	if s.ctx == nil {
		return "", errors.New("取证服务还没初始化(缺少上下文)")
	}
	if len(args) == 0 {
		return "", errors.New("空参数")
	}

	if opt, ok := parseExportArgs(args); nativeSupported(opt, ok) {
		return s.runNative(opt)
	}
	return s.runCLI(args)
}

// runNative 用原生实现跑,不 fork 任何进程
func (s *Service) runNative(opt exportOptions) (string, error) {
	jobID := newJobID()
	runCtx, cancel := context.WithCancel(s.ctx)

	s.mu.Lock()
	s.jobs[jobID] = &job{cancel: cancel}
	s.mu.Unlock()

	logf := func(format string, a ...any) {
		line := fmt.Sprintf(format, a...)
		stream := "stdout"
		// 沿用命令行那套约定:错误走 stderr,界面据此标红
		if strings.HasPrefix(line, "ERROR") || strings.HasPrefix(line, "WARN") {
			stream = "stderr"
		}
		s.pushLine(jobID, stream, line)
	}

	go func() {
		logf("%s", opt.describe())
		err := runAndroidExport(runCtx, opt, logf)

		s.mu.Lock()
		j := s.jobs[jobID]
		delete(s.jobs, jobID)
		s.mu.Unlock()
		if j != nil {
			j.cancel()
		}

		done := DoneEvent{JobID: jobID}
		switch {
		case errors.Is(runCtx.Err(), context.Canceled):
			done.Canceled = true
			done.ExitCode = -1
		case err != nil:
			done.ExitCode = 1
			done.Error = err.Error()
			s.pushLine(jobID, "stderr", err.Error())
		default:
			done.ExitCode = 0
		}
		s.emitWails(EventDone, done)
		s.emitToSubscribers(jobID, EventEnvelope{Type: "done", Done: &done})
		s.closeSubscribers(jobID)
	}()

	return jobID, nil
}

// pushLine 把一行输出同时推给桌面页和订阅者(本地 API / MCP)
func (s *Service) pushLine(jobID, stream, line string) {
	l := LogLine{JobID: jobID, Stream: stream, Line: line}
	s.emitWails(EventLog, l)
	s.emitToSubscribers(jobID, EventEnvelope{Type: "log", Log: &l})
}

// runCLI 老路:fork go-forensic
func (s *Service) runCLI(args []string) (string, error) {
	bin := s.resolveBinary()
	if _, err := exec.LookPath(bin); err != nil {
		return "", fmt.Errorf("找不到 go-forensic，请在 Profile → 外部工具 中配置路径")
	}

	jobID := newJobID()
	runCtx, cancel := context.WithCancel(s.ctx)
	cmd := exec.CommandContext(runCtx, bin, args...)
	// Windows 下隐藏黑窗口
	applyPlatformCmd(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return "", err
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return "", err
	}

	s.mu.Lock()
	s.jobs[jobID] = &job{cmd: cmd, cancel: cancel}
	s.mu.Unlock()

	go s.pumpStream(jobID, "stdout", stdout)
	go s.pumpStream(jobID, "stderr", stderr)

	go func() {
		waitErr := cmd.Wait()
		s.mu.Lock()
		j := s.jobs[jobID]
		delete(s.jobs, jobID)
		s.mu.Unlock()
		if j != nil {
			j.cancel()
		}

		done := DoneEvent{JobID: jobID}
		if waitErr != nil {
			if errors.Is(runCtx.Err(), context.Canceled) {
				done.Canceled = true
				done.ExitCode = -1
			} else if exitErr, ok := waitErr.(*exec.ExitError); ok {
				done.ExitCode = exitErr.ExitCode()
				done.Error = waitErr.Error()
			} else {
				done.ExitCode = -1
				done.Error = waitErr.Error()
			}
		} else {
			done.ExitCode = 0
		}
		s.emitWails(EventDone, done)
		s.emitToSubscribers(jobID, EventEnvelope{Type: "done", Done: &done})
		// 关 channel 让 SSE handler 优雅退出
		s.closeSubscribers(jobID)
	}()

	return jobID, nil
}

// Cancel 终止任务
func (s *Service) Cancel(jobID string) error {
	s.mu.Lock()
	j, ok := s.jobs[jobID]
	s.mu.Unlock()
	if !ok {
		return errors.New("任务不存在或已结束")
	}
	j.cancel()
	if j.cmd != nil && j.cmd.Process != nil {
		_ = j.cmd.Process.Kill()
	}
	return nil
}

func (s *Service) pumpStream(jobID, stream string, r io.ReadCloser) {
	defer r.Close()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := LogLine{
			JobID:  jobID,
			Stream: stream,
			Line:   scanner.Text(),
		}
		s.emitWails(EventLog, line)
		s.emitToSubscribers(jobID, EventEnvelope{Type: "log", Log: &line})
	}
}

func newJobID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
