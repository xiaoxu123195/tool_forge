package mcp

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// connectTimeout 单次连接 + 握手 + 拉工具列表的总时限。
// stdio 服务器第一次跑往往要下依赖(npx / uvx),给宽一点。
const connectTimeout = 60 * time.Second

// callTimeout 单次工具调用的时限
const callTimeout = 120 * time.Second

// Service MCP 服务器的配置管理 + 连接生命周期 + 工具聚合。
//
// 连接是懒建的:第一次要用工具时才连,连上之后一直留着。
// 不在启动时全连是因为 stdio 服务器要起进程、HTTP 服务器要走网络,
// 都可能很慢甚至挂住,不该拖累应用启动。
type Service struct {
	mu      sync.Mutex
	servers []Server
	loaded  bool
	// conns 已建立的连接,key 是 server id
	conns map[string]*client
	// status 每个服务器最近一次连接的结果
	status map[string]*Status
}

func New() *Service {
	return &Service{
		conns:  map[string]*client{},
		status: map[string]*Status{},
	}
}

func (s *Service) ensureLoaded() error {
	if s.loaded {
		return nil
	}
	list, err := loadServers()
	if err != nil {
		return err
	}
	s.servers = list
	s.loaded = true
	return nil
}

// ---- 配置 CRUD ----

// ListServers 列出所有服务器(按创建时间正序,顺序稳定便于 UI)
func (s *Service) ListServers() ([]Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	out := make([]Server, len(s.servers))
	copy(out, s.servers)
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out, nil
}

// SaveServer 新增或更新。改动会断开旧连接 —— 配置变了,连着的那个已经不是它了。
func (s *Service) SaveServer(in Server) (Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return Server{}, err
	}
	if in.Name == "" {
		return Server{}, fmt.Errorf("名称不能为空")
	}
	if in.Kind == "" {
		in.Kind = TransportStdio
	}
	now := time.Now().UnixMilli()
	if in.ID == "" {
		in.ID = uuid.NewString()
		in.CreatedAt = now
		in.UpdatedAt = now
		s.servers = append(s.servers, in)
	} else {
		idx := s.indexOf(in.ID)
		if idx < 0 {
			return Server{}, fmt.Errorf("服务器不存在")
		}
		in.CreatedAt = s.servers[idx].CreatedAt
		in.UpdatedAt = now
		s.servers[idx] = in
		s.disconnectLocked(in.ID)
	}
	if err := saveServers(s.servers); err != nil {
		return Server{}, err
	}
	return in, nil
}

// DeleteServer 删除并断开连接
func (s *Service) DeleteServer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	idx := s.indexOf(id)
	if idx < 0 {
		return nil
	}
	s.disconnectLocked(id)
	s.servers = append(s.servers[:idx], s.servers[idx+1:]...)
	delete(s.status, id)
	return saveServers(s.servers)
}

// ToggleServer 启用 / 停用。停用会立刻断开,工具随之从模型可见的列表里消失。
func (s *Service) ToggleServer(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	idx := s.indexOf(id)
	if idx < 0 {
		return fmt.Errorf("服务器不存在")
	}
	s.servers[idx].Enabled = enabled
	s.servers[idx].UpdatedAt = time.Now().UnixMilli()
	if !enabled {
		s.disconnectLocked(id)
	}
	return saveServers(s.servers)
}

func (s *Service) indexOf(id string) int {
	for i := range s.servers {
		if s.servers[i].ID == id {
			return i
		}
	}
	return -1
}

// disconnectLocked 断开一个连接;调用方须持锁
func (s *Service) disconnectLocked(id string) {
	if c, ok := s.conns[id]; ok {
		_ = c.close()
		delete(s.conns, id)
	}
	if st, ok := s.status[id]; ok {
		st.Connected = false
		st.ToolCount = 0
	}
}

// ---- 连接与工具 ----

// ListStatus 各服务器的连接状态
func (s *Service) ListStatus() []Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return nil
	}
	out := make([]Status, 0, len(s.servers))
	for _, srv := range s.servers {
		if st, ok := s.status[srv.ID]; ok {
			out = append(out, *st)
			continue
		}
		out = append(out, Status{ServerID: srv.ID})
	}
	return out
}

// Tools 所有已启用服务器提供的工具。
//
// 会按需建连接:没连上的启用中服务器在这里被连上并拉一次工具列表。
// 单个服务器连不上不影响其他的 —— 错误记进 status 供 UI 展示,列表照常返回其余工具。
func (s *Service) Tools(ctx context.Context) []ToolInfo {
	s.mu.Lock()
	if err := s.ensureLoaded(); err != nil {
		s.mu.Unlock()
		return nil
	}
	targets := make([]Server, 0, len(s.servers))
	for _, srv := range s.servers {
		if srv.Enabled {
			targets = append(targets, srv)
		}
	}
	s.mu.Unlock()

	var out []ToolInfo
	for _, srv := range targets {
		tools, err := s.toolsOf(ctx, srv)
		if err != nil {
			continue
		}
		out = append(out, tools...)
	}
	return out
}

// CachedTools 只返回已经连好的服务器的工具,绝不发起连接。
//
// 聊天请求构造工具声明时用这个:那条路径上一毫秒都不该等 —— 冷服务器现连要几十秒,
// 用户的消息会卡住不动。没连上的这一轮就不提供它的工具,预热(Warm)会把它连上,
// 下一轮自然就有了。
func (s *Service) CachedTools() []ToolInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ToolInfo
	for _, c := range s.conns {
		c.mu.Lock()
		out = append(out, c.tools...)
		c.mu.Unlock()
	}
	// conns 是 map,遍历顺序随机;排序保证工具声明顺序稳定,
	// 否则每次请求的 tools 顺序都不同,会白白打断供应商侧的提示词缓存
	sort.Slice(out, func(i, j int) bool { return out[i].QualifiedName < out[j].QualifiedName })
	return out
}

