//go:build windows

package adbx

import (
	"os/exec"
	"syscall"
)

// hideWindow 别让 adb 弹一个控制台窗口出来
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
