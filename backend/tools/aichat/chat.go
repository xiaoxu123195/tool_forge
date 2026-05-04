package aichat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// streamCallbacks 各协议实现统一通过这个回调向上推数据
type streamCallbacks struct {
	onText     func(string)
	onThinking func(string)
	onUsage    func(Usage) // 各协议在拿到 usage 时(可能多次)调用,runStream 取最新非零值
	onDone     func()
	onError    func(error)
}

// 事件名前缀(前端按 conversation id 拼后缀订阅)
const (
	EventChunkPrefix    = "ai-chat:chunk:"    // 正文增量
	EventThinkingPrefix = "ai-chat:thinking:" // 思考增量(deepseek-r1 / o1 / claude extended)
	EventDonePrefix     = "ai-chat:done:"
	EventErrorPrefix    = "ai-chat:error:"
)

// streamRegistry 维护正在进行中的流的取消函数,key 是 conversationID
type streamRegistry struct {
	mu sync.Mutex
	m  map[string]context.CancelFunc
}

func (r *streamRegistry) set(id string, c context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m == nil {
		r.m = map[string]context.CancelFunc{}
	}
	// 同一会话先取消旧的(避免双发)
	if old := r.m[id]; old != nil {
		old()
	}
	r.m[id] = c
}

func (r *streamRegistry) clear(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, id)
}

func (r *streamRegistry) cancel(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.m[id]
	if !ok {
		return false
	}
	c()
	delete(r.m, id)
	return true
}

func (s *Service) cancelStream(id string) bool {
	return s.streams.cancel(id)
}

// CancelStream 取消指定会话的进行中流;true=成功取消
func (s *Service) CancelStream(id string) bool {
	return s.cancelStream(id)
}

// SendChat 在指定会话里追加一条 user 消息并启动流式回复。
//  1. 校验 provider/model
//  2. 把 user 消息写入磁盘,创建一条空的 assistant 消息作为占位
//  3. 启动 goroutine 推流;每个 chunk 通过 wails 事件下发,并实时累加到内存
//  4. 流结束(或失败/取消)时,把最终 assistant 消息写回磁盘
//
// 返回的 Conversation 是"刚追加完 user + 空 assistant"的状态,前端拿到后立刻渲染,
// 然后监听三种事件来更新 assistant.content
func (s *Service) SendChat(ctx context.Context, convID, userContent string) (*Conversation, error) {
	c, err := loadConversation(convID)
	if err != nil {
		return nil, err
	}
	prov, err := s.providerSnapshot(c.ProviderID)
	if err != nil {
		return nil, err
	}
	if !prov.Enabled {
		return nil, fmt.Errorf("供应商 %s 未启用", prov.Name)
	}
	if c.ModelID == "" {
		return nil, fmt.Errorf("会话未指定模型")
	}
	if strings.TrimSpace(userContent) == "" {
		return nil, fmt.Errorf("消息不能为空")
	}

	now := time.Now().UnixMilli()
	userMsg := Message{
		ID:        uuid.NewString(),
		Role:      "user",
		Content:   userContent,
		CreatedAt: now,
	}
	asstMsg := Message{
		ID:        uuid.NewString(),
		Role:      "assistant",
		Content:   "",
		Model:     c.ModelID, // 记录这条 assistant 用的模型
		CreatedAt: now + 1,
	}
	c.Messages = append(c.Messages, userMsg, asstMsg)
	c.UpdatedAt = now
	if c.Title == "" || c.Title == "新对话" {
		c.Title = autoTitle(userContent)
	}
	if err := saveConversation(c); err != nil {
		return nil, err
	}

	// 异步流;复制一份 messages 给 goroutine,避免后续磁盘读写并发
	convCopy := *c
	go s.runStream(ctx, prov, convCopy, asstMsg.ID, userContent)
	return c, nil
}

