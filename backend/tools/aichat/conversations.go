package aichat

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ListConversations 列出所有会话(按 UpdatedAt 倒序)
func (s *Service) ListConversations() ([]ConversationSummary, error) {
	d, err := dataDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(d, "conversations"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]ConversationSummary, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		var c Conversation
		if err := readJSON(filepath.Join(d, "conversations", e.Name()), &c); err != nil || c.ID == "" {
			continue
		}
		out = append(out, ConversationSummary{
			ID:           c.ID,
			Title:        c.Title,
			ProviderID:   c.ProviderID,
			ModelID:      c.ModelID,
			UpdatedAt:    c.UpdatedAt,
			MessageCount: len(c.Messages),
		})
	}
	// 没排过的按 UpdatedAt 倒序排在最前,排过的按用户排定的顺序跟在后面。
	//
	// 顺序反过来(排过的在前)会有个坏结果:用户拖过一次之后现有会话全部变成"已排",
	// 之后每新建一个都会掉到列表末尾 —— 刚开的对话反而看不见。现在这样,
	// 新会话总在顶部,用户手工排的那一段稳稳待在下面。
	rank := s.conversationRank()
	sort.SliceStable(out, func(i, j int) bool {
		a, aok := rank[out[i].ID]
		b, bok := rank[out[j].ID]
		if aok != bok {
			return !aok
		}
		if aok && a != b {
			return a < b
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out, nil
}

// conversationRank 会话 ID → 用户排定的位次;没排过的不在表里
func (s *Service) conversationRank() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return nil
	}
	rank := make(map[string]int, len(s.config.ConversationOrder))
	for i, id := range s.config.ConversationOrder {
		rank[id] = i
	}
	return rank
}

// ReorderConversations 按给定顺序重排会话列表。
//
// 只记 ID,不碰会话文件本身。列表里已经不存在的 ID 会被顺手清掉,
// 免得删了几十个会话之后 config 里还留着一堆孤儿。
func (s *Service) ReorderConversations(ids []string) error {
	alive := map[string]bool{}
	if list, err := s.ListConversations(); err == nil {
		for _, c := range list {
			alive[c.ID] = true
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	kept := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] || (len(alive) > 0 && !alive[id]) {
			continue
		}
		seen[id] = true
		kept = append(kept, id)
	}
	s.config.ConversationOrder = kept
	return saveConfig(s.config)
}

// GetConversation 取一条会话(含全部消息)
func (s *Service) GetConversation(id string) (*Conversation, error) {
	if id == "" {
		return nil, fmt.Errorf("会话 ID 不能为空")
	}
	return loadConversation(id)
}

