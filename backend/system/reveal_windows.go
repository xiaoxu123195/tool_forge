//go:build windows

package system

import (
	"os/exec"
	"syscall"
)

// RevealInExplorer 打开文件所在的文件夹,并选中这个文件。
//
// explorer 自己解析命令行,而且解析得很怪:/select, 后面的路径要单独加引号。
// 交给 Go 拼参数的话,带空格的路径会被整段加上引号,explorer 不认,
// 退回去打开「文档」—— 所以命令行原样自己写
func RevealInExplorer(path string) error {
	cmd := exec.Command("explorer")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer /select,"` + path + `"`}
	return cmd.Start()
}
