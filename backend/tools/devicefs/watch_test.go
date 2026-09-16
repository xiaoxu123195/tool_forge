package devicefs

import (
	"testing"
)

// fakeTransport 一个可以随手改内容的假设备,用来测对比逻辑
type fakeTransport struct {
	entries []Entry
	total   int
	trunc   bool
	// tree 递归查找(search)回什么;DiffTree 走的是这条
	tree      []SearchHit
	treeTrunc bool
}

func (f *fakeTransport) list(dir string) (*Listing, error) {
	total := f.total
	if total == 0 {
		total = len(f.entries)
	}
	return &Listing{
		Path: dir, Entries: f.entries, Total: total, Truncated: f.trunc,
	}, nil
}
func (f *fakeTransport) stat(p string) (*Entry, error) { return &Entry{Path: p}, nil }
func (f *fakeTransport) pull(remote, local string, limit int64) (int64, bool, error) {
	return 0, false, nil
}
func (f *fakeTransport) search(root, pattern string, limit int) (*SearchResult, error) {
	return &SearchResult{
		Root: root, Pattern: pattern,
		Hits: append([]SearchHit{}, f.tree...), Truncated: f.treeTrunc,
	}, nil
}
func (f *fakeTransport) exists(p string) bool { return false }
func (f *fakeTransport) startPath() string    { return "/" }
func (f *fakeTransport) previewLimit() int64  { return 1 << 20 }
func (f *fakeTransport) close() error         { return nil }

func withFake(t *testing.T, ft *fakeTransport) (*Manager, string) {
	t.Helper()
	m := NewManager()
	s := &Session{ID: "dev-1", Platform: "android", t: ft, StartPath: "/"}
	m.sessions[s.ID] = s
	return m, s.ID
}

func file(name string, size, mod int64) Entry {
	return Entry{Name: name, Path: "/d/" + name, Size: size, ModTime: mod}
}

// 第一次只能记基线 —— 没有可比的东西时不该编出一堆"新增"
func TestDiffFirstCallIsBaseline(t *testing.T) {
	ft := &fakeTransport{entries: []Entry{file("a", 1, 100), file("b", 2, 100)}}
	m, id := withFake(t, ft)

	res, err := m.Diff(id, "/d")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Baseline {
		t.Error("第一次该是基线")
	}
	if len(res.Changes) != 0 {
		t.Errorf("基线那次不该有变化,得到 %d 条", len(res.Changes))
	}
	// 空数组不能是 nil —— 前端和 agent 都按数组用
	if res.Changes == nil {
		t.Error("Changes 该是空数组而不是 null")
	}
}

// 新增、删除、改动三种都要认出来,而且只认出这三种
func TestDiffDetectsAllThreeKinds(t *testing.T) {
	ft := &fakeTransport{entries: []Entry{
		file("keep", 10, 100),
		file("gone", 20, 100),
		file("grow", 30, 100),
	}}
	m, id := withFake(t, ft)
	if _, err := m.Diff(id, "/d"); err != nil {
		t.Fatal(err)
	}

	// 在"手机上"动一下:删一个、改一个、加一个
	ft.entries = []Entry{
		file("keep", 10, 100),
		file("grow", 55, 200),
		file("fresh", 7, 300),
	}
	res, err := m.Diff(id, "/d")
	if err != nil {
		t.Fatal(err)
	}
	if res.Baseline {
		t.Fatal("第二次不该还是基线")
	}
	got := map[string]Change{}
	for _, c := range res.Changes {
		got[c.Name] = c
	}
	if len(got) != 3 {
		t.Fatalf("该有 3 处变化,得到 %d: %+v", len(got), res.Changes)
	}
	if got["fresh"].Kind != "added" {
		t.Errorf("fresh 该是 added: %+v", got["fresh"])
	}
	if got["gone"].Kind != "removed" {
		t.Errorf("gone 该是 removed: %+v", got["gone"])
	}
	if got["grow"].Kind != "modified" {
		t.Errorf("grow 该是 modified: %+v", got["grow"])
	}
	// 大小变化要给出来 —— 只说"改了"没法判断是写入还是被清空
	if got["grow"].SizeDelta != 25 {
		t.Errorf("grow 的 SizeDelta 该是 +25,得到 %d", got["grow"].SizeDelta)
	}
	if _, ok := got["keep"]; ok {
		t.Error("没动过的不该出现在变化里")
	}
}

