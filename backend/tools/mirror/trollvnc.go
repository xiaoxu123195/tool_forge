package mirror

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"howett.net/plist"

	"tool_forge/backend/tools/adbx"
)

// 越狱 iPhone 上的 TrollVNC:查装没装、写设置、启停服务,全经 SSH。
//
// 工具箱接管它的设置,每次投屏前重写一遍,投屏结束就把服务停掉:
//   - 只听手机本机(127.0.0.1)。电脑经 usbmuxd 进来,在手机看来就是本机连接;Wi-Fi 上的连不进来
//   - 不在局域网广播,不开网页客户端 —— 界面自己带着 VNC 客户端
//   - 每次投屏换一个随机密码,只交给这一次的界面
//   - 不投屏时服务停着。TrollVNC 3.2 绑了 IPv4 本机地址后,IPv6 照样听所有网卡,
//     服务停着是唯一靠得住的办法
//
// 有电脑连着时 TrollVNC 会在手机上发通知,那是它让手机主人知道的方式,保持它的默认

const (
	trollPackage   = "com.82flex.trollvnc"
	trollPrefsFile = "com.82flex.trollvnc.plist"
	// trollPort 手机上 VNC 服务的端口(TrollVNC 默认 5901)
	trollPort = 5901
	// trollShellTimeout 一条普通命令最多等多久
	trollShellTimeout = 30 * time.Second
	// trollInstallTimeout 推包加 dpkg 安装,老手机上要十几秒
	trollInstallTimeout = 3 * time.Minute
	// trollReadyTimeout 服务重启后多久该开始听端口。一般一两秒
	trollReadyTimeout = 12 * time.Second
	// maxPackage 安装包最大多少字节。TrollVNC 的包也就几 MB
	maxPackage = 64 << 20
)

// errTrollMissing 手机上没装 TrollVNC
var errTrollMissing = errors.New("这台 iPhone 上还没装 TrollVNC")

// errVNCNoAuth 服务起来了却不要密码:写进去的设置没生效。这种情况宁可当失败,也不能让它开着
var errVNCNoAuth = errors.New("手机上的 VNC 服务没要密码,说明工具箱写的设置没生效,已经把服务停了")

// shell 在手机上跑一段 sh 脚本,返回标准输出;stdin 可以是 nil。真机上是 SSH,测试里换成假的
type shell interface {
	run(script string, stdin io.Reader, timeout time.Duration) (string, error)
}

// sshShell 越狱设备上的 OpenSSH
type sshShell struct{ c *ssh.Client }

func (s sshShell) run(script string, stdin io.Reader, timeout time.Duration) (string, error) {
	sess, err := s.c.NewSession()
	if err != nil {
		return "", fmt.Errorf("SSH 连接断了: %w", err)
	}
	defer sess.Close()
	var out, errOut bytes.Buffer
	sess.Stdout, sess.Stderr = &out, &errOut
	if stdin != nil {
		sess.Stdin = stdin
	}
	done := make(chan error, 1)
	// 交给 sh 跑:越狱设备上 root 的登录 shell 常是 zsh,有些写法在它那儿意思不一样
	go func() { done <- sess.Run("sh -c " + adbx.Quote(script)) }()
	select {
	case err = <-done:
	case <-time.After(timeout):
		return out.String(), fmt.Errorf("手机上的命令超过 %s 没结束", timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		if msg != "" {
			return out.String(), fmt.Errorf("%s(%w)", lastLines(msg, 6), err)
		}
		return out.String(), err
	}
	return out.String(), nil
}

// ---- 手机上的情况 ----

// TrollStatus 手机上 TrollVNC 的情况,界面据此决定直接投屏还是先教人装
type TrollStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	// Arch 手机的包架构:rootless 越狱(Dopamine、palera1n)是 iphoneos-arm64
	Arch string `json:"arch"`
	// Artifact GitHub Actions 编出来的产物里该下哪个
	Artifact string `json:"artifact"`
	// Loader TrollVNC 的包依赖 PreferenceLoader,没装的话 dpkg 不让装
	Loader bool `json:"loader"`
	// Model 型号,认得出就是 iPhone 8 Plus 这种,认不出就是 iPhone10,2 这种代码
	Model string `json:"model"`
	// Unsupported 非空时是这台用不了的原因
	Unsupported string `json:"unsupported,omitempty"`
}

