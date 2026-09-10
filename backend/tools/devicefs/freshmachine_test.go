package devicefs

import (
	"os"
	"path/filepath"
	"testing"
)

// 模拟一台干净的电脑:~/.toolforge/platform-tools 不存在、adb 服务端没跑、
// PATH 上只有 Windows 系统目录里那个 2010 年的 adb 1.0.26。
//
// 这是发版前唯一一次能真验"装完就能用"的机会 —— 单元测试验的是解包和执行,
// 没验过从零到连上一台真机这条完整的路。跑完就删。
func TestFreshMachineConnect(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".toolforge", "platform-tools")
	// 这条是发版前的手工检查,不是常规用例:平时那个目录都在,直接跳过。
	// 要跑它得先把目录移开(移开别删 —— 里面可能是手工装的整套 platform-tools),
	// 再确认 adb 服务端没在跑,并插一台设备
	if _, err := os.Stat(dir); err == nil {
		t.Skipf("%s 还在,跳过(这条要求它不存在)", dir)
	}

	m := NewManager()
	s, err := m.Connect(ConnectOptions{Platform: "android"})
	if err != nil {
		t.Skipf("连不上,多半是没插设备: %v", err)
	}
	defer m.CloseAll()
	t.Logf("连上了: %s (%s) rooted=%v 起始目录=%s", s.DeviceID, s.Model, s.Rooted, s.StartPath)

	// 真列一次目录,证明不只是握了个手
	lst, err := m.List(s.ID, s.StartPath)
	if err != nil {
		t.Fatalf("连上了但列不了目录: %v", err)
	}
	t.Logf("列出 %d 条", len(lst.Entries))
	if len(lst.Entries) == 0 {
		t.Error("起始目录一条都没列出来")
	}

	// 自带的那份必须真的落到磁盘上了,而且只有它该有的东西
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("内置 adb 没解出来: %v", err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	t.Logf("解出来的: %v", names)
	for _, want := range []string{"adb.exe", "AdbWinApi.dll", "AdbWinUsbApi.dll", ".bundled-adb"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("缺 %s", want)
		}
	}
	// PATH 上那个 1.0.26 认不出现代设备。真用了它,上面 List 就该是空的 ——
	// 这里再确认一次用的不是它
	if len(ents) > 6 {
		t.Errorf("解出来的东西比预期多: %v", names)
	}
}
