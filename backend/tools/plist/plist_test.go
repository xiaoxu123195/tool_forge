package plist

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	hplist "howett.net/plist"
)

// 样本用代码生成而不是塞真文件进仓库:真的 plist 来自别人的设备,
// 不该进版本库;而且生成的样本能精确控制每一种边界。

func mustBinary(t *testing.T, v any) []byte {
	t.Helper()
	b, err := hplist.Marshal(v, hplist.BinaryFormat)
	if err != nil {
		t.Fatalf("造样本失败: %v", err)
	}
	return b
}

const sampleXML = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.app</string>
	<key>Count</key>
	<integer>42</integer>
	<key>Enabled</key>
	<true/>
	<key>Items</key>
	<array>
		<string>a</string>
		<string>b</string>
	</array>
</dict>
</plist>`

func TestParseXML(t *testing.T) {
	res, err := Parse([]byte(sampleXML), DefaultOptions())
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if res.Format != "xml" {
		t.Errorf("格式应该是 xml,得到 %q", res.Format)
	}
	m, ok := res.Value.(map[string]any)
	if !ok {
		t.Fatalf("顶层应该是字典,得到 %T", res.Value)
	}
	if m["CFBundleIdentifier"] != "com.example.app" {
		t.Errorf("字符串不对: %v", m["CFBundleIdentifier"])
	}
	if m["Enabled"] != true {
		t.Errorf("布尔不对: %v", m["Enabled"])
	}
	if items, ok := m["Items"].([]any); !ok || len(items) != 2 {
		t.Errorf("数组不对: %v", m["Items"])
	}
}

func TestParseBinary(t *testing.T) {
	data := mustBinary(t, map[string]any{
		"name": "张三",
		"n":    int64(7),
		"blob": []byte{0xde, 0xad, 0xbe, 0xef},
	})
	if string(data[:6]) != "bplist" {
		t.Fatalf("样本不是 bplist: %q", data[:8])
	}
	res, err := Parse(data, DefaultOptions())
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if res.Format != "binary" {
		t.Errorf("格式应该是 binary,得到 %q", res.Format)
	}
	m := res.Value.(map[string]any)
	if m["name"] != "张三" {
		t.Errorf("中文字符串丢了: %v", m["name"])
	}
	// data 块要变成带 __size 的结构,而不是一串裸 base64 ——
	// 光看 base64 认不出它原本是二进制
	blob, ok := m["blob"].(map[string]any)
	if !ok {
		t.Fatalf("data 应该渲染成对象,得到 %T", m["blob"])
	}
	if blob["__size"] != 4 {
		t.Errorf("__size 不对: %v", blob["__size"])
	}
	if got, _ := blob["__data"].(string); got != base64.StdEncoding.EncodeToString([]byte{0xde, 0xad, 0xbe, 0xef}) {
		t.Errorf("__data 不对: %v", got)
	}
}

// buildArchive 造一个 NSKeyedArchiver 归档:
// root 是个 NSDictionary,里面一个字符串键指向一个 NSArray
func buildArchive() map[string]any {
	return map[string]any{
		"$archiver": "NSKeyedArchiver",
		"$version":  uint64(100000),
		"$top":      map[string]any{"root": hplist.UID(1)},
		"$objects": []any{
			"$null", // 0
			map[string]any{ // 1 root NSDictionary
				"$class":     hplist.UID(6),
				"NS.keys":    []any{hplist.UID(2)},
				"NS.objects": []any{hplist.UID(3)},
			},
			"items", // 2
			map[string]any{ // 3 NSArray
				"$class":     hplist.UID(7),
				"NS.objects": []any{hplist.UID(4), hplist.UID(5)},
			},
			"第一项", // 4
			map[string]any{ // 5 NSDate:2001-01-01 之后 1 小时
				"$class":  hplist.UID(8),
				"NS.time": float64(3600),
			},
			map[string]any{"$classname": "NSDictionary"}, // 6
			map[string]any{"$classname": "NSArray"},      // 7
			map[string]any{"$classname": "NSDate"},       // 8
		},
	}
}

func TestUnwrapNSKeyedArchive(t *testing.T) {
	res, err := Parse(mustBinary(t, buildArchive()), DefaultOptions())
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !res.NSKeyed || !res.Unwrapped {
		t.Fatalf("应该识别成归档并拆包: nsKeyed=%v unwrapped=%v", res.NSKeyed, res.Unwrapped)
	}
	// $top 只有一个 root 键时不该再套一层
	m, ok := res.Value.(map[string]any)
	if !ok {
		t.Fatalf("拆包后应该直接是 root 那个字典,得到 %T", res.Value)
	}
	if _, wrapped := m["root"]; wrapped {
		t.Error("单键 $top 不该保留 root 这层包装")
	}
	items, ok := m["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("items 应该是 2 项的数组,得到 %#v", m["items"])
	}
	if items[0] != "第一项" {
		t.Errorf("NSString 没还原成字符串: %#v", items[0])
	}
	// NSDate 的 NS.time 是相对 2001-01-01 的秒数,不是 Unix 时间戳 ——
	// 认成 Unix 的话会算到 1970 年
	if got, _ := items[1].(string); !strings.HasPrefix(got, "2001-01-01T01:00:00") {
		t.Errorf("NSDate 的纪元算错了: %v", items[1])
	}
}

func TestKeepNSKeyedShowsUIDs(t *testing.T) {
	opt := DefaultOptions()
	opt.UnwrapNSKeyed = false
	res, err := Parse(mustBinary(t, buildArchive()), opt)
	if err != nil {
		t.Fatal(err)
	}
	if !res.NSKeyed {
		t.Error("应该还是认出它是归档")
	}
	if res.Unwrapped {
		t.Error("关掉拆包后 unwrapped 不该为真")
	}
	m := res.Value.(map[string]any)
	top := m["$top"].(map[string]any)
	// UID 渲染成 {"__uid": n},不能只给个裸数字 ——
	// 裸数字看不出它是"指向第 N 个对象的引用"
	uid, ok := top["root"].(map[string]any)
	if !ok || uid["__uid"] != uint64(1) {
		t.Errorf("UID 应该渲染成 __uid 对象,得到 %#v", top["root"])
	}
}

// 归档里对象互相引用是合法的,展开成树时必须能停下来
func TestUnwrapHandlesCircularReference(t *testing.T) {
	archive := map[string]any{
		"$archiver": "NSKeyedArchiver",
		"$version":  uint64(100000),
		"$top":      map[string]any{"root": hplist.UID(1)},
		"$objects": []any{
			"$null",
			map[string]any{"self": hplist.UID(1)}, // 指向自己
		},
	}
	done := make(chan *Result, 1)
	go func() {
		res, err := Parse(mustBinary(t, archive), DefaultOptions())
		if err == nil {
			done <- res
		} else {
			done <- nil
		}
	}()
	res := <-done
	if res == nil {
		t.Fatal("循环引用不该让解析失败")
	}
	m := res.Value.(map[string]any)
	inner, ok := m["self"].(map[string]any)
	if !ok || inner["__circular_uid"] == nil {
		t.Errorf("循环处应该留标记,得到 %#v", m["self"])
	}
	if len(res.Notes) == 0 {
		t.Error("循环引用应该在 notes 里说一声")
	}
}

// UID 指到 $objects 之外:损坏的归档里常见,不能当成正常下标去取
func TestUnwrapHandlesDanglingUID(t *testing.T) {
	archive := map[string]any{
		"$archiver": "NSKeyedArchiver",
		"$version":  uint64(100000),
		"$top":      map[string]any{"root": hplist.UID(1)},
		"$objects": []any{
			"$null",
			map[string]any{"missing": hplist.UID(99)},
		},
	}
	res, err := Parse(mustBinary(t, archive), DefaultOptions())
	if err != nil {
		t.Fatalf("越界的 UID 不该让整个解析失败: %v", err)
	}
	m := res.Value.(map[string]any)
	inner, ok := m["missing"].(map[string]any)
	if !ok || inner["__dangling_uid"] == nil {
		t.Errorf("越界处应该留标记,得到 %#v", m["missing"])
	}
}

// "$null" 是归档里的空值占位,不是一个内容为 "$null" 的字符串
func TestUnwrapTurnsNullPlaceholderIntoNil(t *testing.T) {
	archive := map[string]any{
		"$archiver": "NSKeyedArchiver",
		"$version":  uint64(100000),
		"$top":      map[string]any{"root": hplist.UID(1)},
		"$objects": []any{
			"$null",
			map[string]any{"empty": hplist.UID(0)},
		},
	}
	res, err := Parse(mustBinary(t, archive), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	m := res.Value.(map[string]any)
	if m["empty"] != nil {
		t.Errorf(`"$null" 应该变成 null,得到 %#v`, m["empty"])
	}
}

