package diskclean

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// dupFixture 一套重复文件:
//
//	a1 a2 a3   一样的 300KB(超过 128KB,要走到全文比对那一级)
//	b1         和 a 一样大、头尾也一样,只有中间一个字节不同 —— 头尾初筛分不开,必须靠全文
//	d1 d2      一样的 10KB(头尾初筛读的就是全文)
//	u1         和 d 一样大,内容不同
//	nm/a4      和 a 一样,但在 node_modules 里
type dupFixture struct {
	dir                            string
	a1, a2, a3, b1, d1, d2, u1, a4 string
	big                            []byte
}

func newDupFixture(t *testing.T) dupFixture {
	dir := t.TempDir()
	big := bytes.Repeat([]byte("A"), 300<<10)
	mid := append([]byte{}, big...)
	mid[150<<10] = 'B'
	small := bytes.Repeat([]byte("x"), 10<<10)
	other := bytes.Repeat([]byte("y"), 10<<10)
	return dupFixture{
		dir: dir, big: big,
		a1: writeFile(t, filepath.Join(dir, "a1.iso"), big),
		a2: writeFile(t, filepath.Join(dir, "copy", "a2.iso"), big),
		a3: writeFile(t, filepath.Join(dir, "copy", "again", "a3.iso"), big),
		b1: writeFile(t, filepath.Join(dir, "b1.iso"), mid),
		d1: writeFile(t, filepath.Join(dir, "d1.txt"), small),
		d2: writeFile(t, filepath.Join(dir, "x", "d2.txt"), small),
		u1: writeFile(t, filepath.Join(dir, "u1.txt"), other),
		a4: writeFile(t, filepath.Join(dir, "proj", "node_modules", "a4.iso"), big),
	}
}

func groupPaths(g DupGroup) []string {
	var out []string
	for _, f := range g.Files {
		out = append(out, f.Path)
	}
	sort.Strings(out)
	return out
}

