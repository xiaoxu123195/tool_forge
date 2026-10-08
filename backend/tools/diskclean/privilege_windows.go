//go:build windows

package diskclean

import (
	"sync"

	"golang.org/x/sys/windows"
)

// 以管理员身份运行时,系统里仍有一批目录只给 SYSTEM 账户看:系统日志、服务的私有数据、
// 离线文件缓存之类。备份软件靠「备份权限」(SeBackupPrivilege)去读这些地方 ——
// 打开时带 FILE_FLAG_BACKUP_SEMANTICS 就能越过访问控制,而 Go 以只读方式打开目录时
// 本来就带着这个标志。
//
// 这个权限只放宽"读":删除、修改、换属主要的是另外的权限,不会因此打开。
// 管理员的令牌里本来就有它,只是默认没启用;普通用户的令牌里根本没有,启用会静默失败。
//
// 它是整个进程的开关,而几个扫描可能同时在跑,所以按引用计数:
// 第一个扫描开始时启用,最后一个结束时关掉,不在扫描以外的时间里留着
var backupPriv struct {
	mu sync.Mutex
	n  int
}

// withBackupPrivilege 扫描期间启用备份权限,返回的函数在扫描结束时调用
func withBackupPrivilege() func() {
	if !isElevated() {
		return func() {}
	}
	backupPriv.mu.Lock()
	if backupPriv.n == 0 {
		_ = setPrivilege("SeBackupPrivilege", true)
	}
	backupPriv.n++
	backupPriv.mu.Unlock()
	return func() {
		backupPriv.mu.Lock()
		backupPriv.n--
		if backupPriv.n == 0 {
			_ = setPrivilege("SeBackupPrivilege", false)
		}
		backupPriv.mu.Unlock()
	}
}

func setPrivilege(name string, enable bool) error {
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &tok); err != nil {
		return err
	}
	defer tok.Close()
	n, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, n, &luid); err != nil {
		return err
	}
	tp := windows.Tokenprivileges{PrivilegeCount: 1}
	tp.Privileges[0].Luid = luid
	if enable {
		tp.Privileges[0].Attributes = windows.SE_PRIVILEGE_ENABLED
	}
	return windows.AdjustTokenPrivileges(tok, false, &tp, 0, nil, nil)
}
