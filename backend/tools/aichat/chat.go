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

// chatRequest 一次流式请求的全部输入。协议层只认这个结构,不再各自去猜模型能力。
type chatRequest struct {
	Provider Provider
	Conv     Conversation
	Spec     ModelSpec
}

// streamCallbacks 各协议实现统一通过这个回调向上推数据
type streamCallbacks struct {
	onText     func(string)
	onThinking func(string) // 思考增量,只用于前端实时渲染
	// onThinkingBlock 一个完整思考块结束时调用。与 onThinking 是两条独立的路:
	// 前者给前端看,这里的块要连同 signature 一起落盘,下一轮原样回传给模型。
	onThinkingBlock func(ThinkingBlock)
	onImage         func(ImageBlock) // 模型生成的图片(DALL-E / Gemini imagen / grok-imagine 等)
	onCitation      func(Citation)   // 联网搜索引用到的来源
	// onSearch 供应商内置联网搜索发出了一次检索。同一个 Query 会推两次:
	// 发出时 Status=running,拿到结果后再推一次 done —— 前端据此把"正在搜索"变成"已搜索"
	onSearch func(SearchQuery)
	// onToolCall 模型请求调用一个本地工具。协议层在流结束时一次性抛出(参数是分片到达的,
	// 拼完才知道完整形态);runStream 收齐后执行,再带着结果发下一轮
	onToolCall func(ToolCall)
	onUsage    func(Usage) // 各协议在拿到 usage 时(可能多次)调用,runStream 取最新非零值
	// onTruncated 供应商明确说了"这条是被 token 上限截断的"(而不是模型自己说完了)。
	//
	// 四家的说法各不相同:chat-completions 是 finish_reason=length,
	// responses 是 status=incomplete + incomplete_details.reason=max_output_tokens,
	// Anthropic 是 stop_reason=max_tokens,Gemini 是 finishReason=MAX_TOKENS。
	// 统一成一个"发生了"的信号 —— 调用方只关心要不要给「继续写」,不关心谁怎么叫它。
	onTruncated func()
	onDone      func()
	onError     func(error)
}

// withDefaults 把没设置的回调补成空实现。协议层可以无脑调用而不用逐个判空,
// 以后再加回调也不会让老调用方(如翻译工具)因为漏设一个而 panic。
func (c streamCallbacks) withDefaults() streamCallbacks {
	if c.onText == nil {
		c.onText = func(string) {}
	}
	if c.onThinking == nil {
		c.onThinking = func(string) {}
	}
	if c.onThinkingBlock == nil {
		c.onThinkingBlock = func(ThinkingBlock) {}
	}
	if c.onImage == nil {
		c.onImage = func(ImageBlock) {}
	}
	if c.onCitation == nil {
		c.onCitation = func(Citation) {}
	}
	if c.onSearch == nil {
		c.onSearch = func(SearchQuery) {}
	}
	if c.onToolCall == nil {
		c.onToolCall = func(ToolCall) {}
	}
	if c.onUsage == nil {
		c.onUsage = func(Usage) {}
	}
	if c.onTruncated == nil {
		c.onTruncated = func() {}
	}
	if c.onDone == nil {
		c.onDone = func() {}
	}
	if c.onError == nil {
		c.onError = func(error) {}
	}
	return c
}