// trollLayout 手机上的路径和装了什么。rootless 越狱的东西都在 /var/jb 下面
type trollLayout struct {
	prefix  string // "" 或 "/var/jb"
	arch    string
	model   string // uname -m 给的型号代码
	daemon  string // TrollVNC 的 LaunchDaemon,装了才有
	version string
	loader  bool
}

func (l trollLayout) installed() bool { return l.daemon != "" }

// unsupported 这台用不了的原因;用得了时为空
func (l trollLayout) unsupported() string {
	switch l.arch {
	case "iphoneos-arm64", "iphoneos-arm":
		return ""
	case "iphoneos-arm64e":
		return "这台是 roothide 越狱,工具箱暂时只支持 rootless(Dopamine、palera1n)和老的 rootful 越狱"
	case "":
		return "手机上没有 dpkg,不像是越狱过的设备"
	}
	return "不认识这台手机的包架构 " + l.arch
}

// artifact GitHub Actions 的产物里,这台手机该下哪个
func (l trollLayout) artifact() string {
	if l.arch == "iphoneos-arm" {
		return "packages-default"
	}
	return "packages-rootless"
}

// name 界面和截图文件名里用的机型
func (l trollLayout) name() string {
	if n := iPhoneNames[l.model]; n != "" {
		return n
	}
	if l.model != "" {
		return l.model
	}
	return "iPhone"
}

func (l trollLayout) status() *TrollStatus {
	return &TrollStatus{
		Installed:   l.installed(),
		Version:     l.version,
		Arch:        l.arch,
		Artifact:    l.artifact(),
		Loader:      l.loader,
		Model:       l.name(),
		Unsupported: l.unsupported(),
	}
}

// prefsPaths 设置文件写到哪儿。
// Dopamine 把插件的设置重定向到 /var/jb/var/mobile/Library/Preferences,写在 /var/mobile 下面服务端读不到;
// 别家 rootless 不一定重定向,所以两处都写,内容一样
func (l trollLayout) prefsPaths() []string {
	std := "/var/mobile/Library/Preferences/" + trollPrefsFile
	if l.prefix == "" {
		return []string{std}
	}
	return []string{l.prefix + std, std}
}

// probeScript 一次问全:越狱布局、包架构、型号、TrollVNC 和它依赖的 PreferenceLoader 装没装
const probeScript = `P=""
[ -d /var/jb ] && P=/var/jb
echo "prefix=$P"
echo "arch=$(dpkg --print-architecture 2>/dev/null)"
echo "model=$(uname -m 2>/dev/null)"
D="$P/Library/LaunchDaemons/com.82flex.trollvnc.plist"
[ -f "$D" ] && echo "daemon=$D"
echo "version=$(dpkg-query -W -f='${Version}' com.82flex.trollvnc 2>/dev/null)"
echo "loader=$(dpkg-query -W -f='${Status}' preferenceloader 2>/dev/null)"
`

func probeTroll(sh shell) (trollLayout, error) {
	out, err := sh.run(probeScript, nil, trollShellTimeout)
	if err != nil {
		return trollLayout{}, fmt.Errorf("查手机上的 TrollVNC 失败: %w", err)
	}
	return parseLayout(out), nil
}

func parseLayout(out string) trollLayout {
	var l trollLayout
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "prefix":
			l.prefix = v
		case "arch":
			l.arch = v
		case "model":
			l.model = v
		case "daemon":
			l.daemon = v
		case "version":
			l.version = v
		case "loader":
			l.loader = v == "install ok installed"
		}
	}
	return l
}

// ---- 设置和启停 ----