// CreateConversation 新建会话;providerID / modelID 给定后续聊天使用的默认模型;
// system 系统提示词(可空);contextCount 上下文条数(0 = 不限)
func (s *Service) CreateConversation(providerID, modelID, title, system string, contextCount int) (*Conversation, error) {
	// 用户没填标题才允许自动起;填了就是他要的名字,模型不许动
	titleAuto := strings.TrimSpace(title) == ""
	if title == "" {
		title = "新对话"
	}
	if contextCount < 0 {
		contextCount = 0
	}
	now := time.Now().UnixMilli()
	c := &Conversation{
		ID:           uuid.NewString(),
		Title:        title,
		TitleAuto:    titleAuto,
		ProviderID:   providerID,
		ModelID:      modelID,
		System:       system,
		ContextCount: contextCount,
		Messages:     []Message{},
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := saveConversation(c); err != nil {
		return nil, err
	}
	return c, nil
}

// UpdateConversationContext 更新会话的上下文条数(0 = 不限)
func (s *Service) UpdateConversationContext(id string, count int) error {
	c, err := loadConversation(id)
	if err != nil {
		return err
	}
	if count < 0 {
		count = 0
	}
	c.ContextCount = count
	c.UpdatedAt = time.Now().UnixMilli()
	return saveConversation(c)
}

// InsertClearMarker 在会话末尾插入一条"清除上下文"分隔标记;
// 后续请求只发分隔标记之后的消息(标记本身从不发给模型)
func (s *Service) InsertClearMarker(id string) error {
	c, err := loadConversation(id)
	if err != nil {
		return err
	}
	if len(c.Messages) == 0 {
		return nil // 空会话无需分隔
	}
	if last := c.Messages[len(c.Messages)-1]; last.Role == RoleClear {
		return nil // 末尾已经是分隔标记
	}
	now := time.Now().UnixMilli()
	c.Messages = append(c.Messages, Message{
		ID:        uuid.NewString(),
		Role:      RoleClear,
		Content:   "",
		CreatedAt: now,
	})
	c.UpdatedAt = now
	return saveConversation(c)
}

// ConversationMeta 「会话设置」对话框一次提交的全部字段。
// 用结构体而不是一长串位置参数,是因为这里以后还会继续加(采样参数就是这么加进来的)。
type ConversationMeta struct {
	Title        string `json:"title"`
	System       string `json:"system"`
	ContextCount int    `json:"contextCount"`
	// Temperature / TopP 为 nil 表示不指定,由模型自己决定;0 是合法取值,不能拿零值当未设置
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"topP,omitempty"`
	MaxTokens   int      `json:"maxTokens,omitempty"`
}

// UpdateConversationMeta 更新会话元信息(标题 / 系统提示词 / 上下文条数 / 采样参数)
func (s *Service) UpdateConversationMeta(id string, m ConversationMeta) error {
	c, err := loadConversation(id)
	if err != nil {
		return err
	}
	if t := strings.TrimSpace(m.Title); t != "" && t != c.Title {
		c.Title = t
		c.TitleAuto = false // 手工改过名,自动起标题从此绕开这条会话
	}
	c.System = m.System
	if m.ContextCount < 0 {
		m.ContextCount = 0
	}
	c.ContextCount = m.ContextCount
	c.Temperature = m.Temperature
	c.TopP = m.TopP
	if m.MaxTokens < 0 {
		m.MaxTokens = 0
	}
	c.MaxTokens = m.MaxTokens
	c.UpdatedAt = time.Now().UnixMilli()
	return saveConversation(c)
}

// UpdateConversationOptions 更新会话的思考档位 / 联网 / 工具开关。
//
// 这两个是发消息时的即时开关(在输入栏上,不在"编辑会话"对话框里),所以单独一个方法。
// 不动 UpdatedAt —— 它们是偏好而不是活动,拨一下开关就把会话顶到列表最前面太吵。
func (s *Service) UpdateConversationOptions(id, reasoningEffort string, webSearch, tools bool) error {
	c, err := loadConversation(id)
	if err != nil {
		return err
	}
	c.ReasoningEffort = reasoningEffort
	c.WebSearch = webSearch
	c.Tools = tools
	return saveConversation(c)
}

// UpdateConversationModel 切换会话使用的供应商 / 模型
func (s *Service) UpdateConversationModel(id, providerID, modelID string) error {
	c, err := loadConversation(id)
	if err != nil {
		return err
	}
	c.ProviderID = providerID
	c.ModelID = modelID
	c.UpdatedAt = time.Now().UnixMilli()
	return saveConversation(c)
}

// UpdateConversationSystem 更新会话的系统提示词(空字符串=清除)
func (s *Service) UpdateConversationSystem(id, system string) error {
	c, err := loadConversation(id)
	if err != nil {
		return err
	}
	c.System = system
	c.UpdatedAt = time.Now().UnixMilli()
	return saveConversation(c)
}

// RenameConversation 重命名
func (s *Service) RenameConversation(id, title string) error {
	c, err := loadConversation(id)
	if err != nil {
		return err
	}
	t := strings.TrimSpace(title)
	if t == "" {
		t = "无标题"
	}
	c.Title = t
	c.TitleAuto = false // 同上:用户起的名字不该被模型覆盖
	c.UpdatedAt = time.Now().UnixMilli()
	return saveConversation(c)
}

// DeleteMessage 删除会话里的单条消息(任何 role 都可删,含 clear 分隔标记)
func (s *Service) DeleteMessage(convID, msgID string) error {
	c, err := loadConversation(convID)
	if err != nil {
		return err
	}
	idx := -1
	for i := range c.Messages {
		if c.Messages[i].ID == msgID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("消息不存在")
	}
	c.Messages = append(c.Messages[:idx], c.Messages[idx+1:]...)
	c.UpdatedAt = time.Now().UnixMilli()
	return saveConversation(c)
}

// DeleteConversationByID 删除一条会话(磁盘 + 进行中的流)
func (s *Service) DeleteConversationByID(id string) error {
	// 取消可能正在进行的流
	s.cancelStream(id)
	return deleteConversation(id)
}

// autoTitle 从 user 首条消息生成标题(最多 30 字)
func autoTitle(content string) string {
	t := strings.TrimSpace(content)
	t = strings.ReplaceAll(t, "\n", " ")
	if t == "" {
		return "新对话"
	}
	r := []rune(t)
	if len(r) > 30 {
		return string(r[:30]) + "…"
	}
	return string(r)
}

// ForkConversation 从某条消息处分叉出一条新会话:把它**及之前**的所有消息复制过去,
// 原会话原封不动。
//
// 用途是"同一个问题想试另一种追问方向"。以前只能删掉后半截重来 ——
// 那等于用毁掉一条路的方式去走另一条。
//
// 消息 ID 全部重新生成:两条会话共用同一批 ID 的话,删其中一条里的某条消息、
// 或者续写、重新生成,都会因为 ID 撞车而写错地方。
func (s *Service) ForkConversation(convID, msgID string) (*Conversation, error) {
	src, err := loadConversation(convID)
	if err != nil {
		return nil, err
	}
	cut := -1
	for i := range src.Messages {
		if src.Messages[i].ID == msgID {
			cut = i
			break
		}
	}
	if cut < 0 {
		return nil, fmt.Errorf("消息不存在")
	}

	now := time.Now().UnixMilli()
	msgs := make([]Message, 0, cut+1)
	for _, m := range src.Messages[:cut+1] {
		c := m
		c.ID = uuid.NewString()
		msgs = append(msgs, c)
	}
	// 末尾如果是"模型请求调工具"的那条,它的结果消息被切在外面了。
	// 各家都要求调用和结果成对出现,落单的调用会让下一次请求整个被拒
	for len(msgs) > 0 && msgs[len(msgs)-1].Role == RoleTool {
		msgs = msgs[:len(msgs)-1]
	}

	title := strings.TrimSpace(src.Title)
	if title == "" {
		title = "新对话"
	}
	if r := []rune(title); len(r) > 20 {
		title = string(r[:20])
	}
	dst := &Conversation{
		ID:    uuid.NewString(),
		Title: title + " · 分支",
		// 不留给自动起标题:分支里已经有好几轮对话了,而自动起标题只认第一轮,
		// 挂着一个永远不会兑现的 titleAuto 只会让人以为它会自己改名
		TitleAuto:       false,
		ProviderID:      src.ProviderID,
		ModelID:         src.ModelID,
		System:          src.System,
		ContextCount:    src.ContextCount,
		ReasoningEffort: src.ReasoningEffort,
		WebSearch:       src.WebSearch,
		Tools:           src.Tools,
		Temperature:     src.Temperature,
		TopP:            src.TopP,
		MaxTokens:       src.MaxTokens,
		Messages:        msgs,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := saveConversation(dst); err != nil {
		return nil, err
	}
	return dst, nil
}
