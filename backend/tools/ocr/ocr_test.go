package ocr

import "testing"

// 汉字之间的空格要去掉、中文标点两边的也去掉,英文单词之间、汉字和数字之间的要留着 ——
// 系统 OCR 把每个汉字当一个词
func TestTidyLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"你 好 世 界", "你好世界"},
		{"hello world", "hello world"},
		{"账号 abc 123 已 到账", "账号 abc 123 已到账"},
		{"  转账 ： 500 元  ", "转账：500 元"},
		{"张 三 ， 你 好 ！", "张三，你好！"},
		{"", ""},
	}
	for _, c := range cases {
		if got := tidyLine(c.in); got != c.want {
			t.Errorf("tidyLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAssembleDropsEmptyLines(t *testing.T) {
	r := assemble("zh-Hans-CN", []string{"第 一 行", "", "  ", "second"})
	if len(r.Lines) != 2 || r.Lines[0] != "第一行" || r.Lines[1] != "second" {
		t.Fatalf("lines = %q", r.Lines)
	}
	if r.Text != "第一行\nsecond" || r.Lang != "zh-Hans-CN" {
		t.Fatalf("text = %q lang = %q", r.Text, r.Lang)
	}
}
