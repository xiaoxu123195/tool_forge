package appsearch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"tool_forge/backend/system"
)

// Handler 把 appsearch.Service 封装成 apiserver.ToolHandler。
// PHPSESSID 从 keyring 读取后注入,前端 / 外部 API 客户端都不需要传。
type Handler struct {
	svc *Service
}

// NewHandler 由 app.go 在启动时构造,共享同一个 Service 实例。
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Name() string  { return "app-search" }
func (h *Handler) Title() string { return "包名搜索" }
func (h *Handler) Description() string {
	return "多源搜索 iOS / Android 应用包名(iTunes / 七麦 / 应用宝 / Google Play)"
}
func (h *Handler) Methods() []string { return []string{http.MethodPost} }

func (h *Handler) Handle(ctx context.Context, body []byte) ([]byte, error) {
	if h.svc == nil {
		return nil, errors.New("appsearch service not initialized")
	}
	var req SearchRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, errors.New("invalid JSON body: " + err.Error())
		}
	}
	// 七麦 Android 源需要 PHPSESSID,从 keyring 注入
	if needsQimaiPhpSessIDLocal(req.Sources) {
		if sid, err := system.GetPassword(KeyringQimaiPhpSessID); err == nil && sid != "" {
			req.SetQimaiPhpSessID(sid)
		}
	}
	resp, err := h.svc.Search(ctx, req)
	if err != nil {
		return nil, err
	}
	return json.Marshal(resp)
}

// 复制 app.go 的同名小函数,避免 backend 内部循环 import。
func needsQimaiPhpSessIDLocal(sources []SourceID) bool {
	for _, s := range sources {
		if s == SourceQimaiAndroid {
			return true
		}
	}
	return false
}

// InputSchema 给 MCP 用的入参描述。
//
// agent 是照着这个决定传什么的,所以每个字段的说明要写成"给不认识这个工具的人看"
// 的样子 —— 光写字段名等于让它猜,猜错就是一次白跑。
func (h *Handler) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"keyword"},
		"properties": map[string]any{
			"keyword": map[string]any{
				"type":        "string",
				"description": "要搜的应用名或关键词,如「微信」「WhatsApp」",
			},
			"sources": map[string]any{
				"type":        "array",
				"description": "指定搜索源;留空则用默认组合。itunes/qimai_ios 查 iOS,qimai_android/yingyongbao/googleplay 查 Android",
				"items": map[string]any{
					"type": "string",
					"enum": []string{"itunes", "qimai_ios", "qimai_android", "yingyongbao", "googleplay"},
				},
			},
			"country": map[string]any{
				"type":        "string",
				"description": "iOS 国家码,如 cn / us / jp / gb;只对 iOS 源有效",
			},
			"market": map[string]any{
				"type":        "integer",
				"description": "Android 厂商市场 ID(仅七麦用):华为=6 应用宝=3 小米=4 OPPO=9 VIVO=8 魅族=7 百度=2 360=1 豌豆荚=5 GooglePlay=10 鸿蒙=11",
			},
			"limit_per_source": map[string]any{
				"type":        "integer",
				"description": "每个源最多返回几条,默认 5,上限 50",
			},
		},
	}
}
