package mmkv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
)

// Handler 把 MMKV 解析包成 apiserver.ToolHandler。
//
// 为什么值得暴露给 agent:MMKV 是腾讯那套私有的键值存储,值不带类型标记,
// 光看字节读不出东西。agent 从设备里提到一个 .mmkv 文件时,没有这个工具
// 就只能干瞪眼 —— 而它恰恰是微信一类应用存配置的地方。
type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

func (h *Handler) Name() string  { return "mmkv-parse" }
func (h *Handler) Title() string { return "MMKV 解析" }
func (h *Handler) Description() string {
	return "解析腾讯 MMKV 文件,列出键和值;值不带类型标记,所以每个值会给出所有解得通的类型和一个最可能的判断。加密文件需要同时给 .crc 路径和 AES key"
}
func (h *Handler) Methods() []string { return []string{http.MethodPost} }

type parseRequest struct {
	// Path .mmkv 文件路径
	Path string `json:"path"`
	// CrcPath 配套的 .crc 路径;只有加密文件需要(IV 存在里面)
	CrcPath string `json:"crcPath,omitempty"`
	// Key AES key,十六进制
	Key string `json:"key,omitempty"`
	// KeyFilter 只返回键名包含这段文字的条目;文件大时用它收窄
	KeyFilter string `json:"keyFilter,omitempty"`
	// MaxEntries 最多返回多少条,默认 200
	MaxEntries int `json:"maxEntries,omitempty"`
}

// defaultMaxEntries 一次最多回多少条。
// 微信那种 MMKV 有几千个键,全丢给 agent 会把它的上下文占满,
// 而它多半只关心其中一两个
const defaultMaxEntries = 200

func (h *Handler) Handle(_ context.Context, body []byte) ([]byte, error) {
	var req parseRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, errors.New("请求体不是合法 JSON: " + err.Error())
		}
	}
	if req.Path == "" {
		return nil, errors.New("path 不能为空")
	}
	data, err := os.ReadFile(req.Path)
	if err != nil {
		return nil, err
	}

	// 给了 key 就必须给 .crc:IV 在那里面,只给 key 是解不开的,
	// 而解不开的表现是"解析成功但内容全是乱码",比直接报错难查得多
	if req.Key != "" || req.CrcPath != "" {
		if req.Key == "" || req.CrcPath == "" {
			return nil, errors.New("解密需要同时给 key 和 crcPath ——" +
				"IV 存在 .crc 文件里,缺一个都解不开")
		}
		crc, err := os.ReadFile(req.CrcPath)
		if err != nil {
			return nil, err
		}
		data, err = Decrypt(data, crc, req.Key)
		if err != nil {
			return nil, err
		}
	}

	res, err := Parse(data)
	if err != nil {
		return nil, err
	}
	filtered, truncated := limitEntries(res.Entries, req.KeyFilter, req.MaxEntries)

	return json.Marshal(map[string]any{
		"dbSize":       res.DBSize,
		"consumed":     res.Consumed,
		"removedCount": res.RemovedCount,
		"totalKeys":    len(res.Entries),
		"returnedKeys": len(filtered),
		"truncated":    truncated,
		"entries":      filtered,
	})
}

// limitEntries 按关键字筛选并截断
func limitEntries(all []Entry, filter string, max int) ([]Entry, bool) {
	if max <= 0 {
		max = defaultMaxEntries
	}
	out := make([]Entry, 0, min(len(all), max))
	matched := 0
	for _, e := range all {
		if filter != "" && !containsFold(e.Key, filter) {
			continue
		}
		matched++
		if len(out) < max {
			out = append(out, e)
		}
	}
	return out, matched > len(out)
}

func containsFold(s, sub string) bool {
	return len(sub) == 0 || indexFold(s, sub) >= 0
}

func indexFold(s, sub string) int {
	ls, lsub := lower(s), lower(sub)
	for i := 0; i+len(lsub) <= len(ls); i++ {
		if ls[i:i+len(lsub)] == lsub {
			return i
		}
	}
	return -1
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}

func (h *Handler) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"path"},
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "MMKV 文件的绝对路径(通常没有扩展名,和一个同名 .crc 文件放在一起)",
			},
			"crcPath": map[string]any{
				"type": "string",
				"description": "配套 .crc 文件的绝对路径。只有加密的 MMKV 需要 ——" +
					"解密用的 IV 存在这个文件里,不是存在 .mmkv 里",
			},
			"key": map[string]any{
				"type":        "string",
				"description": "AES key,十六进制字符串。只有加密的 MMKV 需要,必须和 crcPath 一起给",
			},
			"keyFilter": map[string]any{
				"type":        "string",
				"description": "只返回键名包含这段文字的条目(不分大小写)。几千个键的文件用它收窄",
			},
			"maxEntries": map[string]any{
				"type":        "integer",
				"description": "最多返回多少条,默认 200。返回里的 truncated 会告诉你有没有截断",
			},
		},
	}
}
