//go:build !windows

package diskclean

import (
	"os"
	"path/filepath"
)

// NewGuard 按本机的系统位置构造守卫
func NewGuard() *Guard {
	home, _ := os.UserHomeDir()
	spec := unixSpec(home)
	spec.trees = append(spec.trees, appDirs()...)
	return newGuard(spec)
}

func unixSpec(home string) guardSpec {
	s := guardSpec{
		trees: []treeSpec{
			{"/System", "macOS 系统文件", nil},
			{"/bin", "系统命令", nil},
			{"/sbin", "系统命令", nil},
			{"/usr", "系统组件", []string{"/usr/local"}},
			{"/Applications", "应用程序:要删软件,把它拖进废纸篓", nil},
			{"/Library", "所有用户共享的程序数据:删了可能导致软件坏掉", []string{"/Library/Caches", "/Library/Logs"}},
			{"/private", "系统数据", []string{"/private/tmp", "/private/var/tmp", "/private/var/folders"}},
		},
		noWipe:        []string{"/Users"},
		noWipeParents: []string{"/Users", "/Volumes"},
		appDataName:   "Library",
	}
	if home != "" {
		j := func(p string) string { return filepath.Join(home, p) }
		s.trees = append(s.trees,
			treeSpec{j("Library/Mobile Documents"), "iCloud 云盘:删了会从云端一起删", nil},
			treeSpec{j("Library/Keychains"), "钥匙串", nil},
			treeSpec{j(".ssh"), "SSH 密钥", nil},
			treeSpec{j(".gnupg"), "GPG 密钥", nil},
		)
		s.noWipe = append(s.noWipe, home, j("Desktop"), j("Documents"), j("Downloads"),
			j("Pictures"), j("Movies"), j("Music"), j("Library"))
	}
	return s
}
