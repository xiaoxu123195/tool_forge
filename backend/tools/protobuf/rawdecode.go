package protobuf

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protowire"
)

const maxDepth = 100 // 递归深度上限,防御构造数据

// decodeInputBytes 把用户输入(hex/base64/blob 字面量)还原成原始字节。
func decodeInputBytes(data, encoding string) ([]byte, error) {
	s := strings.TrimSpace(data)
	if s == "" {
		return nil, errors.New("空输入")
	}
	switch strings.ToLower(encoding) {
	case "base64":
		s = strings.Join(strings.Fields(s), "") // 去掉换行/空格
		// 兼容 URL-safe 与去 padding
		if strings.ContainsAny(s, "-_") {
			if b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "=")); err == nil {
				return b, nil
			}
		}
		if b, err := base64.StdEncoding.DecodeString(s); err == nil {
			return b, nil
		}
		return base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
	case "blob":
		return parseBlobLiteral(s)
	default: // hex
		return parseHex(s)
	}
}

var reHexJunk = regexp.MustCompile(`(?i)^0x|[\s,]|\\x`)

// parseHex 容错解析十六进制:允许空格/逗号/0x/\x 前缀。
func parseHex(s string) ([]byte, error) {
	clean := reHexJunk.ReplaceAllString(s, "")
	clean = strings.ReplaceAll(clean, "0X", "")
	if len(clean)%2 != 0 {
		return nil, fmt.Errorf("hex 长度为奇数(%d)", len(clean))
	}
	return hex.DecodeString(clean)
}

var reBlob = regexp.MustCompile(`(?is)x'([0-9a-f\s]*)'`)

// parseBlobLiteral 解析 SQLite BLOB 字面量,如 X'0a22...' 或 x'0a 22'。
func parseBlobLiteral(s string) ([]byte, error) {
	m := reBlob.FindStringSubmatch(s)
	if m == nil {
		// 退回按纯 hex 处理
		return parseHex(s)
	}
	return parseHex(m[1])
}

// DecodeRaw 无 schema 递归裸解析。
func DecodeRaw(in DecodeInput) (*RawResult, error) {
	b, err := decodeInputBytes(in.Data, in.Encoding)
	if err != nil {
		return nil, err
	}
	nodes, ok := parseMessage(b, 0, 0)
	if !ok {
		return nil, errors.New("不是合法的 Protobuf 字节流(wire 解析失败)")
	}
	res := &RawResult{
		Nodes:     nodes,
		Size:      len(b),
		ProtoText: renderProtoText(nodes),
		ProtoDef:  generateProto(nodes),
	}
	return res, nil
}

// parseMessage 把 b 当作一条消息解析成字段节点。baseOffset 为 b 在整体中的起始偏移。
// 返回 (节点, 是否完整且合法解析)。任何一步失败或有残留字节即视为非消息。
func parseMessage(b []byte, baseOffset, depth int) ([]Node, bool) {
	if depth > maxDepth {
		return nil, false
	}
	var nodes []Node
	pos := 0
	for pos < len(b) {
		num, typ, tagLen := protowire.ConsumeTag(b[pos:])
		if tagLen < 0 || num <= 0 {
			return nil, false
		}
		fieldStart := pos
		valStart := pos + tagLen
		var n Node
		n.Field = int32(num)
		n.Wire = int(typ)
		n.Offset = baseOffset + fieldStart

		switch typ {
		case protowire.VarintType:
			v, m := protowire.ConsumeVarint(b[valStart:])
			if m < 0 {
				return nil, false
			}
			n.WireName = "varint"
			n.Type = "varint"
			fillVarint(&n, v)
			pos = valStart + m

		case protowire.Fixed64Type:
			v, m := protowire.ConsumeFixed64(b[valStart:])
			if m < 0 {
				return nil, false
			}
			n.WireName = "i64"
			n.Type = "i64"
			fillFixed64(&n, v)
			pos = valStart + m

		case protowire.Fixed32Type:
			v, m := protowire.ConsumeFixed32(b[valStart:])
			if m < 0 {
				return nil, false
			}
			n.WireName = "i32"
			n.Type = "i32"
			fillFixed32(&n, v)
			pos = valStart + m

		case protowire.BytesType:
			v, m := protowire.ConsumeBytes(b[valStart:])
			if m < 0 {
				return nil, false
			}
			n.WireName = "len"
			fillBytes(&n, v, baseOffset+valStart+lenPrefixSize(m, len(v)), depth)
			pos = valStart + m

		case protowire.StartGroupType:
			v, m := protowire.ConsumeGroup(num, b[valStart:])
			if m < 0 {
				return nil, false
			}
			n.WireName = "group"
			n.Type = "group"
			if kids, ok := parseMessage(v, baseOffset+valStart, depth+1); ok {
				n.Message = kids
				n.MsgOK = true
			} else {
				n.Hex = hex.EncodeToString(v)
			}
			pos = valStart + m

		default:
			return nil, false
		}

		n.Size = pos - fieldStart
		nodes = append(nodes, n)
	}
	return nodes, true
}

