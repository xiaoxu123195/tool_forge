package aichat

import (
	_ "embed"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// 内置提示词从文件读,不写成 Go 字符串常量。
//
// 这些提示词又长又带反引号和大量转义字符,塞进代码里既读不了也改不动;
// 拆成 .md 之后想调一句话直接改文件,不用在一堆 \n 里找位置。
var (
	//go:embed prompts/nizi.md
	promptNizi string
	//go:embed prompts/zheli.md
	promptZheli string
	//go:embed prompts/yujie.md
	promptYujie string
)

// Assistant 一套可复用的会话预设。
//
// 系统提示词以前只能一条会话一条会话地填,想重复用同一个角色只能来回复制粘贴。
// 这里把提示词和那几个常调的参数打包存起来,新建会话时挑一个就行。
type Assistant struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Emoji 列表里的小图标,纯装饰
	Emoji  string `json:"emoji,omitempty"`
	System string `json:"system"`
	// 下面这些是可选的参数覆盖;零值 / 空表示"不干预,用会话的默认值"
	ContextCount    int      `json:"contextCount,omitempty"`
	ReasoningEffort string   `json:"reasoningEffort,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxTokens       int      `json:"maxTokens,omitempty"`
	WebSearch       bool     `json:"webSearch,omitempty"`
	Tools           bool     `json:"tools,omitempty"`
	// SortOrder 拖动排序用,从 1 开始
	SortOrder int   `json:"sortOrder,omitempty"`
	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

// builtinAssistants 首次运行时预置的几个,之后就是普通条目 —— 用户改它删它都行。
//
// 不做"删了自动补回来"那套(供应商预设是那样的):供应商删掉还能重新配,
// 而这里是内容,用户删掉就是不想要了,再给他塞回来只会烦人。
func builtinAssistants() []Assistant {
	now := time.Now().UnixMilli()
	mk := func(i int, emoji, name, system string) Assistant {
		return Assistant{
			ID:        fmt.Sprintf("builtin-%d", i),
			Name:      name,
			Emoji:     emoji,
			System:    system,
			SortOrder: i,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}
	return []Assistant{
		mk(1, "😇", "逆子AI", promptNizi),
		mk(2, "🧪", "哲理大师", promptZheli),
		mk(3, "🧣", "东北雨姐", promptYujie),
	}
}

// ListAssistants 按排序位返回全部预设
func (s *Service) ListAssistants() ([]Assistant, error) {
	list, err := loadAssistants()
	if err != nil {
		return nil, err
	}
	// 首次运行(文件不存在 → 空列表)时把内置的写进去
	if list == nil {
		list = builtinAssistants()
		if err := saveAssistants(list); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].SortOrder != list[j].SortOrder {
			return list[i].SortOrder < list[j].SortOrder
		}
		return list[i].CreatedAt < list[j].CreatedAt
	})
	return list, nil
}

// SaveAssistant 新增或更新;ID 为空 → 新增(排到最后)
func (s *Service) SaveAssistant(a Assistant) (Assistant, error) {
	list, err := s.ListAssistants()
	if err != nil {
		return Assistant{}, err
	}
	now := time.Now().UnixMilli()
	a.UpdatedAt = now
	if a.ID == "" {
		a.ID = uuid.NewString()
		a.CreatedAt = now
		for _, e := range list {
			if e.SortOrder >= a.SortOrder {
				a.SortOrder = e.SortOrder + 1
			}
		}
		list = append(list, a)
	} else {
		found := false
		for i := range list {
			if list[i].ID == a.ID {
				a.CreatedAt = list[i].CreatedAt
				a.SortOrder = list[i].SortOrder
				list[i] = a
				found = true
				break
			}
		}
		if !found {
			return Assistant{}, fmt.Errorf("预设不存在: %s", a.ID)
		}
	}
	if err := saveAssistants(list); err != nil {
		return Assistant{}, err
	}
	return a, nil
}

// DeleteAssistant 删除一条
func (s *Service) DeleteAssistant(id string) error {
	list, err := s.ListAssistants()
	if err != nil {
		return err
	}
	out := make([]Assistant, 0, len(list))
	for _, a := range list {
		if a.ID != id {
			out = append(out, a)
		}
	}
	return saveAssistants(out)
}

// ReorderAssistants 按给定顺序重排
func (s *Service) ReorderAssistants(ids []string) error {
	list, err := s.ListAssistants()
	if err != nil {
		return err
	}
	rank := make(map[string]int, len(ids))
	for i, id := range ids {
		rank[id] = i + 1
	}
	for i := range list {
		if r, ok := rank[list[i].ID]; ok {
			list[i].SortOrder = r
		}
	}
	return saveAssistants(list)
}
