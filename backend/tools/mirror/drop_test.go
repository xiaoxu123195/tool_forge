package mirror

import (
	"errors"
	"strings"
	"testing"
)

// 手机上已经有同名的:换成「名字 (1).扩展名」,绝不覆盖
func TestFreeName(t *testing.T) {
	taken := map[string]bool{
		"/sdcard/Download/a.jpg":     true,
		"/sdcard/Download/a (1).jpg": true,
		"/sdcard/Download/相册":        true,
		"/sdcard/Download/.nomedia":  true,
	}
	exists := func(p string) (bool, error) { return taken[p], nil }
	for _, c := range []struct{ name, want string }{
		{"b.jpg", "/sdcard/Download/b.jpg"},
		{"a.jpg", "/sdcard/Download/a (2).jpg"},
		{"相册", "/sdcard/Download/相册 (1)"},
		{".nomedia", "/sdcard/Download/.nomedia (1)"},
	} {
		got, err := freeName(pushDir, c.name, exists)
		if err != nil || got != c.want {
			t.Errorf("%s → %s %v,应为 %s", c.name, got, err, c.want)
		}
	}
	// 查不了就别推:不知道会不会覆盖
	if _, err := freeName(pushDir, "x", func(string) (bool, error) { return false, errors.New("断了") }); err == nil {
		t.Error("查不了该报错")
	}
}

func TestInstallResult(t *testing.T) {
	if err := installResult("Performing Streamed Install\nSuccess\n", nil); err != nil {
		t.Fatalf("成功的被当成失败: %v", err)
	}
	cases := []struct{ out, want string }{
		{"Failure [INSTALL_FAILED_VERSION_DOWNGRADE: Package Verification Result]", "更新的版本"},
		{"adb: failed to install x.apk: Failure [INSTALL_FAILED_UPDATE_INCOMPATIBLE: Package com.x signatures do not match]", "签名不一样"},
		{"Failure [INSTALL_FAILED_USER_RESTRICTED: Install canceled by user]", "USB 安装"},
		{"Failure [INSTALL_FAILED_NO_MATCHING_ABIS: Failed to extract native libraries, res=-113]", "处理器"},
		{"Failure [INSTALL_FAILED_WHATEVER]", "INSTALL_FAILED_WHATEVER"},
		{"Exception occurred while executing 'install':\njava.lang.IllegalArgumentException: Error: Unable to open file", "Unable to open file"},
	}
	for _, c := range cases {
		err := installResult(c.out, nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q → %v,应提到 %s", c.out, err, c.want)
		}
	}
	if code, detail := installFailure("Failure [INSTALL_FAILED_DEPRECATED_SDK_VERSION: App package must target at least SDK version 23]"); code != "INSTALL_FAILED_DEPRECATED_SDK_VERSION" || !strings.Contains(detail, "SDK version 23") {
		t.Errorf("失败代码没取对: %s / %s", code, detail)
	}
	if err := installResult("", errors.New("超时")); err == nil || !strings.Contains(err.Error(), "超时") {
		t.Errorf("命令本身失败要说出来: %v", err)
	}
}

func TestIsMedia(t *testing.T) {
	for p, want := range map[string]bool{
		"/sdcard/Download/a.JPG": true, "/sdcard/Download/v.mp4": true, "/sdcard/Download/s.m4a": true,
		"/sdcard/Download/a.txt": false, "/sdcard/Download/db": false,
	} {
		if isMedia(p) != want {
			t.Errorf("%s 该是 %v", p, want)
		}
	}
}

func TestProgressReader(t *testing.T) {
	var got []int
	r := &progressReader{r: strings.NewReader(strings.Repeat("x", 100)), total: 100, report: func(p int) { got = append(got, p) }}
	buf := make([]byte, 40)
	for {
		if _, err := r.Read(buf); err != nil {
			break
		}
	}
	// 第一次读完马上报一次,之后隔一会儿才报
	if len(got) != 1 || got[0] != 40 {
		t.Errorf("进度报得不对: %v", got)
	}
}
