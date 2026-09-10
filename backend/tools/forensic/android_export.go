package forensic

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/electricbubble/gadb"

	"tool_forge/backend/tools/adbx"
)

// 安卓导出走 adb 协议本身,不再 fork adb 命令行。
//
// 打包这一步仍然在设备上做:要取的是一整个应用目录,几千个小文件一个个拉,
// 每个都是一次协议往返,USB 上慢得没法用。先在设备内 tar 成一个,
// 再一次性拉回来,快一到两个数量级。
//
// 代价是会在设备的 /sdcard 上留一个临时 tar,拉完就删 —— 这一点得说在明处,
// 取证场景里"往被取证的设备写东西"不是可以随手做的事。

const (
	// stageDir 设备上放临时 tar 的位置。
	// /sdcard 是 FUSE 挂载,不认 Unix 属主,shell 用户读得到 root 写的文件 ——
	// 换成 /data/local/tmp 的话 root 打出来的包 shell 反而读不了
	stageDir = "/sdcard"
	// cmdTimeout 普通命令的上限
	cmdTimeout = 60 * time.Second
	// packTimeout 打包一个应用目录可能要好几分钟
	packTimeout = 10 * time.Minute
	// maxDirSamples 输出目录已有内容时,提示里举几个例子
	maxDirSamples = 3
)

// androidRoots 关键词模式下扫哪些地方,以及每处往下翻几层。
//
// depth 是相对这个根的层数:应用目录在 /data/user/0/<包名>、
// /sdcard/Android/data/<包名> 上,要两层才够得着;/sdcard/Download 这类
// 一层就到。再往下就是在翻应用自己的缓存,又慢又不会因此多找到一个应用。
//
// 顺序有意义:同一份数据被多个入口命中时留先扫到的那个,所以
// /data/user 排在 /data/data 前面 —— 现代安卓上两者是同一处,
// 而 /data/user/0 才是规范路径(多用户/分身在 /data/user/10 这样的位置)。
//
// 没 root 时 /data 下面全进不去,会被自动跳过
var androidRoots = []struct {
	path     string
	depth    int
	needRoot bool
}{
	{"/data/user", 2, true},
	// 老路径,和 /data/user/0 指向同一处。留着是为了兜住 /data/user
	// 不存在的老设备;两边都在的话命中项会被 inode 去重掉
	{"/data/data", 1, true},
	{"/sdcard/Android", 2, false},
	// 用户自己看得见的那几个目录:应用名、包名常常出现在这里的文件夹上,
	// 而这些数据往往正是要找的(下载的文件、导出的图片)
	{"/sdcard/Download", 1, false},
	{"/sdcard/Pictures", 1, false},
	{"/sdcard/Movies", 1, false},
	{"/sdcard/Music", 1, false},
	{"/sdcard", 1, false},
}

// androidExporter 一次安卓导出
type androidExporter struct {
	dev    gadb.Device
	root   bool
	output string
	log    func(format string, a ...any)
}

