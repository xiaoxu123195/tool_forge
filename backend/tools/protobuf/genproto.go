package protobuf

import (
	"fmt"
	"strconv"
	"strings"
)

// generateProto 从裸解析结果反推一份 .proto 骨架草稿。
// 消息按字段路径命名(Root、M8、M8_7…),同一路径的多次出现会合并字段;
// 单个实例内同号字段出现多次 → repeated;跨实例类型冲突 → 退化为 bytes 并加注释。
func generateProto(nodes []Node) string {
	if len(nodes) == 0 {
		return ""
	}
	reg := &protoReg{msgs: map[string]*protoMsg{}}
	reg.gather(nodes, nil)

	var b strings.Builder
	b.WriteString("syntax = \"proto3\";\n\n")
	for _, name := range reg.order {
		m := reg.msgs[name]
		fmt.Fprintf(&b, "message %s {\n", name)
		for _, num := range m.order {
			f := m.fields[num]
			label := ""
			if f.repeated {
				label = "repeated "
			}
			comment := ""
			if f.conflict {
				comment = "  // ⚠ 多种类型,已退化为 bytes"
			}
			fmt.Fprintf(&b, "  %s%s f%d = %d;%s\n", label, f.typ, num, num, comment)
		}
		b.WriteString("}\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

type protoField struct {
	typ      string
	repeated bool
	conflict bool
}

type protoMsg struct {
	fields map[int32]*protoField
	order  []int32
}

type protoReg struct {
	msgs  map[string]*protoMsg
	order []string
}

func (r *protoReg) get(name string) *protoMsg {
	m, ok := r.msgs[name]
	if !ok {
		m = &protoMsg{fields: map[int32]*protoField{}}
		r.msgs[name] = m
		r.order = append(r.order, name)
	}
	return m
}

func pathName(path []int32) string {
	if len(path) == 0 {
		return "Root"
	}
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = strconv.Itoa(int(p))
	}
	return "M" + strings.Join(parts, "_")
}

func (r *protoReg) gather(nodes []Node, path []int32) {
	m := r.get(pathName(path))

	counts := map[int32]int{}
	for _, n := range nodes {
		counts[n.Field]++
	}

	for _, n := range nodes {
		f, ok := m.fields[n.Field]
		if !ok {
			f = &protoField{}
			m.fields[n.Field] = f
			m.order = append(m.order, n.Field)
		}
		if counts[n.Field] > 1 {
			f.repeated = true
		}

		childPath := append(append([]int32{}, path...), n.Field)
		t := protoTypeOf(n, pathName(childPath))
		switch {
		case f.typ == "":
			f.typ = t
		case f.typ != t:
			f.conflict = true
			f.typ = "bytes"
		}

		if n.Type == "message" || n.Type == "group" {
			r.gather(n.Message, childPath)
		}
	}
}

func protoTypeOf(n Node, childName string) string {
	switch n.Type {
	case "varint":
		if n.Bool != nil {
			return "int64" // 可能是 bool,但保守用 int64
		}
		return "int64"
	case "i64":
		return "fixed64"
	case "i32":
		return "fixed32"
	case "string":
		return "string"
	case "message", "group":
		return childName
	default:
		return "bytes"
	}
}
