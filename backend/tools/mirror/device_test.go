package mirror

import (
	"strings"
	"testing"
)

func TestParseDeviceInfo(t *testing.T) {
	got := parseDeviceInfo("abc", "30\r\n11\r\nOnePlus\r\nOnePlus\r\n")
	if got.serial != "abc" || got.sdk != 30 || got.release != "11" || got.brand != "oneplus oneplus" {
		t.Fatalf("解析不对: %+v", got)
	}
	// 读不到也不耽误投屏:留空
	if got := parseDeviceInfo("abc", ""); got.sdk != 0 || got.brand != "" {
		t.Fatalf("空输出该留空: %+v", got)
	}
}

func TestInjectDeniedNotice(t *testing.T) {
	for _, c := range []struct{ brand, want, not string }{
		{"redmi xiaomi", "USB 调试（安全设置）", "禁止权限监控"},
		{"poco xiaomi", "USB 调试（安全设置）", "禁止权限监控"},
		{"oppo oppo", "禁止权限监控", "小米"},
		{"realme realme", "禁止权限监控", "小米"},
		{"samsung samsung", "开发者选项", ""},
	} {
		n := injectDeniedNotice(c.brand)
		if n.Code != "inject-denied" || !strings.Contains(n.Text, c.want) || (c.not != "" && strings.Contains(n.Text, c.not)) {
			t.Errorf("%s: %s", c.brand, n.Text)
		}
	}
}
