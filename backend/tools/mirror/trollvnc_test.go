package mirror

import (
	"archive/zip"
	"bytes"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLayout(t *testing.T) {
	l := parseLayout(rootlessProbe)
	if !l.installed() || l.unsupported() != "" || l.version != "3.2-272" || !l.loader || l.name() != "iPhone 8 Plus" {
		t.Fatalf("rootless 解析不对: %+v", l)
	}
	if got := l.prefsPaths(); len(got) != 2 || !strings.HasPrefix(got[0], "/var/jb/") {
		t.Fatalf("rootless 两处都要写: %v", got)
	}
	if l.artifact() != "packages-rootless" {
		t.Fatalf("rootless 该下 packages-rootless: %s", l.artifact())
	}

	rootful := parseLayout("prefix=\narch=iphoneos-arm\nmodel=iPhone9,9\nloader=deinstall ok config-files\n")
	if rootful.installed() || rootful.loader || rootful.artifact() != "packages-default" || rootful.name() != "iPhone9,9" {
		t.Fatalf("rootful 解析不对: %+v", rootful)
	}
	if got := rootful.prefsPaths(); len(got) != 1 || got[0] != "/var/mobile/Library/Preferences/com.82flex.trollvnc.plist" {
		t.Fatalf("rootful 只写系统那一处: %v", got)
	}
	if parseLayout("arch=iphoneos-arm64e\n").unsupported() == "" {
		t.Fatal("roothide 应该说明不支持")
	}
	if parseLayout("arch=\n").unsupported() == "" {
		t.Fatal("没有 dpkg 应该说明不像越狱设备")
	}
}

func TestTrollPrefs(t *testing.T) {
	on, err := trollPrefs(true, "Ab3dEf7h", "iPhone 8 Plus", false)
	if err != nil {
		t.Fatal(err)
	}
	m := decodePrefs(t, on)
	if m["FullPassword"] != "Ab3dEf7h" || m["KeepAliveSec"] != uint64(0) || m["NaturalScroll"] != true || m["ModifierMap"] != "std" {
		t.Fatalf("开着的设置不对: %#v", m)
	}
	off, err := trollPrefs(false, "Ab3dEf7h", "iPhone 8 Plus", true)
	if err != nil {
		t.Fatal(err)
	}
	if m := decodePrefs(t, off); m["Enabled"] != false || m["FullPassword"] != nil {
		t.Fatalf("关着的设置不能带密码: %#v", m)
	}
}

func TestApplyScriptWithoutDaemon(t *testing.T) {
	s := applyScript(parseLayout("prefix=\narch=iphoneos-arm\n"), true, true)
	if strings.Contains(s, "launchctl") {
		t.Fatalf("还没装的时候不该碰 launchctl:\n%s", s)
	}
	if !strings.Contains(s, "for f in '/var/mobile/Library/Preferences/com.82flex.trollvnc.plist'; do") {
		t.Fatalf("设置文件路径不对:\n%s", s)
	}
}

// fakeVNC 用 net.Pipe 扮演服务端,按给定字节应答握手
func fakeVNC(version string, reply []byte) net.Conn {
	a, b := net.Pipe()
	go func() {
		defer b.Close()
		_, _ = b.Write([]byte(version))
		buf := make([]byte, 12)
		if _, err := b.Read(buf); err != nil {
			return
		}
		_, _ = b.Write(reply)
	}()
	return a
}

func TestCheckVNC(t *testing.T) {
	cases := []struct {
		name    string
		version string
		reply   []byte
		want    error
	}{
		{"要密码", "RFB 003.008\n", []byte{1, 2}, nil},
		{"多种里有密码", "RFB 003.008\n", []byte{2, 2, 16}, nil},
		{"不要密码", "RFB 003.008\n", []byte{1, 1}, errVNCNoAuth},
		{"两种都给", "RFB 003.008\n", []byte{2, 1, 2}, errVNCNoAuth},
		{"3.3 要密码", "RFB 003.003\n", []byte{0, 0, 0, 2}, nil},
		{"3.3 不要密码", "RFB 003.003\n", []byte{0, 0, 0, 1}, errVNCNoAuth},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			conn := fakeVNC(c.version, c.reply)
			defer conn.Close()
			if err := checkVNC(conn); !errors.Is(err, c.want) {
				t.Fatalf("得到 %v,应该是 %v", err, c.want)
			}
		})
	}
	conn := fakeVNC("SSH-2.0-Open", nil)
	defer conn.Close()
	if err := checkVNC(conn); err == nil || errors.Is(err, errVNCNoAuth) {
		t.Fatalf("不是 VNC 的端口应该报别的错: %v", err)
	}
}

