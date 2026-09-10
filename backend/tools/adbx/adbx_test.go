package adbx

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

// 这一组守的是一次真实事故:点了连接之后界面一直停在"连接中"。
// 根因有两层,都在下面钉住了。

func TestParseVersion(t *testing.T) {
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
		full, minor := ParseVersion(c.out)
		if full != c.wantFull || minor != c.wantMinor {
			t.Errorf("ParseVersion(%q) = (%q, %d),想要 (%q, %d)",
				c.out, full, minor, c.wantFull, c.wantMinor)
		}
	}
}

// 版本太老的 adb 不会报错,只会给一个空的设备列表 —— 而空列表看起来就是
// "线没插好",人会去查线、换口、重启手机,全是白费功夫。所以必须提前拦下来说清楚
func TestResolveBinaryRejectsAncientVersion(t *testing.T) {
	const ancient = `C:/WINDOWS/adb.exe`
	if _, err := os.Stat(ancient); err != nil {
		t.Skip("这台机器上没有那个老版本,跳过")
	}
	_, err := ResolveBinary(ancient)
	if err == nil {
		t.Fatal("指向老版本时应该直接拦下来")
	}
	for _, want := range []string{"版本过老", "platform-tools"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("报错里应该有 %q,得到: %v", want, err)
		}
	}
}

// 不填路径时优先用应用自己那份,而不是奔 PATH ——
// Windows 上一堆手机助手会把老 adb 塞进系统目录,还排在 PATH 前面
func TestResolveBinaryPrefersBundled(t *testing.T) {
	b := BundledPath()
	if b == "" {
		t.Skip("这台机器上没有自带的 adb,跳过")
	}
	got, err := ResolveBinary("")
	if err != nil {
		t.Fatalf("不填路径时应该能解出一个可用的 adb: %v", err)
	}
	if got != b {
		t.Errorf("应该优先用自带的 %q,得到 %q", b, got)
	}
}

func TestResolveBinaryMissing(t *testing.T) {
	_, err := ResolveBinary("definitely-not-an-adb-binary")
	if err == nil {
		t.Fatal("adb 不存在时该报错")
	}
	if !strings.Contains(err.Error(), "adb") {
		t.Errorf("报错该点明是 adb 的问题,得到: %v", err)
	}
}

// 路径和关键词都是用户输入的,直接拼进命令行等于把设备的 shell 交给对方
func TestQuoteBlocksInjection(t *testing.T) {
	q := Quote(`'; rm -rf /; echo '`)
	if !strings.HasPrefix(q, "'") || !strings.HasSuffix(q, "'") {
		t.Fatalf("没有被引号包住: %s", q)
	}
	inner := q[1 : len(q)-1]
	if strings.Contains(strings.ReplaceAll(inner, `'\''`, ""), "'") {
		t.Errorf("还有没转义的单引号泄出来: %s", q)
	}
	if got := Quote("普通路径"); got != "'普通路径'" {
		t.Errorf("普通输入被改坏了: %s", got)
	}
}

// RunLocal 的超时必须真的能生效 —— 这条守的是那次"连接中"一直转。
//
// 根因:adb 会 fork 一个常驻的 server 守护进程,它继承了我们这条 stdout
// 管道的写端。Go 的 Wait 要等 io 拷贝 goroutine 结束,管道又要等**所有**
// 持有写端的进程退出才关闭 —— 守护进程永远不退,于是 context 到期把 adb
// 本身杀了也没用,Wait 照样卡死,超时形同虚设。
//
// 这里不去调真的 adb(那要求机器上正好装着那个老版本,而且会踢掉别人正在
// 用的 adb 服务),而是用测试二进制自己复刻同一个形状:子进程拉起一个
// 继承了 stdout 的孙进程,然后自己先退出。
func TestRunLocalTimeoutSurvivesLingeringGrandchild(t *testing.T) {
	start := time.Now()
	// 超时压到 1 秒:整条用例的耗时由它 + WaitDelay + 孙进程存活时间决定,
	// 而这条要跟着每次 go test 跑
	const timeout = 1 * time.Second
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = RunLocal(context.Background(), timeout,
			os.Args[0], "-test.run=TestSpawnLingeringChildHelper")
	}()

	limit := timeout + CmdWaitDelay + 5*time.Second
	select {
	case <-done:
		if d := time.Since(start); d > limit {
			t.Errorf("超时兜住了但花了 %s,超过 %s", d, limit)
		}
	case <-time.After(limit):
		t.Fatal("RunLocal 卡住了 —— 孙进程占着 stdout 管道,Wait 回不来。" +
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
		t.Skip("这是给 TestRunLocalTimeoutSurvivesLingeringGrandchild 用的子进程,不单独跑")
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

// TestSleepForeverHelper 孙进程:睡到盖过超时窗口
func TestSleepForeverHelper(t *testing.T) {
	if !slices.Contains(os.Args, "-test.run=TestSleepForeverHelper") {
		t.Skip("这是给上面那条用的孙进程,不单独跑")
	}
	time.Sleep(lingerFor)
}