// 事件名前缀(前端按 conversation id 拼后缀订阅)
const (
	EventChunkPrefix    = "ai-chat:chunk:"    // 正文增量
	EventThinkingPrefix = "ai-chat:thinking:" // 思考增量(deepseek-r1 / o1 / claude extended)
	EventImagePrefix    = "ai-chat:image:"    // 模型生成的图片(payload = ImageBlock)
	EventCitationPrefix = "ai-chat:citation:" // 联网引用来源(payload = Citation)
	EventSearchPrefix   = "ai-chat:search:"   // 供应商联网检索词(payload = SearchQuery)
	EventToolPrefix     = "ai-chat:tool:"     // 工具调用及其结果(payload = ToolCall)
	EventDonePrefix     = "ai-chat:done:"
	EventErrorPrefix    = "ai-chat:error:"
	// EventTitlePrefix 自动起的标题(payload = 新标题)。它比 done 晚到几秒 ——
	// 起标题是流结束之后另发的一次请求,不能让用户为了一个标题多等
	EventTitlePrefix = "ai-chat:title:"
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
func (s *Service) SendChat(ctx context.Context, convID, userContent string, userImages []ImageBlock, userFiles []FileBlock) (*Conversation, error) {
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
	// 允许"只发附件、不带文字"——文本/图/文件至少其一
	if strings.TrimSpace(userContent) == "" && len(userImages) == 0 && len(userFiles) == 0 {
		return nil, fmt.Errorf("消息不能为空")
	}

	// PDF 后端兜底:chat-completions 端点没有原生文件入参,把 PDF 二进制当场提取成文本附进 Files.Text
	userFiles = ensureFileText(endpointFor(prov.Type), userFiles)

	now := time.Now().UnixMilli()
	userMsg := Message{
		ID:        uuid.NewString(),
		Role:      "user",
		Content:   userContent,
		Images:    userImages,
		Files:     userFiles,
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
		// 先用首条消息截个标题顶上,列表里立刻能认出来;
		// 首轮答完后 maybeAutoTitle 会拿模型起的换掉它
		c.Title = autoTitle(userContent)
		c.TitleAuto = true
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
	last.Thinking = nil
	last.Citations = nil
	last.Searches = nil
	last.ToolCalls = nil
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

// ContinueLast 接着写最后一条被截断的 assistant 消息。
//
// 和"重新生成"的区别:重新生成是丢掉重来,这里是保留已有的半截继续往下写。
// 长回答被 max_tokens 截断、或者用户手滑点了停止时,前者等于白等一遍。
func (s *Service) ContinueLast(ctx context.Context, convID string) (*Conversation, error) {
	c, err := loadConversation(convID)
	if err != nil {
		return nil, err
	}
	if len(c.Messages) == 0 {
		return nil, fmt.Errorf("没有可继续的消息")
	}
	last := &c.Messages[len(c.Messages)-1]
	if last.Role != RoleAssistant {
		return nil, fmt.Errorf("最后一条不是助手消息")
	}
	if strings.TrimSpace(last.Content) == "" {
		return nil, fmt.Errorf("这条回复还没有内容,请改用重新生成")
	}
	prov, err := s.providerSnapshot(c.ProviderID)
	if err != nil {
		return nil, err
	}
	if !prov.Enabled {
		return nil, fmt.Errorf("供应商 %s 未启用", prov.Name)
	}

	s.cancelStream(convID)
	last.Truncated = false
	c.UpdatedAt = time.Now().UnixMilli()
	if err := saveConversation(c); err != nil {
		return nil, err
	}

	convCopy := *c
	go s.runStream(ctx, prov, convCopy, last.ID, last.Content)
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
// runStream 跑一次完整的流式回复。
//
// continueFrom 非空表示"接着写":这条 assistant 消息已经有一截内容了,新产出要接在它后面。
// 请求里最后一条就是这半截 assistant 消息,模型看到它会自然往下续 —— Anthropic 管这叫
// prefill,其余几家虽然没给它起名字,行为是一样的。
func (s *Service) runStream(parent context.Context, prov Provider, conv Conversation, asstMsgID, continueFrom string) {
	ctx, cancel := context.WithCancel(parent)
	s.streams.set(conv.ID, cancel)
	defer s.streams.clear(conv.ID)
	defer cancel()

	spec := InferModelSpec(prov, conv.ModelID)
	req := chatRequest{Provider: prov, Conv: conv, Spec: spec}

	startTime := time.Now()
	var bText, bThink strings.Builder
	// 续写:把已有内容垫进累加器,落盘时才是完整的一条。
	// 注意只垫累加器不补发事件 —— 那段内容前端早就显示着了,再发一遍会重复
	if continueFrom != "" {
		bText.WriteString(continueFrom)
	}
	var accumUsage Usage
	var accumImages []ImageBlock
	var accumThinking []ThinkingBlock
	var accumCitations []Citation
	// accumSearches 供应商内置联网搜索发出过的检索词;按 Query 去重后落盘
	var accumSearches []SearchQuery
	// roundCalls 本轮模型请求的工具调用;每轮开始前清空
	var roundCalls []ToolCall
	// lengthCapped 这一轮的回复是被 token 上限截断的。
	// 每轮(以及换密钥重来时)清零 —— 只有最后一轮的结论才代表这条消息的最终状态
	var lengthCapped bool
	// accumToolCalls 整次提问里所有轮次的调用+结果,落盘用
	var accumToolCalls []ToolCall
	// finalThinking 落盘用的思考块。协议层能给出带 signature 的完整块时以它为准;
	// 给不出(多数协议只有纯文本增量)就把累加的文本合成一个块,保证不丢内容。
	finalThinking := func() []ThinkingBlock {
		if len(accumThinking) > 0 {
			return accumThinking
		}
		if t := bThink.String(); t != "" {
			return []ThinkingBlock{{Text: t}}
		}
		return nil
	}
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
		onThinkingBlock: func(b ThinkingBlock) {
			if b.Text == "" && b.Signature == "" && b.Redacted == "" {
				return
			}
			accumThinking = append(accumThinking, b)
		},
		onImage: func(img ImageBlock) {
			if img.Data == "" && img.URL == "" {
				return
			}
			accumImages = append(accumImages, img)
			if s.ctx != nil {
				wailsruntime.EventsEmit(s.ctx, EventImagePrefix+conv.ID, img)
			}
		},
		onToolCall: func(tc ToolCall) {
			roundCalls = append(roundCalls, tc)
		},
		onCitation: func(c Citation) {
			if c.URL == "" {
				return
			}
			// 同一条来源在流里会被反复推送(每引用一次一条),这里先去重再往前端发,
			// 免得引用列表里全是重复项
			for _, exist := range accumCitations {
				if exist.URL == c.URL {
					return
				}
			}
			accumCitations = append(accumCitations, c)
			if s.ctx != nil {
				wailsruntime.EventsEmit(s.ctx, EventCitationPrefix+conv.ID, c)
			}
		},
		onSearch: func(q SearchQuery) {
			if strings.TrimSpace(q.Query) == "" {
				return
			}
			// 同一个检索词会推两次(running → done),按 Query 找到原来那条就地更新;
			// 只追加的话前端会看到两条一模一样的"正在搜索"
			found := false
			for i := range accumSearches {
				if accumSearches[i].Query == q.Query {
					accumSearches[i] = q
					found = true
					break
				}
			}
			if !found {
				accumSearches = append(accumSearches, q)
			}
			if s.ctx != nil {
				wailsruntime.EventsEmit(s.ctx, EventSearchPrefix+conv.ID, q)
			}
		},
		onTruncated: func() { lengthCapped = true },
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
			s.persistAssistant(conv.ID, asstMsgID, assistantResult{
				Content:   bText.String(),
				Thinking:  finalThinking(),
				Images:    accumImages,
				Citations: accumCitations,
				Searches:  accumSearches,
				ToolCalls: accumToolCalls,
				Usage:     accumUsage,
				Duration:  time.Since(startTime),
				// 模型自己写到上限停下的,流是正常结束的,但这条确实没写完 ——
				// 同样该给「继续写」,否则用户只能重新生成、把已有的几千字扔掉
				Truncated: lengthCapped,
			})
			writeUsage()
			if s.ctx != nil {
				wailsruntime.EventsEmit(s.ctx, EventDonePrefix+conv.ID, bText.String())
			}
			// 起标题是另发一次请求,放后台跑:done 事件已经发出去了,
			// 界面该出的都出了,不该被这件小事拖着
			go s.maybeAutoTitle(conv.ID)
		},
		onError: func(err error) {
			res := assistantResult{
				Content:   bText.String(),
				Thinking:  finalThinking(),
				Images:    accumImages,
				Citations: accumCitations,
				Searches:  accumSearches,
				ToolCalls: accumToolCalls,
				Usage:     accumUsage,
				Duration:  time.Since(startTime),
				Truncated: true,
			}
			// 用户主动取消(StopAIChat):保留已收到的内容并加截断标记,
			// 不弹错误对话框 — 改走 done 通道
			if ctx.Err() != nil || isCanceledErr(err) {
				s.persistAssistant(conv.ID, asstMsgID, res)
				writeUsage()
				if s.ctx != nil {
					wailsruntime.EventsEmit(s.ctx, EventDonePrefix+conv.ID, bText.String())
				}
				// 主动停止同样能起标题:用户是看够了才按的停止,
				// 已经写出来的几段完全够概括这段对话在谈什么。
				// (真出错那条路不走这里 —— 那时正文往往是空的,起不出东西)
				go s.maybeAutoTitle(conv.ID)
				return
			}
			s.persistAssistant(conv.ID, asstMsgID, res)
			writeUsage()
			if s.ctx != nil {
				wailsruntime.EventsEmit(s.ctx, EventErrorPrefix+conv.ID, err.Error())
			}
		},
	}

	// 工具调用循环。
	//
	// 一轮 = 发一次请求、把流读完。模型这一轮如果只是回答,循环就此结束;如果它请求调工具,
	// 就在本地执行,把「调用 + 结果」追加进消息列表再发下一轮,直到它不再要工具。
	//
	// 之所以能这么写,是因为 streamXxx 是同步的 —— 它读完整条流才返回。
	// 中间轮次的 onDone / onError 要拦下来:onDone 不能提前告诉前端"结束了",
	// onError 则要立刻中止整个循环。正文、思考、用量这些照常往外抛,
	// 用户能看到模型在调工具前说的话。
	// 密钥池:第一把由轮转游标选出,其余的作为失败后的备选(见 keys.go)
	attempts := keyAttempts(prov)
	keyIdx := 0
	req.Provider = withKey(prov, attempts[0].Key)

	for round := 0; ; round++ {
		var roundErr error
		roundCB := cb
		roundCB.onDone = func() {}
		roundCB.onError = func(err error) { roundErr = err }

		// 换密钥重试。只有"这一次尝试什么都没吐出来"时才敢重来 ——
		// 已经通过事件推给前端的内容收不回去,重试会让用户看到两遍开头。
		for {
			roundCalls = nil
			roundErr = nil
			lengthCapped = false
			mark := progressMark{
				text: bText.Len(), think: bThink.Len(),
				images: len(accumImages), citations: len(accumCitations),
				searches: len(accumSearches),
			}

			switch spec.Endpoint {
			case EndpointGemini:
				streamGemini(ctx, req, roundCB)
			case EndpointAnthropic:
				streamAnthropic(ctx, req, roundCB)
			case EndpointOpenAIChat:
				streamOpenAI(ctx, req, false, roundCB)
			default:
				streamOpenAI(ctx, req, true, roundCB)
			}

			clean := mark.text == bText.Len() && mark.think == bThink.Len() &&
				mark.images == len(accumImages) && mark.citations == len(accumCitations) &&
				mark.searches == len(accumSearches)
			if roundErr == nil || !clean || keyIdx+1 >= len(attempts) || !isKeyLevelError(roundErr) {
				break
			}
			keyIdx++
			req.Provider = withKey(prov, attempts[keyIdx].Key)
		}

		if roundErr != nil {
			cb.onError(roundErr)
			return
		}
		if len(roundCalls) == 0 {
			break
		}
		if round+1 >= maxToolRounds {
			// 到这儿说明模型停不下来了。已经拿到的正文照常保留,只是不再让它接着调。
			cb.onError(fmt.Errorf("模型连续请求了 %d 轮工具调用仍未给出回答,已中止", maxToolRounds))
			return
		}

		// 先把"要调什么"推给前端,再去执行。工具可能跑好几秒(MCP 冷启动更久),
		// 期间界面上得有东西在动,否则用户只能对着空白等 —— 执行完再推就晚了。
		for _, c := range roundCalls {
			c.Status = ToolStatusRunning
			if s.ctx != nil {
				wailsruntime.EventsEmit(s.ctx, EventToolPrefix+conv.ID, c)
			}
		}

		// 执行并把这一轮的调用 + 结果接进对话,下一轮模型就能看到结果了
		executed := executeToolCalls(ctx, roundCalls)
		accumToolCalls = append(accumToolCalls, executed...)
		for _, c := range executed {
			if s.ctx != nil {
				wailsruntime.EventsEmit(s.ctx, EventToolPrefix+conv.ID, c)
			}
		}
		req.Conv.Messages = append(req.Conv.Messages,
			Message{
				Role:      RoleAssistant,
				Content:   bText.String(),
				Thinking:  finalThinking(),
				ToolCalls: executed,
			},
			Message{Role: RoleTool, ToolCalls: executed},
		)
		// 下一轮的正文要接着写,思考块则各轮独立,不重复回传
		accumThinking = nil
		bThink.Reset()
	}

	// 循环里各轮的 onDone 被拦掉了,真正的结束在这里发
	cb.onDone()
}

// progressMark 一次尝试开始时,各累加器的长度快照。
// 用来回答一个问题:这次尝试到底有没有往用户那边吐过东西?没有才敢换密钥重来。
type progressMark struct {
	text, think                 int
	images, citations, searches int
}

// assistantResult 一次流跑完(或中断)后要落盘的 assistant 消息内容
type assistantResult struct {
	Content   string
	Thinking  []ThinkingBlock
	Images    []ImageBlock
	Citations []Citation
	Searches  []SearchQuery
	ToolCalls []ToolCall
	Usage     Usage
	Duration  time.Duration
	// Truncated 流被中断(用户取消 / 出错),正文尾部加省略号标记
	Truncated bool
}

// persistAssistant 流结束时把 assistant 消息写回磁盘
func (s *Service) persistAssistant(convID, msgID string, res assistantResult) {
	c, err := loadConversation(convID)
	if err != nil {
		return
	}
	for i := range c.Messages {
		if c.Messages[i].ID == msgID {
			c.Messages[i].Content = res.Content
			c.Messages[i].Thinking = res.Thinking
			if len(res.Images) > 0 {
				c.Messages[i].Images = res.Images
			}
			if len(res.Citations) > 0 {
				c.Messages[i].Citations = dedupeCitations(res.Citations)
			}
			if len(res.Searches) > 0 {
				c.Messages[i].Searches = res.Searches
			}
			if len(res.ToolCalls) > 0 {
				c.Messages[i].ToolCalls = res.ToolCalls
			}
			if res.Usage != (Usage{}) {
				u := res.Usage
				c.Messages[i].Usage = &u
			}
			if res.Duration > 0 {
				c.Messages[i].DurationMs = int(res.Duration.Milliseconds())
			}
			c.Messages[i].Truncated = res.Truncated
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
	if n > 0 && len(msgs) > n {
		msgs = msgs[len(msgs)-n:]
	}
	// 截断可能把「模型请求调工具」和「工具结果」拆散,只留下后半截。
	// 各家都要求结果消息前面必须有对应的调用,落单的结果会让整个请求被拒 —— 直接丢掉。
	for len(msgs) > 0 && msgs[0].Role == RoleTool {
		msgs = msgs[1:]
	}
	return msgs
}
