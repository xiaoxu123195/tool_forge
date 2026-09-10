package forensic

import (
	"archive/tar"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 参数解析是原生实现和命令行之间的岔路口。解错了有两种后果,都很糟:
// 认成原生却少解了一个 flag —— 那个 flag 被悄悄忽略;
// 认不出来白白回落到命令行 —— 用户以为在走原生其实没有。

func TestParseExportArgs(t *testing.T) {
	opt, ok := parseExportArgs([]string{
		"android", "export",
		"-k", "wechat", "-k", "微信",
		"-s", "/data/data/com.tencent.mm",
		"-o", "D:/out",
	})
	if !ok {
		t.Fatal("这是一条标准的导出命令,应该解得出来")
	}
	if opt.platform != "android" || opt.output != "D:/out" {
		t.Errorf("平台或输出目录不对: %+v", opt)
	}
	if len(opt.keywords) != 2 || opt.keywords[1] != "微信" {
		t.Errorf("关键词应该能重复给: %+v", opt.keywords)
	}
	if len(opt.paths) != 1 {
		t.Errorf("路径不对: %+v", opt.paths)
	}
}

// 出现没实现的 flag 时必须回落到命令行,而不是把它当没看见 ——
// 悄悄忽略一个 flag 意味着用户以为设了某个条件,实际没生效
func TestParseExportArgsFallsBackOnUnknownFlag(t *testing.T) {
	cases := [][]string{
		{"android", "export", "-o", "D:/out", "--some-new-flag", "x"},
		{"ios", "export", "-o", "D:/out", "-a", "root@127.0.0.1:22"},
		{"android", "export", "-o"},           // flag 后面没值
		{"android", "device", "list"},         // 不是 export
		{"windows", "export", "-o", "D:/out"}, // 不认识的平台
		{"android", "export", "-k", "wechat"}, // 没给输出目录
		{"android"},                           // 不完整
		{},                                    // 空
	}
	for _, args := range cases {
		if opt, ok := parseExportArgs(args); nativeSupported(opt, ok) {
			t.Errorf("%v 不该被当成原生能跑的命令", args)
		}
	}
}

// iOS 解得出来,但目前不走原生 —— 必须回落
func TestIOSStillFallsBackToCLI(t *testing.T) {
	opt, ok := parseExportArgs([]string{"ios", "export", "-s", "/var/mobile", "-o", "D:/out"})
	if !ok {
		t.Fatal("iOS 的参数本身是能解的")
	}
	if nativeSupported(opt, ok) {
		t.Error("iOS 还没有原生实现,不该走原生那条")
	}
}

// tar 包是从被取证的设备上拿来的,内容不可信。
// 包里可以写 ../../ 这样的成员名,不拦的话解包会把文件写到目标目录之外
func TestUntarRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	evil := filepath.Join(dir, "evil.tar")
	f, err := os.Create(evil)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	body := []byte("被写到外面去了")
	hdr := &tar.Header{
		Name:     "../../escaped.txt",
		Mode:     0o644,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = f.Close()

	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = untar(evil, dest)
	if err == nil {
		t.Fatal("指向目录之外的成员应该被拒绝")
	}
	if !strings.Contains(err.Error(), "目标目录之外") {
		t.Errorf("报错该点明是路径越界,得到: %v", err)
	}
	// 确认真的没写出去
	if _, err := os.Stat(filepath.Join(dir, "..", "escaped.txt")); err == nil {
		t.Error("文件真的被写到目标目录之外了")
	}
}

// 正常的包要能解开,而且目录结构保持住
func TestUntarNormal(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "ok.tar")
	f, _ := os.Create(src)
	tw := tar.NewWriter(f)
	_ = tw.WriteHeader(&tar.Header{Name: "app", Mode: 0o755, Typeflag: tar.TypeDir})
	body := []byte("hello")
	_ = tw.WriteHeader(&tar.Header{
		Name: "app/shared_prefs/a.xml", Mode: 0o644,
		Size: int64(len(body)), Typeflag: tar.TypeReg,
	})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = f.Close()

	dest := filepath.Join(dir, "out")
	res, err := untar(src, dest)
	if err != nil {
		t.Fatalf("正常的包应该解得开: %v", err)
	}
	if res.files != 1 {
		t.Errorf("应该解出 1 个文件,得到 %d", res.files)
	}
	got, err2 := os.ReadFile(filepath.Join(dest, "app", "shared_prefs", "a.xml"))
	if err2 != nil {
		t.Fatalf("解出来的文件不在: %v", err2)
	}
	if string(got) != "hello" {
		t.Errorf("内容不对: %q", got)
	}
}

