//go:build !windows

package adbx

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