// 未知的类不能丢:里面的数据照样要能读到
func TestUnwrapKeepsUnknownClass(t *testing.T) {
	archive := map[string]any{
		"$archiver": "NSKeyedArchiver",
		"$version":  uint64(100000),
		"$top":      map[string]any{"root": hplist.UID(1)},
		"$objects": []any{
			"$null",
			map[string]any{"$class": hplist.UID(2), "token": "abc123"},
			map[string]any{"$classname": "MyPrivateClass"},
		},
	}
	res, err := Parse(mustBinary(t, archive), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	m := res.Value.(map[string]any)
	if m["__class"] != "MyPrivateClass" {
		t.Errorf("类名应该保留成 __class,得到 %#v", m["__class"])
	}
	if m["token"] != "abc123" {
		t.Errorf("陌生类里的字段不该丢: %#v", m)
	}
}

func TestDataTruncation(t *testing.T) {
	big := make([]byte, 4096)
	res, err := Parse(mustBinary(t, map[string]any{"blob": big}), Options{MaxData: 16})
	if err != nil {
		t.Fatal(err)
	}
	blob := res.Value.(map[string]any)["blob"].(map[string]any)
	if blob["__size"] != 4096 {
		t.Errorf("__size 要报真实长度,得到 %v", blob["__size"])
	}
	if blob["__truncated"] == nil {
		t.Error("截断了就必须留标记 —— 悄悄少给数据比给少了更糟")
	}
	raw, _ := base64.StdEncoding.DecodeString(blob["__data"].(string))
	if len(raw) != 16 {
		t.Errorf("应该只给 16 字节,得到 %d", len(raw))
	}
	if len(res.Notes) == 0 {
		t.Error("截断应该在 notes 里提一句")
	}
}

func TestArrayTruncation(t *testing.T) {
	items := make([]any, 50)
	for i := range items {
		items[i] = int64(i)
	}
	res, err := Parse(mustBinary(t, map[string]any{"list": items}), Options{MaxArray: 3})
	if err != nil {
		t.Fatal(err)
	}
	got := res.Value.(map[string]any)["list"].([]any)
	// 3 项 + 1 个标记
	if len(got) != 4 {
		t.Fatalf("应该是 3 项加一个标记,得到 %d 项", len(got))
	}
	mark, ok := got[3].(map[string]any)
	if !ok || !strings.Contains(mark["__truncated"].(string), "47") {
		t.Errorf("标记里应该说还剩 47 项,得到 %#v", got[3])
	}
}

func TestSelectPath(t *testing.T) {
	data := mustBinary(t, map[string]any{
		"a": map[string]any{"b": []any{"x", "y", "z"}},
	})
	opt := DefaultOptions()
	opt.SubPath = "a/b/1"
	res, err := Parse(data, opt)
	if err != nil {
		t.Fatalf("钻路径失败: %v", err)
	}
	if res.Value != "y" {
		t.Errorf("应该取到 y,得到 %#v", res.Value)
	}
	if res.SubPath != "a/b/1" {
		t.Errorf("结果里要带上钻的路径,得到 %q", res.SubPath)
	}
}

// 一层是字典时,"0" 是键名不是下标 —— 反过来会把真实存在的键跳过去
func TestSelectPathNumericDictKey(t *testing.T) {
	data := mustBinary(t, map[string]any{"0": "命中的是键"})
	opt := DefaultOptions()
	opt.SubPath = "0"
	res, err := Parse(data, opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Value != "命中的是键" {
		t.Errorf("字典层的数字段应该当键名,得到 %#v", res.Value)
	}
}

// 真实 plist 里带斜杠的键太常见了 —— Apple 自己的 types.plist
// 整张表的键就是 "application/pdf" 这种 MIME type。
// 按斜杠一刀切的话,这类键一个都取不到
func TestSelectPathKeyContainingSlash(t *testing.T) {
	data := mustBinary(t, map[string]any{
		"application/pdf": map[string]any{"ext": []any{"pdf"}},
	})
	opt := DefaultOptions()
	opt.SubPath = "application/pdf/ext/0"
	res, err := Parse(data, opt)
	if err != nil {
		t.Fatalf("带斜杠的键应该能取到: %v", err)
	}
	if res.Value != "pdf" {
		t.Errorf("应该取到 pdf,得到 %#v", res.Value)
	}
}

// 同时存在 "a" 和 "a/b" 两个键时,长的先试;
// 长的走不通要能退回来走短的,不然会把调用方引到死路上
func TestSelectPathBacktracksToShorterKey(t *testing.T) {
	data := mustBinary(t, map[string]any{
		"a/b": map[string]any{"x": "长键这条路"},
		"a":   map[string]any{"b": map[string]any{"y": "短键这条路"}},
	})
	opt := DefaultOptions()

	opt.SubPath = "a/b/x"
	res, err := Parse(data, opt)
	if err != nil || res.Value != "长键这条路" {
		t.Errorf("长键优先: %#v %v", res, err)
	}

	// "a/b" 下面没有 y,必须退回去走 a -> b -> y
	opt.SubPath = "a/b/y"
	res, err = Parse(data, opt)
	if err != nil {
		t.Fatalf("长键走不通时应该回退: %v", err)
	}
	if res.Value != "短键这条路" {
		t.Errorf("回退后应该取到短键那条,得到 %#v", res.Value)
	}
}

// 路径写错时要说清楚错在哪一段、这一层有什么可选的,
// 只回一句"没找到"会让调用方只能一层层试
func TestSelectPathErrorNamesAvailableKeys(t *testing.T) {
	data := mustBinary(t, map[string]any{"alpha": 1, "beta": 2})
	opt := DefaultOptions()
	opt.SubPath = "gamma"
	_, err := Parse(data, opt)
	if err == nil {
		t.Fatal("路径不存在应该报错")
	}
	if !strings.Contains(err.Error(), "alpha") || !strings.Contains(err.Error(), "beta") {
		t.Errorf("报错里应该列出这一层可选的键,得到: %v", err)
	}
}

// 从设备里提出来的文件损坏是常态,一个坏文件不能把整个 app 带走
func TestCorruptBinaryDoesNotPanic(t *testing.T) {
	good := mustBinary(t, map[string]any{"a": "b"})
	cases := map[string][]byte{
		"截断到一半":  good[:len(good)/2],
		"只有魔数":   []byte("bplist00"),
		"魔数后全是零": append([]byte("bplist00"), make([]byte, 40)...),
		"随机字节":   []byte("bplist00\xff\xff\xff\xff\xff\xff\xff\xff"),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			// 崩了就是测试失败 —— Parse 里那层 recover 就是为这个存在的
			if _, err := Parse(data, DefaultOptions()); err == nil {
				t.Log("这个样本居然解开了,也算合格:没崩就行")
			}
		})
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	if _, err := Parse(nil, DefaultOptions()); err == nil {
		t.Error("空输入该报错")
	}
}

// 结果要能直接 json.Marshal —— 这是 MCP 那头的硬要求
func TestResultIsJSONSerializable(t *testing.T) {
	res, err := Parse(mustBinary(t, buildArchive()), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("结果序列化失败: %v", err)
	}
	// notes 必须是 [] 不能是 null:前端拿到 null 再 .length 会当场崩
	if !strings.Contains(string(b), `"notes":[`) {
		t.Errorf("notes 应该序列化成数组,得到: %s", b)
	}
}

func TestDecodeInlineAutoDetect(t *testing.T) {
	raw := mustBinary(t, map[string]any{"k": "v"})
	b64 := base64.StdEncoding.EncodeToString(raw)

	cases := map[string]string{
		"base64":     b64,
		"hex":        toHex(raw),
		"blob 字面量":   "X'" + toHex(raw) + "'",
		"blob 小写带空格": "x'" + toHex(raw) + " '",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := decodeInline(in, "")
			if err != nil {
				t.Fatalf("自动识别失败: %v", err)
			}
			if string(got) != string(raw) {
				t.Errorf("还原出来的字节和原始不一致(%d vs %d 字节)", len(got), len(raw))
			}
		})
	}
}

func toHex(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&0x0f])
	}
	return string(out)
}
