package mmkv

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hplist "howett.net/plist"
)

// buildMMKV 按 MMKV 的落盘格式造一个文件。
// 手写字节而不是找个真文件当样本:真文件不能进仓库(里面是别人的数据),
// 而且手写能精确控制每一种边界。
func buildMMKV(pairs [][2][]byte) []byte {
	var body []byte
	body = appendVarint(body, 0xffffff07) // 那个用途不明的 varint
	for _, kv := range pairs {
		body = appendVarint(body, uint64(len(kv[0])))
		body = append(body, kv[0]...)
		body = appendVarint(body, uint64(len(kv[1])))
		body = append(body, kv[1]...)
	}
	out := make([]byte, 4+len(body))
	binary.LittleEndian.PutUint32(out, uint32(len(body)))
	copy(out[4:], body)
	return out
}

func appendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

// mmkvString 一个 MMKV 字符串值:[varint 长度][utf8]
func mmkvString(s string) []byte {
	return append(appendVarint(nil, uint64(len(s))), s...)
}

func kv(k string, v []byte) [2][]byte { return [2][]byte{[]byte(k), v} }

func TestParseBasic(t *testing.T) {
	data := buildMMKV([][2][]byte{
		kv("name", mmkvString("张三")),
		kv("age", appendVarint(nil, 30)),
		kv("enabled", []byte{1}),
	})
	res, err := Parse(data)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(res.Entries) != 3 {
		t.Fatalf("应该有 3 个键,得到 %d", len(res.Entries))
	}
	// 顺序要和文件里一致 —— 用 map 收集的话每次跑出来的顺序都不一样,没法比对
	want := []string{"name", "age", "enabled"}
	for i, e := range res.Entries {
		if e.Key != want[i] {
			t.Errorf("第 %d 个键应该是 %q,得到 %q", i, want[i], e.Key)
		}
	}
	if got := res.Entries[0].Values[0].Best; got != TypeString {
		t.Errorf("中文字符串应该被判成 string,得到 %q", got)
	}
	if got := res.Entries[0].Values[0].Decoded; !hasType(got, TypeString) {
		t.Error("候选里应该有 string")
	}
	if got := res.Entries[2].Values[0].Best; got != TypeBool {
		t.Errorf("单字节 0x01 应该被判成 bool,得到 %q", got)
	}
}

// 同一个键写过多次时,历史值都还在文件里 —— 这对取证是有意义的信息,
// 而且最新的必须排在最前面
func TestParseKeepsHistoryNewestFirst(t *testing.T) {
	data := buildMMKV([][2][]byte{
		kv("token", mmkvString("旧值")),
		kv("token", mmkvString("新值")),
	})
	res, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("同一个键应该合成一条,得到 %d 条", len(res.Entries))
	}
	vals := res.Entries[0].Values
	if len(vals) != 2 {
		t.Fatalf("应该保留 2 个历史值,得到 %d", len(vals))
	}
	if !strings.Contains(displayOf(vals[0], TypeString), "新值") {
		t.Errorf("最新的值应该排在最前面,当前第一个是 %v", vals[0].Decoded)
	}
}

// 删除标记(值长度为 0):键还在日志里,但代表已被移除
func TestParseCountsRemoved(t *testing.T) {
	data := buildMMKV([][2][]byte{
		kv("keep", mmkvString("在")),
		kv("gone", []byte{}),
	})
	res, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if res.RemovedCount != 1 {
		t.Errorf("应该数出 1 个删除标记,得到 %d", res.RemovedCount)
	}
	if len(res.Entries) != 1 {
		t.Errorf("被删的键不该出现在结果里,得到 %d 条", len(res.Entries))
	}
}

// 截断的文件要尽量读:取证场景里半个文件也是线索,
// 整个报错等于把已经读出来的也丢了
func TestParseTruncatedFileKeepsWhatItGot(t *testing.T) {
	full := buildMMKV([][2][]byte{
		kv("first", mmkvString("完整")),
		kv("second", mmkvString("会被砍掉")),
	})
	res, err := Parse(full[:len(full)-6])
	if err != nil {
		t.Fatalf("截断的文件不该整个报错: %v", err)
	}
	if len(res.Entries) == 0 {
		t.Error("应该至少读出前面完整的那部分")
	}
	if res.Entries[0].Key != "first" {
		t.Errorf("第一个键应该还在,得到 %q", res.Entries[0].Key)
	}
}

func TestParseRejectsTooSmall(t *testing.T) {
	if _, err := Parse([]byte{1, 2}); err == nil {
		t.Error("两个字节不可能是 MMKV,该报错")
	}
}

