package mmkv

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MMKV 的值不带类型标记 —— 存 int 和存 string 落到磁盘上都只是一串字节,
// 类型信息在写入方的代码里。桌面工具页的做法是让人点着循环各种类型看哪个像。
//
// 但 agent 点不了。所以这里换个做法:每个值把所有解得通的类型全列出来,
// 再给一个"最可能是哪个"的判断。agent 拿到的是候选清单而不是一堆十六进制。

// Decoded 一个值按某种类型解出来的结果
type Decoded struct {
	Type    string `json:"type"`
	Display string `json:"display"`
}

// Value 一个值的全貌:原始十六进制 + 所有解得通的类型
type Value struct {
	Hex  string `json:"hex"`
	Size int    `json:"size"`
	// Best 最可能的类型。判断依据见 pickBest —— 它是启发式的,不是真相
	Best    string    `json:"best"`
	Decoded []Decoded `json:"decoded"`
}

// 类型名。和前端那份保持一致,两边说的是同一件事
const (
	TypeString    = "string"
	TypeInt32     = "int32"
	TypeInt64     = "int64"
	TypeUint32    = "uint32"
	TypeUint64    = "uint64"
	TypeFloat32   = "float32"
	TypeFloat64   = "float64"
	TypeBool      = "bool"
	TypeBytes     = "bytes"
	TypeStringSet = "stringSet"
)

// describeValue 把一个原始值摊开成"所有可能的读法"
func describeValue(v []byte) Value {
	out := Value{Hex: hexPreview(v), Size: len(v)}
	add := func(t, d string, ok bool) {
		if ok {
			out.Decoded = append(out.Decoded, Decoded{Type: t, Display: d})
		}
	}

	s, ok := asString(v)
	add(TypeString, s, ok)
	if set, ok := asStringSet(v); ok {
		b, _ := json.Marshal(set)
		add(TypeStringSet, string(b), true)
	}
	if b, ok := asBool(v); ok {
		add(TypeBool, strconv.FormatBool(b), true)
	}
	if n, ok := asInt(v, 32, true); ok {
		add(TypeInt32, n, true)
	}
	if n, ok := asInt(v, 64, true); ok {
		add(TypeInt64, n, true)
	}
	if n, ok := asInt(v, 32, false); ok {
		add(TypeUint32, n, true)
	}
	if n, ok := asInt(v, 64, false); ok {
		add(TypeUint64, n, true)
	}
	if len(v) == 4 {
		f := math.Float32frombits(binary.LittleEndian.Uint32(v))
		add(TypeFloat32, strconv.FormatFloat(float64(f), 'g', -1, 32), true)
	}
	if len(v) == 8 {
		f := math.Float64frombits(binary.LittleEndian.Uint64(v))
		add(TypeFloat64, strconv.FormatFloat(f, 'g', -1, 64), true)
	}
	if h, ok := asBytes(v); ok {
		add(TypeBytes, h, true)
	}

	out.Best = pickBest(v, out.Decoded)
	return out
}

// pickBest 猜这个值最可能是什么类型。
//
// 顺序是按"猜错的代价"排的,不是按常见程度:
//  1. 可读字符串排第一 —— 它最可能是真的(随机字节几乎不可能凑出一段可打印文本),
//     而且猜错了人一眼就看得出来
//  2. 字符串集合次之,它的结构约束比字符串更强
//  3. 单字节的 0/1 认成 bool
//  4. 整数垫底 —— 任何一串字节都能被当成 varint 读出个数来,信息量最低
//
// 返回的是猜测,所以完整候选清单照样给出去:agent 拿不准时可以自己换一个看。
func pickBest(raw []byte, cands []Decoded) string {
	has := func(t string) string {
		for _, c := range cands {
			if c.Type == t {
				return c.Display
			}
		}
		return ""
	}
	if s := has(TypeString); s != "" && isMostlyPrintable(s) {
		return TypeString
	}
	if has(TypeStringSet) != "" {
		return TypeStringSet
	}
	if len(raw) == 1 && has(TypeBool) != "" {
		return TypeBool
	}
	for _, t := range []string{TypeInt32, TypeInt64, TypeUint32, TypeUint64} {
		if has(t) != "" {
			return t
		}
	}
	return TypeBytes
}

