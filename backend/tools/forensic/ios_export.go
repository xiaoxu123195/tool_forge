package forensic

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"tool_forge/backend/tools/iosmux"
)

// iOS 导出走 usbmuxd 直连,不再 fork go-forensic。
//
// 和安卓那条路的关键差别:tar 是**流式**从设备过来的 —— ssh 给的是一个
// stdout 管道,边收边解,设备上一个字节都不写。安卓那边只能先在设备内打包成
// 文件再拉,因为 adb 的同步协议要一个现成的文件。
// 取证时"不往被取证的设备写东西"是硬要求,这边能做到就做到。

const (
	// iosSSHPort 越狱设备上 OpenSSH 的端口
	iosSSHPort = 22
	// metaReaders 同时读几个容器的元信息。
	// 一台机器上百来个容器,一个个读要上百次往返;并发一开就是一两秒的事。
	// 不开更大是因为再大收益已经很小,而 sftp 那头也有并发上限
	metaReaders = 8
)

// iosRoots 关键词模式下扫哪些地方。
//
// 三处装的东西不一样,少一处就少一块:
//   - Application    应用自己的沙盒,聊天记录、数据库都在这
//   - AppGroup       同一开发者的多个 App 共享的那份(微信的很多数据在这儿)
//   - PluginKitPlugin 扩展(分享面板、小组件)的沙盒
var iosRoots = []string{
	"/private/var/mobile/Containers/Data/Application",
	"/private/var/mobile/Containers/Shared/AppGroup",
	"/private/var/mobile/Containers/Data/PluginKitPlugin",
}

type iosExporter struct {
	ssh    *ssh.Client
	sftp   *sftp.Client
	output string
	log    func(format string, a ...any)
}

// runIOSExport 原生执行一次 iOS 导出
func runIOSExport(ctx context.Context, opt exportOptions, log func(string, ...any)) error {
	started := time.Now()
	log("connecting...")
	dev, err := iosmux.PickDevice(opt.deviceID)
	if err != nil {
		return err
	}
	client, _, err := iosmux.DialSSH(dev, iosSSHPort, opt.user, opt.password)
	if err != nil {
		return err
	}
	defer client.Close()

	sf, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("SFTP 子系统起不来(设备上的 sshd 可能没开 sftp): %w", err)
	}
	defer sf.Close()

	e := &iosExporter{ssh: client, sftp: sf, output: opt.output, log: log}
	log("device %s", dev.UDID)

	if err := os.MkdirAll(e.output, 0o755); err != nil {
		return err
	}
	if opt.clear {
		if err := clearDir(e.output, log); err != nil {
			return err
		}
	} else {
		warnIfNotEmpty(e.output, log)
	}

	targets := opt.paths
	if len(targets) == 0 {
		if len(opt.keywords) == 0 {
			return fmt.Errorf("既没给路径也没给关键词,不知道要导什么")
		}
		if targets, err = e.findByKeywords(ctx, opt.keywords); err != nil {
			return err
		}
		if len(targets) == 0 {
			return fmt.Errorf("按关键词 %s 没有找到任何应用容器", strings.Join(opt.keywords, ", "))
		}
		log("matched %d container(s) by keywords", len(targets))
	}

	var failed int
	for _, p := range targets {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := e.exportOne(ctx, p); err != nil {
			// 一条失败不该让整批停下:取证时常有几个目录读不了,剩下的照样有价值
			log("ERROR %s: %v", p, err)
			failed++
		}
	}
	if failed == len(targets) {
		return fmt.Errorf("%d 个目标全部失败", failed)
	}
	log("export done, %d ok / %d failed, 共 %s", len(targets)-failed, failed, round(time.Since(started)))
	return nil
}

// findByKeywords 按关键词找应用容器。
//
// iOS 上不能像安卓那样按目录名找:容器目录名是一串 UUID,和包名毫无关系。
// 真正的对应关系写在每个容器里的一份 plist 里,只能挨个打开来看。
func (e *iosExporter) findByKeywords(ctx context.Context, keywords []string) ([]string, error) {
	var out []string
	for _, root := range iosRoots {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		infos, err := e.sftp.ReadDir(root)
		if err != nil {
			// 某一处不存在很正常(比如设备上没装过带扩展的应用),不该整个失败
			e.log("searching %s (跳过: %v)", root, err)
			continue
		}
		e.log("searching %s (%d 个容器)", root, len(infos))

		var dirs []string
		for _, fi := range infos {
			if fi.IsDir() {
				dirs = append(dirs, path.Join(root, fi.Name()))
			}
		}
		hits, unread := e.matchContainers(ctx, dirs, keywords)
		out = append(out, hits...)
		// 读不出身份的容器要说出来。不说的话,"一个都没匹配上"和
		// "根本没读成几个"在界面上长得一模一样 —— 而后者是漏证据,
		// 前者只是这台机器上没装那个应用
		if unread > 0 {
			e.log("WARN %s 下有 %d/%d 个容器读不出所属应用,这些没有参与关键词匹配",
				root, unread, len(dirs))
		}
	}
	return out, nil
}