// RegenerateLast 重新生成最后一条 assistant 消息:
//   1. 取消可能进行中的流
//   2. 把最后一条 assistant 消息的内容/思考清空(沿用同一 ID,前端能原地刷新)
//   3. 重新启动 runStream,流的产物写回这条 assistant
// 要求最后一条是 assistant 且前一条是 user
func (s *Service) RegenerateLast(ctx context.Context, convID string) (*Conversation, error) {
	c, err := loadConversation(convID)
	if err != nil {
		return nil, err
	}
	if len(c.Messages) < 2 {
		return nil, fmt.Errorf("没有可重新生成的消息")
	}
	last := &c.Messages[len(c.Messages)-1]
	if last.Role != "assistant" {
		return nil, fmt.Errorf("最后一条不是助手消息")
	}
	prev := c.Messages[len(c.Messages)-2]
	if prev.Role != "user" {
		return nil, fmt.Errorf("找不到对应的用户消息")
	}
	prov, err := s.providerSnapshot(c.ProviderID)
	if err != nil {
		return nil, err
	}
	if !prov.Enabled {
		return nil, fmt.Errorf("供应商 %s 未启用", prov.Name)
	}
	if c.ModelID == "" {
		return nil, fmt.Errorf("会话未指定模型")
	}

	s.cancelStream(convID)

	now := time.Now().UnixMilli()
	last.Content = ""
	last.Thinking = ""
	last.Model = c.ModelID
	last.CreatedAt = now
	c.UpdatedAt = now
	if err := saveConversation(c); err != nil {
		return nil, err
	}

	convCopy := *c
	go s.runStream(ctx, prov, convCopy, last.ID, prev.Content)
	return c, nil
}

// EditAndResend 编辑某条 user 消息内容,截断它之后的所有消息,然后重新发起流。
//
//	常用场景:用户发完消息后发现写错了,改一下重答
func (s *Service) EditAndResend(ctx context.Context, convID, msgID, newContent string) (*Conversation, error) {
	if strings.TrimSpace(newContent) == "" {
		return nil, fmt.Errorf("消息不能为空")
	}
	c, err := loadConversation(convID)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i := range c.Messages {
		if c.Messages[i].ID == msgID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("消息不存在")
	}
	if c.Messages[idx].Role != "user" {
		return nil, fmt.Errorf("只能编辑用户消息")
	}
	prov, err := s.providerSnapshot(c.ProviderID)
	if err != nil {
		return nil, err
	}
	if !prov.Enabled {
		return nil, fmt.Errorf("供应商 %s 未启用", prov.Name)
	}
	if c.ModelID == "" {
		return nil, fmt.Errorf("会话未指定模型")
	}

	s.cancelStream(convID)

	now := time.Now().UnixMilli()
	c.Messages[idx].Content = newContent
	c.Messages[idx].CreatedAt = now
	c.Messages = c.Messages[:idx+1] // 截断后续

	asstMsg := Message{
		ID:        uuid.NewString(),
		Role:      "assistant",
		Content:   "",
		Model:     c.ModelID,
		CreatedAt: now + 1,
	}
	c.Messages = append(c.Messages, asstMsg)
	c.UpdatedAt = now
	if err := saveConversation(c); err != nil {
		return nil, err
	}

	convCopy := *c
	go s.runStream(ctx, prov, convCopy, asstMsg.ID, newContent)
	return c, nil
}

func (s *Service) providerSnapshot(id string) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return Provider{}, err
	}
	p, idx := s.getProviderLocked(id)
	if idx < 0 {
		return Provider{}, fmt.Errorf("供应商不存在: %s", id)
	}
	return *p, nil
}