// 值不带类型标记,所以要把所有解得通的读法都列出来
func TestDescribeValueListsCandidates(t *testing.T) {
	// 单字节 0x01:既是 bool true,也是 varint 1
	v := describeValue([]byte{1}, mcpHexLimit)
	if !hasType(v.Decoded, TypeBool) || !hasType(v.Decoded, TypeInt32) {
		t.Errorf("0x01 应该同时列出 bool 和整数: %+v", v.Decoded)
	}
	if v.Best != TypeBool {
		t.Errorf("单字节 0/1 优先判成 bool,得到 %q", v.Best)
	}

	// 一段可读文本:整数读法虽然也"通",但不该被选成 best
	s := describeValue(mmkvString("hello world"), mcpHexLimit)
	if s.Best != TypeString {
		t.Errorf("可读文本应该判成 string,得到 %q", s.Best)
	}
}

// 空字符串谁都能"解出来",不能因此把随机字节判成 string
func TestBestIgnoresEmptyString(t *testing.T) {
	// 0x00 开头 = 长度为 0 的字符串,后面还有字节
	v := describeValue([]byte{0x00, 0xff, 0xfe}, mcpHexLimit)
	if v.Best == TypeString {
		t.Errorf("空字符串不该被当成最佳判断: %+v", v)
	}
}

func TestStringSet(t *testing.T) {
	inner := append(mmkvString("a"), mmkvString("bb")...)
	val := append(appendVarint(nil, uint64(len(inner))), inner...)
	v := describeValue(val, mcpHexLimit)
	if v.Best != TypeStringSet {
		t.Errorf("应该判成 stringSet,得到 %q;候选 %+v", v.Best, v.Decoded)
	}
	if d := displayOf(v, TypeStringSet); !strings.Contains(d, `"a"`) || !strings.Contains(d, `"bb"`) {
		t.Errorf("集合内容不对: %s", d)
	}
}

// archived iOS 上 MMKV 存对象时落盘的样子:NSKeyedArchiver 归档的原样字节,前面不带长度。
// 归档的是一个 NSDictionary,形状照真机上见过的:键名、修改时间、真正的值。
// 修改时间故意给一个超过 2^53 的数:过一道浮点就会差一位
func archived(t *testing.T) []byte {
	t.Helper()
	b, err := hplist.Marshal(map[string]any{
		"$archiver": "NSKeyedArchiver",
		"$version":  uint64(100000),
		"$top":      map[string]any{"root": hplist.UID(1)},
		"$objects": []any{
			"$null", // 0
			map[string]any{ // 1 root NSDictionary
				"$class":     hplist.UID(8),
				"NS.keys":    []any{hplist.UID(2), hplist.UID(3), hplist.UID(4)},
				"NS.objects": []any{hplist.UID(5), hplist.UID(6), hplist.UID(7)},
			},
			"key", "modification_time", "value", // 2 3 4
			"searchHistoryKey", uint64(9007199254740993), "[\"茅台\"]", // 5 6 7
			map[string]any{"$classname": "NSDictionary", "$classes": []any{"NSDictionary", "NSObject"}}, // 8
		},
	}, hplist.BinaryFormat)
	if err != nil {
		t.Fatalf("造归档失败: %v", err)
	}
	return b
}

// iOS 上存的对象要拆成能读的结构。以前开头的 'b'(0x62)被当成长度 98,
// 读出来的「bytes」是截掉一个字节的半截归档,看着像对的
func TestArchivedObjectIsPlist(t *testing.T) {
	v := describeValue(archived(t), desktopHexLimit)
	if v.Best != TypePlist {
		t.Fatalf("归档对象应该判成 plist,得到 %q;候选 %+v", v.Best, v.Decoded)
	}
	d := displayOf(v, TypePlist)
	for _, want := range []string{`"key":"searchHistoryKey"`, `"modification_time":9007199254740993`, `茅台`} {
		if !strings.Contains(d, want) {
			t.Errorf("拆出来的内容里没有 %s: %s", want, d)
		}
	}
	if hasType(v.Decoded, TypeBytes) || hasType(v.Decoded, TypeString) {
		t.Errorf("开头的 'b' 不该被当成长度读出 bytes / string: %+v", v.Decoded)
	}
}

// 开了键过期的 MMKV,值后面多 4 字节过期时间;bplist 的索引表在最末尾,不去掉就解不开
func TestArchivedObjectWithExpireTime(t *testing.T) {
	v := describeValue(append(archived(t), 0, 0, 0, 0), desktopHexLimit)
	if v.Best != TypePlist {
		t.Errorf("带过期时间的归档也该认成 plist,得到 %q", v.Best)
	}
}

// 存进去的 NSData 本身是个 plist 时带长度:plist 和 bytes 两种读法都该有
func TestDataHoldingPlist(t *testing.T) {
	inner := archived(t)
	v := describeValue(append(appendVarint(nil, uint64(len(inner))), inner...), desktopHexLimit)
	if v.Best != TypePlist || !hasType(v.Decoded, TypeBytes) {
		t.Errorf("带长度的 plist 应该判成 plist,也能按 bytes 看: best %q,候选 %+v", v.Best, v.Decoded)
	}
}

