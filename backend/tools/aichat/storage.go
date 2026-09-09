package aichat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// dataDir 返回 ~/.toolforge/ai-chat/ ,自动创建
func dataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, ".toolforge", "ai-chat")
	if err := os.MkdirAll(filepath.Join(d, "conversations"), 0o755); err != nil {
		return "", err
	}
	return d, nil
}

func providersPath() (string, error) {
	d, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "providers.json"), nil
}

func configPath() (string, error) {
	d, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

func conversationPath(id string) (string, error) {
	d, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "conversations", id+".json"), nil
}

// readJSON 读 JSON;文件不存在 → 返回零值不报错
func readJSON(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// writeJSONAtomic 原子写(.tmp + rename)
func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func assistantsPath() (string, error) {
	d, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "assistants.json"), nil
}

// loadAssistants 文件不存在时返回 nil(而不是空切片)—— 调用方靠这个区分
// "还没初始化过"和"用户把内置的全删了",前者要写入内置预设,后者不能再塞回去
func loadAssistants() ([]Assistant, error) {
	path, err := assistantsPath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	list := []Assistant{}
	if err := readJSON(path, &list); err != nil {
		return nil, fmt.Errorf("读 assistants.json 失败: %w", err)
	}
	return list, nil
}

func saveAssistants(list []Assistant) error {
	path, err := assistantsPath()
	if err != nil {
		return err
	}
	if list == nil {
		list = []Assistant{}
	}
	return writeJSONAtomic(path, list)
}

func loadProviders() ([]Provider, error) {
	path, err := providersPath()
	if err != nil {
		return nil, err
	}
	var list []Provider
	if err := readJSON(path, &list); err != nil {
		return nil, fmt.Errorf("读 providers.json 失败: %w", err)
	}
	return list, nil
}

func saveProviders(list []Provider) error {
	path, err := providersPath()
	if err != nil {
		return err
	}
	return writeJSONAtomic(path, list)
}

func loadConfig() (Config, error) {
	path, err := configPath()
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := readJSON(path, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

func saveConfig(c Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	return writeJSONAtomic(path, c)
}

func loadConversation(id string) (*Conversation, error) {
	path, err := conversationPath(id)
	if err != nil {
		return nil, err
	}
	var c Conversation
	if err := readJSON(path, &c); err != nil {
		return nil, err
	}
	if c.ID == "" {
		return nil, fmt.Errorf("对话不存在: %s", id)
	}
	return &c, nil
}

func saveConversation(c *Conversation) error {
	path, err := conversationPath(c.ID)
	if err != nil {
		return err
	}
	return writeJSONAtomic(path, c)
}

func deleteConversation(id string) error {
	path, err := conversationPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// listConversations 列对话目录下所有 *.json,只读 metadata
func listConversations() ([]ConversationSummary, error) {
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
		path := filepath.Join(d, "conversations", e.Name())
		var c Conversation
		if err := readJSON(path, &c); err != nil || c.ID == "" {
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
	return out, nil
}
