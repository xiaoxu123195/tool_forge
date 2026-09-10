package forensic

import (
	"context"
	"strings"
	"testing"
	"time"
)

// waitDone 等一个任务的结束事件
func waitDone(t *testing.T, ch <-chan EventEnvelope) DoneEvent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case env, ok := <-ch:
			if !ok {
				t.Fatal("事件流被关掉了,但没收到 done")
			}
			if env.Type == "done" && env.Done != nil {
				return *env.Done
			}
		case <-deadline:
			t.Fatal("等 done 超时")
		}
	}
}

// 跑完了就该报成功。
//
// 这条守的是一次真实的误报:任务结束时先 cancel 了自己的 context 再去问
// "是不是被取消的",于是每一次都答"是" —— 界面显示「已取消」,
// 「打开输出目录」的按钮也跟着不出来,而数据其实已经完整导出了。
// 用户以为白跑一趟,这比直接失败更糟
func TestJobReportsSuccessNotCanceled(t *testing.T) {
	s := New()
	s.SetRunContext(context.Background())

	// 挡住任务,等订阅上了再放行 —— 不然它可能在我们订阅之前就跑完了
	release := make(chan struct{})
	id, err := s.runJob(func(ctx context.Context, log func(string, ...any)) error {
		<-release
		log("干完了")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ch, unsub := s.Subscribe(id)
	defer unsub()
	close(release)

	done := waitDone(t, ch)
	if done.Canceled {
		t.Error("任务是正常跑完的,不该报成已取消")
	}
	if done.ExitCode != 0 {
		t.Errorf("退出码该是 0,得到 %d(error=%q)", done.ExitCode, done.Error)
	}
}

// 反过来也得对:真被取消时必须报取消,不能为了修上面那条就写死成 false
func TestJobReportsCancel(t *testing.T) {
	s := New()
	s.SetRunContext(context.Background())

	started := make(chan struct{})
	id, err := s.runJob(func(ctx context.Context, log func(string, ...any)) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	ch, unsub := s.Subscribe(id)
	defer unsub()

	<-started
	if err := s.Cancel(id); err != nil {
		t.Fatal(err)
	}

	done := waitDone(t, ch)
	if !done.Canceled {
		t.Error("这次是真取消,该报已取消")
	}
}

// 任务里出错时报的必须是那个错,而不是"已取消"
func TestJobReportsFailure(t *testing.T) {
	s := New()
	s.SetRunContext(context.Background())

	release := make(chan struct{})
	id, err := s.runJob(func(ctx context.Context, log func(string, ...any)) error {
		<-release
		return context.DeadlineExceeded
	})
	if err != nil {
		t.Fatal(err)
	}
	ch, unsub := s.Subscribe(id)
	defer unsub()
	close(release)

	done := waitDone(t, ch)
	if done.Canceled {
		t.Error("是执行出错,不是被取消")
	}
	if done.ExitCode == 0 || done.Error == "" {
		t.Errorf("该带上错误信息: exit=%d error=%q", done.ExitCode, done.Error)
	}
}

// --engine 是我们自己加的伪 flag,go-forensic 不认识它。
// 回落到命令行之前必须摘干净,否则它会因为一个不认识的参数直接罢工
func TestSplitEngine(t *testing.T) {
	cases := []struct {
		in   []string
		rest []string
		want engine
	}{
		{
			in:   []string{"android", "export", "-o", "D:/out"},
			rest: []string{"android", "export", "-o", "D:/out"},
			want: engineAuto,
		},
		{
			in:   []string{"android", "export", "--engine=cli", "-o", "D:/out"},
			rest: []string{"android", "export", "-o", "D:/out"},
			want: engineCLI,
		},
		{
			in:   []string{"--engine=builtin", "android", "export", "-o", "D:/out"},
			rest: []string{"android", "export", "-o", "D:/out"},
			want: engineBuiltin,
		},
		{
			// 值不认识就当没写过,别把整条命令卡死
			in:   []string{"android", "export", "--engine=什么", "-o", "D:/out"},
			rest: []string{"android", "export", "-o", "D:/out"},
			want: engineAuto,
		},
	}
	for _, c := range cases {
		rest, got := splitEngine(c.in)
		if got != c.want {
			t.Errorf("%v: 引擎解成 %q,想要 %q", c.in, got, c.want)
		}
		if strings.Join(rest, " ") != strings.Join(c.rest, " ") {
			t.Errorf("%v: 剩下的参数是 %v,想要 %v", c.in, rest, c.rest)
		}
	}
}

// 明确点了内置、这条命令又没有内置实现时,要直说,
// 不能悄悄换成 go-forensic —— 那是另一个程序,用户未必装了,装了也未必想用
func TestBuiltinEngineRefusesWhatItCannotRun(t *testing.T) {
	s := New()
	s.SetRunContext(context.Background())

	_, err := s.Run([]string{"ios", "export", "--engine=builtin", "-s", "/var/mobile", "-o", "D:/out"})
	if err == nil {
		t.Fatal("iOS 还没有内置实现,应该明确报错而不是默默跑别的")
	}
	if !strings.Contains(err.Error(), "内置") {
		t.Errorf("错误信息该说清楚是内置实现的问题: %v", err)
	}
}