// 只改了修改时间、大小没变,也算动过 —— 数据库被重写成同样大小是常事
func TestDiffCatchesMtimeOnlyChange(t *testing.T) {
	ft := &fakeTransport{entries: []Entry{file("db", 4096, 100)}}
	m, id := withFake(t, ft)
	_, _ = m.Diff(id, "/d")

	ft.entries = []Entry{file("db", 4096, 999)}
	res, _ := m.Diff(id, "/d")
	if len(res.Changes) != 1 || res.Changes[0].Kind != "modified" {
		t.Errorf("大小没变但时间变了,也该算改动: %+v", res.Changes)
	}
}

// 什么都没动就该是空的。有噪声的话"没变化"这个结论就不可信了
func TestDiffQuietWhenNothingChanged(t *testing.T) {
	ft := &fakeTransport{entries: []Entry{file("a", 1, 100), file("b", 2, 200)}}
	m, id := withFake(t, ft)
	_, _ = m.Diff(id, "/d")
	res, _ := m.Diff(id, "/d")
	if len(res.Changes) != 0 {
		t.Errorf("什么都没动却报了变化: %+v", res.Changes)
	}
	if res.Since == 0 {
		t.Error("该带上上一张快照的时间")
	}
}

// 基线是每次对比之后就换新的:连着调三次,第三次比的是第二次
func TestDiffBaselineMovesForward(t *testing.T) {
	ft := &fakeTransport{entries: []Entry{file("a", 1, 100)}}
	m, id := withFake(t, ft)
	_, _ = m.Diff(id, "/d")

	ft.entries = []Entry{file("a", 2, 200)}
	if res, _ := m.Diff(id, "/d"); len(res.Changes) != 1 {
		t.Fatalf("第二次该看到 1 处变化: %+v", res.Changes)
	}
	// 再调一次,这次和上一次之间没动过
	if res, _ := m.Diff(id, "/d"); len(res.Changes) != 0 {
		t.Errorf("基线没往前挪,同一处变化被报了两遍: %+v", res.Changes)
	}
}

// reset 之后要重新当成基线
func TestResetSnapshot(t *testing.T) {
	ft := &fakeTransport{entries: []Entry{file("a", 1, 100)}}
	m, id := withFake(t, ft)
	_, _ = m.Diff(id, "/d")
	m.ResetSnapshot(id, "/d")
	res, _ := m.Diff(id, "/d")
	if !res.Baseline {
		t.Error("reset 之后该重新记基线")
	}
}

// 目录被截断时必须说出来。
// 截断之后"没看到变化"可能只是因为那一条根本没被列出来 —— 不说的话结论是错的
func TestDiffReportsTruncation(t *testing.T) {
	ft := &fakeTransport{
		entries: []Entry{file("a", 1, 100)},
		total:   5000, trunc: true,
	}
	m, id := withFake(t, ft)
	res, _ := m.Diff(id, "/d")
	if !res.Truncated {
		t.Error("目录被截断了却没说")
	}
	if res.Total != 5000 {
		t.Errorf("该报截断前的总数 5000,得到 %d", res.Total)
	}
}

// 同一个会话下不同目录各记各的基线,别串
func TestDiffPerDirectory(t *testing.T) {
	ft := &fakeTransport{entries: []Entry{file("a", 1, 100)}}
	m, id := withFake(t, ft)
	if res, _ := m.Diff(id, "/one"); !res.Baseline {
		t.Fatal("/one 第一次该是基线")
	}
	if res, _ := m.Diff(id, "/two"); !res.Baseline {
		t.Error("/two 是另一个目录,也该从基线开始,而不是拿 /one 的来比")
	}
}

