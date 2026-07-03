package protobuf

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// renderProtoText 把字段树渲染成与 `protoc --decode_raw` 一致的文本,
// 便于与系统 protoc 对拍或粘贴到别处。字符串按字节做 C 转义(非 ASCII 走八进制)。
func renderProtoText(nodes []Node) string {
	var b strings.Builder
	writeProtoText(&b, nodes, 0)
	return b.String()
}

func writeProtoText(b *strings.Builder, nodes []Node, indent int) {
	pad := strings.Repeat("  ", indent)
	for _, n := range nodes {
		switch n.Type {
		case "message", "group":
			fmt.Fprintf(b, "%s%d {\n", pad, n.Field)
			writeProtoText(b, n.Message, indent+1)
			fmt.Fprintf(b, "%s}\n", pad)
		case "varint":
			fmt.Fprintf(b, "%s%d: %s\n", pad, n.Field, n.Uint)
		case "i64", "i32":
			fmt.Fprintf(b, "%s%d: %s\n", pad, n.Field, n.Hex)
		default: // string / bytes / packed —— protoc 一律当字符串输出
			raw, _ := hex.DecodeString(n.Hex)
			fmt.Fprintf(b, "%s%d: \"%s\"\n", pad, n.Field, cEscape(raw))
		}
	}
}

// cEscape 复刻 protoc 的 CEscape:可打印 ASCII 原样,其余(含 UTF-8 字节)转八进制。
func cEscape(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		switch c {
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		case '\\':
			sb.WriteString(`\\`)
		case '\'':
			sb.WriteString(`\'`)
		case '"':
			sb.WriteString(`\"`)
		default:
			if c >= 0x20 && c < 0x7f {
				sb.WriteByte(c)
			} else {
				fmt.Fprintf(&sb, `\%03o`, c)
			}
		}
	}
	return sb.String()
}
