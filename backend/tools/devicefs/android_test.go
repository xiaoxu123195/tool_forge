package devicefs

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

// Android 那边没有 SFTP,所有信息都是从 stat 的文本输出里抠出来的,
// 所以解析这一步错了就是整个目录都不对。下面每一条都是真机上取的实际输出。

func TestParseStatLine(t *testing.T) {
	cases := []struct {
		name string
		line string
		want Entry
	}{
		{
			"目录",
			"directory|4096|1788762581|/data",
			Entry{Name: "data", Path: "/data", IsDir: true, Size: 4096, ModTime: 1788762581},
		},
		{
			"普通文件",
			"regular file|502056|1230768000|/system/bin/toybox",
			Entry{Name: "toybox", Path: "/system/bin/toybox", Size: 502056, ModTime: 1230768000},
		},
		{
			// %N 对软链给的是 "路径 -> '目标'",目标带单引号
			"软链",
			"symbolic link|21|1230768000|/sdcard -> '/storage/self/primary'",
			Entry{
				Name: "sdcard", Path: "/sdcard", Size: 21, ModTime: 1230768000,
				Symlink: "/storage/self/primary",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseStatLine(c.line)
			if !ok {
				t.Fatal("没解出来")
			}
			if got.Name != c.want.Name || got.Path != c.want.Path ||
				got.IsDir != c.want.IsDir || got.Size != c.want.Size ||
				got.ModTime != c.want.ModTime || got.Symlink != c.want.Symlink {
				t.Errorf("解错了:\n得到 %+v\n想要 %+v", got, c.want)
			}
		})
	}
}

// 文件名里带 | 是合法的,而 stat 的分隔符也是 | ——
// 只切前三个、路径整段留在最后,是这里唯一切得对的做法
func TestParseStatLineKeepsPipeInName(t *testing.T) {
	got, ok := parseStatLine("regular file|10|1700000000|/sdcard/a|b|c.txt")
	if !ok {
		t.Fatal("没解出来")
	}
	if got.Path != "/sdcard/a|b|c.txt" {
		t.Errorf("路径里的 | 被切掉了,得到 %q", got.Path)
	}
	if got.Name != "a|b|c.txt" {
		t.Errorf("文件名不对,得到 %q", got.Name)
	}
}

func TestParseStatLineRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "   ", "只有一段", "两|段", "三|段|了"} {
		if _, ok := parseStatLine(bad); ok {
			t.Errorf("%q 不该被当成有效行", bad)
		}
	}
}

func TestParseStatLinesCollectsHits(t *testing.T) {
	out := "regular file|1|100|/a/x\ndirectory|2|200|/a/sub\n垃圾行\n"
	res := parseStatLines(out, "/a", "*", 10)
	if len(res.Hits) != 2 {
		t.Fatalf("应该 2 条,得到 %d", len(res.Hits))
	}
	if !res.Hits[1].IsDir {
		t.Error("目录没标出来")
	}
	if res.Truncated {
		t.Error("没超上限不该标截断")
	}
	// 超出上限要标出来,不然人以为就这么多
	if r := parseStatLines(out, "/a", "*", 1); !r.Truncated || len(r.Hits) != 1 {
		t.Errorf("截断标记不对: %+v", r)
	}
}

// Hits 必须是空切片不能是 nil —— nil 序列化成 null,前端 .map 当场白屏
func TestParseStatLinesEmptyIsNotNil(t *testing.T) {
	r := parseStatLines("", "/a", "*", 10)
	if r.Hits == nil {
		t.Fatal("Hits 是 nil")
	}
}

// 没 root 时 /data 下面是读不了的,报错得说清楚是这个原因,
// 不然人会以为是路径打错了
func TestRootHint(t *testing.T) {
	if h := (&androidTransport{root: true}).rootHint(); h != "" {
		t.Errorf("有 root 时不该加提示,得到 %q", h)
	}
	if h := (&androidTransport{root: false}).rootHint(); !strings.Contains(h, "root") {
		t.Errorf("没 root 时该点明原因,得到 %q", h)
	}
}

// 有 root 落在 /data/data(各家 App 的数据在那儿),没 root 只能看 /sdcard
func TestAndroidStartPath(t *testing.T) {
	if p := (&androidTransport{root: true}).startPath(); p != "/data/data" {
		t.Errorf("有 root 该落在 /data/data,得到 %q", p)
	}
	if p := (&androidTransport{root: false}).startPath(); p != "/sdcard" {
		t.Errorf("没 root 该落在 /sdcard,得到 %q", p)
	}
}

// 两个平台的预览上限差一个数量级,是因为通道速度差二十倍
func TestPreviewLimitsDifferByPlatform(t *testing.T) {
	if (&androidTransport{}).previewLimit() >= (&iosTransport{}).previewLimit() {
		t.Error("Android 走 base64 文本通道,预览上限应该比 iOS 小")
	}
}

// adb 可执行文件不存在时报错要点明是 adb 的问题
func TestResolveAdbMissingBinary(t *testing.T) {
	_, err := resolveAdb("definitely-not-an-adb-binary")
	if err == nil {
		t.Fatal("adb 不存在时该报错")
	}
	if !strings.Contains(err.Error(), "adb") {
		t.Errorf("报错该点明是 adb 的问题,得到: %v", err)
	}
}

// ---------- adb 版本 ----------
//
// 这一组守的是一次真实事故:点了连接之后界面一直停在"连接中"。
// 根因有两层,都在下面钉住了。