// EnsureSession 要复用现成的,不能每次都新连一条
func TestEnsureSessionReusesLiveOne(t *testing.T) {
	ft := &fakeTransport{}
	m, id := withFake(t, ft)
	s, err := m.EnsureSession(ConnectOptions{Platform: "android"})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != id {
		t.Errorf("该复用已经连着的 %s,却给了 %s", id, s.ID)
	}
	if n := len(m.Sessions()); n != 1 {
		t.Errorf("不该多出会话,现在有 %d 条", n)
	}
}

func hit(p string, size, mod int64) SearchHit { return SearchHit{Path: p, Size: size, ModTime: mod} }

// 递归对比要看到深处的改动 —— 只看一层时 databases/msg.db 被写了一笔,上层目录纹丝不动
func TestDiffTreeSeesDeepChanges(t *testing.T) {
	ft := &fakeTransport{tree: []SearchHit{
		{Path: "/d", IsDir: true, ModTime: 100},
		{Path: "/d/databases", IsDir: true, ModTime: 100},
		hit("/d/databases/msg.db", 4096, 100),
		hit("/d/files/a.txt", 10, 100),
	}}
	m, id := withFake(t, ft)
	res, err := m.DiffTree(id, "/d")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Baseline {
		t.Fatal("第一次该是基线")
	}
	if res.Total != 3 {
		t.Errorf("起点自己不算条目,该是 3,得到 %d", res.Total)
	}

	ft.tree = []SearchHit{
		{Path: "/d", IsDir: true, ModTime: 200}, // 起点的时间跟着直接子项变,不算变化
		{Path: "/d/databases", IsDir: true, ModTime: 100},
		hit("/d/databases/msg.db", 8192, 200),
		hit("/d/databases/msg.db-wal", 512, 200),
	}
	res, err = m.DiffTree(id, "/d")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Change{}
	for _, c := range res.Changes {
		got[c.Path] = c
	}
	if len(got) != 3 {
		t.Fatalf("该有 3 处变化,得到 %+v", res.Changes)
	}
	if c := got["/d/databases/msg.db"]; c.Kind != "modified" || c.SizeDelta != 4096 || c.Name != "msg.db" {
		t.Errorf("msg.db: %+v", c)
	}
	if got["/d/databases/msg.db-wal"].Kind != "added" {
		t.Errorf("wal: %+v", got["/d/databases/msg.db-wal"])
	}
	if got["/d/files/a.txt"].Kind != "removed" {
		t.Errorf("a.txt: %+v", got["/d/files/a.txt"])
	}
	if _, bad := got["/d"]; bad {
		t.Error("起点自己不该算一处变化")
	}
}

// 一层的基线和整棵树的基线分开记;reset 两个一起丢
func TestDiffTreeKeepsSeparateBaselineAndReset(t *testing.T) {
	ft := &fakeTransport{
		entries: []Entry{file("a", 1, 100)},
		tree:    []SearchHit{hit("/d/a", 1, 100)},
	}
	m, id := withFake(t, ft)
	if res, _ := m.Diff(id, "/d"); !res.Baseline {
		t.Fatal("Diff 第一次该是基线")
	}
	if res, _ := m.DiffTree(id, "/d"); !res.Baseline {
		t.Error("DiffTree 有自己的基线,不该拿 Diff 的来比")
	}
	if res, _ := m.DiffTree(id, "/d"); res.Baseline {
		t.Error("第二次不该还是基线")
	}
	m.ResetSnapshot(id, "/d")
	if res, _ := m.DiffTree(id, "/d"); !res.Baseline {
		t.Error("reset 之后整棵树也该重新记基线")
	}
}

func TestDiffTreeReportsTruncation(t *testing.T) {
	ft := &fakeTransport{tree: []SearchHit{hit("/d/a", 1, 100)}, treeTrunc: true}
	m, id := withFake(t, ft)
	if res, _ := m.DiffTree(id, "/d"); !res.Truncated {
		t.Error("子树被截断了却没说")
	}
}