// 「长度 + 内容」的长度要正好盖到末尾,或者只剩 4 字节过期时间;
// 剩别的数目说明开头那个数根本不是长度
func TestPayloadMustReachTheEnd(t *testing.T) {
	if s := describeValue(append(mmkvString("abc"), 1, 2, 3, 4), desktopHexLimit); displayOf(s, TypeString) != "abc" {
		t.Errorf("带 4 字节过期时间的字符串应该还读得出来: %+v", s.Decoded)
	}
	odd := describeValue(append(mmkvString("abc"), 1, 2, 3), desktopHexLimit)
	if hasType(odd.Decoded, TypeString) || hasType(odd.Decoded, TypeBytes) {
		t.Errorf("后面多出 3 个字节,不该还当成字符串 / bytes: %+v", odd.Decoded)
	}
}

// 桌面页是点着一种种类型看的:读不通的类型也要有个硬读的结果,并标明只用了几个字节。
// MCP 那头只给读得通的,硬读的结果对 agent 是噪音
func TestLooseReadingsOnlyOnDesktop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample")
	data := buildMMKV([][2][]byte{
		kv("obj", archived(t)),
		// 开了键过期的整数:varint 后面跟 4 字节过期时间,按整数读不通,硬读才看得到 30
		kv("age", append(appendVarint(nil, 30), 0x10, 0x20, 0x30, 0x40)),
	})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := ParseFile(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	obj := res.Entries[0].Values[0]
	// 开头的 'b' 硬读成整数就是 98,只用了 1 字节;当成长度的话 bytes 用了 99 字节
	if d := looseOf(obj, TypeInt32); d == nil || d.Display != "98" || d.Used != 1 {
		t.Errorf("归档对象硬读成 int32 应该是 98、用了 1 字节: %+v", d)
	}
	if d := looseOf(obj, TypeBytes); d == nil || d.Used != 99 {
		t.Errorf("归档对象硬读成 bytes 应该用了 99 字节: %+v", d)
	}
	if looseOf(obj, TypePlist) != nil || looseOf(obj, TypeStringSet) != nil {
		t.Errorf("读通了的、硬读也没意义的类型不该出现在硬读结果里: %+v", obj.Loose)
	}
	if d := looseOf(res.Entries[1].Values[0], TypeInt32); d == nil || d.Display != "30" || d.Used != 1 {
		t.Errorf("带过期时间的整数硬读应该是 30: %+v", d)
	}

	mcp, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if mcp.Entries[0].Values[0].Loose != nil {
		t.Errorf("MCP 那头不该带硬读的结果: %+v", mcp.Entries[0].Values[0].Loose)
	}
}

func looseOf(v Value, t string) *Decoded {
	for i := range v.Loose {
		if v.Loose[i].Type == t {
			return &v.Loose[i]
		}
	}
	return nil
}

// 超长的值只给前面一段十六进制 —— 一个值可能是几百 KB 的图片,
// 全展开会把 agent 的上下文占满
func TestHexPreviewTruncates(t *testing.T) {
	big := make([]byte, mcpHexLimit*2)
	v := describeValue(big, mcpHexLimit)
	if !strings.Contains(v.Hex, "还有") {
		t.Error("超长的值应该截断并说明还剩多少")
	}
	if v.Size != len(big) {
		t.Errorf("size 要报真实长度,得到 %d", v.Size)
	}
}

func hasType(ds []Decoded, t string) bool {
	for _, d := range ds {
		if d.Type == t {
			return true
		}
	}
	return false
}

func displayOf(v Value, t string) string {
	for _, d := range v.Decoded {
		if d.Type == t {
			return d.Display
		}
	}
	return ""
}

// 一种类型都解不通的值(比如单个 0xff)也必须给出空的候选清单,不能是 nil。
// Go 的 nil 切片 json.Marshal 出来是 null,前端拿到 null 再 .map 就是整页白屏 ——
// 这个项目已经因为同一类问题炸过一次(跨会话搜索的 hits)
func TestDecodedNeverNil(t *testing.T) {
	v := describeValue([]byte{0xff}, mcpHexLimit)
	if v.Decoded == nil {
		t.Fatal("Decoded 是 nil,会被序列化成 null")
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"decoded":null`) {
		t.Errorf("序列化出了 null: %s", b)
	}
}

// 桌面页那条路返回的结构里,切片字段同样不能是 nil
func TestFileResultSlicesNeverNil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample")
	data := buildMMKV([][2][]byte{
		kv("a", mmkvString("x")),
		kv("weird", []byte{0xff}),
	})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := ParseFile(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Entries == nil {
		t.Fatal("Entries 是 nil")
	}
	for _, e := range res.Entries {
		if e.Values == nil {
			t.Fatalf("%s 的 Values 是 nil", e.Key)
		}
		for i, v := range e.Values {
			if v.Decoded == nil {
				t.Errorf("%s[%d] 的 Decoded 是 nil", e.Key, i)
			}
		}
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), ":null") {
		t.Errorf("结果里有 null 数组: %s", b)
	}
}
