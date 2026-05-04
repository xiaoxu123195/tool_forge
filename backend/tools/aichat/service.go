package aichat

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Service 是 aichat 工具的对外门面;线程安全
type Service struct {
	mu        sync.Mutex
	providers []Provider
	config    Config
	loaded    bool
	ctx       context.Context // wails 启动后注入,用于 EventsEmit
	streams   streamRegistry  // 进行中的会话流(chat.go)
}

// New 构造一个 Service;首次访问时懒加载磁盘内容
func New() (*Service, error) {
	return &Service{}, nil
}

func (s *Service) ensureLoaded() error {
	if s.loaded {
		return nil
	}
	ps, err := loadProviders()
	if err != nil {
		return err
	}
	c, err := loadConfig()
	if err != nil {
		return err
	}
	// 旧版数据补字段:type 默认 openai
	changed := false
	for i := range ps {
		if ps[i].Type == "" {
			ps[i].Type = TypeOpenAI
			changed = true
		}
		// 历史 SiliconFlow 预设之前用 type=openai(走 host fallback 到 /chat/completions),
		// 现在拆出独立的 "openai-compatible";若仍是 openai 则迁移
		if ps[i].ID == "system-siliconflow" && ps[i].Type == TypeOpenAI {
			ps[i].Type = TypeOpenAICompat
			changed = true
		}
	}
	// 系统预设:确保 OpenAI / Gemini / SiliconFlow 始终存在(用户改动会保留,
	// 但若被删过会按 ID 重新补回)
	wantedIDs := map[string]bool{}
	for _, def := range defaultProviders() {
		wantedIDs[def.ID] = true
	}
	exist := map[string]bool{}
	for _, p := range ps {
		if wantedIDs[p.ID] {
			exist[p.ID] = true
		}
	}
	for _, def := range defaultProviders() {
		if !exist[def.ID] {
			ps = append(ps, def)
			changed = true
		}
	}
	if changed {
		_ = saveProviders(ps)
	}
	s.providers = ps
	s.config = c
	s.loaded = true
	return nil
}

// defaultProviders 内置 3 个预设(关闭状态,用户填 key + 启用)
func defaultProviders() []Provider {
	now := time.Now().UnixMilli()
	return []Provider{
		{
			ID:        "system-openai",
			Name:      "OpenAI",
			Type:      TypeOpenAI,
			Logo:      "openai",
			BaseURL:   "https://api.openai.com/v1",
			APIKey:    "",
			Enabled:   false,
			Models:    []string{},
			IsSystem:  true,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        "system-gemini",
			Name:      "Gemini",
			Type:      TypeGemini,
			Logo:      "gemini",
			BaseURL:   "https://generativelanguage.googleapis.com",
			APIKey:    "",
			Enabled:   false,
			Models:    []string{},
			IsSystem:  true,
			CreatedAt: now,
			UpdatedAt: now - 1,
		},
		{
			ID:        "system-siliconflow",
			Name:      "硅基流动",
			Type:      TypeOpenAICompat,
			Logo:      "siliconflow",
			BaseURL:   "https://api.siliconflow.cn/v1",
			APIKey:    "",
			Enabled:   false,
			Models:    []string{},
			IsSystem:  true,
			CreatedAt: now,
			UpdatedAt: now - 2,
		},
	}
}

// ================ Provider CRUD ================

// ListProviders 按 UpdatedAt 倒序返回所有供应商
func (s *Service) ListProviders() ([]Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	out := make([]Provider, len(s.providers))
	copy(out, s.providers)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out, nil
}

// GetProvider 按 ID 取一条;调用方需自行加锁或仅读不写
func (s *Service) getProviderLocked(id string) (*Provider, int) {
	for i := range s.providers {
		if s.providers[i].ID == id {
			return &s.providers[i], i
		}
	}
	return nil, -1
}

