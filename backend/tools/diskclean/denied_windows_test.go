//go:build windows

package diskclean

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// denyList 不让当前用户列这个目录。测试结束时撤掉,不然 t.TempDir 收不掉它
func denyList(t *testing.T, dir string) {
	t.Helper()
	who, err := exec.Command("whoami").Output()
	if err != nil {
		t.Skipf("取不到当前用户:%v", err)
	}
	user := strings.TrimSpace(string(who))
	if out, err := exec.Command("icacls", dir, "/deny", user+":(RD)").CombinedOutput(); err != nil {
		t.Skipf("改不了权限:%v %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("icacls", dir, "/remove:d", user).Run() })
}

func TestDeniedDirsAreReported(t *testing.T) {
	if isElevated() {
		t.Skip("管理员身份下扫描会启用备份权限,越过这条拒绝 —— 那正是它该做的")
	}
	root := t.TempDir()
	writeN(t, filepath.Join(root, "a.bin"), 10)
	locked := filepath.Join(root, "locked")
	writeN(t, filepath.Join(locked, "hidden.bin"), 10)
	denyList(t, locked)
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("拒绝列目录没生效")
	}

	s := testService()
	res, err := s.ScanLarge(LargeOptions{Roots: []string{root}, MinSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	d := res.Denied
	if d.Count != 1 || len(d.Dirs) != 1 || !strings.EqualFold(d.Dirs[0], locked) {
		t.Fatalf("进不去的目录要报出来是哪个:%+v", d)
	}
	if d.Protected != 0 {
		t.Error("临时目录不在受保护的位置,不该算进 Protected")
	}
	if d.Elevated {
		t.Error("测试不是以管理员身份跑的")
	}

	// 同一个目录要是在受保护的位置里,就该算进 Protected:那里的东西本来也删不了
	s.guard = newGuard(guardSpec{trees: []treeSpec{{root: locked, reason: "测试"}}})
	res, _ = s.ScanLarge(LargeOptions{Roots: []string{root}, MinSize: 1})
	if res.Denied.Protected != 1 {
		t.Fatalf("受保护位置里进不去的没分出来:%+v", res.Denied)
	}

	dup, err := testService().ScanDuplicates(DupOptions{Roots: []string{root}, MinSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if dup.Denied.Count != 1 || len(dup.Denied.Dirs) != 1 {
		t.Fatalf("重复文件扫描也要报出进不去的目录:%+v", dup.Denied)
	}
}

func TestBackupPrivilegeBalances(t *testing.T) {
	r1 := withBackupPrivilege()
	r2 := withBackupPrivilege()
	r2()
	r1()
	backupPriv.mu.Lock()
	defer backupPriv.mu.Unlock()
	if backupPriv.n != 0 {
		t.Fatalf("开关计数没归零:%d —— 备份权限会一直开着", backupPriv.n)
	}
}
