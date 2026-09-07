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
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
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
	if t := strings.TrimSpace(m.Title); t != "" {
		c.Title = t
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
