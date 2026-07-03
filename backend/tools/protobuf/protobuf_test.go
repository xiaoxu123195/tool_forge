package protobuf

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"

	"context"
)

func protowireString(b []byte, field int32, s string) []byte {
	b = protowire.AppendTag(b, protowire.Number(field), protowire.BytesType)
	return protowire.AppendString(b, s)
}

func protowireBytes(b []byte, field int32, v []byte) []byte {
	b = protowire.AppendTag(b, protowire.Number(field), protowire.BytesType)
	return protowire.AppendBytes(b, v)
}

func protowireVarint(b []byte, field int32, v uint64) []byte {
	b = protowire.AppendTag(b, protowire.Number(field), protowire.VarintType)
	return protowire.AppendVarint(b, v)
}

// sampleBytes 构造一条含字符串/varint时间戳/嵌套消息/数字串时间戳的消息。
func sampleBytes() []byte {
	inner := protowireString(nil, 1, "x")

	var b []byte
	b = protowireString(b, 1, "hi")
	b = protowireVarint(b, 3, 1700000000) // 秒级时间戳
	b = protowireBytes(b, 8, inner)       // 嵌套消息
	b = protowireString(b, 16, "1783044615422")
	return b
}

func TestDecodeRaw_Tree(t *testing.T) {
	b := sampleBytes()
	res, err := DecodeRaw(DecodeInput{Data: hex.EncodeToString(b), Encoding: "hex"})
	if err != nil {
		t.Fatalf("DecodeRaw: %v", err)
	}
	if len(res.Nodes) != 4 {
		t.Fatalf("字段数应为 4,得到 %d", len(res.Nodes))
	}
	byField := map[int32]Node{}
	for _, n := range res.Nodes {
		byField[n.Field] = n
	}
	if n := byField[1]; n.Type != "string" || n.Str != "hi" {
		t.Errorf("field1 应为 string=hi,得到 %+v", n)
	}
	if n := byField[3]; n.Type != "varint" || n.Uint != "1700000000" || n.TimeUnit != "s" || n.Time == "" {
		t.Errorf("field3 应为 varint 秒级时间戳,得到 %+v", n)
	}
	if n := byField[8]; !n.MsgOK || n.Type != "message" || len(n.Message) != 1 || n.Message[0].Str != "x" {
		t.Errorf("field8 应为嵌套消息{1:\"x\"},得到 %+v", n)
	}
	if n := byField[16]; n.Type != "string" || n.TimeUnit != "ms" || n.Time == "" {
		t.Errorf("field16 数字串应识别为毫秒时间戳,得到 %+v", n)
	}
	if res.Nodes[0].Offset != 0 {
		t.Errorf("首字段偏移应为 0,得到 %d", res.Nodes[0].Offset)
	}
}

func TestDecodeRaw_ProtoText(t *testing.T) {
	b := sampleBytes()
	res, _ := DecodeRaw(DecodeInput{Data: hex.EncodeToString(b), Encoding: "hex"})
	want := []string{`1: "hi"`, `3: 1700000000`, "8 {", "}", `16: "1783044615422"`}
	for _, w := range want {
		if !strings.Contains(res.ProtoText, w) {
			t.Errorf("protoText 缺少 %q\n---\n%s", w, res.ProtoText)
		}
	}
}

func TestDecodeRaw_CEscapeCJK(t *testing.T) {
	// UTF-8 "你" = e4 bd a0 → protoc 输出应为八进制转义
	var b []byte
	b = protowireString(b, 1, "你")
	res, _ := DecodeRaw(DecodeInput{Data: hex.EncodeToString(b), Encoding: "hex"})
	if !strings.Contains(res.ProtoText, `\344\275\240`) {
		t.Errorf("CJK 应转义为八进制,得到 %q", res.ProtoText)
	}
	// 但树里应还原成可读字符串
	if res.Nodes[0].Str != "你" {
		t.Errorf("树中应还原为「你」,得到 %q", res.Nodes[0].Str)
	}
}

