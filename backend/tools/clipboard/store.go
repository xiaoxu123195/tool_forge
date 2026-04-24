package clipboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Store 内存 + JSON 持久化的剪贴板历史
type Store struct {
	mu        sync.RWMutex
	items     []Item // 倒序：[0] 最新
	config    Config
	dir       string // 数据目录 ~/.toolforge/clipboard
	itemsFile string
	cfgFile   string
	imagesDir string
}

// NewStore 初始化数据目录并加载已有数据
func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".toolforge", "clipboard")
	imagesDir := filepath.Join(dir, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		dir:       dir,
		imagesDir: imagesDir,
		itemsFile: filepath.Join(dir, "items.json"),
		cfgFile:   filepath.Join(dir, "config.json"),
		config:    DefaultConfig(),
	}
	s.loadConfig()
	s.loadItems()
	return s, nil
}

// ImagesDir 返回图片存储目录
func (s *Store) ImagesDir() string {
	return s.imagesDir
}

// loadConfig 读取配置；缺文件时写入默认值
func (s *Store) loadConfig() {
	data, err := os.ReadFile(s.cfgFile)
	if err != nil {
		_ = s.saveConfig()
		return
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	if cfg.Limit <= 0 {
		cfg.Limit = DefaultLimit
	}
	if cfg.MaxTextBytes <= 0 {
		cfg.MaxTextBytes = DefaultMaxTextBytes
	}
	if cfg.MaxImageBytes <= 0 {
		cfg.MaxImageBytes = DefaultMaxImageBytes
	}
	s.config = cfg
}

func (s *Store) saveConfig() error {
	data, err := json.MarshalIndent(s.config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.cfgFile, data, 0o644)
}

func (s *Store) loadItems() {
	data, err := os.ReadFile(s.itemsFile)
	if err != nil {
		return
	}
	var items []Item
	if err := json.Unmarshal(data, &items); err != nil {
		return
	}
	// 兜底：清掉指向不存在文件的图片项
	cleaned := items[:0]
	for _, it := range items {
		if it.Kind == KindImage {
			if _, err := os.Stat(it.ImagePath); err != nil {
				continue
			}
		}
		cleaned = append(cleaned, it)
	}
	sortItems(cleaned)
	s.items = cleaned
}

func (s *Store) saveItems() error {
	data, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.itemsFile, data, 0o644)
}

// sortItems pinned 优先,其次 createdAt 倒序
func sortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Pinned != items[j].Pinned {
			return items[i].Pinned
		}
		return items[i].CreatedAt > items[j].CreatedAt
	})
}

// Snapshot 拷贝一份当前列表（避免外部修改内部数组）
func (s *Store) Snapshot() ListResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Item, len(s.items))
	copy(out, s.items)
	return ListResult{
		Items:         out,
		Enabled:       s.config.Enabled,
		Limit:         s.config.Limit,
		MaxTextBytes:  s.config.MaxTextBytes,
		MaxImageBytes: s.config.MaxImageBytes,
	}
}

// LatestKindContent 返回最新一条该类型的内容（用于去重；text 返回 Text,image 返回 ImagePath）
func (s *Store) LatestKindContent(kind ItemKind) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, it := range s.items {
		if it.Kind == kind {
			if kind == KindText {
				return it.Text
			}
			return it.ImagePath
		}
	}
	return ""
}

// Add 新增一条;触发上限裁剪。返回新增项与是否被采纳（去重时返回 false）
func (s *Store) Add(item Item) (Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 去重：与最新同类型一致则不入库
	for _, it := range s.items {
		if it.Kind == item.Kind {
			if item.Kind == KindText && it.Text == item.Text {
				return it, false
			}
			if item.Kind == KindImage && it.SizeBytes == item.SizeBytes {
				// 图片用大小近似判等（避免读全图比对）；后续做 hash 更准
				return it, false
			}
			break
		}
	}
	s.items = append([]Item{item}, s.items...)
	s.trimLocked()
	_ = s.saveItems()
	return item, true
}

// trimLocked 上限裁剪：保留所有 pinned + 最新若干非 pinned。
// 调用方需持锁。
func (s *Store) trimLocked() {
	limit := s.config.Limit
	if limit <= 0 || len(s.items) <= limit {
		return
	}
	pinned := make([]Item, 0)
	others := make([]Item, 0)
	for _, it := range s.items {
		if it.Pinned {
			pinned = append(pinned, it)
		} else {
			others = append(others, it)
		}
	}
	keepOthers := limit - len(pinned)
	if keepOthers < 0 {
		keepOthers = 0
	}
	if len(others) > keepOthers {
		// 先按时间排序,留前 keepOthers
		sort.SliceStable(others, func(i, j int) bool {
			return others[i].CreatedAt > others[j].CreatedAt
		})
		dropped := others[keepOthers:]
		others = others[:keepOthers]
		// 删被淘汰的图片磁盘文件
		for _, d := range dropped {
			if d.Kind == KindImage && d.ImagePath != "" {
				_ = os.Remove(d.ImagePath)
			}
		}
	}
	merged := append(pinned, others...)
	sortItems(merged)
	s.items = merged
}

// Delete 删除指定 id;若是图片同时清磁盘
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, it := range s.items {
		if it.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	old := s.items[idx]
	s.items = append(s.items[:idx], s.items[idx+1:]...)
	if old.Kind == KindImage && old.ImagePath != "" {
		_ = os.Remove(old.ImagePath)
	}
	return s.saveItems()
}

// TogglePin 切换某条 pin 状态
func (s *Store) TogglePin(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == id {
			s.items[i].Pinned = !s.items[i].Pinned
			break
		}
	}
	sortItems(s.items)
	return s.saveItems()
}

// ClearAll 清空所有非置顶项;若 includePinned 为 true 则全清
func (s *Store) ClearAll(includePinned bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]Item, 0)
	for _, it := range s.items {
		if it.Pinned && !includePinned {
			kept = append(kept, it)
			continue
		}
		if it.Kind == KindImage && it.ImagePath != "" {
			_ = os.Remove(it.ImagePath)
		}
	}
	s.items = kept
	return s.saveItems()
}

// Get 读取单条
func (s *Store) Get(id string) (Item, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, it := range s.items {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// SetConfig 更新配置（持久化）
func (s *Store) SetConfig(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg.Limit <= 0 {
		cfg.Limit = DefaultLimit
	}
	if cfg.MaxTextBytes <= 0 {
		cfg.MaxTextBytes = DefaultMaxTextBytes
	}
	if cfg.MaxImageBytes <= 0 {
		cfg.MaxImageBytes = DefaultMaxImageBytes
	}
	s.config = cfg
	s.trimLocked()
	if err := s.saveConfig(); err != nil {
		return err
	}
	return s.saveItems()
}

// Config 返回当前配置副本
func (s *Store) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}
