package diskclean

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanLargeKeepsTheBiggest(t *testing.T) {
	dir := t.TempDir()
	writeN(t, filepath.Join(dir, "a.bin"), 3000)
	writeN(t, filepath.Join(dir, "sub", "b.bin"), 5000)
	writeN(t, filepath.Join(dir, "sub", "deep", "c.bin"), 4000)
	writeN(t, filepath.Join(dir, "small.txt"), 10)
	writeN(t, filepath.Join(dir, "disk.vhdx"), 2000)

	s := testService()
	res, err := s.ScanLarge(LargeOptions{Roots: []string{dir}, MinSize: 1000, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.Scanned != 5 {
		t.Errorf("看了 %d 个文件,应该是 5 个", res.Scanned)
	}
	// 超过阈值的有 4 个,只交回最大的 2 个 —— 被截掉的必须让人知道
	if res.Matched != 4 || res.MatchedBytes != 14000 {
		t.Errorf("超过阈值的:%d 个 / %d 字节,应为 4 / 14000", res.Matched, res.MatchedBytes)
	}
	if len(res.Files) != 2 || res.Files[0].Name != "b.bin" || res.Files[1].Name != "c.bin" {
		t.Fatalf("应该按大小从大到小给出 b.bin、c.bin,实际 %+v", res.Files)
	}

	res, _ = s.ScanLarge(LargeOptions{Roots: []string{dir}, MinSize: 1000})
	var vhdx *LargeFile
	for i := range res.Files {
		if res.Files[i].Ext == "vhdx" {
			vhdx = &res.Files[i]
		}
	}
	if vhdx == nil || !strings.Contains(vhdx.Warn, "磁盘镜像") || vhdx.Blocked {
		t.Fatalf("虚拟机磁盘应该能删、但要带提醒:%+v", vhdx)
	}
}

func TestScanLargeRoots(t *testing.T) {
	dir := t.TempDir()
	writeN(t, filepath.Join(dir, "sub", "a.bin"), 3000)
	s := testService()

	// 选了父目录又选了子目录,子目录里的文件只能算一次
	res, err := s.ScanLarge(LargeOptions{Roots: []string{filepath.Join(dir, "sub"), dir}, MinSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched != 1 {
		t.Fatalf("同一个文件被算了 %d 次", res.Matched)
	}
	if _, err := s.ScanLarge(LargeOptions{Roots: []string{}}); err == nil {
		t.Fatal("没选起点应该报错")
	}
	if _, err := s.ScanLarge(LargeOptions{Roots: []string{"relative"}}); err == nil {
		t.Fatal("相对路径应该报错")
	}
	if _, err := s.ScanLarge(LargeOptions{Roots: []string{filepath.Join(dir, "nope")}}); err == nil {
		t.Fatal("不存在的目录应该报错")
	}
}

func TestScanLargeCancelled(t *testing.T) {
	dir := t.TempDir()
	writeN(t, filepath.Join(dir, "a.bin"), 3000)
	s := testService()
	// 应用退出时 Wails 的 ctx 就是这样被取消的,进行中的扫描要跟着停
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.ctx = ctx
	res, err := s.ScanLarge(LargeOptions{Roots: []string{dir}, MinSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Cancelled {
		t.Fatal("被取消的扫描必须如实说被取消了,不能装作扫完了")
	}
}

func TestCancelByJobID(t *testing.T) {
	s := testService()
	ctx, end := s.begin("job-1")
	defer end()
	s.Cancel("job-1")
	if ctx.Err() == nil {
		t.Fatal("按任务 ID 取消没有生效")
	}
	s.Cancel("no-such-job") // 取消一个不存在的任务不该出事
}

func TestDeleteFilesChecksBeforeDeleting(t *testing.T) {
	dir := t.TempDir()
	keep := writeN(t, filepath.Join(dir, "changed.bin"), 100)
	gone := writeN(t, filepath.Join(dir, "gone.bin"), 100)
	ok := writeN(t, filepath.Join(dir, "ok.bin"), 100)

	s := testService()
	stale := refOf(t, keep)
	// 扫描之后文件被改过了:大小变了就不能照着扫描结果去删
	writeN(t, keep, 200)
	missing := refOf(t, gone)
	os.Remove(gone)

	res, err := s.DeleteFiles(DeleteRequest{
		Files:     []FileRef{stale, missing, refOf(t, ok), {Path: `relative.bin`}},
		Permanent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 1 || res.Failed != 3 || res.Bytes != 100 {
		t.Fatalf("应该只删掉 ok.bin:%+v", res)
	}
	if !exists(keep) || exists(ok) {
		t.Fatal("删错了文件")
	}
	reasons := map[string]string{}
	for _, it := range res.Items {
		reasons[filepath.Base(it.Path)] = it.Reason
	}
	if !strings.Contains(reasons["changed.bin"], "变了") {
		t.Errorf("改过的文件该说「变了」,实际:%q", reasons["changed.bin"])
	}
	if !strings.Contains(reasons["gone.bin"], "不在") {
		t.Errorf("没了的文件该说「不在了」,实际:%q", reasons["gone.bin"])
	}
	if res.Recycled {
		t.Error("选了永久删除,结果却说进了回收站")
	}
}
