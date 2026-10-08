//go:build windows

package diskclean

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mklinkJ 建一个目录联接。联接不需要管理员权限,符号链接才要
func mklinkJ(t *testing.T, link, target string) {
	t.Helper()
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Skipf("建不了目录联接:%v %s", err, out)
	}
}

func TestWalkDoesNotFollowJunctions(t *testing.T) {
	base := t.TempDir()
	writeN(t, filepath.Join(base, "outside", "big.bin"), 5000)
	root := filepath.Join(base, "root")
	writeN(t, filepath.Join(root, "a.bin"), 3000)
	mklinkJ(t, filepath.Join(root, "link"), filepath.Join(base, "outside"))

	res, err := testService().ScanLarge(LargeOptions{Roots: []string{root}, MinSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched != 1 || res.SkippedLinks != 1 {
		t.Fatalf("不该跟进联接:找到 %d 个,跳过 %d 个链接", res.Matched, res.SkippedLinks)
	}
}

// 缓存目录里混进一个联接,指向用户的文档:清缓存时绝不能顺着它删过去
func TestCleanDoesNotFollowJunctions(t *testing.T) {
	base := t.TempDir()
	precious := writeN(t, filepath.Join(base, "precious", "doc.txt"), 100)
	age(t, precious, 72*time.Hour)
	root := filepath.Join(base, "cache")
	junk := writeN(t, filepath.Join(root, "c.bin"), 10)
	mklinkJ(t, filepath.Join(root, "evil"), filepath.Join(base, "precious"))

	t.Setenv("TF_TEST_CACHE", root)
	s := testService(testRule("TF_TEST_CACHE", nil))
	if _, err := s.CleanCache("", []string{"t"}); err != nil {
		t.Fatal(err)
	}
	if !exists(precious) {
		t.Fatal("顺着联接删到了缓存目录外面的文件")
	}
	if exists(junk) {
		t.Error("缓存目录里的普通文件没删")
	}
}

// 守卫光看字符串的话,一个指向受保护目录的联接就能绕过去
func TestCheckFinalSeesThroughJunctions(t *testing.T) {
	base := t.TempDir()
	protected := filepath.Join(base, "protected")
	sys := writeN(t, filepath.Join(protected, "sys.dll"), 10)
	g := newGuard(guardSpec{trees: []treeSpec{{root: protected, reason: "测试用的受保护目录"}}})
	link := filepath.Join(base, "innocent")
	mklinkJ(t, link, protected)

	p := filepath.Join(link, "sys.dll")
	if g.Check(p).Blocked {
		t.Fatal("只看字符串时这里本来拦不住 —— 测试本身失效了")
	}
	if v := g.CheckFinal(p); !v.Blocked || !strings.Contains(v.Reason, "测试用的受保护目录") {
		t.Fatalf("解到真实路径之后应该拦住:%+v", v)
	}
	if v := g.CheckContentsFinal(link); !v.Blocked {
		t.Fatal("指向受保护目录的联接不能被当成缓存目录清空")
	}

	s := testService()
	s.guard = g
	res, _ := s.DeleteFiles(DeleteRequest{Files: []FileRef{refOf(t, p)}, Permanent: true})
	if res.Deleted != 0 || !exists(sys) {
		t.Fatal("通过联接删到了受保护的文件")
	}
}

func TestCleanSkipsLockedFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	locked := writeN(t, filepath.Join(root, "locked.bin"), 10)
	free := writeN(t, filepath.Join(root, "free.bin"), 10)
	f, err := os.Open(locked) // Go 在 Windows 上打开文件不带"允许删除"的共享位,正好模拟被占用
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	t.Setenv("TF_TEST_CACHE", root)
	s := testService(testRule("TF_TEST_CACHE", nil))
	res, _ := s.CleanCache("", []string{"t"})
	c := res.Items[0]
	if c.InUse != 1 || c.Deleted != 1 || !exists(locked) || exists(free) {
		t.Fatalf("被占用的要跳过并记下来,其余照删:%+v", c)
	}
}

func TestVolumes(t *testing.T) {
	vs := volumes()
	if len(vs) == 0 {
		t.Fatal("一块盘都没列出来")
	}
	sys := 0
	for _, v := range vs {
		if v.System {
			sys++
			if v.Total <= 0 || v.Free < 0 || v.Free > v.Total {
				t.Errorf("系统盘的容量不对:%+v", v)
			}
		}
	}
	if sys != 1 {
		t.Errorf("应该恰好有一块系统盘,实际 %d 块", sys)
	}
}

// 真往回收站里放东西,默认不跑:每跑一次用户的回收站里就多一个测试文件。
// 改动 moveToTrash 或那几个结构体之后,设 TF_TEST_RECYCLE=1 手动跑一次
func TestMoveToTrash(t *testing.T) {
	if os.Getenv("TF_TEST_RECYCLE") != "1" {
		t.Skip("设 TF_TEST_RECYCLE=1 才跑")
	}
	_, before, err := recycleBinInfo()
	if err != nil {
		t.Fatal(err)
	}
	p := writeN(t, filepath.Join(t.TempDir(), "tool-forge-recycle-test.txt"), 16)
	if err := moveToTrash(p); err != nil {
		t.Fatal(err)
	}
	if exists(p) {
		t.Fatal("文件还在原处")
	}
	_, after, err := recycleBinInfo()
	if err != nil {
		t.Fatal(err)
	}
	if after != before+1 {
		t.Fatalf("回收站里的条目数从 %d 变成了 %d,应该多一个", before, after)
	}
}
