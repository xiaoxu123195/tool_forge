package system

import "testing"

func TestParseSpecAcceptsAltAndOption(t *testing.T) {
	for _, spec := range []string{"Alt+V", "Option+V"} {
		mods, key, err := parseSpec(spec)
		if err != nil {
			t.Fatalf("parseSpec(%q) 返回错误: %v", spec, err)
		}
		if len(mods) != 1 || !isHotkeyAltModifier(mods[0]) {
			t.Fatalf("parseSpec(%q) 未解析为平台 Alt 修饰键: %#v", spec, mods)
		}
		if keyName(key) != "V" {
			t.Fatalf("parseSpec(%q) 主键 = %s, 期望 V", spec, keyName(key))
		}
	}
}

func TestNormalizeSpecKeepsAltName(t *testing.T) {
	got := normalizeSpec("option+v")
	if got != "Alt+V" {
		t.Fatalf("normalizeSpec() = %q, 期望 Alt+V", got)
	}
}