// toolsOf 取一个服务器的工具,必要时先连上
func (s *Service) toolsOf(ctx context.Context, srv Server) ([]ToolInfo, error) {
	s.mu.Lock()
	c, connected := s.conns[srv.ID]
	if connected {
		c.mu.Lock()
		cached := append([]ToolInfo(nil), c.tools...)
		c.mu.Unlock()
		s.mu.Unlock()
		return cached, nil
	}
	s.mu.Unlock()

	connCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	c, err := connect(connCtx, srv)
	if err != nil {
		s.setStatus(srv.ID, &Status{ServerID: srv.ID, Error: err.Error()})
		return nil, err
	}
	tools, err := c.listTools(connCtx, srv)
	if err != nil {
		_ = c.close()
		s.setStatus(srv.ID, &Status{ServerID: srv.ID, Error: err.Error(), ServerInfo: c.serverInfo})
		return nil, err
	}

	s.mu.Lock()
	// 两个 goroutine 同时按需建连时,后到的那个把自己的连接关掉,复用先到的
	if existing, ok := s.conns[srv.ID]; ok {
		s.mu.Unlock()
		_ = c.close()
		existing.mu.Lock()
		cached := append([]ToolInfo(nil), existing.tools...)
		existing.mu.Unlock()
		return cached, nil
	}
	s.conns[srv.ID] = c
	s.status[srv.ID] = &Status{
		ServerID:   srv.ID,
		Connected:  true,
		ServerInfo: c.serverInfo,
		ToolCount:  len(tools),
	}
	s.mu.Unlock()
	return tools, nil
}

func (s *Service) setStatus(id string, st *Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[id] = st
}

// CallTool 按限定名调用一个工具。找不到就报错 —— 那说明模型编了个不存在的名字,
// 或者服务器在这次对话中途被停用了。
func (s *Service) CallTool(ctx context.Context, qualifiedName string, args map[string]any) (string, error) {
	tools := s.Tools(ctx)
	var target *ToolInfo
	for i := range tools {
		if tools[i].QualifiedName == qualifiedName {
			target = &tools[i]
			break
		}
	}
	if target == nil {
		return "", fmt.Errorf("没有名为 %q 的 MCP 工具", qualifiedName)
	}

	s.mu.Lock()
	c, ok := s.conns[target.ServerID]
	s.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("MCP 服务器 %s 未连接", target.ServerName)
	}

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	out, err := c.callTool(callCtx, target.Name, args)
	if err != nil {
		// 连接大概率已经坏了,断掉让下次重连,而不是一直失败
		s.mu.Lock()
		s.disconnectLocked(target.ServerID)
		if st, ok := s.status[target.ServerID]; ok {
			st.Error = err.Error()
		}
		s.mu.Unlock()
		return "", err
	}
	return out, nil
}

// TestServer 连一次、拉一次工具列表就断开,不影响常驻连接。
// 用于配置界面上的"测试"按钮。
func (s *Service) TestServer(ctx context.Context, srv Server) TestResult {
	start := time.Now()
	testCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	c, err := connect(testCtx, srv)
	if err != nil {
		return TestResult{OK: false, Message: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	defer c.close()

	tools, err := c.listTools(testCtx, srv)
	if err != nil {
		return TestResult{
			OK:         false,
			Message:    err.Error(),
			ServerInfo: c.serverInfo,
			DurationMs: time.Since(start).Milliseconds(),
		}
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return TestResult{
		OK:         true,
		ServerInfo: c.serverInfo,
		Tools:      names,
		DurationMs: time.Since(start).Milliseconds(),
	}
}

// Reconnect 强制断开重连一个服务器(改完配置或服务器重启后用)
func (s *Service) Reconnect(ctx context.Context, id string) error {
	s.mu.Lock()
	if err := s.ensureLoaded(); err != nil {
		s.mu.Unlock()
		return err
	}
	idx := s.indexOf(id)
	if idx < 0 {
		s.mu.Unlock()
		return fmt.Errorf("服务器不存在")
	}
	srv := s.servers[idx]
	s.disconnectLocked(id)
	s.mu.Unlock()

	if !srv.Enabled {
		return fmt.Errorf("服务器未启用")
	}
	_, err := s.toolsOf(ctx, srv)
	return err
}

// Warm 在后台把已启用的服务器都连上。
//
// 连接虽然是懒建的,但"懒"不能懒在聊天请求的关键路径上 —— stdio 服务器首次启动
// 常要 npx / uvx 下依赖,几十秒起步,那会让用户发出的第一条消息卡住不动。
// 所以启动时和配置变更后各预热一次,等真要用工具时连接早就在了。
func (s *Service) Warm(ctx context.Context) {
	go func() {
		for _, srv := range s.enabledServers() {
			select {
			case <-ctx.Done():
				return
			default:
			}
			_, _ = s.toolsOf(ctx, srv)
		}
	}()
}

func (s *Service) enabledServers() []Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return nil
	}
	out := make([]Server, 0, len(s.servers))
	for _, srv := range s.servers {
		if srv.Enabled {
			out = append(out, srv)
		}
	}
	return out
}

// Shutdown 应用退出时断开所有连接,把起的进程一并收掉
func (s *Service) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.conns {
		s.disconnectLocked(id)
	}
}