// trollPrefs 写进手机的设置。enabled 为假时服务起来也只是空转,不开端口
func trollPrefs(enabled bool, password, name string, keepAwake bool) ([]byte, error) {
	p := map[string]any{
		"Enabled":          enabled,
		"DesktopName":      name,
		"BindHost":         "127.0.0.1",
		"Port":             trollPort,
		"HttpPort":         0,
		"BonjourEnabled":   false,
		"ClipboardEnabled": true,
		// 界面发 Command 用的是 Super 键,两种映射里它都是 Command;Alt 照常当 Option
		"ModifierMap": "std",
		// 滚轮:默认每格只拖 48 像素,3 倍屏上 iOS 还没认出是在滑就结束了,只会抖一下。
		// 每格 160、0.2 秒内连着滚的合成一次拖动,方向和电脑上滚网页一致 —— 在 3 倍屏的 iPhone 上调出来的
		"WheelStepPx":   160,
		"WheelTuning":   "coalesce=0.2,max=800,clamp=3,minratio=0.6,durbase=0.1,durk=0.004,durmin=0.1,durmax=0.3",
		"NaturalScroll": true,
		// 有电脑连着时每 30 秒按一下「唤醒」,手机就不会到点自动锁屏
		"KeepAliveSec": 0,
	}
	if enabled {
		p["FullPassword"] = password
	}
	if keepAwake {
		p["KeepAliveSec"] = 30
	}
	return plist.Marshal(p, plist.XMLFormat)
}

// applyScript 设置文件从标准输入来:写到位、让 cfprefsd 重读,然后停掉服务;start 为真再按新设置起来
func applyScript(l trollLayout, start bool) string {
	var b strings.Builder
	b.WriteString("set -e\numask 077\nt=/tmp/tf-trollvnc.$$\ncat > \"$t\"\nfor f in")
	for _, p := range l.prefsPaths() {
		b.WriteString(" " + adbx.Quote(p))
	}
	b.WriteString("; do\n" +
		"  [ -d \"${f%/*}\" ] || continue\n" +
		"  cp \"$t\" \"$f.new\"\n" +
		"  chown mobile:mobile \"$f.new\"\n" +
		"  mv -f \"$f.new\" \"$f\"\n" +
		"done\n" +
		"rm -f \"$t\"\n" +
		// 服务端经 cfprefsd 读设置,它手里有缓存:不让它重读,服务拿到的还是上一次的
		"killall -9 cfprefsd 2>/dev/null || true\n")
	if l.daemon != "" {
		d := adbx.Quote(l.daemon)
		b.WriteString("launchctl unload " + d + " 2>/dev/null || true\n")
		if start {
			b.WriteString("launchctl load -w " + d + "\n")
		}
	}
	return b.String()
}

// startTroll 写好这一次的设置(新密码),重启服务
func startTroll(sh shell, l trollLayout, password string, keepAwake bool) error {
	prefs, err := trollPrefs(true, password, l.name(), keepAwake)
	if err != nil {
		return err
	}
	if _, err := sh.run(applyScript(l, true), bytes.NewReader(prefs), trollShellTimeout); err != nil {
		return fmt.Errorf("启动手机上的 TrollVNC 失败: %w", err)
	}
	return nil
}

// stopTroll 停掉服务,设置里也改成关着、去掉密码。
// 手机重启再越狱后 launchd 会按设置把它拉起来,那时它也只是空转
func stopTroll(sh shell, l trollLayout) error {
	prefs, err := trollPrefs(false, "", l.name(), false)
	if err != nil {
		return err
	}
	_, err = sh.run(applyScript(l, false), bytes.NewReader(prefs), trollShellTimeout)
	return err
}

// trollLog 服务最近的日志,启动失败时附在错误后面
func trollLog(sh shell) string {
	out, _ := sh.run("tail -n 6 /tmp/trollvnc-stderr.log 2>/dev/null", nil, 10*time.Second)
	return strings.TrimSpace(out)
}

