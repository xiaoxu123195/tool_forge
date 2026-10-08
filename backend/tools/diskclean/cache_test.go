package diskclean

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testRule(env string, extra func(*CacheRule)) CacheRule {
	r := CacheRule{ID: "t", Group: "测试", Name: "测试缓存", Desc: "测试", Paths: []string{"%" + env + "%"}}
	if extra != nil {
		extra(&r)
	}
	return r
}

func TestCleanCacheRule(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	old := writeN(t, filepath.Join(root, "old.bin"), 100)
	fresh := writeN(t, filepath.Join(root, "new.bin"), 100)
	deep := writeN(t, filepath.Join(root, "sub", "deep", "old2.bin"), 50)
	freshDir := filepath.Join(root, "fresh-empty")
	staleDir := filepath.Join(root, "stale-empty")
	os.MkdirAll(freshDir, 0o755)
	os.MkdirAll(staleDir, 0o755)
	for _, p := range []string{old, deep, filepath.Join(root, "sub", "deep"), filepath.Join(root, "sub"), staleDir} {
		age(t, p, 72*time.Hour)
	}

	t.Setenv("TF_TEST_CACHE", root)
	s := testService(testRule("TF_TEST_CACHE", func(r *CacheRule) { r.MinAge = 24 * time.Hour }))

	scan, err := s.ScanCache("")
	if err != nil {
		t.Fatal(err)
	}
	it := scan.Items[0]
	// 量出来的必须就是会删的:两个旧文件,不含一天以内的那个
	if !it.Found || it.Files != 2 || it.Size != 150 {
		t.Fatalf("量得不对:%+v", it)
	}

	res, err := s.CleanCache("", []string{"t"})
	if err != nil {
		t.Fatal(err)
	}
	c := res.Items[0]
	if c.Deleted != 2 || c.Freed != 150 || c.Recent != 1 {
		t.Fatalf("清理结果不对:%+v", c)
	}
	if exists(old) || exists(deep) {
		t.Error("旧文件没删掉")
	}
	if !exists(fresh) {
		t.Error("一天以内的文件不该删 —— 正在安装的程序可能还在往里写")
	}
	// 删空了的旧目录要收掉。删文件会把目录的修改时间刷新成"现在",
	// 按删完之后的时间判断的话,这两个目录会被当成新目录留下来
	if exists(filepath.Join(root, "sub")) {
		t.Error("删空了的旧子目录没收掉")
	}
	if exists(staleDir) {
		t.Error("旧的空目录没收掉")
	}
	if !exists(freshDir) {
		t.Error("刚建的空目录不该删 —— 程序可能马上要往里写")
	}
	if !exists(root) {
		t.Fatal("缓存目录本身不该删,有的程序发现它没了会报错")
	}
}

func TestCleanCacheMatchOnlyTopLevel(t *testing.T) {
	root := filepath.Join(t.TempDir(), "explorer")
	a := writeN(t, filepath.Join(root, "thumbcache_32.db"), 10)
	b := writeN(t, filepath.Join(root, "THUMBCACHE_IDX.DB"), 10)
	icon := writeN(t, filepath.Join(root, "iconcache_32.db"), 10)
	nested := writeN(t, filepath.Join(root, "sub", "thumbcache_99.db"), 10)

	t.Setenv("TF_TEST_CACHE", root)
	s := testService(testRule("TF_TEST_CACHE", func(r *CacheRule) { r.Match = []string{"thumbcache_*.db"} }))
	res, _ := s.CleanCache("", []string{"t"})
	if res.Items[0].Deleted != 2 || exists(a) || exists(b) {
		t.Fatalf("按名字匹配的两个(大小写不同)都该删掉:%+v", res.Items[0])
	}
	if !exists(icon) {
		t.Error("名字不匹配的删了")
	}
	if !exists(nested) {
		t.Error("按名字匹配的规则只看第一层,子目录里的不该动")
	}
}

// 规则展开出来的目录要是落在家目录、文档这种地方,必须一个都不留下 ——
// 这里只调 resolve(只展开、不删),守卫万一失效也不会真去删家目录
func TestResolveRefusesDangerousRoots(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	t.Setenv("TF_TEST_HOME", home)
	s := testService()
	for _, p := range []string{`%TF_TEST_HOME%`, `%TF_TEST_HOME%` + string(filepath.Separator) + "Documents", `%TF_NO_SUCH_VAR%`} {
		r := CacheRule{ID: "x", Paths: []string{p}}
		if got := s.resolve(r); len(got.dirs) != 0 {
			t.Errorf("%s 不该展开出可清理的目录:%v", p, got.dirs)
		}
	}
}

func TestExpandEnv(t *testing.T) {
	env := map[string]string{"A": `C:\x`, "ProgramFiles(x86)": `C:\pf86`}
	get := func(k string) string { return env[k] }
	if got, ok := expandEnv(`%A%\cache`, get); !ok || got != `C:\x\cache` {
		t.Errorf("展开错了:%q %v", got, ok)
	}
	if got, ok := expandEnv(`%ProgramFiles(x86)%\y`, get); !ok || got != `C:\pf86\y` {
		t.Errorf("带括号的变量名展开错了:%q %v", got, ok)
	}
	// 变量取不到值,拼出来的会是 \cache 这种完全不是本意的地方,整条作废
	if _, ok := expandEnv(`%MISSING%\cache`, get); ok {
		t.Error("取不到值的变量应该让整条作废")
	}
}

func TestExpandGlob(t *testing.T) {
	// 路径前半截里有 [ ]:交给 filepath.Glob 会被当成通配符,一个都匹配不上
	base := filepath.Join(t.TempDir(), "we[ird]", "User Data")
	for _, p := range []string{"Default", "Profile 1"} {
		writeN(t, filepath.Join(base, p, "Cache", "data_0"), 1)
	}
	writeN(t, filepath.Join(base, "System Profile", "prefs"), 1)
	writeN(t, filepath.Join(base, "Local State"), 1) // 叫什么都行的普通文件,不是目录

	got := expandGlob(filepath.Join(base, "*", "Cache"))
	if len(got) != 2 {
		t.Fatalf("应该展开出两个用户配置的缓存目录:%v", got)
	}
	for _, g := range got {
		if !strings.HasSuffix(g, "Cache") {
			t.Errorf("展开错了:%s", g)
		}
	}
	if got := expandGlob(filepath.Join(base, "nope", "Cache")); len(got) != 0 {
		t.Errorf("不存在的路径应该展开成空:%v", got)
	}
}
