package diskclean

import (
	"path/filepath"
	"testing"
)

func TestUsageTree(t *testing.T) {
	dir := t.TempDir()
	writeN(t, filepath.Join(dir, "a", "x.bin"), 1000)
	writeN(t, filepath.Join(dir, "a", "deep", "y.bin"), 500)
	writeN(t, filepath.Join(dir, "b", "z.bin"), 3000)
	writeN(t, filepath.Join(dir, "top.bin"), 200)

	s := testService()
	// 门槛设得很高:一个大文件都没有,目录树照样要建出来
	res, err := s.ScanLarge(LargeOptions{Roots: []string{dir}, MinSize: 1 << 40})
	if err != nil {
		t.Fatal(err)
	}
	if res.UsageID == "" {
		t.Fatal("扫描没有给出目录树")
	}

	top, err := s.UsageChildren(res.UsageID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(top.Entries) != 1 || top.Entries[0].Size != 4700 || top.Entries[0].Files != 4 {
		t.Fatalf("最顶层应该只有扫描起点,共 4700 字节 4 个文件:%+v", top.Entries)
	}

	lv, err := s.UsageChildren(res.UsageID, dir)
	if err != nil {
		t.Fatal(err)
	}
	// 按大小从大到小:b(3000) > a(1000+500,连同子目录) > 直接放在这里的文件(200)
	if len(lv.Entries) != 3 {
		t.Fatalf("应该是 b、a、这一层的文件三项:%+v", lv.Entries)
	}
	e := lv.Entries
	if e[0].Name != "b" || e[0].Size != 3000 || e[1].Name != "a" || e[1].Size != 1500 {
		t.Errorf("顺序或大小不对:%+v", e)
	}
	if e[2].IsDir || e[2].Size != 200 || e[2].Files != 1 {
		t.Errorf("直接放在这一层的文件要合成一项:%+v", e[2])
	}

	sub, err := s.UsageChildren(res.UsageID, e[1].Path)
	if err != nil {
		t.Fatal(err)
	}
	if sub.Size != 1500 || len(sub.Entries) != 2 || sub.Entries[0].Size != 1000 || sub.Entries[1].Name != "deep" {
		t.Fatalf("点进 a 之后不对:%+v", sub)
	}

	// 下一次扫描会换掉这棵树,拿旧的 ID 来取要说清楚
	if _, err := s.UsageChildren("old-scan", dir); err == nil {
		t.Fatal("过期的扫描结果应该报错")
	}
	if _, err := s.UsageChildren(res.UsageID, filepath.Join(dir, "nope")); err == nil {
		t.Fatal("不在扫描结果里的目录应该报错")
	}
}
