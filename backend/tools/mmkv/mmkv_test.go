package mmkv

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
