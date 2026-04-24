// Package clipboard 提供剪贴板监听、历史持久化与回写能力。
package clipboard

// ItemKind 剪贴板条目类型
type ItemKind string

const (
	KindText  ItemKind = "text"
	KindImage ItemKind = "image"
)

// Item 单条剪贴板历史
type Item struct {
	ID          string   `json:"id"`
	Kind        ItemKind `json:"kind"`
	Text        string   `json:"text,omitempty"`        // 文本内容
	Preview     string   `json:"preview,omitempty"`     // 列表展示用的截断预览（前 280 字）
	ImagePath   string   `json:"imagePath,omitempty"`   // 原图磁盘路径
	Thumbnail   string   `json:"thumbnail,omitempty"`   // dataURL 缩略图
	ImageWidth  int      `json:"imageWidth,omitempty"`
	ImageHeight int      `json:"imageHeight,omitempty"`
	SizeBytes   int      `json:"sizeBytes"`
	Pinned      bool     `json:"pinned"`
	CreatedAt   int64    `json:"createdAt"` // unix ms
}

// Config 剪贴板模块配置（持久化）
type Config struct {
	Enabled       bool `json:"enabled"`       // 是否监听
	Limit         int  `json:"limit"`         // 历史条数上限（含置顶；置顶不参与裁剪）
	MaxTextBytes  int  `json:"maxTextBytes"`  // 单条文本最大字节，超过直接丢弃
	MaxImageBytes int  `json:"maxImageBytes"` // 单张图片最大字节，超过直接丢弃
}

// 默认阈值
const (
	DefaultLimit         = 100
	DefaultMaxTextBytes  = 1 * 1024 * 1024  // 1 MB
	DefaultMaxImageBytes = 10 * 1024 * 1024 // 10 MB
)

// DefaultConfig 默认配置
func DefaultConfig() Config {
	return Config{
		Enabled:       true,
		Limit:         DefaultLimit,
		MaxTextBytes:  DefaultMaxTextBytes,
		MaxImageBytes: DefaultMaxImageBytes,
	}
}

// ListResult 给前端的列表响应
type ListResult struct {
	Items         []Item `json:"items"`
	Enabled       bool   `json:"enabled"`
	Limit         int    `json:"limit"`
	MaxTextBytes  int    `json:"maxTextBytes"`
	MaxImageBytes int    `json:"maxImageBytes"`
}
