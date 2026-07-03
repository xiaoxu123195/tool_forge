package protobuf

import (
	"math"
	"strconv"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

func formatUint(u uint64) string { return strconv.FormatUint(u, 10) }
func formatInt(i int64) string   { return strconv.FormatInt(i, 10) }

func parseUint(s string) (uint64, error) { return strconv.ParseUint(s, 10, 64) }

// fillVarint 填充 wire type 0 的多重解读。
func fillVarint(n *Node, v uint64) {
	n.Uint = formatUint(v)
	if s := int64(v); s < 0 {
		n.Sint = formatInt(s) // 只在与无符号不同(高位置位)时给出
	}
	n.Zigzag = formatInt(protowire.DecodeZigZag(v))
	if v == 0 || v == 1 {
		b := v == 1
		n.Bool = &b
	}
	annotateTime(n, v)
}

// fillFixed64 填充 wire type 1(8 字节定长)。
func fillFixed64(n *Node, v uint64) {
	n.Uint = formatUint(v)
	n.Sint = formatInt(int64(v))
	n.Double = strconv.FormatFloat(math.Float64frombits(v), 'g', -1, 64)
	n.Hex = "0x" + zpad(strconv.FormatUint(v, 16), 16)
	annotateTime(n, v)
}

// fillFixed32 填充 wire type 5(4 字节定长)。
func fillFixed32(n *Node, v uint32) {
	n.Uint = formatUint(uint64(v))
	n.Sint = formatInt(int64(int32(v)))
	n.Float = strconv.FormatFloat(float64(math.Float32frombits(v)), 'g', -1, 32)
	n.Hex = "0x" + zpad(strconv.FormatUint(uint64(v), 16), 8)
}

func zpad(s string, width int) string {
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// annotateTime 若数值落在合理纪元区间(秒/毫秒/微秒/纳秒),标注为 UTC 时间。
// 各区间互不重叠:[1e9,1e10)=秒(2001–2286)、[1e12,1e13)=毫秒、[1e15,1e16)=微秒、[1e18,1e19)=纳秒。
func annotateTime(n *Node, u uint64) {
	const layout = "2006-01-02 15:04:05"
	var t time.Time
	switch {
	case u >= 1e9 && u < 1e10:
		t, n.TimeUnit = time.Unix(int64(u), 0), "s"
	case u >= 1e12 && u < 1e13:
		t, n.TimeUnit = time.UnixMilli(int64(u)), "ms"
	case u >= 1e15 && u < 1e16:
		t, n.TimeUnit = time.UnixMicro(int64(u)), "us"
	case u >= 1e18 && u < 1e19:
		t, n.TimeUnit = time.Unix(0, int64(u)), "ns"
	default:
		return
	}
	n.Time = t.UTC().Format(layout) + " UTC"
}
