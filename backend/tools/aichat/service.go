package aichat

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
	ctx        context.Context // wails 启动后注入,用于 EventsEmit
	streams    streamRegistry  // 进行中的会话流(chat.go)
	translates streamRegistry  // 进行中的翻译任务(translate.go)
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
		// 单密钥 → 密钥池。老配置里 apiKey 可能是用户自己用逗号塞的一串,
		// normalizeKeys 会一并拆开;拆不出东西的(没填 key)不写盘,免得每次启动都改文件
		if len(ps[i].APIKeys) == 0 && strings.TrimSpace(ps[i].APIKey) != "" {
			ps[i].APIKeys = normalizeKeys(ps[i])
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
		{
			ID:        "system-anthropic",
			Name:      "Anthropic",
			Type:      TypeAnthropic,
			Logo:      "anthropic",
			BaseURL:   "https://api.anthropic.com",
			APIKey:    "",
			Enabled:   false,
			Models:    []string{},
			IsSystem:  true,
			CreatedAt: now,
			UpdatedAt: now - 3,
		},
		{
			ID:        "system-xai",
			Name:      "xAI Grok",
			Type:      TypeXAI,
			Logo:      "xai",
			BaseURL:   "https://api.x.ai/v1",
			APIKey:    "",
			Enabled:   false,
			Models:    []string{},
			IsSystem:  true,
			CreatedAt: now,
			UpdatedAt: now - 4,
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
	// 拖动排过序的排在前面(SortOrder 从 1 开始);没排过的按 UpdatedAt 倒序跟在后面。
	// 这样"从没拖过"时的顺序和以前完全一致,拖过一次之后才由用户说了算。
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].SortOrder, out[j].SortOrder
		if (a == 0) != (b == 0) {
			return a != 0
		}
		if a != b {
			return a < b
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out, nil
}

// ReorderProviders 按给定的 ID 顺序重排供应商。
//
// 只认列表里出现的 ID,没出现的保持原样 —— 前端拖的是过滤后的可见列表时,
// 不该把搜索框外面那些的顺序也一起冲掉。
func (s *Service) ReorderProviders(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	for i, id := range ids {
		if p, idx := s.getProviderLocked(id); idx >= 0 {
			p.SortOrder = i + 1
		}
	}
	return saveProviders(s.providers)
}

// ModelSpecs 一次返回该供应商所有模型的能力画像。
//
// 逐个调 ModelSpec 也行,但列表里十几个模型就是十几次 IPC 往返;
// 推断本身是纯计算,一次取回来最省事。
func (s *Service) ModelSpecs(providerID string) []ModelSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return nil
	}
	p, idx := s.getProviderLocked(providerID)
	if idx < 0 {
		return nil
	}
	out := make([]ModelSpec, 0, len(p.Models))
	for _, m := range p.Models {
		out = append(out, InferModelSpec(*p, m))
	}
	return out
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
		// 新建时接受调用方带来的密钥;它可能是逗号/换行分隔的一串,拆成池
		p.APIKeys = normalizeKeys(p)
		// 新供应商排在最前。刚建完接着就要填密钥、拉模型,让它掉到二十条开外
		// 等于逼用户自己去找。已排过序的整体后移一位给它腾地方;
		// 一条都没排过时不用动 —— SortOrder 全是 0,按 UpdatedAt 倒序它天然就在最前
		bumped := false
		for i := range s.providers {
			if s.providers[i].SortOrder > 0 {
				s.providers[i].SortOrder++
				bumped = true
			}
		}
		if bumped {
			p.SortOrder = 1
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
		p.SortOrder = s.providers[idx].SortOrder
		// 密钥池归 SaveProviderKeys 独占,这里一律沿用磁盘上的。
		//
		// 前端保存供应商时发的是整个对象,里面的 apiKeys 可能是几秒前读到的旧值 ——
		// 照单全收的话,"改个名字"这种操作会把中间新加的密钥悄悄抹掉。
		// 更糟的是 apiKeys 缺省时会回落到单密钥字段,整个池直接塌成一把。
		p.APIKeys = s.providers[idx].APIKeys
		p.APIKey = s.providers[idx].APIKey
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
	provider := pickKey(*p)
	s.mu.Unlock()
	switch provider.Type {
	case TypeGemini:
		return fetchGeminiModels(provider)
	case TypeAnthropic:
		return fetchAnthropicModels(provider)
	default:
		// openai / openai-compatible / xai 都走 /v1/models
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
	provider := pickKey(*p)
	s.mu.Unlock()
	return testProviderModelWithKey(provider, modelID)
}

// ModelSpec 返回某个模型在指定供应商下的能力画像。
// 前端据此决定给不给思考档位选择器、联网开关,以及能不能拖图 / 传 PDF。
func (s *Service) ModelSpec(providerID, modelID string) (ModelSpec, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return ModelSpec{}, err
	}
	p, idx := s.getProviderLocked(providerID)
	if idx < 0 {
		return ModelSpec{}, fmt.Errorf("供应商不存在: %s", providerID)
	}
	return InferModelSpec(*p, modelID), nil
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