func TestNewVNCPassword(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		p := newVNCPassword()
		if len(p) != 8 || strings.ContainsAny(p, "0O1lI") {
			t.Fatalf("密码 %q 不对", p)
		}
		seen[p] = true
	}
	if len(seen) < 50 {
		t.Fatal("随机密码重复了")
	}
}

// writeZip 造一个 GitHub Actions 产物那样的 zip
func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "packages.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPickDeb(t *testing.T) {
	zipPath := writeZip(t, map[string]string{
		"com.82flex.trollvnc_3.2-272_iphoneos-arm.deb":            "rootful",
		"packages/com.82flex.trollvnc_3.2-272_iphoneos-arm64.deb": "rootless",
		"README.txt": "x",
	})
	name, data, err := pickDeb(zipPath, "iphoneos-arm64", "packages-rootless")
	if err != nil || string(data) != "rootless" || name != "com.82flex.trollvnc_3.2-272_iphoneos-arm64.deb" {
		t.Fatalf("应该挑中 rootless 的包: %q %q %v", name, data, err)
	}

	only := writeZip(t, map[string]string{"com.82flex.trollvnc_3.2-272_iphoneos-arm64e.deb": "roothide"})
	if _, _, err := pickDeb(only, "iphoneos-arm64", "packages-rootless"); err == nil || !strings.Contains(err.Error(), "packages-rootless") {
		t.Fatalf("包不对时要说该下哪个: %v", err)
	}
	empty := writeZip(t, map[string]string{"a.txt": "x"})
	if _, _, err := pickDeb(empty, "iphoneos-arm64", "packages-rootless"); err == nil {
		t.Fatal("没有包应该报错")
	}

	deb := filepath.Join(t.TempDir(), "com.82flex.trollvnc_3.2-272_iphoneos-arm64.deb")
	_ = os.WriteFile(deb, []byte("direct"), 0o644)
	if _, data, err := pickDeb(deb, "iphoneos-arm64", "packages-rootless"); err != nil || string(data) != "direct" {
		t.Fatalf("直接选 .deb 也要能装: %v", err)
	}
	other := filepath.Join(t.TempDir(), "com.example.tweak_1.0_iphoneos-arm64.deb")
	_ = os.WriteFile(other, []byte("x"), 0o644)
	if _, _, err := pickDeb(other, "iphoneos-arm64", "packages-rootless"); err == nil {
		t.Fatal("别的包不能拿来装")
	}
}

func TestInstallTroll(t *testing.T) {
	zipPath := writeZip(t, map[string]string{"com.82flex.trollvnc_3.2-272_iphoneos-arm64.deb": "deb-bytes"})
	phone := newFakePhone("prefix=/var/jb\narch=iphoneos-arm64\nmodel=iPhone10,2\nloader=install ok installed\n")
	res, err := installTroll(phone, zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != "3.2-272" || res.Package != "com.82flex.trollvnc_3.2-272_iphoneos-arm64.deb" {
		t.Fatalf("结果不对: %+v", res)
	}
	// 顺序:查 → 先写好关着的设置 → 推包装 → 再查一遍
	if len(phone.scripts) != 4 || phone.scripts[0] != probeScript || phone.scripts[3] != probeScript {
		t.Fatalf("脚本顺序不对: %d 段", len(phone.scripts))
	}
	if m := decodePrefs(t, phone.stdins[1]); m["Enabled"] != false || m["BindHost"] != "127.0.0.1" {
		t.Fatalf("装之前要先写好关着、只听本机的设置: %#v", m)
	}
	if !strings.Contains(phone.scripts[2], "dpkg -i") || string(phone.stdins[2]) != "deb-bytes" {
		t.Fatalf("安装脚本或推上去的包不对:\n%s", phone.scripts[2])
	}
	if !strings.Contains(phone.scripts[2], "launchctl unload '/var/jb/Library/LaunchDaemons/com.82flex.trollvnc.plist'") {
		t.Fatalf("装完要把服务停掉:\n%s", phone.scripts[2])
	}

	noLoader := newFakePhone("prefix=/var/jb\narch=iphoneos-arm64\nmodel=iPhone10,2\n")
	if _, err := installTroll(noLoader, zipPath); err == nil || !strings.Contains(err.Error(), "PreferenceLoader") {
		t.Fatalf("缺 PreferenceLoader 要说清楚: %v", err)
	}
	if len(noLoader.scripts) != 1 {
		t.Fatal("缺依赖时不该往手机写东西")
	}
}