// isMostlyPrintable 判断一段文本像不像"人写的东西"。
//
// 不要求全部可打印:真实数据里混着换行、制表、偶尔的控制字符很正常。
// 但空串直接否掉 —— 任何字节流都能"解出"一个空字符串,那不是信息。
func isMostlyPrintable(s string) bool {
	if s == "" {
		return false
	}
	printable := 0
	total := 0
	for _, r := range s {
		total++
		if unicode.IsPrint(r) || r == '\n' || r == '\t' || r == '\r' {
			printable++
		}
	}
	return total > 0 && printable*10 >= total*9 // 九成以上可打印
}

// asString MMKV 的字符串是 [varint 长度][utf8 内容]
func asString(v []byte) (string, bool) {
	n, read, err := readVarintU32(v, 0)
	if err != nil || read+int(n) > len(v) {
		return "", false
	}
	b := v[read : read+int(n)]
	if !utf8.Valid(b) {
		return "", false
	}
	return string(b), true
}

// asBytes 和字符串同构,只是内容不要求是合法 UTF-8
func asBytes(v []byte) (string, bool) {
	n, read, err := readVarintU32(v, 0)
	if err != nil || read+int(n) > len(v) {
		return "", false
	}
	return hexPreview(v[read : read+int(n)]), true
}

func asBool(v []byte) (bool, bool) {
	if len(v) != 1 {
		return false, false
	}
	switch v[0] {
	case 0:
		return false, true
	case 1:
		return true, true
	}
	return false, false
}

// asInt 按 varint 读一个整数。
//
// 要求 varint 恰好吃掉整个值:剩字节说明这段不是一个纯整数,
// 不加这个约束的话任何字节串都会被认成整数,候选清单就没意义了。
func asInt(v []byte, bits int, signed bool) (string, bool) {
	if len(v) == 0 {
		return "", false
	}
	if bits == 32 {
		u, n, err := readVarintU32(v, 0)
		if err != nil || n != len(v) {
			return "", false
		}
		if signed {
			return strconv.FormatInt(int64(int32(u)), 10), true
		}
		return strconv.FormatUint(uint64(u), 10), true
	}
	u, n, err := readVarintU64(v, 0)
	if err != nil || n != len(v) {
		return "", false
	}
	if signed {
		return strconv.FormatInt(int64(u), 10), true
	}
	return strconv.FormatUint(u, 10), true
}

// asStringSet MMKV 的 Set<String>:[varint 总长][ [varint 元素长][utf8] ... ]
func asStringSet(v []byte) ([]string, bool) {
	total, read, err := readVarintU32(v, 0)
	if err != nil {
		return nil, false
	}
	pos := read
	end := pos + int(total)
	if end > len(v) {
		return nil, false
	}
	out := []string{}
	for pos < end {
		n, b, err := readVarintU32(v, pos)
		if err != nil {
			return nil, false
		}
		pos += b
		if pos+int(n) > end {
			return nil, false
		}
		item := v[pos : pos+int(n)]
		if !utf8.Valid(item) {
			return nil, false
		}
		out = append(out, string(item))
		pos += int(n)
	}
	// 空集合和"任何以 0 开头的字节"长得一样,不算命中 —— 否则满屏都是空集合
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// hexPreviewLimit 单个值最多展示多少字节的十六进制。
// 一个值可能是几百 KB 的图片,全展开会把 agent 的上下文占满
const hexPreviewLimit = 512

func hexPreview(b []byte) string {
	if len(b) <= hexPreviewLimit {
		return hex.EncodeToString(b)
	}
	var sb strings.Builder
	sb.WriteString(hex.EncodeToString(b[:hexPreviewLimit]))
	fmt.Fprintf(&sb, "… (还有 %d 字节)", len(b)-hexPreviewLimit)
	return sb.String()
}
