// Protobuf 工具前端辅助:输入编码、文件读取、示例数据。
// 解析全部在 Go 后端完成(protowire + protocompile),前端只做输入/展示。

export type InputEncoding = 'hex' | 'base64' | 'blob'
export type SchemaKind = 'proto' | 'descriptor'
export type RawView = 'tree' | 'text' | 'proto'
export type Direction = 'decode' | 'encode'

/** 把文件读成 base64(分块避免超大 spread 爆栈)。 */
export async function fileToBase64(file: File): Promise<string> {
  const buf = await file.arrayBuffer()
  const bytes = new Uint8Array(buf)
  let bin = ''
  const chunk = 0x8000
  for (let i = 0; i < bytes.length; i += chunk) {
    bin += String.fromCharCode(...bytes.subarray(i, i + chunk))
  }
  return btoa(bin)
}

export function copyText(s: string): void {
  void navigator.clipboard?.writeText(s)
}

export function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

/** 示例:一条仿 Threads「MDCore」记录(嵌套富文本 + 毫秒时间戳),用来演示裸解析。 */
export const EXAMPLE_HEX =
  '0a216d69642e24674141635657767548634f536c56774578616d706c655265636f7264121337343738' +
  '363331313630353439343539383436' +
  '3a1642554d505f4d525f4f4e5f464f4c4c4f575f584d4154' +
  '428b013a88010a290a09706c61696e74657874121ce4bda0e59ca8205468726561647320e4b88ae585' +
  'b3e6b3a8e4ba86200a480a046c696e6b12096277673139383136351a2f0a09627767313938313635' +
  '122268747470733a2f2f7777772e746872656164732e6e65742f406277673139383136352204626f' +
  '6c640a110a09706c61696e74657874120420e38082' +
  '600082010d31373833303434363135343232'

/** Schema 模式示例。 */
export const EXAMPLE_PROTO = `syntax = "proto3";

package demo;

message Person {
  string name = 1;
  int32 age = 2;
  repeated string tags = 3;
  Address address = 4;
  map<string, string> attrs = 5;
  Status status = 6;
}

message Address {
  string city = 1;
  string street = 2;
  int32 zip = 3;
}

enum Status {
  UNKNOWN = 0;
  ACTIVE = 1;
  ARCHIVED = 2;
}
`

export const EXAMPLE_JSON = `{
  "name": "Alice",
  "age": 30,
  "tags": ["dev", "ops"],
  "address": { "city": "Shanghai", "street": "Pudong", "zip": 200120 },
  "attrs": { "role": "admin", "team": "platform" },
  "status": "ACTIVE"
}`

const INPUT_PLACEHOLDER: Record<InputEncoding, string> = {
  hex: '粘贴 hex,如 0a22...(容许空格/逗号/0x 前缀);或把 .bin 拖进来',
  base64: '粘贴 base64;或把 .bin 拖进来',
  blob: "粘贴 SQLite BLOB 字面量,如 X'0a22...';或把 .bin 拖进来",
}
export function inputPlaceholder(enc: InputEncoding): string {
  return INPUT_PLACEHOLDER[enc]
}
