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
	ctx     context.Context
	mu      sync.Mutex
	jobs    map[string]*job
	binPath string
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

// SetContext 保存 Wails 上下文（用于事件推送与取消）
func (s *Service) SetContext(ctx context.Context) {
	s.ctx = ctx
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

// Run 启动一个取证任务，返回 jobID；后续通过事件推送输出
func (s *Service) Run(args []string) (string, error) {
	if s.ctx == nil {
		return "", errors.New("service context not initialized")
	}
	if len(args) == 0 {
		return "", errors.New("空参数")
	}

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
		wailsruntime.EventsEmit(s.ctx, EventDone, done)
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
		wailsruntime.EventsEmit(s.ctx, EventLog, line)
		s.emitToSubscribers(jobID, EventEnvelope{Type: "log", Log: &line})
	}
}

func newJobID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