// waitVNC 等服务开始听端口,并确认它要密码
func waitVNC(dial func() (net.Conn, error), timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for {
		c, err := dial()
		if err == nil {
			err = checkVNC(c)
			c.Close()
			if err == nil || errors.Is(err, errVNCNoAuth) {
				return err
			}
		}
		last = err
		if time.Now().After(deadline) {
			return fmt.Errorf("等了 %d 秒,手机上的 TrollVNC 还没开始服务: %w", int(timeout.Seconds()), last)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// checkVNC 握手握到「认证方式」为止:要有 VNC 密码这一种,而且不能有「不认证」
func checkVNC(c net.Conn) error {
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	var ver [12]byte
	if _, err := io.ReadFull(c, ver[:]); err != nil {
		return fmt.Errorf("读不到 VNC 版本: %w", err)
	}
	if !bytes.HasPrefix(ver[:], []byte("RFB 003.")) {
		return fmt.Errorf("端口上不是 VNC 服务(%q)", ver[:])
	}
	if _, err := c.Write(ver[:]); err != nil {
		return err
	}
	// 3.3 版是服务端直接指定一种(4 字节);3.7 起是列出几种让客户端挑
	if string(ver[:]) == "RFB 003.003\n" {
		var t [4]byte
		if _, err := io.ReadFull(c, t[:]); err != nil {
			return err
		}
		if t != [4]byte{0, 0, 0, 2} {
			return errVNCNoAuth
		}
		return nil
	}
	var n [1]byte
	if _, err := io.ReadFull(c, n[:]); err != nil {
		return err
	}
	if n[0] == 0 {
		return errors.New("VNC 服务拒绝了连接")
	}
	types := make([]byte, n[0])
	if _, err := io.ReadFull(c, types); err != nil {
		return err
	}
	if bytes.IndexByte(types, 1) >= 0 || bytes.IndexByte(types, 2) < 0 {
		return errVNCNoAuth
	}
	return nil
}

// newVNCPassword 8 位随机密码:经典 VNC 认证只认前 8 位。去掉了 0/O、1/l/I 这种认错的字
func newVNCPassword() string {
	const chars = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			panic(err)
		}
		b[i] = chars[n.Int64()]
	}
	return string(b)
}

// ---- 安装 ----

// TrollInstall 装好的结果
type TrollInstall struct {
	Version string `json:"version"`
	// Package 从选的文件里挑中的那个安装包
	Package string `json:"package"`
}

// installTroll 把 TrollVNC 装到手机上。pkgPath 是 .deb,或者 GitHub Actions 下载下来的 zip。
//
// 先把「关着」的设置写好再装:包的安装脚本装完会立刻启动服务,
// 没有设置的话它会按默认的来 —— 听所有网卡、不要密码、在局域网里广播
func installTroll(sh shell, pkgPath string) (*TrollInstall, error) {
	l, err := probeTroll(sh)
	if err != nil {
		return nil, err
	}
	if why := l.unsupported(); why != "" {
		return nil, errors.New(why)
	}
	if !l.loader {
		return nil, errors.New("手机上还缺 PreferenceLoader(TrollVNC 的包依赖它):" +
			"在手机的 Sileo 里搜「PreferenceLoader」装上,装好再点一次安装")
	}
	name, data, err := pickDeb(pkgPath, l.arch, l.artifact())
	if err != nil {
		return nil, err
	}
	if err := stopTroll(sh, l); err != nil {
		return nil, fmt.Errorf("装之前写设置失败: %w", err)
	}
	daemon := adbx.Quote(l.prefix + "/Library/LaunchDaemons/" + trollPackage + ".plist")
	script := "t=/tmp/tf-trollvnc.$$.deb\n" +
		"cat > \"$t\"\n" +
		"dpkg -i \"$t\" 2>&1\n" +
		"rc=$?\n" +
		"rm -f \"$t\"\n" +
		// 装完它已经按「关着」的设置起来空转了;停掉,等投屏时再按那一次的设置起
		"[ -f " + daemon + " ] && launchctl unload " + daemon + " 2>/dev/null\n" +
		"exit $rc\n"
	if _, err := sh.run(script, bytes.NewReader(data), trollInstallTimeout); err != nil {
		return nil, fmt.Errorf("dpkg 安装失败: %w", err)
	}
	after, err := probeTroll(sh)
	if err != nil {
		return nil, err
	}
	if !after.installed() {
		return nil, errors.New("dpkg 说装完了,手机上却找不到 TrollVNC 的服务")
	}
	return &TrollInstall{Version: after.version, Package: name}, nil
}

// pickDeb 从选的文件里挑出这台手机能装的 TrollVNC 包
func pickDeb(path, arch, artifact string) (string, []byte, error) {
	if strings.EqualFold(filepath.Ext(path), ".deb") {
		name := filepath.Base(path)
		if err := matchDeb(name, arch, artifact); err != nil {
			return "", nil, err
		}
		data, err := readLimited(path)
		return name, data, err
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", nil, fmt.Errorf("打不开 %s:选 .deb,或者 GitHub Actions 下载下来的 zip", filepath.Base(path))
	}
	defer zr.Close()
	var seen []string
	for _, f := range zr.File {
		name := pathBase(f.Name)
		if !strings.HasSuffix(strings.ToLower(name), ".deb") {
			continue
		}
		if matchDeb(name, arch, artifact) != nil {
			seen = append(seen, name)
			continue
		}
		if f.UncompressedSize64 > maxPackage {
			return "", nil, fmt.Errorf("%s 太大了,不像是 TrollVNC 的包", name)
		}
		rc, err := f.Open()
		if err != nil {
			return "", nil, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxPackage))
		rc.Close()
		if err != nil {
			return "", nil, fmt.Errorf("解压 %s 失败: %w", name, err)
		}
		return name, data, nil
	}
	if len(seen) > 0 {
		return "", nil, fmt.Errorf("zip 里的包(%s)不是给这台手机的:它要 %s 的包,在 Actions 的产物里下 %s",
			strings.Join(seen, "、"), arch, artifact)
	}
	return "", nil, fmt.Errorf("zip 里没有 TrollVNC 的安装包:在 Actions 的产物里下 %s", artifact)
}

