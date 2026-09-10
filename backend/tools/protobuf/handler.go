package protobuf

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// Handler 把裸解析包成 apiserver.ToolHandler,供本地 API / MCP 调用。
//
// 为什么值得暴露给 agent:protobuf 的裸字节没有 schema 就读不出结构,
// 而 agent 手上拿到一段 SQLite 里抠出来的 blob 时,除了干瞪眼没别的办法。
// 这里给的是 protoc --decode_raw 那一套 —— 不需要 .proto 也能把字段树推出来。
type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

func (h *Handler) Name() string  { return "protobuf-decode" }
func (h *Handler) Title() string { return "Protobuf 裸解析" }
func (h *Handler) Description() string {
	return "无 schema 递归解析 protobuf 字节,返回字段树、protoc --decode_raw 文本,以及反推出的 .proto 骨架"
}
func (h *Handler) Methods() []string { return []string{http.MethodPost} }

func (h *Handler) Handle(_ context.Context, body []byte) ([]byte, error) {
	var in DecodeInput
	if len(body) > 0 {
		if err := json.Unmarshal(body, &in); err != nil {
			return nil, errors.New("请求体不是合法 JSON: " + err.Error())
		}
	}
	if in.Data == "" {
		return nil, errors.New("data 不能为空")
	}
	res, err := DecodeRaw(in)
	if err != nil {
		return nil, err
	}
	return json.Marshal(res)
}

// InputSchema 给 MCP 用。
//
// encoding 的三个取值都写清楚了用途:agent 最常见的来源就是从 SQLite 里
// 抠出来的 X'..' 字面量,直接原样贴进来能省掉一次手工转换。
func (h *Handler) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"data", "encoding"},
		"properties": map[string]any{
			"data": map[string]any{
				"type":        "string",
				"description": "要解析的 protobuf 字节,按 encoding 指定的形式给",
			},
			"encoding": map[string]any{
				"type": "string",
				"enum": []string{"hex", "base64", "blob"},
				"description": "data 的编码:hex 十六进制字符串;base64;" +
					"blob 是 SQLite 里那种 X'0A0B...' 字面量(可以带 X 和引号,原样贴进来即可)",
			},
		},
	}
}