func TestSanitize(t *testing.T) {
	if got := sanitize("com.tencent.mm"); got != "com.tencent.mm" {
		t.Errorf("正常包名不该被改: %q", got)
	}
	for _, bad := range []string{"/", "\\", ":", "*", "?", `"`, "<", ">", "|"} {
		if strings.Contains(sanitize("a"+bad+"b"), bad) {
			t.Errorf("%q 没被替换掉", bad)
		}
	}
}

// 安卓上合法的名字在 Windows 上未必合法 —— 微信就有个目录叫
// com.tencent.mm:appbrand0,冒号在 Windows 上是非法字符,mkdir 直接失败。
// 这条守的是"整包解包因为一个名字全废"那次真实失败
func TestSafeSegmentForWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("只在 Windows 上改名 —— 别的系统上冒号是合法的,在那儿改名等于把证据改坏了")
	}
	cases := map[string]string{
		"com.tencent.mm:appbrand0": "com.tencent.mm_appbrand0",
		"normal_name":              "normal_name",
		"a<b>c":                    "a_b_c",
		"trailing.":                "trailing",
		"trailing ":                "trailing",
		"con":                      "con_",
		"CON.txt":                  "CON.txt_",
		"aux":                      "aux_",
		"":                         "",
	}
	for in, want := range cases {
		if got := safeSegment(in); got != want {
			t.Errorf("safeSegment(%q) = %q,想要 %q", in, got, want)
		}
	}
	p, changed := localSafePath("app/com.tencent.mm:appbrand0/x.txt")
	if !changed || p != "app/com.tencent.mm_appbrand0/x.txt" {
		t.Errorf("整条路径没改对: %q changed=%v", p, changed)
	}
	// 正常路径一个字节都不该动
	if p2, changed2 := localSafePath("app/ok/x.txt"); changed2 || p2 != "app/ok/x.txt" {
		t.Errorf("正常路径不该动: %q changed=%v", p2, changed2)
	}
}

// 改了名必须能被上报出去 —— 取证里文件名本身就是证据的一部分,
// 悄悄换掉而不吭声是在给后面的人埋雷
func TestUntarReportsRenames(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("只在 Windows 上会改名")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "r.tar")
	f, _ := os.Create(src)
	tw := tar.NewWriter(f)
	body := []byte("x")
	_ = tw.WriteHeader(&tar.Header{
		Name: "app/com.tencent.mm:appbrand0/a.txt", Mode: 0o644,
		Size: int64(len(body)), Typeflag: tar.TypeReg,
	})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = f.Close()

	dest := filepath.Join(dir, "out")
	res, err := untar(src, dest)
	if err != nil {
		t.Fatalf("带冒号的名字应该改名后解开,而不是整包失败: %v", err)
	}
	if res.files != 1 {
		t.Errorf("应该解出 1 个文件,得到 %d", res.files)
	}
	if res.renamed != 1 {
		t.Errorf("应该报告 1 处改名,得到 %d", res.renamed)
	}
	if len(res.samples) == 0 || !strings.Contains(res.samples[0], "appbrand0") {
		t.Errorf("改名记录里应该看得出改的是哪个: %v", res.samples)
	}
	if _, err := os.Stat(filepath.Join(dest, "app", "com.tencent.mm_appbrand0", "a.txt")); err != nil {
		t.Errorf("改名后的文件不在: %v", err)
	}
}
