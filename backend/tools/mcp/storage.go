package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// serversPath 返回 ~/.toolforge/mcp/servers.json,自动建目录
func serversPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, ".toolforge", "mcp")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(d, "servers.json"), nil
}

func loadServers() ([]Server, error) {
	path, err := serversPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var list []Server
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("读 servers.json 失败: %w", err)
	}
	return list, nil
}

// saveServers 原子写(.tmp + rename),避免写一半断电留下坏文件。
// 权限 0600:headers / env 里可能有 API Key。
func saveServers(list []Server) error {
	path, err := serversPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
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