func TestParseAdbVersion(t *testing.T) {
	cases := []struct {
		out       string
		wantFull  string
		wantMinor int
	}{
		{"Android Debug Bridge version 1.0.41\nVersion 37.0.1-15733141\n", "1.0.41", 41},
		{"Android Debug Bridge version 1.0.26\n", "1.0.26", 26},
		{"", "", 0},
		{"什么都不是", "", 0},
	}
	for _, c := range cases {
		full, minor := parseAdbVersion(c.out)
		if full != c.wantFull || minor != c.wantMinor {
			t.Errorf("parseAdbVersion(%q) = (%q, %d),想要 (%q, %d)",
				c.out, full, minor, c.wantFull, c.wantMinor)
		}
	}
}

// 版本太老的 adb 不会报错,只会给一个空的设备列表 —— 而空列表看起来就是
// "线没插好",人会去查线、换口、重启手机,全是白费功夫。所以必须提前拦下来说清楚
func TestResolveAdbRejectsAncientVersion(t *testing.T) {
	const ancient = `C:/WINDOWS/adb.exe`
	if _, err := os.Stat(ancient); err != nil {
		t.Skip("这台机器上没有那个老版本,跳过")
	}
	_, err := resolveAdb(ancient)
	if err == nil {
		t.Fatal("指向 1.0.26 时应该直接拦下来")
	}
	for _, want := range []string{"版本过老", "1.0.26", "platform-tools"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("报错里应该有 %q,得到: %v", want, err)
		}
	}
}

// 不填路径时优先用应用自己那份,而不是奔 PATH ——
// Windows 上一堆手机助手会把老 adb 塞进系统目录,还排在 PATH 前面
func TestResolveAdbPrefersBundled(t *testing.T) {
	b := bundledAdbPath()
	if b == "" {
		t.Skip("这台机器上没有自带的 adb,跳过")
	}
	got, err := resolveAdb("")
	if err != nil {
		t.Fatalf("不填路径时应该能解出一个可用的 adb: %v", err)
	}
	if got != b {
		t.Errorf("应该优先用自带的 %q,得到 %q", b, got)
	}
}

// runCmd 的超时必须真的能生效 —— 这条守的是一次真实事故:
// 点了连接之后界面一直停在"连接中",而代码里每一步都设了超时。
//
// 根因:adb 第一次被调用会 fork 一个常驻的 server 守护进程,它继承了
// 我们这条 stdout 管道的写端。Go 的 Wait 要等 io 拷贝 goroutine 结束,
// 管道又要等**所有**持有写端的进程退出才关闭 —— 守护进程永远不退,
// 于是 context 到期把 adb 本身杀了也没用,Wait 照样卡死,超时形同虚设。
//
// 这里不去调真的 adb(那要求机器上正好装着那个老版本,而且会踢掉
// 别人正在用的 adb 服务),而是用测试二进制自己复刻同一个形状:
// 子进程拉起一个继承了 stdout 的孙进程,然后自己先退出。
func TestRunCmdTimeoutSurvivesLingeringGrandchild(t *testing.T) {
	start := time.Now()
	// 超时压到 1 秒:整条用例的耗时由它 + WaitDelay + 孙进程存活时间决定,
	// 而这条要跟着每次 go test 跑
	const timeout = 1 * time.Second
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = runCmd(context.Background(), timeout,
			os.Args[0], "-test.run=TestSpawnLingeringChildHelper")
	}()

	// 上限 = 超时 + WaitDelay,再留一点余量
	limit := timeout + cmdWaitDelay + 5*time.Second
	select {
	case <-done:
		if d := time.Since(start); d > limit {
			t.Errorf("超时兜住了但花了 %s,超过 %s", d, limit)
		}
	case <-time.After(limit):
		t.Fatal("runCmd 卡住了 —— 孙进程占着 stdout 管道,Wait 回不来。" +
			"这就是界面上「连接中」一直转的那个原因")
	}
	// 等孙进程自己死掉再走。不等的话它会一直握着测试二进制,
	// go test 收尾时删不掉那个 exe,整包报 "Access is denied"
	if left := lingerFor - time.Since(start); left > 0 {
		time.Sleep(left + 300*time.Millisecond)
	}
}

// lingerFor 孙进程活多久。只要盖过 timeout + WaitDelay 这个窗口就够,
// 再长就是白等 —— 它退不掉的话会卡住整包测试的收尾
const lingerFor = 5 * time.Second

// TestSpawnLingeringChildHelper 不是一条用例,是上面那条的子进程。
// 只有被显式点名跑时才干活,正常跑整包测试时直接跳过。
func TestSpawnLingeringChildHelper(t *testing.T) {
	if !slices.Contains(os.Args, "-test.run=TestSpawnLingeringChildHelper") {
		t.Skip("这是给 TestRunCmdTimeoutSurvivesLingeringGrandchild 用的子进程,不单独跑")
	}
	// 拉起一个活得比自己久、而且继承了 stdout 的孙进程 —— 这就是 adb 守护进程的形状
	child := exec.Command(os.Args[0], "-test.run=TestSleepForeverHelper")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatalf("拉不起孙进程: %v", err)
	}
	// 自己立刻返回,不 Wait:父进程那头的管道现在只剩孙进程握着
}

// TestSleepForeverHelper 孙进程:睡到被杀
func TestSleepForeverHelper(t *testing.T) {
	if !slices.Contains(os.Args, "-test.run=TestSleepForeverHelper") {
		t.Skip("这是给上面那条用的孙进程,不单独跑")
	}
	time.Sleep(lingerFor)
}
