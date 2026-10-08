//go:build !windows

package diskclean

// withBackupPrivilege 只有 Windows 上有「备份权限」这回事
func withBackupPrivilege() func() { return func() {} }
