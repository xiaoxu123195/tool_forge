package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// stdio 传输:起一个本地进程,JSON-RPC 按行走它的 stdin/stdout。
//
// 这是异步双工的 —— 响应回来的顺序不保证和请求顺序一致,服务器还会主动推通知。
// 所以要一个读协程按 id 派发到等待中的通道,而不是"写一条读一条"。

// stderrKeepBytes 保留多少 stderr 用于报错。
// 服务器起不来时,原因几乎总是在 stderr 里(找不到命令、缺依赖、配置错),
// 不带上它用户只能看到"连接失败"四个字。
const stderrKeepBytes = 2048

type stdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	ids    idGen
	stderr *ringBuffer

	writeMu sync.Mutex

	mu      sync.Mutex
	pending map[int64]chan rpcResponse
	closed  bool
	// exitErr 进程意外退出时记下原因,让还在等的调用能拿到有用的错误
	exitErr error
}

func newStdioTransport(ctx context.Context, s Server) (transport, error) {
	if strings.TrimSpace(s.Command) == "" {
		return nil, fmt.Errorf("未指定启动命令")
	}
	// 故意不绑 ctx:ctx 是"这次连接操作"的,进程要活到显式 close 为止
	cmd := exec.Command(s.Command, s.Args...)
	cmd.Env = append(os.Environ(), envPairs(s.Env)...)
	hideConsoleWindow(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("无法接管 stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("无法接管 stdout: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("无法接管 stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动失败: %w", err)
	}

	t := &stdioTransport{
		cmd:     cmd,
		stdin:   stdin,
		stderr:  &ringBuffer{limit: stderrKeepBytes},
		pending: map[int64]chan rpcResponse{},
	}
	go t.readLoop(stdout)
	go func() { _, _ = io.Copy(t.stderr, stderrPipe) }()
	go t.waitExit()
	return t, nil
}

// readLoop 逐行读 stdout,按 id 把响应派发给等待者。
// 没有 id 的是服务器主动发的通知 / 请求 —— 我们不处理,直接丢掉。
func (t *stdioTransport) readLoop(stdout io.Reader) {
	// MCP 服务器返回大结果时单行可能很长,给一个宽松的上限
	r := bufio.NewReaderSize(stdout, 64*1024)
	for {
		line, err := r.ReadString('\n')
		if line != "" {
			t.dispatch(strings.TrimSpace(line))
		}
		if err != nil {
			return
		}
	}
}

func (t *stdioTransport) dispatch(line string) {
	if line == "" {
		return
	}
	var resp rpcResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil || resp.ID == nil {
		return
	}
	t.mu.Lock()
	ch, ok := t.pending[*resp.ID]
	if ok {
		delete(t.pending, *resp.ID)
	}
	t.mu.Unlock()
	if ok {
		ch <- resp
	}
}

// waitExit 进程退出后,把还在等的调用全部叫醒,免得它们一直挂到 ctx 超时
func (t *stdioTransport) waitExit() {
	err := t.cmd.Wait()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	t.exitErr = fmt.Errorf("MCP 服务器进程已退出(%v)%s", err, t.stderr.suffix())
	for id, ch := range t.pending {
		ch <- rpcResponse{Error: &rpcError{Message: t.exitErr.Error()}}
		delete(t.pending, id)
	}
}

func (t *stdioTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := t.ids.next()
	ch := make(chan rpcResponse, 1)

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("连接已关闭")
	}
	if t.exitErr != nil {
		err := t.exitErr
		t.mu.Unlock()
		return nil, err
	}
	t.pending[id] = ch
	t.mu.Unlock()

	if err := t.write(rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params}); err != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

func (t *stdioTransport) notify(_ context.Context, method string, params any) error {
	return t.write(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
}

func (t *stdioTransport) write(req rpcRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if _, err := t.stdin.Write(data); err != nil {
		return fmt.Errorf("写入 MCP 服务器失败: %w%s", err, t.stderr.suffix())
	}
	return nil
}

// close 先关 stdin 让服务器自己退出,给一点时间,还赖着就杀掉
func (t *stdioTransport) close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	for id, ch := range t.pending {
		ch <- rpcResponse{Error: &rpcError{Message: "连接已关闭"}}
		delete(t.pending, id)
	}
	t.mu.Unlock()

	_ = t.stdin.Close()
	done := make(chan struct{})
	go func() {
		_, _ = t.cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = t.cmd.Process.Kill()
	}
	return nil
}

func envPairs(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// ringBuffer 只保留最后 limit 字节。stderr 可能一直刷,但报错时只需要最后那点。
type ringBuffer struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func (b *ringBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.limit {
		b.buf = b.buf[len(b.buf)-b.limit:]
	}
	return len(p), nil
}

// suffix 把 stderr 拼成可以直接接在错误信息后面的一段
func (b *ringBuffer) suffix() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := strings.TrimSpace(string(b.buf))
	if s == "" {
		return ""
	}
	return "\n服务器输出:\n" + s
}