func TestScanDuplicates(t *testing.T) {
	f := newDupFixture(t)
	s := testService()
	res, err := s.ScanDuplicates(DupOptions{Roots: []string{f.dir}, MinSize: 1, SkipDevDirs: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 2 {
		t.Fatalf("应该是 2 组,实际 %d 组:%+v", len(res.Groups), res.Groups)
	}
	// 按能腾出的空间排:a 组在前
	a, d := res.Groups[0], res.Groups[1]
	want := []string{f.a1, f.a3, f.a2}
	sort.Strings(want)
	if got := groupPaths(a); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("a 组不对(b1 头尾一样、中间不同,不能混进来;node_modules 里的要跳过):%v", got)
	}
	if a.Wasted != 2*int64(len(f.big)) {
		t.Errorf("a 组可腾出 %d,应为两份的大小", a.Wasted)
	}
	if got := groupPaths(d); len(got) != 2 {
		t.Errorf("d 组应该是 d1、d2:%v", got)
	}
	if res.Wasted != a.Wasted+d.Wasted || res.TotalGroups != 2 {
		t.Errorf("汇总不对:%+v", res)
	}

	// 不跳过开发目录时,node_modules 里那份也算
	res, _ = s.ScanDuplicates(DupOptions{Roots: []string{f.dir}, MinSize: 1})
	if len(res.Groups[0].Files) != 4 {
		t.Fatalf("不跳过 node_modules 时 a 组应有 4 份,实际 %d", len(res.Groups[0].Files))
	}
}

// 分级筛的意义在于能不读的就不读:大小独一份的一个字节都不读,
// 头尾就分得开的不读全文。全盘扫几百 GB,这决定了是一分钟还是一小时
func TestScanDuplicatesReadsOnlyWhatItMust(t *testing.T) {
	dir := t.TempDir()
	x := bytes.Repeat([]byte("p"), 1<<20)
	y := append([]byte{}, x...)
	y[0] = 'q'
	writeFile(t, filepath.Join(dir, "x.bin"), x)
	writeFile(t, filepath.Join(dir, "y.bin"), y)
	writeN(t, filepath.Join(dir, "lonely.bin"), 5<<20)

	res, err := testService().ScanDuplicates(DupOptions{Roots: []string{dir}, MinSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 0 {
		t.Fatalf("不该有重复:%+v", res.Groups)
	}
	if want := int64(2 * 2 * sampleSize); res.HashedBytes != want {
		t.Fatalf("应该只读两个文件的头尾(%d 字节),实际读了 %d", want, res.HashedBytes)
	}
}

// 硬链接是同一个文件的两个名字,删掉一个名字一个字节都腾不出来,不能算重复
func TestScanDuplicatesHardlinks(t *testing.T) {
	f := newDupFixture(t)
	link := filepath.Join(f.dir, "hard.iso")
	if err := os.Link(f.a1, link); err != nil {
		t.Skipf("建不了硬链接:%v", err)
	}
	res, err := testService().ScanDuplicates(DupOptions{Roots: []string{f.dir}, MinSize: 1, SkipDevDirs: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Hardlinks != 1 {
		t.Errorf("应该认出 1 个硬链接,实际 %d", res.Hardlinks)
	}
	got := groupPaths(res.Groups[0])
	n := 0
	for _, p := range got {
		if p == f.a1 || p == link {
			n++
		}
	}
	if len(got) != 3 || n != 1 {
		t.Fatalf("a1 和它的硬链接只能出现一个:%v", got)
	}
}

func TestDeleteDuplicatesVerifies(t *testing.T) {
	f := newDupFixture(t)
	s := testService()
	res, err := s.ScanDuplicates(DupOptions{Roots: []string{f.dir}, MinSize: 1, SkipDevDirs: true})
	if err != nil {
		t.Fatal(err)
	}
	g := res.Groups[0]
	del := func(keep, drop []string) *DeleteResult {
		t.Helper()
		r, err := s.DeleteDuplicates(DupDeleteRequest{
			Groups:    []DupDeleteGroup{{ID: g.ID, Size: g.Size, Keep: keep, Delete: drop}},
			Permanent: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	// 一份都不留:整组拒绝
	r := del(nil, []string{f.a1, f.a2, f.a3})
	if r.Deleted != 0 || !exists(f.a1) || !exists(f.a2) || !exists(f.a3) {
		t.Fatal("一份都不留的请求必须整组拒绝")
	}
	if !strings.Contains(r.Items[0].Reason, "一份都没留") {
		t.Errorf("原因没说清楚:%q", r.Items[0].Reason)
	}

	// 既要留又要删
	r = del([]string{f.a1}, []string{f.a1, f.a2})
	if r.Deleted != 0 || !exists(f.a1) {
		t.Fatal("同一个文件既留又删,不能删")
	}

	// 扫描之后 a2 被改了(大小不变、内容变了):a2 不能删
	changed := append([]byte{}, f.big...)
	changed[0] = 'Z'
	writeFile(t, f.a2, changed)
	r = del([]string{f.a1}, []string{f.a2, f.a3})
	if exists(f.a3) || !exists(f.a2) || r.Deleted != 1 {
		t.Fatalf("应该只删掉 a3、保住内容变了的 a2:%+v", r)
	}
	for _, it := range r.Items {
		if it.Path == f.a2 && !strings.Contains(it.Reason, "变了") {
			t.Errorf("a2 的原因该说内容变了:%q", it.Reason)
		}
	}

	// 留下的那份自己变了:删掉其余的就等于把这份内容彻底删没了,整组拒绝
	writeFile(t, f.a3, f.big)
	writeFile(t, f.a1, changed)
	r = del([]string{f.a1}, []string{f.a3})
	if r.Deleted != 0 || !exists(f.a3) {
		t.Fatal("要保留的那份已经变了,这一组一个都不能删")
	}
	if !strings.Contains(r.Items[0].Reason, "要保留的那份") {
		t.Errorf("原因没说清楚:%q", r.Items[0].Reason)
	}
}

func TestDeleteDuplicatesSkipsHardlinkOfKeeper(t *testing.T) {
	f := newDupFixture(t)
	s := testService()
	res, _ := s.ScanDuplicates(DupOptions{Roots: []string{f.dir}, MinSize: 1, SkipDevDirs: true})
	g := res.Groups[0]
	link := filepath.Join(f.dir, "hard.iso")
	if err := os.Link(f.a1, link); err != nil {
		t.Skipf("建不了硬链接:%v", err)
	}
	r, _ := s.DeleteDuplicates(DupDeleteRequest{
		Groups:    []DupDeleteGroup{{ID: g.ID, Size: g.Size, Keep: []string{f.a1}, Delete: []string{link}}},
		Permanent: true,
	})
	if r.Deleted != 0 || !exists(link) || !strings.Contains(r.Items[0].Reason, "同一个文件") {
		t.Fatalf("要保留的那份的硬链接删了也腾不出空间,不该删:%+v", r)
	}
}
