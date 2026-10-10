package diskclean

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testService 用真的守卫、不带缓存规则。测试目录都在系统临时目录下,不受保护
func testService(rules ...CacheRule) *Service {
	return &Service{guard: NewGuard(), rules: rules, jobs: map[string]context.CancelFunc{}}
}

func writeFile(t *testing.T, p string, data []byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeN(t *testing.T, p string, n int) string {
	return writeFile(t, p, bytes.Repeat([]byte{'z'}, n))
}

// age 把文件或目录的修改时间往前拨
func age(t *testing.T, p string, d time.Duration) {
	t.Helper()
	old := time.Now().Add(-d)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
}

// realTempDir 解到底的临时目录。GitHub 的 Windows 机器上临时目录是 8.3 短文件名
// (C:\Users\RUNNER~1\…),而快捷方式里记的、系统转出来的路径都是长名:拿短名去比永远对不上
func realTempDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if p, err := finalPath(d); err == nil {
		return p
	}
	return d
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// refOf 拿到一个文件此刻的样子,模拟"扫描时看到的"
func refOf(t *testing.T, p string) FileRef {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return FileRef{Path: p, Size: fi.Size(), ModTime: fi.ModTime().Unix()}
}
