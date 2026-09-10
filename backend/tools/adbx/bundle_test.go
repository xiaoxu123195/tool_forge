package adbx

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeHome 把 HOME 指到一个临时目录,免得测试动到真的 ~/.toolforge
func fakeHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// os.UserHomeDir 在 Windows 上看 USERPROFILE,别的平台看 HOME
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)
	got, err := os.UserHomeDir()
	if err != nil || got != dir {
		t.Skipf("这个平台上改不掉 home(拿到 %q),跳过", got)
	}
	return dir
}

// 内置的 adb 解出来之后必须真的能跑。
//
// 断言"三个文件出现了"是不够的:少一个 DLL、或者解包时把目录层级带出来,
// 文件数照样对,adb.exe 却起不来 —— 而那正是这套东西唯一的意义
func TestEnsureBundledProducesRunnableAdb(t *testing.T) {
	if !HasBundledPayload() {
		t.Skip("这次构建没带内置 adb")
	}
	fakeHome(t)

	exe, err := EnsureBundled()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		t.Skip("载荷目前是 Windows 的三件套,别的平台只验证解包")
	}
	out, _, err := RunLocal(context.Background(), 20*time.Second, exe, "version")
	if err != nil {
		t.Fatalf("解出来的 adb 跑不起来: %v", err)
	}
	_, minor := ParseVersion(out)
	if minor < MinUsableVersion {
		t.Errorf("内置的 adb 版本太老了(末位 %d),认不出现代设备", minor)
	}
	// 两个 DLL 必须和 adb.exe 躺在同一层,adb 是在自己旁边找它们的
	dir := filepath.Dir(exe)
	for _, name := range []string{"AdbWinApi.dll", "AdbWinUsbApi.dll"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s 没和 adb.exe 放在一起: %v", name, err)
		}
	}
}

// 第二次不该重解一遍 —— 每次连设备都往磁盘写 8MB 没有道理
func TestEnsureBundledSkipsSecondTime(t *testing.T) {
	if !HasBundledPayload() {
		t.Skip("这次构建没带内置 adb")
	}
	fakeHome(t)

	exe, err := EnsureBundled()
	if err != nil {
		t.Fatal(err)
	}
	st1, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	// 改一下内容,再解一次;没被重写就说明走了跳过那条
	if err := os.WriteFile(exe, []byte("动过了"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureBundled(); err != nil {
		t.Fatal(err)
	}
	st2, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	if st2.Size() == st1.Size() {
		t.Error("标记在,却还是重解了一遍")
	}
}

// 用户自己放进去的 adb 不许覆盖。
//
// 他把某个版本手动放在那儿多半是有原因的(比如厂商定制的那份才认得出他的设备),
// 我们拿自带的盖掉,他的问题就又回来了,而且完全看不出是谁干的
func TestEnsureBundledKeepsManualInstall(t *testing.T) {
	if !HasBundledPayload() {
		t.Skip("这次构建没带内置 adb")
	}
	home := fakeHome(t)

	dir := filepath.Join(home, ".toolforge", "platform-tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(dir, adbExeName())
	const marker = "这是用户自己放的"
	if err := os.WriteFile(mine, []byte(marker), 0o755); err != nil {
		t.Fatal(err)
	}

	exe, err := EnsureBundled()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != marker {
		t.Error("把用户自己装的 adb 覆盖掉了")
	}
}

// 载荷是入了库的二进制,重打包打错了不会有任何编译错误 ——
// 症状要到用户那台机器上才出现,而且长得像"adb 起不来"这种最难查的样子。
// 所以把要求写死在这儿:就那三个文件、平铺、都不是空的。
//
// 这条不依赖 Windows,在哪儿都能跑
func TestPayloadHasExactlyWhatAdbNeeds(t *testing.T) {
	if !HasBundledPayload() {
		t.Skip("这次构建没带内置 adb")
	}
	payload, err := bundledFS.ReadFile(payloadName)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("载荷不是个 gzip: %v", err)
	}
	defer zr.Close()

	want := map[string]bool{
		"adb.exe":          false,
		"AdbWinApi.dll":    false,
		"AdbWinUsbApi.dll": false,
	}
	var total int64
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("载荷读不下去: %v", err)
		}
		if _, ok := want[hdr.Name]; !ok {
			t.Errorf("载荷里有多余的东西: %q —— 只该带 adb 要用的那三个", hdr.Name)
			continue
		}
		// 带了目录层级的话,adb.exe 就找不到旁边的 DLL 了
		if strings.ContainsAny(hdr.Name, `/\`) {
			t.Errorf("%q 带了目录层级,必须平铺", hdr.Name)
		}
		if hdr.Size == 0 {
			t.Errorf("%q 是空的", hdr.Name)
		}
		want[hdr.Name] = true
		total += hdr.Size
	}
	for name, got := range want {
		if !got {
			t.Errorf("载荷里缺 %q", name)
		}
	}
	// 整包 platform-tools 解开是 17MB 以上。真装进来了说明打包脚本没走对,
	// 每个用户白下十几 MB
	const tooBig = 12 << 20
	if total > tooBig {
		t.Errorf("载荷解开有 %d 字节,像是把整个 platform-tools 都装进来了", total)
	}
}

// 没带载荷的构建要老实说没有,而不是报个看不懂的错
func TestNoPayloadIsNotAnError(t *testing.T) {
	if HasBundledPayload() {
		t.Skip("这次构建带了载荷")
	}
	if _, err := EnsureBundled(); err == nil {
		t.Error("没有载荷时该明确说没有")
	} else if !strings.Contains(err.Error(), "内置") {
		t.Errorf("错误信息该说清楚是没有内置 adb: %v", err)
	}
}