func TestDecodeRaw_GenProto(t *testing.T) {
	b := sampleBytes()
	res, _ := DecodeRaw(DecodeInput{Data: hex.EncodeToString(b), Encoding: "hex"})
	for _, w := range []string{"syntax = \"proto3\"", "message Root", "message M8", "f8 = 8"} {
		if !strings.Contains(res.ProtoDef, w) {
			t.Errorf("protoDef 缺少 %q\n---\n%s", w, res.ProtoDef)
		}
	}
}

func TestInputEncodings(t *testing.T) {
	raw := []byte{0x0a, 0x02, 0x68, 0x69} // 1: "hi"
	cases := []struct{ enc, data string }{
		{"hex", "0a026869"},
		{"hex", "0a 02 68 69"},
		{"hex", "0x0a026869"},
		{"base64", base64.StdEncoding.EncodeToString(raw)},
		{"blob", "X'0a026869'"},
		{"blob", "x'0a 02 68 69'"},
	}
	for _, c := range cases {
		got, err := decodeInputBytes(c.data, c.enc)
		if err != nil {
			t.Errorf("%s %q: %v", c.enc, c.data, err)
			continue
		}
		if !strings.EqualFold(hex.EncodeToString(got), "0a026869") {
			t.Errorf("%s %q 解出 %x", c.enc, c.data, got)
		}
	}
}

const demoProto = `syntax = "proto3";
package demo;
message Person {
  string name = 1;
  int32 age = 2;
  repeated string tags = 3;
}`

func TestSchema_Roundtrip(t *testing.T) {
	src := SchemaSource{Kind: "proto", Proto: demoProto}

	info := InspectSchema(src)
	if info.Error != "" {
		t.Fatalf("InspectSchema: %s", info.Error)
	}
	if !contains(info.Types, "demo.Person") {
		t.Fatalf("类型列表应含 demo.Person,得到 %v", info.Types)
	}

	enc, err := EncodeSchema(SchemaEncodeInput{
		Src: src, Type: "demo.Person", Encoding: "hex",
		JSON: `{"name":"Alice","age":30,"tags":["a","b"]}`,
	})
	if err != nil {
		t.Fatalf("EncodeSchema: %v", err)
	}

	dec, err := DecodeSchema(SchemaDecodeInput{
		Src: src, Type: "demo.Person", Data: enc.Data, Encoding: "hex",
	})
	if err != nil {
		t.Fatalf("DecodeSchema: %v", err)
	}
	for _, w := range []string{"Alice", "30", "\"a\""} {
		if !strings.Contains(dec.JSON, w) {
			t.Errorf("解码 JSON 缺少 %q\n%s", w, dec.JSON)
		}
	}
}

func TestSchema_DescriptorMode(t *testing.T) {
	// 用 protocompile 编出 FileDescriptorSet,喂给 descriptor 模式
	comp := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			Accessor: protocompile.SourceAccessorFromMap(map[string]string{"d.proto": demoProto}),
		}),
	}
	files, err := comp.Compile(context.Background(), "d.proto")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{protodesc.ToFileDescriptorProto(files[0])},
	}
	raw, _ := proto.Marshal(fds)
	src := SchemaSource{Kind: "descriptor", Desc: base64.StdEncoding.EncodeToString(raw)}

	info := InspectSchema(src)
	if info.Error != "" || !contains(info.Types, "demo.Person") {
		t.Fatalf("descriptor InspectSchema 失败: %+v", info)
	}
	if !contains(info.Files, "d.proto") {
		t.Errorf("文件列表应含 d.proto,得到 %v", info.Files)
	}
}

func TestSchema_UnknownFallback(t *testing.T) {
	// 用只含 name 的 schema 解一条含额外字段的消息,额外字段应回退裸解析
	full := sampleBytes()
	src := SchemaSource{Kind: "proto", Proto: "syntax = \"proto3\";\nmessage M { string f1 = 1; }"}
	dec, err := DecodeSchema(SchemaDecodeInput{Src: src, Type: "M", Data: hex.EncodeToString(full), Encoding: "hex"})
	if err != nil {
		t.Fatalf("DecodeSchema: %v", err)
	}
	if len(dec.Unknown) == 0 {
		t.Fatalf("应有未知字段回退,得到 0")
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
