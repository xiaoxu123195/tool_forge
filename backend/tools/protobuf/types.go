// Package protobuf 提供 Protobuf 的无 Schema 裸解析(等价并强于 `protoc --decode_raw`)
// 与有 Schema 的按名编解码。
//
// 裸解析:仅凭 wire 格式递归还原字段树,不需要 .proto。对每个 length-delimited 字段
// 做歧义消解(嵌套消息 / 字符串 / 字节 / packed 数组),并对数值给出多重解读
// (有符号 / zigzag / float / double / 时间戳),便于取证逆向。
//
// Schema 模式:粘贴 .proto 文本(protocompile 编译)或上传 .pb/.desc(FileDescriptorSet),
// 用 dynamicpb + protojson 做 JSON⇄二进制,schema 未覆盖到的未知字段自动退回裸解析展示。
package protobuf

// Node 裸解析出的一个字段。同一个结构承载所有 wire type,前端按 Type 选择展示,
// 对 length-delimited 字段同时给出多种候选(message/str/hex/packed),前端可即时切换而无需二次请求。
type Node struct {
	Field    int32  `json:"field"`    // 字段号
	Wire     int    `json:"wire"`     // wire type: 0 varint / 1 i64 / 2 len / 3 sgroup / 4 egroup / 5 i32
	WireName string `json:"wireName"` // varint / i64 / len / i32 / group
	Type     string `json:"type"`     // 最佳猜测的展示种类: varint|i64|i32|message|string|bytes|packed|group
	Offset   int    `json:"offset"`   // 字段(tag)在所属消息内的起始字节偏移
	Size     int    `json:"size"`     // 本字段(tag+value)总字节数

	// —— 数值类(varint / i64 / i32)——
	Uint   string `json:"uint,omitempty"`   // 无符号十进制(varint 原值 / fixed 原值),用字符串保 uint64 精度
	Sint   string `json:"sint,omitempty"`   // 有符号(二进制补码)解读
	Zigzag string `json:"zigzag,omitempty"` // zigzag 解读(仅 varint)
	Bool   *bool  `json:"bool,omitempty"`   // 值为 0/1 时给出布尔解读
	Double string `json:"double,omitempty"` // i64 按 float64 解读(字符串,含 NaN/Inf)
	Float  string `json:"float,omitempty"`  // i32 按 float32 解读
	Hex    string `json:"hex,omitempty"`    // fixed 原始字节 / LEN 原始字节 的十六进制

	// —— 时间戳标注(数值或全数字字符串命中合理纪元区间时)——
	Time     string `json:"time,omitempty"`     // 格式化后的 UTC 时间
	TimeUnit string `json:"timeUnit,omitempty"` // s / ms / us / ns

	// —— length-delimited 候选 ——
	Str      string `json:"str,omitempty"`     // 按 UTF-8 解出的字符串
	StrOK    bool   `json:"strOk,omitempty"`   // 是否是合法 UTF-8(可作字符串展示)
	Message  []Node `json:"message,omitempty"` // 递归解析为子消息的结果
	MsgOK    bool   `json:"msgOk,omitempty"`   // 子消息是否成功完整解析
	Packed   []Item `json:"packed,omitempty"`  // packed 候选(varint 序列)
	PackedOK bool   `json:"packedOk,omitempty"`
}

// Item packed 数组里的一项,附带同样的多重数值解读。
type Item struct {
	Uint   string `json:"uint"`
	Sint   string `json:"sint,omitempty"`
	Zigzag string `json:"zigzag,omitempty"`
}

// DecodeInput 裸解析输入。
type DecodeInput struct {
	Data     string `json:"data"`     // 编码后的字节
	Encoding string `json:"encoding"` // hex | base64 | blob(SQLite X'..' 字面量)
}

// RawResult 裸解析结果:字段树 + 两种导出。
type RawResult struct {
	Nodes     []Node `json:"nodes"`
	Size      int    `json:"size"`      // 输入字节数
	ProtoText string `json:"protoText"` // 与 protoc --decode_raw 一致的文本
	ProtoDef  string `json:"protoDef"`  // 反推出的 .proto 骨架草稿
}

// SchemaSource schema 来源。
type SchemaSource struct {
	Kind  string `json:"kind"`  // proto | descriptor
	Proto string `json:"proto"` // .proto 文本(kind=proto)
	Desc  string `json:"desc"`  // .pb/.desc 的 base64(kind=descriptor)
}

// SchemaInfo 解析 schema 后的概览。
type SchemaInfo struct {
	Types []string `json:"types"` // 全限定消息名
	Files []string `json:"files"` // descriptor 内含文件名(kind=descriptor)
	Error string   `json:"error,omitempty"`
}

// SchemaDecodeInput 有 schema 解码(二进制→JSON)。
type SchemaDecodeInput struct {
	Src      SchemaSource `json:"src"`
	Type     string       `json:"type"`     // 全限定消息名
	Data     string       `json:"data"`     // 编码后的字节
	Encoding string       `json:"encoding"` // hex | base64 | blob
}

// SchemaDecodeResult 解码结果。
type SchemaDecodeResult struct {
	JSON    string `json:"json"`
	Unknown []Node `json:"unknown"` // schema 未覆盖的未知字段(裸解析)
}

// SchemaEncodeInput 有 schema 编码(JSON→二进制)。
type SchemaEncodeInput struct {
	Src      SchemaSource `json:"src"`
	Type     string       `json:"type"`
	JSON     string       `json:"json"`
	Encoding string       `json:"encoding"` // 输出编码 hex | base64
}

// SchemaEncodeResult 编码结果。
type SchemaEncodeResult struct {
	Data string `json:"data"`
	Size int    `json:"size"`
}