// matchDeb 文件名得是 com.82flex.trollvnc_<版本>_<架构>.deb,架构和手机一致
func matchDeb(name, arch, artifact string) error {
	lower := strings.ToLower(name)
	if !strings.HasPrefix(lower, trollPackage+"_") || !strings.HasSuffix(lower, ".deb") {
		return fmt.Errorf("%s 不是 TrollVNC 的安装包(文件名应该是 %s_版本_架构.deb)", name, trollPackage)
	}
	got := lower[strings.LastIndex(lower[:len(lower)-4], "_")+1 : len(lower)-4]
	if got != arch {
		return fmt.Errorf("%s 是给 %s 的,这台手机要 %s 的包:在 Actions 的产物里下 %s", name, got, arch, artifact)
	}
	return nil
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPackage+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPackage {
		return nil, fmt.Errorf("%s 太大了,不像是 TrollVNC 的包", filepath.Base(path))
	}
	return data, nil
}

// pathBase zip 里的路径不管在哪个系统上打的包都是 / 分隔
func pathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// lastLines 只留最后几行:dpkg 一类的输出前面都是进度,原因在最后
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// iPhoneNames 型号代码 → 名字。只列现在的越狱工具支持的这几代,认不出就显示代码本身
var iPhoneNames = map[string]string{
	"iPhone8,1": "iPhone 6s", "iPhone8,2": "iPhone 6s Plus", "iPhone8,4": "iPhone SE",
	"iPhone9,1": "iPhone 7", "iPhone9,3": "iPhone 7", "iPhone9,2": "iPhone 7 Plus", "iPhone9,4": "iPhone 7 Plus",
	"iPhone10,1": "iPhone 8", "iPhone10,4": "iPhone 8", "iPhone10,2": "iPhone 8 Plus", "iPhone10,5": "iPhone 8 Plus",
	"iPhone10,3": "iPhone X", "iPhone10,6": "iPhone X",
	"iPhone11,2": "iPhone XS", "iPhone11,4": "iPhone XS Max", "iPhone11,6": "iPhone XS Max", "iPhone11,8": "iPhone XR",
	"iPhone12,1": "iPhone 11", "iPhone12,3": "iPhone 11 Pro", "iPhone12,5": "iPhone 11 Pro Max", "iPhone12,8": "iPhone SE 2",
	"iPhone13,1": "iPhone 12 mini", "iPhone13,2": "iPhone 12", "iPhone13,3": "iPhone 12 Pro", "iPhone13,4": "iPhone 12 Pro Max",
	"iPhone14,4": "iPhone 13 mini", "iPhone14,5": "iPhone 13", "iPhone14,2": "iPhone 13 Pro", "iPhone14,3": "iPhone 13 Pro Max",
	"iPhone14,6": "iPhone SE 3", "iPhone14,7": "iPhone 14", "iPhone14,8": "iPhone 14 Plus",
	"iPhone15,2": "iPhone 14 Pro", "iPhone15,3": "iPhone 14 Pro Max",
}
