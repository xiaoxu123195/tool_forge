package diskclean

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 自定义缓存规则:内置规则覆盖不到的目录(模拟器缓存、某个工具的下载目录之类),
// 用户自己加。
//
// 和内置规则过同一道守卫,而且加的时候、清的时候各过一次:
// 存下来的文件可能被改过,加的时候放行不代表清的时候还该放行

// CustomRule 一条自定义规则
type CustomRule struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Dir  string `json:"dir"`
	// MinAgeDays 只删多少天以前的;0 表示全部
	MinAgeDays int `json:"minAgeDays"`
}

func defaultCustomPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".toolforge", "diskclean-rules.json")
}

// CustomRules 已经加了的自定义规则。读不出来就当没有,不让一个坏文件把缓存页弄崩
func (s *Service) CustomRules() []CustomRule {
	s.customMu.Lock()
	defer s.customMu.Unlock()
	return s.loadCustom()
}

func (s *Service) loadCustom() []CustomRule {
	out := []CustomRule{}
	if s.customPath == "" {
		return out
	}
	data, err := os.ReadFile(s.customPath)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	if out == nil {
		out = []CustomRule{}
	}
	return out
}

func (s *Service) storeCustom(rules []CustomRule) error {
	if s.customPath == "" {
		return errors.New("找不到配置目录")
	}
	if err := os.MkdirAll(filepath.Dir(s.customPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	// 先写临时文件再换名:写到一半断电,也不会留下一个半截的规则文件
	tmp := s.customPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.customPath)
}

// SaveCustomRule 新加或修改一条。目录过不了守卫的直接拒绝,原因原样给前端
func (s *Service) SaveCustomRule(r CustomRule) (*CustomRule, error) {
	dir := strings.TrimSpace(r.Dir)
	if dir == "" || !filepath.IsAbs(dir) {
		return nil, errors.New("要选一个完整的目录")
	}
	dir = filepath.Clean(dir)
	fi, err := os.Lstat(dir)
	if err != nil {
		return nil, describe(err)
	}
	if kindOf(fi) != kindDir {
		return nil, errors.New("这不是一个普通目录(可能是链接或网盘同步的目录),不能加")
	}
	if v := s.guard.CheckContentsFinal(dir); v.Blocked {
		return nil, fmt.Errorf("这个目录不能当缓存清:%s", v.Reason)
	}
	if r.MinAgeDays < 0 || r.MinAgeDays > 3650 {
		return nil, errors.New("天数不对")
	}
	r.Dir = dir
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		r.Name = filepath.Base(dir)
	}

	s.customMu.Lock()
	defer s.customMu.Unlock()
	rules := s.loadCustom()
	idx := -1
	for i, old := range rules {
		if r.ID != "" && old.ID == r.ID {
			idx = i
		} else if norm(old.Dir) == norm(dir) {
			return nil, fmt.Errorf("这个目录已经加过了(%s)", old.Name)
		}
	}
	if idx >= 0 {
		rules[idx] = r
	} else {
		r.ID = fmt.Sprintf("%x", time.Now().UnixNano())
		rules = append(rules, r)
	}
	if err := s.storeCustom(rules); err != nil {
		return nil, err
	}
	return &r, nil
}

// DeleteCustomRule 删掉一条自定义规则(只是不再清它,目录本身不动)
func (s *Service) DeleteCustomRule(id string) error {
	s.customMu.Lock()
	defer s.customMu.Unlock()
	rules := s.loadCustom()
	out := rules[:0]
	for _, r := range rules {
		if r.ID != id {
			out = append(out, r)
		}
	}
	return s.storeCustom(out)
}

// customPrefix 自定义规则在缓存列表里的 ID 前缀,和内置规则的 ID 分开
const customPrefix = "custom:"

// allRules 内置规则加上自定义的
func (s *Service) allRules() []CacheRule {
	out := append([]CacheRule{}, s.rules...)
	for _, c := range s.CustomRules() {
		desc := "自己加的目录,清掉里面的全部内容"
		if c.MinAgeDays > 0 {
			desc = fmt.Sprintf("自己加的目录,只清 %d 天以前的", c.MinAgeDays)
		}
		out = append(out, CacheRule{
			ID:      customPrefix + c.ID,
			Group:   "自定义",
			Name:    c.Name,
			Desc:    desc,
			Paths:   []string{c.Dir},
			MinAge:  time.Duration(c.MinAgeDays) * 24 * time.Hour,
			Literal: true,
			Custom:  true,
		})
	}
	return out
}
