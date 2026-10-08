//go:build !windows

package system

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// RevealInExplorer 打开文件所在的文件夹,并选中这个文件
func RevealInExplorer(path string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", "-R", path).Start()
	}
	return exec.Command("xdg-open", filepath.Dir(path)).Start()
}
