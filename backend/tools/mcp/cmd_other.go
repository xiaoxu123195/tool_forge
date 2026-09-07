//go:build !windows

package mcp

import "os/exec"

// hideConsoleWindow 非 Windows 上没有控制台窗口这回事
func hideConsoleWindow(cmd *exec.Cmd) {}