// matchContainers 并发读一批容器的元信息,返回命中关键词的那些,
// 以及有多少个压根没读出身份
func (e *iosExporter) matchContainers(ctx context.Context, dirs []string, keywords []string) ([]string, int) {
	type hit struct {
		idx    int
		dir    string
		bundle string
	}
	jobs := make(chan int)
	results := make(chan hit, len(dirs))
	var unread atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < metaReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				dir := dirs[idx]
				bundle, err := e.bundleID(dir)
				if err != nil || bundle == "" {
					unread.Add(1)
					continue
				}
				if !matchesAny(bundle, keywords) {
					continue
				}
				results <- hit{idx: idx, dir: dir, bundle: bundle}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := range dirs {
			if ctx.Err() != nil {
				return
			}
			jobs <- i
		}
	}()
	wg.Wait()
	close(results)

	// 并发出来的顺序是乱的,按原顺序排回去 —— 同样的输入应该给同样的结果,
	// 取证的东西每跑一次换个顺序会让人怀疑是不是漏了
	byIdx := map[int]hit{}
	var idxs []int
	for h := range results {
		byIdx[h.idx] = h
		idxs = append(idxs, h.idx)
	}
	sortInts(idxs)

	out := make([]string, 0, len(idxs))
	for _, i := range idxs {
		h := byIdx[i]
		e.log("hit %s → %s", h.bundle, path.Base(h.dir))
		out = append(out, h.dir)
	}
	return out, int(unread.Load())
}

// bundleID 读出一个容器属于哪个 App
func (e *iosExporter) bundleID(dir string) (string, error) {
	f, err := e.sftp.Open(path.Join(dir, iosmux.ContainerMeta))
	if err != nil {
		return "", err
	}
	defer f.Close()
	// 这份 plist 很小,几百字节;设个上限是防着遇到一个不该在这儿的大文件
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return "", err
	}
	return iosmux.BundleIDFromMeta(data)
}

// exportOne 导出一个容器:设备上打包 → 直接流回来 → 边收边解。
//
// 包里存的是去掉开头斜杠的完整设备路径,解到输出目录后复原成设备上的目录树 ——
// 和安卓那边一致,而且这里更要紧:容器目录名都是 UUID,没有上层路径的话
// 几个容器解出来就是一堆认不出来的 UUID 目录
func (e *iosExporter) exportOne(ctx context.Context, remote string) error {
	clean := path.Clean("/" + strings.TrimSpace(remote))
	rel := strings.TrimPrefix(clean, "/")
	if rel == "" {
		return fmt.Errorf("不能导出根目录")
	}

	sess, err := e.ssh.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	// --ignore-failed-read:总有几个文件读不了(正在被占用、权限特殊),
	// 没有这个开关的话 tar 会因为其中一个直接失败,整个容器一个字节都拿不到
	cmd := fmt.Sprintf("tar --ignore-failed-read -cf - -C / %s", shellQuote(rel))
	e.log("packing %s", clean)
	if err := sess.Start(cmd); err != nil {
		return err
	}

	// 取消时把会话关掉,让下面的解包读到 EOF 退出来
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = sess.Close()
		case <-done:
		}
	}()

	e.log("streaming → %s", rel)
	t0 := time.Now()
	counted := &countingReader{r: stdout}
	res, err := untarFrom(counted, e.output)
	if err != nil {
		return fmt.Errorf("解包失败: %w", err)
	}
	// tar 那头的退出码要收:忽略的话,一个中途失败的打包会被当成正常结束,
	// 拿到半个容器还以为是全的
	if werr := sess.Wait(); werr != nil && ctx.Err() == nil {
		e.log("WARN 设备上的 tar 没有正常结束(%v)—— 这一份可能不完整", werr)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	e.log("extracted %d file(s), %s (%s)", res.files, humanSize(counted.n), round(time.Since(t0)))
	reportUntar(res, e.log)
	return nil
}

// countingReader 数一下到底流过来多少字节
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func matchesAny(s string, keywords []string) bool {
	low := strings.ToLower(s)
	for _, k := range keywords {
		if strings.Contains(low, strings.ToLower(strings.TrimSpace(k))) {
			return true
		}
	}
	return false
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// shellQuote 包成单引号,把里面的单引号拆开转义。
// 容器路径是设备给的,不是我们拼的,但照样不能直接塞进命令行
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
