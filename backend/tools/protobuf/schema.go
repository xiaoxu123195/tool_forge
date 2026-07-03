package protobuf

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// loadSchema 从 .proto 文本或 .pb/.desc 描述符构建可查找的文件集。
func loadSchema(src SchemaSource) (*protoregistry.Files, error) {
	if src.Kind == "descriptor" {
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(src.Desc))
		if err != nil {
			return nil, fmt.Errorf("描述符 base64 解码失败: %w", err)
		}
		var fds descriptorpb.FileDescriptorSet
		if err := proto.Unmarshal(b, &fds); err != nil {
			return nil, fmt.Errorf("解析 FileDescriptorSet 失败(需 protoc --descriptor_set_out 的产物): %w", err)
		}
		return protodesc.NewFiles(&fds)
	}
	return compileProto(src.Proto)
}

// compileProto 用 protocompile 编译单个内存中的 .proto 文本(自带 google/protobuf/* 标准导入)。
func compileProto(text string) (*protoregistry.Files, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("空的 .proto")
	}
	const fname = "input.proto"
	comp := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			Accessor: protocompile.SourceAccessorFromMap(map[string]string{fname: text}),
		}),
		SourceInfoMode: protocompile.SourceInfoNone,
	}
	files, err := comp.Compile(context.Background(), fname)
	if err != nil {
		return nil, err
	}
	reg := new(protoregistry.Files)
	for _, f := range files {
		_ = reg.RegisterFile(f) // 重复(well-known)忽略
	}
	return reg, nil
}

func findMessage(reg *protoregistry.Files, fullName string) (protoreflect.MessageDescriptor, error) {
	if strings.TrimSpace(fullName) == "" {
		return nil, errors.New("未指定消息类型")
	}
	d, err := reg.FindDescriptorByName(protoreflect.FullName(fullName))
	if err != nil {
		return nil, fmt.Errorf("找不到消息类型 %q: %w", fullName, err)
	}
	md, ok := d.(protoreflect.MessageDescriptor)
	if !ok {
		return nil, fmt.Errorf("%q 不是 message", fullName)
	}
	return md, nil
}

func listTypes(reg *protoregistry.Files) []string {
	var out []string
	reg.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		collectMsgs(fd.Messages(), &out)
		return true
	})
	return out
}

func collectMsgs(mds protoreflect.MessageDescriptors, out *[]string) {
	for i := 0; i < mds.Len(); i++ {
		md := mds.Get(i)
		if md.IsMapEntry() {
			continue // 跳过 map 的合成 entry 消息
		}
		*out = append(*out, string(md.FullName()))
		collectMsgs(md.Messages(), out)
	}
}

func listFiles(reg *protoregistry.Files) []string {
	var out []string
	reg.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		out = append(out, fd.Path())
		return true
	})
	return out
}

// InspectSchema 解析 schema,返回可选的消息类型与内含文件。
func InspectSchema(src SchemaSource) SchemaInfo {
	reg, err := loadSchema(src)
	if err != nil {
		return SchemaInfo{Error: err.Error()}
	}
	info := SchemaInfo{Types: listTypes(reg)}
	if src.Kind == "descriptor" {
		info.Files = listFiles(reg)
	}
	return info
}

// DecodeSchema 按指定消息类型把二进制解码为 JSON;schema 未覆盖的未知字段回退裸解析。
func DecodeSchema(in SchemaDecodeInput) (*SchemaDecodeResult, error) {
	reg, err := loadSchema(in.Src)
	if err != nil {
		return nil, err
	}
	md, err := findMessage(reg, in.Type)
	if err != nil {
		return nil, err
	}
	raw, err := decodeInputBytes(in.Data, in.Encoding)
	if err != nil {
		return nil, err
	}
	msg := dynamicpb.NewMessage(md)
	if err := proto.Unmarshal(raw, msg); err != nil {
		return nil, fmt.Errorf("按 %s 解码失败: %w", in.Type, err)
	}
	mo := protojson.MarshalOptions{Multiline: true, Indent: "  "}
	js, err := mo.Marshal(msg)
	if err != nil {
		return nil, err
	}
	res := &SchemaDecodeResult{JSON: string(js)}
	if unk := msg.GetUnknown(); len(unk) > 0 {
		if nodes, ok := parseMessage(unk, 0, 0); ok {
			res.Unknown = nodes
		}
	}
	return res, nil
}

// EncodeSchema 按指定消息类型把 JSON 编码为二进制(hex/base64 输出)。
func EncodeSchema(in SchemaEncodeInput) (*SchemaEncodeResult, error) {
	reg, err := loadSchema(in.Src)
	if err != nil {
		return nil, err
	}
	md, err := findMessage(reg, in.Type)
	if err != nil {
		return nil, err
	}
	msg := dynamicpb.NewMessage(md)
	if err := protojson.Unmarshal([]byte(in.JSON), msg); err != nil {
		return nil, fmt.Errorf("JSON 不符合 %s: %w", in.Type, err)
	}
	raw, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}
	out := hex.EncodeToString(raw)
	if strings.ToLower(in.Encoding) == "base64" {
		out = base64.StdEncoding.EncodeToString(raw)
	}
	return &SchemaEncodeResult{Data: out, Size: len(raw)}, nil
}
