//go:build windows

package diskclean

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// makeShortcuts 用系统自己的接口建快捷方式 —— 拿真实的文件测,而不是只测自己拼的
func makeShortcuts(t *testing.T, pairs map[string]string) {
	t.Helper()
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	var b strings.Builder
	b.WriteString("$w = New-Object -ComObject WScript.Shell\n")
	for lnk, target := range pairs {
		b.WriteString("$s = $w.CreateShortcut(" + q(lnk) + "); $s.TargetPath = " + q(target) + "; $s.Save()\n")
	}
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", encodeCommand(b.String()))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("建不了快捷方式:%v %s", err, out)
	}
}

// freeDrive 找一个没在用的盘符
func freeDrive() string {
	for c := 'Z'; c >= 'M'; c-- {
		root := string(c) + `:\`
		if _, err := os.Stat(root); err != nil {
			return root
		}
	}
	return ""
}

func TestJudgeRealShortcuts(t *testing.T) {
	dir := t.TempDir()
	alive := writeN(t, filepath.Join(dir, "工具", "应用.exe"), 10)
	gone := writeN(t, filepath.Join(dir, "gone.exe"), 10)
	pairs := map[string]string{
		filepath.Join(dir, "alive.lnk"): alive,
		filepath.Join(dir, "gone.lnk"):  gone,
	}
	if d := freeDrive(); d != "" {
		pairs[filepath.Join(dir, "offline.lnk")] = d + `nowhere\x.exe`
	}
	makeShortcuts(t, pairs)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	if target, st := judgeShortcut(filepath.Join(dir, "alive.lnk")); st != lnkOK || !strings.EqualFold(target, alive) {
		t.Errorf("目标在的快捷方式判错了:%v %q(中文路径要原样读出来)", st, target)
	}
	if target, st := judgeShortcut(filepath.Join(dir, "gone.lnk")); st != lnkBroken || !strings.EqualFold(target, gone) {
		t.Errorf("目标没了的快捷方式应该判成无效:%v %q", st, target)
	}
	if _, ok := pairs[filepath.Join(dir, "offline.lnk")]; ok {
		if _, st := judgeShortcut(filepath.Join(dir, "offline.lnk")); st != lnkUnknown {
			t.Errorf("目标所在的盘不在,说不准坏没坏,不该报成无效:%v", st)
		}
	}

	s := testService()
	res, err := s.DeleteShortcuts(DeleteRequest{
		Files:     []FileRef{refOf(t, filepath.Join(dir, "gone.lnk")), refOf(t, filepath.Join(dir, "alive.lnk"))},
		Permanent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 1 || exists(filepath.Join(dir, "gone.lnk")) || !exists(filepath.Join(dir, "alive.lnk")) {
		t.Fatalf("应该只删掉无效的那个:%+v", res)
	}
	for _, it := range res.Items {
		if strings.HasSuffix(it.Path, "alive.lnk") && !strings.Contains(it.Reason, "找得到") {
			t.Errorf("有效的快捷方式没删,原因要说清楚:%q", it.Reason)
		}
	}
}

// 真机上碰到过的:安装程序在别处建好快捷方式再拷进开始菜单,里面没有 LinkInfo,
// 相对路径是按原来的位置算的,拿它去拼就指到一个根本不存在的地方 —— 好好的快捷方式被报成无效。
// 这里用系统建的快捷方式里的项目标识列表,拼一个同样形状的出来
func TestJudgeUsesIDListNotRelativePath(t *testing.T) {
	dir := t.TempDir()
	target := writeN(t, filepath.Join(dir, "app", "real.exe"), 10)
	src := filepath.Join(dir, "src.lnk")
	makeShortcuts(t, map[string]string{src: target})
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	info, err := parseLnk(data, decodeANSI)
	if err != nil || len(info.idList) == 0 {
		t.Skip("系统建的快捷方式里没有项目标识列表")
	}

	moved := filepath.Join(dir, "Start Menu", "Programs", "moved.lnk")
	writeFile(t, moved, buildLnk(lnkSpec{idList: info.idList, relPath: `..\..\..\nowhere\real.exe`}))
	if got, st := judgeShortcut(moved); st != lnkOK || !strings.EqualFold(got, target) {
		t.Fatalf("目标好好的,被相对路径带偏了:%v %q", st, got)
	}

	// 目标真没了:项目标识列表转出来的路径照样能用来判"坏了"
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if got, st := judgeShortcut(moved); st != lnkBroken || !strings.EqualFold(got, target) {
		t.Fatalf("目标没了应该判成无效:%v %q", st, got)
	}
}

func TestShortcutPlaces(t *testing.T) {
	places := shortcutPlaces()
	names := map[string]bool{}
	for _, p := range places {
		names[p.name] = true
		if !filepath.IsAbs(p.dir) {
			t.Errorf("%s 的位置不是完整路径:%s", p.name, p.dir)
		}
	}
	for _, want := range []string{"桌面", "开始菜单"} {
		if !names[want] {
			t.Errorf("没有扫%s", want)
		}
	}
}
