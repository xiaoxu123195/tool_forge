//go:build windows

package devicefs

import (
	"os/exec"
	"syscall"
)

// hideWindow 别让转发进程弹一个控制台窗口出来。
// 这是个后台的管子,用户不该看见它
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
