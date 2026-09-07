//go:build windows

package mcp

import (
	"os/exec"
	"syscall"
)

// hideConsoleWindow 起 MCP 服务器进程时不要弹出黑框。
// 很多 MCP 服务器是 node / python 脚本,不加这个每连一个就闪一个控制台窗口。
func hideConsoleWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
