//go:build !windows

package devicefs

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