// runAndroidExport 原生执行一次安卓导出。
// log 每被调用一次,前端就多一行输出 —— 契约和以前从子进程 stdout 捞行时一样
func runAndroidExport(ctx context.Context, opt exportOptions, log func(string, ...any)) error {
	started := time.Now()
	log("connecting...")
	client, err := adbx.Dial(opt.adbPath)
	if err != nil {
		return err
	}
	dev, model, err := adbx.PickDevice(client, opt.deviceID)
	if err != nil {
		return err
	}
	e := &androidExporter{dev: dev, output: opt.output, log: log}
	e.root = adbx.HasRoot(dev, cmdTimeout)
	log("device %s (%s), root=%v", dev.Serial(), model, e.root)

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
			return fmt.Errorf("按关键词 %s 没有找到任何目录", strings.Join(opt.keywords, ", "))
		}
		log("matched %d path(s) by keywords", len(targets))
	}
	targets = e.dedupeTargets(targets)

	var failed int
	for _, p := range targets {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := e.exportOne(ctx, p); err != nil {
			// 一条失败不该让整批停下:取证时常有几个目录读不了,
			// 剩下的照样有价值
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

// round 把耗时截到 0.1 秒,日志里不需要纳秒
func round(d time.Duration) time.Duration {
	return d.Round(100 * time.Millisecond)
}

// humanSize 拉回来多大,取证报告里要写的数字之一
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

// findByKeywords 按关键词找应用目录。
//
// 一条 find 解决,而不是逐层 ls 再递归 —— 后者每层都是一次协议往返,
// 在几百个包名的目录上会慢到无法接受
func (e *androidExporter) findByKeywords(ctx context.Context, keywords []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, r := range androidRoots {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if r.needRoot && !e.root {
			continue
		}
		// 每个关键词一个 -iname,用 -o 连起来,一次 find 全找完
		var conds []string
		for _, k := range keywords {
			conds = append(conds, "-iname "+adbx.Quote("*"+k+"*"))
		}
		script := fmt.Sprintf("find %s -maxdepth %d \\( %s \\) 2>/dev/null",
			adbx.Quote(r.path), r.depth, strings.Join(conds, " -o "))
		e.log("searching %s", r.path)
		res, err := adbx.Text(e.dev, script, e.root, cmdTimeout)
		if err != nil {
			e.log("ERROR search %s: %v", r.path, err)
			continue
		}
		for _, line := range strings.Split(res, "\n") {
			p := strings.TrimSpace(line)
			// find 会把被搜的目录自己也算一条,得排掉
			if p == "" || p == r.path || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out, nil
}

// dedupeTargets 去掉会把同一份数据导两遍的目标。
//
// 两种重复,都真实发生过:
//
//  1. /data/data/<包名> 和 /data/user/0/<包名> 是指向同一处的 bind mount。
//     readlink -f 分辨不出来 —— 谁都不是软链,它老老实实各返回各的路径;
//     设备号 + inode 才认得出(实测两边都是 107)。
//  2. 一个目标落在另一个目标里面时,父目录那一包已经含了它。
//
// 认不出来就全留着:多导一遍浪费时间,漏导一个是丢证据,两者不对等
func (e *androidExporter) dedupeTargets(paths []string) []string {
	if len(paths) < 2 {
		return paths
	}
	ids := e.fileIDs(paths)

	var out []string
	firstAt := map[string]string{}
	for i, p := range paths {
		// 被已选中的目标包住的,跳过
		if outer, ok := containedIn(p, out); ok {
			e.log("skipping %s (已经包含在 %s 里)", p, outer)
			continue
		}
		id := ids[i]
		if id != "" {
			if prev, dup := firstAt[id]; dup {
				e.log("skipping %s (和 %s 是同一处)", p, prev)
				continue
			}
			firstAt[id] = p
		}
		out = append(out, p)
	}
	return out
}

// fileIDs 一次问出一批路径的"设备号:inode",取不到的那条给空串。
//
// 一条命令问完而不是一条路径一次:每次往返都要几十毫秒,
// 命中几十个目标时就是好几秒
func (e *androidExporter) fileIDs(paths []string) []string {
	var b strings.Builder
	b.WriteString("for p in")
	for _, p := range paths {
		b.WriteString(" " + adbx.Quote(p))
	}
	// stat 失败时补一个空行,保证"一个路径一行",否则行和路径就对不上号了
	b.WriteString(`; do stat -c '%d:%i' "$p" 2>/dev/null || echo; done`)

	out, err := adbx.Text(e.dev, b.String(), e.root, cmdTimeout)
	if err != nil {
		return make([]string, len(paths))
	}
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	// 行数对不上说明这套输出没法信,宁可一个都不去重
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) != len(paths) {
		return make([]string, len(paths))
	}
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return lines
}

// containedIn p 是不是落在 list 里某一条的下面。
//
// 比的是路径分段,不是字符串前缀 —— /sdcard/Download 和 /sdcard/Downloads
// 前缀是包含关系,位置上却毫无关系,按前缀比会把后者整个漏掉
func containedIn(p string, list []string) (string, bool) {
	for _, outer := range list {
		if p == outer {
			return outer, true
		}
		if strings.HasPrefix(p, strings.TrimSuffix(outer, "/")+"/") {
			return outer, true
		}
	}
	return "", false
}

// clearOutput 导出前清空输出目录。
//
// 默认不做这件事,只有明确要求了才做:这是用户自己填的路径,填错一个字
// 就是删掉不相干的东西,而且删完没法撤。所以这里比"照做"多两层:
// 明显会闯祸的目标直接拒绝,真删了也要把删掉多少如实说出来
func clearDir(dir string, log func(string, ...any)) error {
	if err := safeToClear(dir); err != nil {
		return err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(ents) == 0 {
		return nil
	}
	for _, ent := range ents {
		if err := os.RemoveAll(filepath.Join(dir, ent.Name())); err != nil {
			return fmt.Errorf("清空输出目录失败: %w", err)
		}
	}
	log("cleared %d 个已有条目", len(ents))
	return nil
}

// safeToClear 拦住那些一看就会闯祸的清空目标。
//
// 盘符根、文件系统根是底线 —— 路径少打几个字就会变成它们,
// 而"清空 D:\"和"清空 D:\exhibits\案件一"在代码里长得一模一样
func safeToClear(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	abs = filepath.Clean(abs)
	root := filepath.VolumeName(abs) + string(os.PathSeparator)
	if abs == root || abs == string(os.PathSeparator) {
		return fmt.Errorf("拒绝清空 %s:这是根目录,不像是要放取证结果的地方", abs)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s 不是目录", abs)
	}
	return nil
}

// warnIfNotEmpty 输出目录里已经有东西时提醒一声。
//
// 没要求清空就不替他清:里面可能是上一次取的证。
// 但混在一起也不能不吭声 —— 旧文件看上去和这次取的一模一样,分不出来
func warnIfNotEmpty(dir string, log func(string, ...any)) {
	ents, err := os.ReadDir(dir)
	if err != nil || len(ents) == 0 {
		return
	}
	var names []string
	for _, ent := range ents {
		if len(names) >= maxDirSamples {
			break
		}
		names = append(names, ent.Name())
	}
	log("WARN 输出目录里已经有 %d 个条目(%s…),这次导出会和它们混在一起",
		len(ents), strings.Join(names, ", "))
}

// reportUntar 把一次解包里"动过手脚"的部分如实说出来。
// 两个平台共用 —— 改名和跳过在哪边都一样要交代
func reportUntar(res untarResult, log func(string, ...any)) {
	// 改过名的必须说出来。取证里文件名本身就是证据的一部分,
	// 悄悄换掉几百个名字而不吭声,是在给后面的人埋雷
	if res.renamed > 0 {
		log("WARN %d 个名字在本地文件系统上非法,已替换其中的字符;例如 %s",
			res.renamed, strings.Join(res.samples, " / "))
	}
	if res.skipped > 0 {
		log("WARN 跳过 %d 个成员(软链、设备节点之类)", res.skipped)
	}
}

// exportOne 导出一个目录或文件:设备内打包 → 拉回来 → 解包 → 删掉设备上那份。
//
// 包里存的是去掉开头斜杠的完整设备路径(data/user/0/<包名>/...),解到输出目录后
// 正好复原成设备上的目录树。不能只存最后一层:同一个应用在 /data/user/0、
// /sdcard/Android/data、/sdcard/Android/media 下各有一个同名目录,只存最后一层
// 的话三份会解到同一个地方相互覆盖 —— 既丢数据,也再也说不清哪个文件原来在哪。
// 取证结果要能对着设备核对,路径本身就是证据的一部分
func (e *androidExporter) exportOne(ctx context.Context, remote string) error {
	// 归一化成 /a/b/c 的形状,再转成 tar 里用的相对路径 a/b/c
	clean := path.Clean("/" + strings.TrimSpace(remote))
	rel := strings.TrimPrefix(clean, "/")
	if rel == "" {
		return fmt.Errorf("不能导出根目录")
	}
	stage := fmt.Sprintf("%s/.toolforge-%s-%d.tar", stageDir,
		sanitize(path.Base(clean)), time.Now().UnixNano())
	// 本地临时包按完整路径命名:三个目标的最后一层同名,只用最后一层的话
	// 后一个会覆盖前一个正在用的包
	localTar := filepath.Join(e.output, ".toolforge-"+sanitize(rel)+".tar")

	e.log("packing %s", clean)
	// -C / 之后给相对路径,包里就是完整的设备路径。
	// 不靠 tar 自己剥掉开头的斜杠 —— 那是各家实现自便的行为,写明确的更稳
	script := fmt.Sprintf("tar -cf %s -C / %s && chmod 666 %s",
		adbx.Quote(stage), adbx.Quote(rel), adbx.Quote(stage))
	packStart := time.Now()
	if _, err := adbx.Text(e.dev, script, e.root, packTimeout); err != nil {
		return fmt.Errorf("在设备上打包失败%s: %w", e.rootHint(), err)
	}
	packTook := time.Since(packStart)
	// 无论后面成不成,设备上那份临时包都要删掉
	defer func() {
		if _, err := adbx.Text(e.dev, "rm -f "+adbx.Quote(stage), e.root, cmdTimeout); err != nil {
			e.log("WARN 设备上的临时包没删掉 %s: %v", stage, err)
		}
	}()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	e.log("pulling %s", stage)
	f, err := os.Create(localTar)
	if err != nil {
		return err
	}
	pullStart := time.Now()
	if err := e.dev.Pull(stage, f); err != nil {
		_ = f.Close()
		return fmt.Errorf("拉取失败: %w", err)
	}
	size, _ := f.Seek(0, io.SeekCurrent)
	_ = f.Close()
	pullTook := time.Since(pullStart)

	e.log("extracting → %s", rel)
	untarStart := time.Now()
	res, err := untar(localTar, e.output)
	if err != nil {
		return fmt.Errorf("解包失败: %w", err)
	}
	// 三段耗时写在一起。取证经常是几分钟起步的活,不给分段的话
	// 只能盯着一行不动的日志猜是卡住了还是本来就慢
	e.log("extracted %d file(s), %s (打包 %s / 拉取 %s / 解包 %s)",
		res.files, humanSize(size),
		round(packTook), round(pullTook), round(time.Since(untarStart)))
	reportUntar(res, e.log)
	// 解完就不留 tar 了,不然输出目录里每个应用都多一份重复的压缩包
	if err := os.Remove(localTar); err != nil {
		e.log("WARN 本地临时包没删掉 %s: %v", localTar, err)
	}
	return nil
}

func (e *androidExporter) rootHint() string {
	if e.root {
		return ""
	}
	return "(当前没有 root,/data 下面读不了)"
}

// sanitize 把设备上的名字压成能当本地文件名的东西
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// untarResult 一次解包的统计
type untarResult struct {
	files   int
	renamed int
	skipped int
	// samples 前几条改名记录,写进日志给人看
	samples []string
}

// maxRenameSamples 日志里最多举几个改名的例子。
// 微信那种目录一改就是几百个,全打出来只会把日志淹掉
const maxRenameSamples = 3

// untar 解开一个 tar 到 dest。
//
// 两件事必须做,都是被真实数据逼出来的:
//
//  1. 逐条校验路径落在 dest 里面。tar 包里可以写 ../../ 这样的成员名,
//     不拦的话解包会把文件写到目标目录之外。这个包是从被取证的设备上拿来的,
//     内容不可信,这道检查不能省。
//
//  2. 安卓上合法的名字在 Windows 上未必合法。微信就有个目录叫
//     com.tencent.mm:appbrand0 —— 冒号在 Windows 上是非法字符,
//     mkdir 直接失败,而原来的写法会让整包解包中断,前面拉下来的全丢。
//     现在改成替换非法字符并计数上报:数据留住,改动如实说出来。
func untar(tarPath, dest string) (untarResult, error) {
	f, err := os.Open(tarPath)
	if err != nil {
		return untarResult{}, err
	}
	defer f.Close()
	return untarFrom(f, dest)
}

// untarFrom 从一个流里解包。
//
// iOS 那条路用它:tar 直接从设备的 ssh 通道流过来,边收边解,
// 设备上不落任何文件 —— 取证时"不往被取证的设备写东西"是硬要求,
// 能做到就该做到
func untarFrom(r io.Reader, dest string) (untarResult, error) {
	var res untarResult
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return res, err
	}
	if err := os.MkdirAll(absDest, 0o755); err != nil {
		return res, err
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return res, nil
		}
		if err != nil {
			return res, err
		}

		cleaned, renamed := localSafePath(hdr.Name)
		target := filepath.Join(absDest, filepath.FromSlash(cleaned))
		absTarget, err := filepath.Abs(target)
		if err != nil {
			return res, err
		}
		if !strings.HasPrefix(absTarget, absDest+string(os.PathSeparator)) && absTarget != absDest {
			return res, fmt.Errorf("包里有指向目标目录之外的成员: %s", hdr.Name)
		}
		if renamed {
			res.renamed++
			if len(res.samples) < maxRenameSamples {
				res.samples = append(res.samples, hdr.Name+" → "+cleaned)
			}
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(absTarget, 0o755); err != nil {
				return res, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(absTarget), 0o755); err != nil {
				return res, err
			}
			out, err := os.Create(absTarget)
			if err != nil {
				return res, err
			}
			// tar 是流式读的,再大的成员也不会整个进内存
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return res, err
			}
			if err := out.Close(); err != nil {
				return res, err
			}
			res.files++
		default:
			// 软链、设备节点之类跳过:落到本地文件系统上没有意义,
			// 而软链还可能指到目标目录之外
			res.skipped++
		}
	}
}

// windowsIllegal Windows 文件名里不能出现的字符。
// 安卓那边这些全是合法的 —— 冒号尤其常见,应用的多进程目录就叫 <包名>:<进程名>
const windowsIllegal = `<>:"|?*`

// windowsReserved Windows 上不能当文件名的保留字(不分大小写,带扩展名也不行)
var windowsReserved = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// localSafePath 把 tar 里的成员名改成本地文件系统能接受的。
//
// 只在 Windows 上真的改。Linux / macOS 上冒号之类都是合法的,
// 在那儿改名反而是把原始证据改坏了。
func localSafePath(name string) (string, bool) {
	if runtime.GOOS != "windows" {
		return name, false
	}
	parts := strings.Split(name, "/")
	changed := false
	for i, seg := range parts {
		fixed := safeSegment(seg)
		if fixed != seg {
			changed = true
			parts[i] = fixed
		}
	}
	return strings.Join(parts, "/"), changed
}

func safeSegment(seg string) string {
	if seg == "" || seg == "." || seg == ".." {
		return seg
	}
	var b strings.Builder
	for _, r := range seg {
		if r < 0x20 || strings.ContainsRune(windowsIllegal, r) {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	// 结尾的点和空格 Windows 会自己吞掉,留着会造成两个不同的名字撞在一起
	out = strings.TrimRight(out, ". ")
	if out == "" {
		return "_"
	}
	// 保留字要加个后缀躲开;带扩展名的也算(con.txt 同样开不了)
	base := strings.ToLower(out)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	if windowsReserved[base] {
		out += "_"
	}
	return out
}
