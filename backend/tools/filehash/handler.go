package filehash

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// Handler 把文件哈希包成 apiserver.ToolHandler。
//
// 为什么值得暴露给 agent:几百 MB 的文件读不进上下文,但"这两份提取出来的数据
// 是不是同一份"「这个固件的 SHA256 是多少」这类问题,恰恰要靠哈希回答。
// agent 自己起个 shell 也能算,但各平台命令不一样(certutil / shasum / sha256sum),
// 而且一次只能算一种算法。
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string  { return "file-hash" }
func (h *Handler) Title() string { return "文件哈希" }
func (h *Handler) Description() string {
	return "计算本机文件的哈希(md5/sha1/sha256/sha512/crc32),一次读盘算出多种算法"
}
func (h *Handler) Methods() []string { return []string{http.MethodPost} }

type hashRequest struct {
	// Paths 要算的文件绝对路径
	Paths []string `json:"paths"`
	// Algos 算法名;留空用默认组合
	Algos []string `json:"algos,omitempty"`
}

func (h *Handler) Handle(ctx context.Context, body []byte) ([]byte, error) {
	if h.svc == nil {
		return nil, errors.New("filehash service 未初始化")
	}
	var req hashRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, errors.New("请求体不是合法 JSON: " + err.Error())
		}
	}
	if len(req.Paths) == 0 {
		return nil, errors.New("paths 不能为空")
	}
	// 数量卡一下:agent 有时会把整个目录列表一股脑塞进来,
	// 那会变成一次跑几个小时、还没法中途看结果的调用
	if len(req.Paths) > 50 {
		return nil, errors.New("一次最多 50 个文件,当前 " + itoa(len(req.Paths)))
	}
	results := h.svc.HashFiles(ctx, req.Paths, req.Algos)
	return json.Marshal(map[string]any{"results": results})
}

func (h *Handler) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"paths"},
		"properties": map[string]any{
			"paths": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "文件的绝对路径,一次最多 50 个",
			},
			"algos": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "enum": SupportedAlgos},
				"description": "要算的算法;留空用默认组合。一次读盘同时算多种,不必分多次调用",
			},
		},
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