// runStream 单个会话的流执行体;一定会发 done 或 error 中的一个,然后清理 registry
func (s *Service) runStream(parent context.Context, prov Provider, conv Conversation, asstMsgID, _ string) {
	ctx, cancel := context.WithCancel(parent)
	s.streams.set(conv.ID, cancel)
	defer s.streams.clear(conv.ID)
	defer cancel()

	startTime := time.Now()
	var bText, bThink strings.Builder
	var accumUsage Usage
	writeUsage := func() {
		_ = appendUsageRecord(UsageRecord{
			Ts:              time.Now().UnixMilli(),
			ConvID:          conv.ID,
			ProviderID:      prov.ID,
			ProviderName:    prov.Name,
			Model:           conv.ModelID,
			InputTokens:     accumUsage.InputTokens,
			OutputTokens:    accumUsage.OutputTokens,
			ReasoningTokens: accumUsage.ReasoningTokens,
			CachedTokens:    accumUsage.CachedTokens,
			DurationMs:      time.Since(startTime).Milliseconds(),
		})
	}
	cb := streamCallbacks{
		onText: func(d string) {
			if d == "" {
				return
			}
			bText.WriteString(d)
			if s.ctx != nil {
				wailsruntime.EventsEmit(s.ctx, EventChunkPrefix+conv.ID, d)
			}
		},
		onThinking: func(d string) {
			if d == "" {
				return
			}
			bThink.WriteString(d)
			if s.ctx != nil {
				wailsruntime.EventsEmit(s.ctx, EventThinkingPrefix+conv.ID, d)
			}
		},
		onUsage: func(u Usage) {
			// 同一次请求 usage 可能多次到达(Anthropic message_start 给 input、
			// message_delta 给 output);取每个字段的最新非零值即可
			if u.InputTokens > 0 {
				accumUsage.InputTokens = u.InputTokens
			}
			if u.OutputTokens > 0 {
				accumUsage.OutputTokens = u.OutputTokens
			}
			if u.ReasoningTokens > 0 {
				accumUsage.ReasoningTokens = u.ReasoningTokens
			}
			if u.CachedTokens > 0 {
				accumUsage.CachedTokens = u.CachedTokens
			}
		},
		onDone: func() {
			s.persistAssistant(conv.ID, asstMsgID, bText.String(), bThink.String(), false)
			writeUsage()
			if s.ctx != nil {
				wailsruntime.EventsEmit(s.ctx, EventDonePrefix+conv.ID, bText.String())
			}
		},
		onError: func(err error) {
			// 用户主动取消(StopAIChat):保留已收到的内容并加截断标记,
			// 不弹错误对话框 — 改走 done 通道
			if ctx.Err() != nil || isCanceledErr(err) {
				s.persistAssistant(conv.ID, asstMsgID, bText.String(), bThink.String(), true)
				writeUsage()
				if s.ctx != nil {
					wailsruntime.EventsEmit(s.ctx, EventDonePrefix+conv.ID, bText.String())
				}
				return
			}
			s.persistAssistant(conv.ID, asstMsgID, bText.String(), bThink.String(), true)
			writeUsage()
			if s.ctx != nil {
				wailsruntime.EventsEmit(s.ctx, EventErrorPrefix+conv.ID, err.Error())
			}
		},
	}

	switch prov.Type {
	case TypeGemini:
		streamGemini(ctx, prov, conv, cb)
	case TypeAnthropic:
		streamAnthropic(ctx, prov, conv, cb)
	case TypeOpenAICompat:
		streamOpenAI(ctx, prov, conv, false, cb)
	default:
		// "openai" 默认走新版 Responses API
		streamOpenAI(ctx, prov, conv, true, cb)
	}
}

// persistAssistant 流结束时把 assistant 消息(正文 + 思考)写回磁盘
func (s *Service) persistAssistant(convID, msgID, content, thinking string, truncated bool) {
	c, err := loadConversation(convID)
	if err != nil {
		return
	}
	for i := range c.Messages {
		if c.Messages[i].ID == msgID {
			c.Messages[i].Content = content
			c.Messages[i].Thinking = thinking
			if truncated {
				c.Messages[i].Content += " …" // 标记中断
			}
			break
		}
	}
	c.UpdatedAt = time.Now().UnixMilli()
	_ = saveConversation(c)
}

// SetWailsContext 让 Service 持有 wails ctx 用于 EventsEmit
func (s *Service) SetWailsContext(ctx context.Context) {
	s.ctx = ctx
}

// isCanceledErr 判断 err 是不是"用户主动取消"造成的(context.Canceled / "已取消" / 包装文本)。
// 各协议 stream 函数在 ctx 被取消时,可能通过 scanner.Err() 间接产出 context canceled,
// 也可能自己显式构造 fmt.Errorf("已取消"),都视为同一种情况。
func isCanceledErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "context canceled") || strings.Contains(msg, "已取消")
}

// contextMessages 计算"参与本次请求"的消息切片:
//   1. 跳过最后一个 role=clear 之前的所有消息(分隔线后才是当前会话上下文)
//   2. 再按 conv.ContextCount 限制最近 N 条 user/assistant
//   3. clear 标记本身从不发给模型,各 build 函数也会显式跳过
//
// system 消息独立处理(各协议 build 函数把 conv.System 放到顶层),不算 contextCount
func contextMessages(conv Conversation) []Message {
	msgs := conv.Messages
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleClear {
			msgs = msgs[i+1:]
			break
		}
	}
	n := conv.ContextCount
	if n <= 0 || len(msgs) <= n {
		return msgs
	}
	return msgs[len(msgs)-n:]
}
