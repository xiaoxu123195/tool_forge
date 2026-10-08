package diskclean

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestScanEmptyDirs(t *testing.T) {
	root := t.TempDir()
	mk := func(p string) string {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
		return full
	}
	e1 := mk("empty1")
	mk("tree/a/b") // 三层都是空的:只报最外层 tree,里面套着 2 个
	full := mk("full")
	writeN(t, filepath.Join(full, "f.txt"), 1)
	child := mk("half/emptychild") // half 自己有文件,里面那个空子目录单独报
	writeN(t, filepath.Join(root, "half", "f.txt"), 1)
	mk("fresh")               // 刚建的:不报,算进 Recent
	mk("proj/node_modules/x") // 开发目录没进去:proj 不能算空
	// 程序的数据目录:里面的空目录是程序自己的结构,不进去
	dot := mk(".cache/profile/empty")
	wx := mk("WeChat Files/wxid_x/FileStorage/Image")
	old := []string{e1, filepath.Join(root, "tree", "a", "b"), filepath.Join(root, "tree", "a"),
		filepath.Join(root, "tree"), child, filepath.Join(root, "proj"), dot, wx}
	for _, p := range old {
		age(t, p, 72*time.Hour)
	}

	s := testService()
	res, err := s.ScanEmptyDirs(EmptyOptions{Roots: []string{root}, SkipDevDirs: true})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range res.Dirs {
		rel, _ := filepath.Rel(root, d.Path)
		got = append(got, filepath.ToSlash(rel))
		if rel == "tree" && d.Nested != 2 {
			t.Errorf("tree 里面套着 2 个空子目录,报的是 %d", d.Nested)
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "empty1,half/emptychild,tree" {
		t.Fatalf("报出来的空目录不对:%v", got)
	}
	if res.Recent != 1 {
		t.Errorf("刚建的空目录应该算进 Recent,实际 %d", res.Recent)
	}

	// 删:三层的那棵整个删掉
	r, err := s.DeleteEmptyDirs(EmptyDeleteRequest{Paths: []string{filepath.Join(root, "tree"), e1}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Deleted != 2 || exists(filepath.Join(root, "tree")) || exists(e1) {
		t.Fatalf("空目录没删掉:%+v", r)
	}
	if r.Recycled {
		t.Error("空目录是直接删的,不该说进了回收站")
	}

	// 扫描之后里面多了文件:不删,文件原样留着
	f := writeN(t, filepath.Join(child, "new.txt"), 1)
	r, _ = s.DeleteEmptyDirs(EmptyDeleteRequest{Paths: []string{child}})
	if r.Deleted != 0 || !exists(f) || !strings.Contains(r.Items[0].Reason, "多了东西") {
		t.Fatalf("里面多了文件的目录不能删:%+v", r)
	}
}

// 个人目录本身和它下面直接那一层,空着也不删
func TestGuardStructuralAndAppData(t *testing.T) {
	users := t.TempDir()
	g := newGuard(guardSpec{noWipeParents: []string{users}, appDataName: "AppData"})
	cases := []struct {
		p                 string
		structural, appDB bool
	}{
		{filepath.Join(users, "u"), true, false},
		{filepath.Join(users, "u", "Contacts"), true, false},
		{filepath.Join(users, "u", "AppData"), true, true},
		{filepath.Join(users, "u", "Documents", "old"), false, false},
		{filepath.Join(users, "u", "Documents", "AppData"), false, false},
	}
	for _, c := range cases {
		if got := g.Structural(c.p); got != c.structural {
			t.Errorf("Structural(%s) = %v", c.p, got)
		}
		if got := g.IsAppData(c.p); got != c.appDB {
			t.Errorf("IsAppData(%s) = %v", c.p, got)
		}
	}
}
