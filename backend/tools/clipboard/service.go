package clipboard

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"

	clipx "golang.design/x/clipboard"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// EventNew 新增条目时通过 Wails emit 给前端
const EventNew = "clipboard:new"

// Service 暴露给 app.go 的剪贴板服务
type Service struct {
	store  *Store
	ctx    context.Context
	cancel context.CancelFunc
}

// New 创建服务（不启动监听）
func New() (*Service, error) {
	st, err := NewStore()
	if err != nil {
		return nil, err
	}
	return &Service{store: st}, nil
}

// Start 在 Wails startup 后调用,把 ctx 注入并起监听 goroutine
func (s *Service) Start(ctx context.Context) {
	s.ctx = ctx
	mctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go startMonitor(mctx, s.store, func(item Item) {
		wailsruntime.EventsEmit(ctx, EventNew, item)
	})
}

// Stop 停止监听（应用退出时）
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

// List 列出当前历史
func (s *Service) List() ListResult {
	return s.store.Snapshot()
}

// Delete 删除单条
func (s *Service) Delete(id string) error {
	return s.store.Delete(id)
}

// TogglePin 切换 pin 状态
func (s *Service) TogglePin(id string) error {
	return s.store.TogglePin(id)
}

// Clear 清空（不动 pinned）
func (s *Service) Clear() error {
	return s.store.ClearAll(false)
}

// ClearAll 全部清空（含 pinned）
func (s *Service) ClearAll() error {
	return s.store.ClearAll(true)
}

// SetEnabled 切换监听开关
func (s *Service) SetEnabled(enabled bool) error {
	cfg := s.store.Config()
	cfg.Enabled = enabled
	return s.store.SetConfig(cfg)
}

// SetLimit 设置历史上限（会立即按新上限裁剪）
func (s *Service) SetLimit(limit int) error {
	cfg := s.store.Config()
	cfg.Limit = limit
	return s.store.SetConfig(cfg)
}

// SetMaxImageBytes 设置图片单条大小上限（字节）
func (s *Service) SetMaxImageBytes(n int) error {
	cfg := s.store.Config()
	cfg.MaxImageBytes = n
	return s.store.SetConfig(cfg)
}

// GetConfig 读取当前配置
func (s *Service) GetConfig() Config {
	return s.store.Config()
}

// CopyText 把指定文本写回系统剪贴板（不再触发监听器入库,因为去重逻辑会拦掉）
func (s *Service) CopyText(text string) error {
	if err := clipx.Init(); err != nil {
		return err
	}
	clipx.Write(clipx.FmtText, []byte(text))
	return nil
}

// CopyItem 把指定历史条目写回剪贴板
func (s *Service) CopyItem(id string) error {
	it, ok := s.store.Get(id)
	if !ok {
		return fmt.Errorf("item not found: %s", id)
	}
	if err := clipx.Init(); err != nil {
		return err
	}
	if it.Kind == KindText {
		clipx.Write(clipx.FmtText, []byte(it.Text))
		return nil
	}
	data, err := os.ReadFile(it.ImagePath)
	if err != nil {
		return err
	}
	clipx.Write(clipx.FmtImage, data)
	return nil
}

// GetImage 返回指定图片项的原图 dataURL（用于查看大图）
func (s *Service) GetImage(id string) (string, error) {
	it, ok := s.store.Get(id)
	if !ok {
		return "", fmt.Errorf("item not found: %s", id)
	}
	if it.Kind != KindImage {
		return "", fmt.Errorf("not an image: %s", id)
	}
	data, err := os.ReadFile(it.ImagePath)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
}

// LogStartup 日志辅助（不暴露给前端）
func (s *Service) LogStartup() {
	cfg := s.store.Config()
	log.Printf("[clipboard] started, enabled=%v limit=%d existing=%d",
		cfg.Enabled, cfg.Limit, len(s.store.Snapshot().Items))
}