// lenPrefixSize 由“value+prefix 总消费 m”和“value 长度 vlen”反推长度前缀字节数。
func lenPrefixSize(m, vlen int) int {
	p := m - vlen
	if p < 0 {
		return 0
	}
	return p
}

var reAllDigits = regexp.MustCompile(`^\d{9,19}$`)

// fillBytes 处理 length-delimited:同时给出 message/string/bytes/packed 候选并选最佳。
func fillBytes(n *Node, v []byte, valOffset, depth int) {
	n.Hex = hex.EncodeToString(v)

	if len(v) == 0 {
		n.Type = "string"
		n.Str = ""
		n.StrOK = true
		return
	}

	// 候选 1:嵌套消息
	if kids, ok := parseMessage(v, valOffset, depth+1); ok {
		n.Message = kids
		n.MsgOK = true
	}
	// 候选 2:UTF-8 字符串
	if utf8.Valid(v) {
		n.Str = string(v)
		n.StrOK = true
	}
	// 候选 3:packed varint 序列
	if items, ok := parsePackedVarint(v); ok {
		n.Packed = items
		n.PackedOK = true
	}

	// 选最佳展示。比 protoc --decode_raw 更聪明:当整段是可打印文本时优先当字符串
	// (真正的嵌套消息几乎总含不可打印的 tag/长度字节),否则消息优先。所有候选都已填好,前端可即时切换。
	switch {
	case n.StrOK && looksLikeText(v):
		n.Type = "string"
	case n.MsgOK:
		n.Type = "message"
	case n.StrOK:
		n.Type = "string"
	default:
		n.Type = "bytes"
	}

	if n.Type == "string" && reAllDigits.MatchString(n.Str) {
		if u, err := parseUint(n.Str); err == nil {
			annotateTime(n, u)
		}
	}
}

// looksLikeText 判断字节是否整体像可读文本(合法 UTF-8 且除常见空白外全部可打印)。
func looksLikeText(v []byte) bool {
	if !utf8.Valid(v) {
		return false
	}
	for _, r := range string(v) {
		if r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// parsePackedVarint 尝试把字节完整解析为 varint 序列(≥2 项才算有意义)。
func parsePackedVarint(v []byte) ([]Item, bool) {
	var items []Item
	pos := 0
	for pos < len(v) {
		u, m := protowire.ConsumeVarint(v[pos:])
		if m < 0 {
			return nil, false
		}
		it := Item{Uint: formatUint(u)}
		if si := int64(u); si < 0 {
			it.Sint = formatInt(si)
		}
		it.Zigzag = formatInt(protowire.DecodeZigZag(u))
		items = append(items, it)
		pos += m
	}
	if len(items) < 2 {
		return nil, false
	}
	return items, true
}
