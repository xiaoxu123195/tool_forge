package devicefs

import (
	"strings"
	"testing"
)

// Android 那边没有 SFTP,所有信息都是从 stat 的文本输出里抠出来的,
// 所以解析这一步错了就是整个目录都不对。下面每一条都是真机上取的实际输出。

func TestParseStatLine(t *testing.T) {
	cases := []struct {
		name string
		line string
		want Entry
	}{
		{
			"目录",
			"directory|4096|1788762581|/data",
			Entry{Name: "data", Path: "/data", IsDir: true, Size: 4096, ModTime: 1788762581},
		},
		{
			"普通文件",
			"regular file|502056|1230768000|/system/bin/toybox",
			Entry{Name: "toybox", Path: "/system/bin/toybox", Size: 502056, ModTime: 1230768000},
		},
		{
			// %N 对软链给的是 "路径 -> '目标'",目标带单引号
			"软链",
			"symbolic link|21|1230768000|/sdcard -> '/storage/self/primary'",
			Entry{
				Name: "sdcard", Path: "/sdcard", Size: 21, ModTime: 1230768000,
				Symlink: "/storage/self/primary",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseStatLine(c.line)
			if !ok {
				t.Fatal("没解出来")
			}
			if got.Name != c.want.Name || got.Path != c.want.Path ||
				got.IsDir != c.want.IsDir || got.Size != c.want.Size ||
				got.ModTime != c.want.ModTime || got.Symlink != c.want.Symlink {
				t.Errorf("解错了:\n得到 %+v\n想要 %+v", got, c.want)
			}
		})
	}
}

// 文件名里带 | 是合法的,而 stat 的分隔符也是 | ——
// 只切前三个、路径整段留在最后,是这里唯一切得对的做法
func TestParseStatLineKeepsPipeInName(t *testing.T) {
	got, ok := parseStatLine("regular file|10|1700000000|/sdcard/a|b|c.txt")
	if !ok {
		t.Fatal("没解出来")
	}
	if got.Path != "/sdcard/a|b|c.txt" {
		t.Errorf("路径里的 | 被切掉了,得到 %q", got.Path)
	}
	if got.Name != "a|b|c.txt" {
		t.Errorf("文件名不对,得到 %q", got.Name)
	}
}

func TestParseStatLineRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "   ", "只有一段", "两|段", "三|段|了"} {
		if _, ok := parseStatLine(bad); ok {
			t.Errorf("%q 不该被当成有效行", bad)
		}
	}
}

func TestParseStatLinesCollectsHits(t *testing.T) {
	out := "regular file|1|100|/a/x\ndirectory|2|200|/a/sub\n垃圾行\n"
	res := parseStatLines(out, "/a", "*", 10)
	if len(res.Hits) != 2 {
		t.Fatalf("应该 2 条,得到 %d", len(res.Hits))
	}
	if !res.Hits[1].IsDir {
		t.Error("目录没标出来")
	}
	if res.Truncated {
		t.Error("没超上限不该标截断")
	}
	// 超出上限要标出来,不然人以为就这么多
	if r := parseStatLines(out, "/a", "*", 1); !r.Truncated || len(r.Hits) != 1 {
		t.Errorf("截断标记不对: %+v", r)
	}
}

// Hits 必须是空切片不能是 nil —— nil 序列化成 null,前端 .map 当场白屏
func TestParseStatLinesEmptyIsNotNil(t *testing.T) {
	r := parseStatLines("", "/a", "*", 10)
	if r.Hits == nil {
		t.Fatal("Hits 是 nil")
	}
}

// 没 root 时 /data 下面是读不了的,报错得说清楚是这个原因,
// 不然人会以为是路径打错了
func TestRootHint(t *testing.T) {
	if h := (&androidTransport{root: true}).rootHint(); h != "" {
		t.Errorf("有 root 时不该加提示,得到 %q", h)
	}
	if h := (&androidTransport{root: false}).rootHint(); !strings.Contains(h, "root") {
		t.Errorf("没 root 时该点明原因,得到 %q", h)
	}
}

// 有 root 落在 /data/data(各家 App 的数据在那儿),没 root 只能看 /sdcard
func TestAndroidStartPath(t *testing.T) {
	if p := (&androidTransport{root: true}).startPath(); p != "/data/data" {
		t.Errorf("有 root 该落在 /data/data,得到 %q", p)
	}
	if p := (&androidTransport{root: false}).startPath(); p != "/sdcard" {
		t.Errorf("没 root 该落在 /sdcard,得到 %q", p)
	}
}

// 两个平台的预览上限差一个数量级,是因为通道速度差二十倍
func TestPreviewLimitsDifferByPlatform(t *testing.T) {
	if (&androidTransport{}).previewLimit() >= (&iosTransport{}).previewLimit() {
		t.Error("Android 走 base64 文本通道,预览上限应该比 iOS 小")
	}
}
