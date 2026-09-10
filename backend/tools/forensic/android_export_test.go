package forensic

import (
	"archive/tar"
	"fmt"
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
		{"ios", "export", "-o", "D:/out", "-a", "root@192.168.1.9:22"}, // 地址指向别处
		{"android", "export", "-o"},                                    // flag 后面没值
		{"android", "device", "list"},                                  // 不是 export
		{"windows", "export", "-o", "D:/out"},                          // 不认识的平台
		{"android", "export", "-k", "wechat"},                          // 没给输出目录
		{"android"},                                                    // 不完整
		{},                                                             // 空
	}
	for _, args := range cases {
		if opt, ok := parseExportArgs(args); nativeSupported(opt, ok) {
			t.Errorf("%v 不该被当成原生能跑的命令", args)
		}
	}
}

// iOS 走原生,而且要把 SSH 账号从地址里拆出来
func TestIOSGoesNative(t *testing.T) {
	opt, ok := parseExportArgs([]string{
		"ios", "export", "-s", "/var/mobile", "-o", "D:/out",
		"-a", "root@127.0.0.1:22", "-p", "123456",
	})
	if !ok || !nativeSupported(opt, ok) {
		t.Fatal("iOS 的 export 现在有原生实现了")
	}
	if opt.user != "root" || opt.password != "123456" {
		t.Errorf("SSH 账号没解出来: user=%q password=%q", opt.user, opt.password)
	}
}

// 地址指向别处时必须回落到命令行。
//
// 原生这条路是顺着 USB 找设备的,而一个指向网络上某台机器的地址,
// 说的是完全另一件事 —— 悄悄换成 USB 就是连错了对象,
// 而且连错之后还会一切正常地导出另一台设备的数据
func TestIOSRemoteAddrFallsBackToCLI(t *testing.T) {
	for _, addr := range []string{
		"root@192.168.1.9:22",
		"root@10.0.0.2",
		"mobile@my-iphone.local:2222",
	} {
		opt, ok := parseExportArgs([]string{"ios", "export", "-s", "/var/mobile", "-o", "D:/out", "-a", addr})
		if nativeSupported(opt, ok) {
			t.Errorf("%s 指向的不是本机,不该走原生", addr)
		}
	}
	// 本机的几种写法都该算本机
	for _, addr := range []string{"root@127.0.0.1:22", "root@localhost:22", "root@127.0.0.1"} {
		opt, ok := parseExportArgs([]string{"ios", "export", "-s", "/var/mobile", "-o", "D:/out", "-a", addr})
		if !nativeSupported(opt, ok) {
			t.Errorf("%s 是本机地址,应该走原生", addr)
		}
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

// 去重的边界:比的是路径分段而不是字符串前缀。
// /sdcard/Download 和 /sdcard/Downloads 前缀上是包含关系,位置上却毫无关系 ——
// 按前缀比会把后一个整个当成前一个的子目录漏掉
func TestContainedIn(t *testing.T) {
	list := []string{"/sdcard/Download", "/data/user/0/com.taobao.idlefish"}
	yes := []string{
		"/sdcard/Download",
		"/sdcard/Download/a.txt",
		"/data/user/0/com.taobao.idlefish/databases",
	}
	for _, p := range yes {
		if _, ok := containedIn(p, list); !ok {
			t.Errorf("%q 应该算在里面", p)
		}
	}
	no := []string{
		"/sdcard/Downloads",
		"/sdcard/Download2/x",
		"/data/user/0/com.taobao.idlefish2",
		"/sdcard",
	}
	for _, p := range no {
		if outer, ok := containedIn(p, list); ok {
			t.Errorf("%q 不该被当成 %q 的子目录", p, outer)
		}
	}
}

// 清空是不可撤销的操作,路径少打一层就成了别的目录。
// 根目录必须拦住 —— 那是最容易打出来、后果也最大的一种
func TestSafeToClearRefusesRoots(t *testing.T) {
	bad := []string{string(os.PathSeparator)}
	if runtime.GOOS == "windows" {
		bad = append(bad, `C:\`, `D:\`, `c:\`)
	}
	for _, p := range bad {
		if err := safeToClear(p); err == nil {
			t.Errorf("%q 是根目录,应该拒绝清空", p)
		}
	}
	// 正常的输出目录不该被误伤
	dir := t.TempDir()
	if err := safeToClear(dir); err != nil {
		t.Errorf("%q 是个普通目录,不该被拦: %v", dir, err)
	}
	// 文件不是目录
	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := safeToClear(f); err == nil {
		t.Error("目标是文件时应该拒绝")
	}
}

// 清空要真的清干净,而且要把删了多少说出来 ——
// 取证里"这个目录原来有东西"本身就是需要留痕的事
func TestClearOutputReportsWhatItRemoved(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "旧的一次", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "旧的一次", "sub", "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	var logged []string
	log := func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }
	if err := clearDir(dir, log); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Errorf("没清干净,还剩 %d 个", len(ents))
	}
	if len(logged) == 0 || !strings.Contains(logged[0], "2") {
		t.Errorf("删了多少要说出来,日志是: %v", logged)
	}
	// 目录本身要留着,不然接下来没地方写
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Errorf("输出目录本身被删掉了: %v", err)
	}
}

// 没要求清空时,一个字节都不许动
func TestNoClearKeepsEverything(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "上一次取的证.txt")
	if err := os.WriteFile(keep, []byte("重要"), 0o644); err != nil {
		t.Fatal(err)
	}
	var logged []string
	log := func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }
	warnIfNotEmpty(dir, log)
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("只是提醒,不该删东西: %v", err)
	}
	if len(logged) != 1 || !strings.HasPrefix(logged[0], "WARN") {
		t.Errorf("目录非空该警告一声,日志是: %v", logged)
	}
}
