package devicefs

import (
	"strings"
	"testing"
)

// 连设备那部分没法在测试里跑(要有一台插着的越狱手机),
// 但路径处理、shell 转义、类型判断这些是纯函数,而且恰好是最容易出事的部分。

func TestCleanRemote(t *testing.T) {
	cases := map[string]string{
		"/private/var//mobile///Library": "/private/var/mobile/Library",
		"/private/var/mobile/":           "/private/var/mobile",
		"private/var":                    "/private/var",
		"  /var/mobile  ":                "/var/mobile",
		"/":                              "/",
		// Windows 上从别处复制过来的路径可能带反斜杠,但发给 iOS 的必须是正斜杠。
		// 这里不能用 filepath.Clean —— 它在 Windows 上会把正斜杠换成反斜杠
		`\var\mobile`: "/var/mobile",
	}
	for in, want := range cases {
		if got := cleanRemote(in); got != want {
			t.Errorf("cleanRemote(%q) = %q,想要 %q", in, got, want)
		}
	}
	if got := cleanRemote(""); got != "" {
		t.Errorf("空输入应该原样返回空(由调用方决定用哪个默认目录),得到 %q", got)
	}
}

func TestParentOf(t *testing.T) {
	if got := parentOf("/a/b/c"); got != "/a/b" {
		t.Errorf("得到 %q", got)
	}
	// 根目录没有上级 —— 返回 "/" 的话界面上那个"返回上级"按钮会一直亮着
	if got := parentOf("/"); got != "" {
		t.Errorf("根目录不该有上级,得到 %q", got)
	}
}

// 查找的模式和路径都是用户输入的,直接拼进 shell 命令等于把设备交出去
func TestShellQuoteBlocksInjection(t *testing.T) {
	evil := `'; rm -rf /; echo '`
	q := shellQuote(evil)
	// 转义后整串必须还是一个参数:单引号要么被包住,要么被转义成 '\''
	if strings.HasPrefix(q, "'") && strings.HasSuffix(q, "'") {
		inner := q[1 : len(q)-1]
		if strings.Contains(strings.ReplaceAll(inner, `'\''`, ""), "'") {
			t.Errorf("还有没转义的单引号泄出来: %s", q)
		}
	} else {
		t.Errorf("没有被引号包住: %s", q)
	}
	if got := shellQuote("普通路径"); got != "'普通路径'" {
		t.Errorf("普通输入被改坏了: %s", got)
	}
}

// 两个不同目录下的同名文件不能落到同一个本地缓存文件上 ——
// iOS 上到处都是叫 Info.plist / Cache.db 的文件,撞了就是互相覆盖
func TestSafeLocalNameIsUnique(t *testing.T) {
	a := safeLocalName("/private/var/mobile/Library/A/Info.plist")
	b := safeLocalName("/private/var/mobile/Library/B/Info.plist")
	if a == b {
		t.Errorf("不同路径的同名文件压成了同一个名字: %s", a)
	}
	for _, bad := range []string{"/", "\\", ":", "*", "?", `"`, "<", ">", "|"} {
		if strings.Contains(a, bad) {
			t.Errorf("%q 里还留着非法字符 %q", a, bad)
		}
	}
	// 太长的路径要截短,否则 Windows 上创建就失败
	long := safeLocalName("/" + strings.Repeat("verylongsegment/", 40) + "file.bin")
	if len(long) > 160 {
		t.Errorf("截断没生效,长度 %d", len(long))
	}
	if !strings.HasSuffix(long, "file.bin") {
		t.Errorf("截断应该从头砍、保住文件名那一头,得到 %q", long)
	}
}

func TestLooksLikeXMLPlist(t *testing.T) {
	if !looksLikeXMLPlist([]byte(`<?xml version="1.0"?>`)) {
		t.Error("XML 声明没认出来")
	}
	if !looksLikeXMLPlist([]byte("\n  <plist version=\"1.0\">")) {
		t.Error("前面有空白的 <plist 没认出来")
	}
	if looksLikeXMLPlist([]byte("<html>")) {
		t.Error("HTML 不该被认成 plist")
	}
}

func TestIsMostlyText(t *testing.T) {
	if !isMostlyText([]byte("hello 世界\n")) {
		t.Error("正常 UTF-8 文本应该判成文本")
	}
	// 二进制里 NUL 遍地都是,一个就够否掉
	if isMostlyText([]byte{'a', 'b', 0, 'c'}) {
		t.Error("含 NUL 的不该判成文本")
	}
	if isMostlyText([]byte{0xff, 0xfe, 0xfd}) {
		t.Error("非法 UTF-8 不该判成文本")
	}
	if isMostlyText(nil) {
		t.Error("空的不该判成文本")
	}
}

// 列目录的结果里切片不能是 nil —— Go 的 nil 切片序列化出来是 null,
// 前端拿到 null 再 .map 就是整页白屏
func TestListingSlicesNeverNil(t *testing.T) {
	l := &Listing{Path: "/x", Entries: []Entry{}}
	if l.Entries == nil {
		t.Fatal("Entries 是 nil")
	}
	r := &SearchResult{Hits: []SearchHit{}}
	if r.Hits == nil {
		t.Fatal("Hits 是 nil")
	}
}

// 找不到会话时要说人话,不能让调用方对着 nil 猜
func TestGetMissingSession(t *testing.T) {
	m := NewManager()
	_, err := m.get("不存在")
	if err == nil {
		t.Fatal("应该报错")
	}
	if !strings.Contains(err.Error(), "重新连接") {
		t.Errorf("报错里应该说下一步怎么办,得到: %v", err)
	}
}

func TestConnectRejectsBadInput(t *testing.T) {
	m := NewManager()
	if _, err := m.Connect(ConnectOptions{Platform: "android"}); err == nil {
		t.Error("目前只支持 ios,别的该明确拒绝")
	}
	if _, err := m.Connect(ConnectOptions{Platform: "ios"}); err == nil {
		t.Error("没给密码该报错")
	} else if !strings.Contains(err.Error(), "密码") {
		t.Errorf("报错该点明缺的是密码,得到: %v", err)
	}
}

// Disconnect 一个不存在的会话不该报错:界面上可能重复点,
// 或者连接已经自己断了 —— 这时候再抛个错只会让人以为出了新问题
func TestDisconnectUnknownIsQuiet(t *testing.T) {
	m := NewManager()
	if err := m.Disconnect("dev-999"); err != nil {
		t.Errorf("断开一个不存在的会话不该报错: %v", err)
	}
}
