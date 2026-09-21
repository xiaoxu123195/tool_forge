package apitool

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Service 管理接口包:导入、增删改、以及把生成的工具挂到本地 API server 上。
//
// 注册用回调而不是直接依赖 apiserver:那边已经依赖了一堆工具包,
// 反过来再依赖回去就成了环。上层(app.go)把两头接起来。

// Registrar 本地 API server 那一侧需要的两个动作
type Registrar interface {
	Register(name string, h any)
	Unregister(name string)
}

// maxSpecBytes 一份 OpenAPI 文档最大读多少。真实文档几百 KB 顶天了
const maxSpecBytes = 16 << 20

type Service struct {
	mu    sync.Mutex
	packs []Pack
	// registered 当前已注册的工具名,删包时按它撤销
	registered map[string][]string
	reg        Registrar
	// secret / saveSecret / dropSecret 系统凭据库的三个口,由上层注入,
	// 便于测试时换成内存实现
	secret     func(key string) (string, error)
	saveSecret func(key, val string) error
	dropSecret func(key string) error
	loaded     bool
}

func New(reg Registrar,
	get func(string) (string, error),
	save func(string, string) error,
	del func(string) error,
) *Service {
	return &Service{
		registered: map[string][]string{},
		reg:        reg,
		secret:     get,
		saveSecret: save,
		dropSecret: del,
	}
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".toolforge", "api-tools.json"), nil
}

func (s *Service) ensureLoaded() error {
	if s.loaded {
		return nil
	}
	p, err := configPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			s.packs = []Pack{}
			s.loaded = true
			return nil
		}
		return err
	}
	var packs []Pack
	if err := json.Unmarshal(data, &packs); err != nil {
		return fmt.Errorf("%s 解析失败: %w", p, err)
	}
	s.packs = packs
	s.loaded = true
	return nil
}

func (s *Service) save() error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.packs, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// ParseSpec 解析一份文档。source 是本地路径或 http(s) 地址
func ParseSpec(source string) (*ParseResult, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, fmt.Errorf("没有指定文档")
	}
	var data []byte
	var err error
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		data, err = fetch(source)
	} else {
		data, err = os.ReadFile(source)
	}
	if err != nil {
		return nil, err
	}
	if len(data) > maxSpecBytes {
		return nil, fmt.Errorf("文档太大(%d 字节)", len(data))
	}
	res, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ParseSpecText 直接解析粘贴进来的文档内容
func ParseSpecText(text string) (*ParseResult, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("内容是空的")
	}
	return Parse([]byte(text))
}

func fetch(u string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("下载文档失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载文档失败: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxSpecBytes+1))
}

// ListPacks 列出所有接口包
func (s *Service) ListPacks() ([]Pack, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	out := make([]Pack, len(s.packs))
	copy(out, s.packs)
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out, nil
}

// SavePack 新增或更新一个包,并重新注册它的工具。
// secret 非空时写进凭据库;为空表示不动已存的那个
func (s *Service) SavePack(in Pack, secret string) (Pack, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return Pack{}, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return Pack{}, fmt.Errorf("名称不能为空")
	}
	if sanitize(in.Name) == "x" {
		return Pack{}, fmt.Errorf("名称里要有字母或数字,它会作为工具名的一部分")
	}
	if strings.TrimSpace(in.BaseURL) == "" {
		return Pack{}, fmt.Errorf("请求地址不能为空")
	}
	if len(in.Ops) == 0 {
		return Pack{}, fmt.Errorf("至少要选一个接口")
	}
	// 同名包会生成同名工具,后者覆盖前者,于是有一个包静悄悄失效
	for _, p := range s.packs {
		if p.ID != in.ID && sanitize(p.Name) == sanitize(in.Name) {
			return Pack{}, fmt.Errorf("已经有一个叫 %q 的接口包了,换个名字", p.Name)
		}
	}

	now := time.Now().UnixMilli()
	if in.ID == "" {
		in.ID = uuid.NewString()
		in.CreatedAt = now
	} else {
		for _, p := range s.packs {
			if p.ID == in.ID {
				in.CreatedAt = p.CreatedAt
				break
			}
		}
	}
	in.UpdatedAt = now

	if secret != "" {
		if s.saveSecret != nil {
			if err := s.saveSecret(keyringKey(in.ID), secret); err != nil {
				return Pack{}, fmt.Errorf("密钥存进系统凭据库失败: %w", err)
			}
		}
		in.Auth.HasSecret = true
	} else if in.Auth.Kind == AuthNone {
		// 关掉认证时把密钥一起清掉,别在凭据库里留一个没人用的
		if s.dropSecret != nil {
			_ = s.dropSecret(keyringKey(in.ID))
		}
		in.Auth.HasSecret = false
	}

	idx := -1
	for i := range s.packs {
		if s.packs[i].ID == in.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.packs = append(s.packs, in)
	} else {
		s.packs[idx] = in
	}
	if err := s.save(); err != nil {
		return Pack{}, err
	}
	s.registerLocked(in)
	return in, nil
}

// DeletePack 删掉一个包:撤销它的工具,清掉凭据
func (s *Service) DeletePack(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	idx := -1
	for i := range s.packs {
		if s.packs[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	s.unregisterLocked(id)
	s.packs = append(s.packs[:idx], s.packs[idx+1:]...)
	if s.dropSecret != nil {
		_ = s.dropSecret(keyringKey(id))
	}
	return s.save()
}

// RegisterAll 启动时把已保存的包全部注册上
func (s *Service) RegisterAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	for _, p := range s.packs {
		s.registerLocked(p)
	}
	return nil
}

// registerLocked 注册一个包的全部工具;调用方须持锁
func (s *Service) registerLocked(p Pack) {
	s.unregisterLocked(p.ID)
	if s.reg == nil {
		return
	}
	names := make([]string, 0, len(p.Ops))
	for _, op := range p.Ops {
		h := NewHandler(p, op, s.secretOf)
		s.reg.Register(h.Name(), h)
		names = append(names, h.Name())
	}
	s.registered[p.ID] = names
}

func (s *Service) unregisterLocked(packID string) {
	if s.reg == nil {
		return
	}
	for _, name := range s.registered[packID] {
		s.reg.Unregister(name)
	}
	delete(s.registered, packID)
}

// secretOf 发请求那一刻才去读凭据库
func (s *Service) secretOf(packID string) string {
	if s.secret == nil {
		return ""
	}
	v, err := s.secret(keyringKey(packID))
	if err != nil {
		return ""
	}
	return v
}

// ToolNamesOf 一个包会生成哪些工具名(界面上要显示,也用于勾选暴露)
func ToolNamesOf(p Pack) []string {
	out := make([]string, 0, len(p.Ops))
	for _, op := range p.Ops {
		out = append(out, ToolName(p.Name, op.ID))
	}
	return out
}
