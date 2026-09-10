package forensic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"tool_forge/backend/apiserver"
)

// StreamHandler 把 forensic.Service 包成 apiserver.StreamHandler。
// 客户端 POST 一次 → server SSE 返回:
//
//	data: {"type":"log","data":{...}}
//	data: {"type":"log","data":{...}}
//	...
//	data: {"type":"done","data":{...}}
//
// 客户端关闭连接(curl Ctrl+C / EventSource.close)→ ctx 取消 → handler
// 通过 Service.Cancel 杀掉 go-forensic 进程,优雅退出。
type StreamHandler struct {
	svc *Service
}

// NewStreamHandler 由 app.go 在启动时构造,共享同一个 Service 实例。
func NewStreamHandler(svc *Service) *StreamHandler {
	return &StreamHandler{svc: svc}
}

// ====== ToolHandler 元信息 ======

func (h *StreamHandler) Name() string  { return "mobile-forensic" }
func (h *StreamHandler) Title() string { return "移动取证" }
func (h *StreamHandler) Description() string {
	return "抽取移动 App 数据(Android/iOS);Android 走内置 adb 实现,iOS 调 go-forensic CLI;SSE 流式返回日志,关闭连接即取消"
}
func (h *StreamHandler) Methods() []string { return []string{http.MethodPost} }

// Handle 不支持同步调用(用 405 提示客户端走流式)
func (h *StreamHandler) Handle(ctx context.Context, body []byte) ([]byte, error) {
	return nil, errors.New("mobile-forensic 只支持 SSE 流式调用,请用支持 SSE 的客户端(curl -N 或 EventSource)")
}

// ====== StreamHandler ======

type runRequest struct {
	// Args 完整的 go-forensic CLI 参数,例如 ["android","export","-k","wechat","-o","/tmp/out"]
	Args []string `json:"args"`
}

func (h *StreamHandler) HandleStream(
	ctx context.Context,
	body []byte,
	emit func(apiserver.StreamEvent) error,
) error {
	if h.svc == nil {
		return errors.New("forensic service not initialized")
	}
	var req runRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			return errors.New("invalid JSON body: " + err.Error())
		}
	}
	if len(req.Args) == 0 {
		return errors.New("args 不能为空,需指定 go-forensic 的 CLI 参数")
	}

	jobID, err := h.svc.Run(req.Args)
	if err != nil {
		return err
	}

	// 立刻吐一个 started 事件,客户端能拿到 jobID(虽然 SSE 没法 url cancel,主要是日志可追溯)
	if err := emit(apiserver.StreamEvent{
		Type: "started",
		Data: map[string]string{"jobId": jobID},
	}); err != nil {
		// 写不出去就是客户端断了,杀 job 后返回
		_ = h.svc.Cancel(jobID)
		return nil
	}

	sub, unsub := h.svc.Subscribe(jobID)
	defer unsub()

	for {
		select {
		case env, ok := <-sub:
			if !ok {
				// channel 已关闭(任务结束),正常退出
				return nil
			}
			ev := apiserver.StreamEvent{Type: env.Type}
			switch env.Type {
			case "log":
				ev.Data = env.Log
			case "done":
				ev.Data = env.Done
			}
			if err := emit(ev); err != nil {
				// 客户端断了,杀 job
				_ = h.svc.Cancel(jobID)
				return nil
			}
			if env.Type == "done" {
				return nil
			}
		case <-ctx.Done():
			_ = h.svc.Cancel(jobID)
			return nil
		}
	}
}

// InputSchema 给 MCP 用的入参描述。
//
// 参数是 go-forensic 那套命令行的形状,即使 Android 已经换成内置实现也照旧收 ——
// 两个平台得说同一种话,而且这个 schema 已经定下来,agent 也是照着它写的。
// schema 能给的最有用的东西不是字段类型,而是几条真实可用的样例。
func (h *StreamHandler) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"args"},
		"properties": map[string]any{
			"args": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
				"description": "完整参数,一个参数一个元素(不要把整条命令塞成一个字符串)。" +
					"Android 的 export 默认走内置实现;加 --engine=cli 可以强制改调 go-forensic;" +
					"加 --clear 会在导出前清空输出目录(不可撤销,根目录会被拒绝)",
				"examples": []any{
					[]string{"android", "export", "-k", "wechat", "-o", "/tmp/out"},
					[]string{"android", "export", "--engine=cli", "-k", "wechat", "-o", "/tmp/out"},
					[]string{"ios", "export", "-s", "/private/var/mobile/Library/Mail/", "-o", "/tmp/out"},
				},
			},
		},
	}
}