// SaveProvider 新增或更新;ID 为空 → 新增
func (s *Service) SaveProvider(p Provider) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return Provider{}, err
	}
	now := time.Now().UnixMilli()
	if p.ID == "" {
		p.ID = uuid.NewString()
		p.CreatedAt = now
		p.UpdatedAt = now
		if p.Type == "" {
			p.Type = TypeOpenAI
		}
		if p.Models == nil {
			p.Models = []string{}
		}
		s.providers = append(s.providers, p)
	} else {
		_, idx := s.getProviderLocked(p.ID)
		if idx < 0 {
			return Provider{}, fmt.Errorf("供应商不存在: %s", p.ID)
		}
		// 保留 createdAt 与 isSystem(系统预设的标志位不允许通过 Save 翻转)
		p.CreatedAt = s.providers[idx].CreatedAt
		p.IsSystem = s.providers[idx].IsSystem
		p.UpdatedAt = now
		if p.Type == "" {
			p.Type = TypeOpenAI
		}
		if p.Models == nil {
			p.Models = []string{}
		}
		s.providers[idx] = p
	}
	if err := saveProviders(s.providers); err != nil {
		return Provider{}, err
	}
	return p, nil
}

// DeleteProvider 删除一条供应商
func (s *Service) DeleteProvider(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	_, idx := s.getProviderLocked(id)
	if idx < 0 {
		return nil
	}
	s.providers = append(s.providers[:idx], s.providers[idx+1:]...)
	return saveProviders(s.providers)
}

// ToggleProvider 切换 enabled
func (s *Service) ToggleProvider(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	p, idx := s.getProviderLocked(id)
	if idx < 0 {
		return fmt.Errorf("供应商不存在: %s", id)
	}
	p.Enabled = enabled
	p.UpdatedAt = time.Now().UnixMilli()
	return saveProviders(s.providers)
}

// FetchModels 按 Provider.Type 路由到对应实现
func (s *Service) FetchModels(providerID string) FetchModelsResult {
	s.mu.Lock()
	if err := s.ensureLoaded(); err != nil {
		s.mu.Unlock()
		return FetchModelsResult{OK: false, Message: err.Error()}
	}
	p, idx := s.getProviderLocked(providerID)
	if idx < 0 {
		s.mu.Unlock()
		return FetchModelsResult{OK: false, Message: "供应商不存在"}
	}
	provider := *p
	s.mu.Unlock()
	switch provider.Type {
	case TypeGemini:
		return fetchGeminiModels(provider)
	case TypeAnthropic:
		return fetchAnthropicModels(provider)
	default:
		// openai 与 openai-compatible 都走 /v1/models
		return fetchModels(provider)
	}
}

// TestProviderModel 按 Provider.Type 路由(发 stream:true 最小请求,首 chunk 即成功)
func (s *Service) TestProviderModel(providerID, modelID string) TestResult {
	s.mu.Lock()
	if err := s.ensureLoaded(); err != nil {
		s.mu.Unlock()
		return TestResult{OK: false, Message: err.Error()}
	}
	p, idx := s.getProviderLocked(providerID)
	if idx < 0 {
		s.mu.Unlock()
		return TestResult{OK: false, Message: "供应商不存在"}
	}
	provider := *p
	s.mu.Unlock()
	switch provider.Type {
	case TypeGemini:
		return testGeminiModel(provider, modelID)
	case TypeAnthropic:
		return testAnthropicModel(provider, modelID)
	case TypeOpenAICompat:
		return testModel(provider, modelID, false)
	default:
		// "openai" 默认走新版 Responses API
		return testModel(provider, modelID, true)
	}
}

// ================ Config ================

// GetConfig 默认助手模型
func (s *Service) GetConfig() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return Config{}, err
	}
	return s.config, nil
}

// SaveConfig 保存默认助手模型
func (s *Service) SaveConfig(c Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	s.config = c
	return saveConfig(c)
}

// ListUsage 返回 usage.jsonl 的全量记录(按时间正序)
func (s *Service) ListUsage() ([]UsageRecord, error) {
	return readUsageRecords()
}

// FindEnabledProvider 给前端的便利方法:校验 providerID 是 enabled 且 model 在其 Models 列表里
func (s *Service) FindEnabledProvider(providerID, modelID string) (*Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	p, idx := s.getProviderLocked(providerID)
	if idx < 0 {
		return nil, fmt.Errorf("供应商不存在")
	}
	if !p.Enabled {
		return nil, fmt.Errorf("供应商未启用")
	}
	found := false
	for _, m := range p.Models {
		if m == modelID {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("供应商未启用模型 %s", modelID)
	}
	cp := *p
	return &cp, nil
}
