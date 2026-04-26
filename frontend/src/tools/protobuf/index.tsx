import { useEffect, useMemo, useState } from 'react'
import {
  AlertCircle,
  Binary,
  FileCode2,
  FileUp,
  Info,
  Upload,
  X,
} from 'lucide-react'
import { ToolShell } from '@/components/tool/ToolShell'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { meta } from './meta'
import {
  EXAMPLE_JSON,
  EXAMPLE_PROTO,
  decode,
  encode,
  parseDescriptor,
  parseProto,
  rawDecode,
  type ParsedSchema,
} from './lib'
import {
  bytesToBase64,
  bytesToHex,
  fromBytes,
  hexToBytes,
  toBytes,
  type DataEncoding,
} from '../crypto-lab/lib/encoding'
import { base64ToBytes } from '../crypto-lab/lib/encoding'

type Direction = 'encode' | 'decode'
type Source = 'proto' | 'descriptor'

export default function ProtobufTool() {
  const [source, setSource] = useState<Source>('proto')
  const [proto, setProto] = useState(EXAMPLE_PROTO)
  const [descBytes, setDescBytes] = useState<Uint8Array | null>(null)
  const [descName, setDescName] = useState('')
  const [direction, setDirection] = useState<Direction>('encode')
  const [typeName, setTypeName] = useState('demo.Person')
  const [jsonText, setJsonText] = useState(EXAMPLE_JSON)
  const [bytesText, setBytesText] = useState('')
  const [bytesEnc, setBytesEnc] = useState<DataEncoding>('hex')
  const [error, setError] = useState('')
  const [rawView, setRawView] = useState<ReturnType<typeof rawDecode> | null>(null)

  const schema: { parsed?: ParsedSchema; error?: string } = useMemo(() => {
    try {
      if (source === 'descriptor') {
        if (!descBytes) return { error: '请先加载 .pb / .desc 描述符文件' }
        return { parsed: parseDescriptor(descBytes) }
      }
      return { parsed: parseProto(proto) }
    } catch (e: any) {
      return { error: e?.message ?? String(e) }
    }
  }, [source, proto, descBytes])

  // 初次解析成功后自动选第一个类型
  useEffect(() => {
    if (schema.parsed && schema.parsed.types.length > 0) {
      if (!schema.parsed.types.includes(typeName)) {
        setTypeName(schema.parsed.types[0])
      }
    }
  }, [schema.parsed, typeName])

  const execute = () => {
    setError('')
    setRawView(null)
    if (!schema.parsed) {
      setError(schema.error || '解析失败')
      return
    }
    try {
      if (direction === 'encode') {
        const json = JSON.parse(jsonText)
        const bytes = encode(schema.parsed.root, typeName, json)
        setBytesText(fromBytes(bytes, bytesEnc))
      } else {
        const bytes = toBytes(bytesText, bytesEnc)
        try {
          const obj = decode(schema.parsed.root, typeName, bytes)
          setJsonText(JSON.stringify(obj, null, 2))
        } catch (e: any) {
          // 主解码失败时附带裸解析，帮用户定位问题
          setError(`按 ${typeName} 解码失败：${e?.message ?? e}`)
          try {
            setRawView(rawDecode(bytes))
          } catch {
            // 裸解也失败，不展示
          }
        }
      }
    } catch (e: any) {
      setError(e?.message ?? String(e))
    }
  }

  const tryConvertBytesEnc = (next: DataEncoding) => {
    if (!bytesText || next === bytesEnc) return setBytesEnc(next)
    try {
      const b = toBytes(bytesText, bytesEnc)
      setBytesText(fromBytes(b, next))
      setBytesEnc(next)
    } catch {
      setBytesEnc(next)
    }
  }

  const loadExample = () => {
    setProto(EXAMPLE_PROTO)
    setJsonText(EXAMPLE_JSON)
    setBytesText('')
    setTypeName('demo.Person')
    setDirection('encode')
    setError('')
    setRawView(null)
  }
  const clear = () => {
    setProto('')
    setJsonText('')
    setBytesText('')
    setError('')
    setRawView(null)
  }

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      onLoadExample={loadExample}
      onClear={clear}
    >
      <div className="grid h-full grid-cols-1 gap-3 lg:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)]">
        {/* 左：Schema 来源 */}
        <div className="flex min-h-0 flex-col gap-2">
          <div className="flex items-center gap-2 text-xs">
            <div className="inline-flex items-center rounded-md border border-border bg-background p-0.5">
              <button
                onClick={() => setSource('proto')}
                className={cn(
                  'inline-flex items-center gap-1 rounded-sm px-2.5 py-1 text-[11px] font-medium',
                  source === 'proto'
                    ? 'bg-info text-white'
                    : 'text-muted-foreground hover:text-foreground',
                )}
              >
                <FileCode2 className="h-3 w-3" />
                .proto 文本
              </button>
              <button
                onClick={() => setSource('descriptor')}
                className={cn(
                  'inline-flex items-center gap-1 rounded-sm px-2.5 py-1 text-[11px] font-medium',
                  source === 'descriptor'
                    ? 'bg-info text-white'
                    : 'text-muted-foreground hover:text-foreground',
                )}
              >
                <FileUp className="h-3 w-3" />
                .pb / .desc
              </button>
            </div>
            <span className="ml-auto text-[10px]">
              {schema.parsed ? (
                <span className="text-emerald-600 dark:text-emerald-400">
                  {schema.parsed.files.length > 0
                    ? `${schema.parsed.files.length} 个文件 · `
                    : ''}
                  {schema.parsed.types.length} 个 message
                </span>
              ) : schema.error ? (
                <span className="text-red-500">{schema.error}</span>
              ) : null}
            </span>
          </div>

          {source === 'proto' ? (
            <>
              <textarea
                value={proto}
                onChange={(e) => setProto(e.target.value)}
                spellCheck={false}
                className="flex-1 resize-none rounded-md border border-border bg-background px-3 py-2 font-mono text-xs outline-none focus:border-primary/50"
                placeholder="粘贴 .proto 定义（不支持 import；若需 import 请用 .pb）"
              />
              <p className="text-[10px] text-muted-foreground">
                小提示：有 import 时请先 <code>protoc --descriptor_set_out=x.pb --include_imports *.proto</code>
                ，再用右上"来源"切换到 <code>.pb / .desc</code>
              </p>
            </>
          ) : (
            <DescriptorDrop
              bytes={descBytes}
              fileName={descName}
              filesInPb={schema.parsed?.files ?? []}
              onLoaded={(b, name) => {
                setDescBytes(b)
                setDescName(name)
              }}
              onClear={() => {
                setDescBytes(null)
                setDescName('')
              }}
            />
          )}
        </div>

        {/* 右：操作 + IO */}
        <div className="flex min-h-0 flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <DirectionToggle direction={direction} onChange={setDirection} />
            <span className="text-xs text-muted-foreground">消息类型</span>
            <select
              value={typeName}
              onChange={(e) => setTypeName(e.target.value)}
              disabled={!schema.parsed}
              className="min-w-0 flex-1 rounded-md border border-border bg-background px-2 py-1 font-mono text-xs outline-none"
            >
              {schema.parsed?.types.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
            <Button size="sm" onClick={execute} disabled={!schema.parsed || !typeName}>
              执行
            </Button>
          </div>

          {error && (
            <div className="flex items-center gap-2 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">
              <AlertCircle className="h-3.5 w-3.5 shrink-0" />
              <span className="font-mono">{error}</span>
            </div>
          )}

          {/* JSON 区 */}
          <div className="flex min-h-0 flex-1 flex-col gap-1">
            <div className="flex items-center justify-between">
              <span className="text-xs text-muted-foreground">JSON</span>
              <span className="text-[10px] text-muted-foreground">
                {direction === 'encode' ? '编码输入' : '解码输出'}
              </span>
            </div>
            <textarea
              value={jsonText}
              onChange={(e) => setJsonText(e.target.value)}
              spellCheck={false}
              className={cn(
                'min-h-[120px] flex-1 resize-none rounded-md border border-border bg-background px-3 py-2 font-mono text-xs outline-none focus:border-primary/50',
                direction === 'decode' && 'bg-secondary/30',
              )}
              readOnly={direction === 'decode'}
            />
          </div>

          {/* 字节区 */}
          <div className="flex min-h-0 flex-1 flex-col gap-1">
            <div className="flex items-center justify-between">
              <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Binary className="h-3.5 w-3.5" />
                二进制（{bytesText ? toBytesSafe(bytesText, bytesEnc) : 0} 字节）
              </span>
              <div className="inline-flex items-center rounded-md border border-border bg-background p-0.5">
                {(['hex', 'base64'] as DataEncoding[]).map((e) => (
                  <button
                    key={e}
                    onClick={() => tryConvertBytesEnc(e)}
                    className={cn(
                      'rounded-sm px-2 py-0.5 text-[10px] font-medium',
                      bytesEnc === e
                        ? 'bg-info text-white'
                        : 'text-muted-foreground hover:text-foreground',
                    )}
                  >
                    {e}
                  </button>
                ))}
              </div>
            </div>
            <textarea
              value={bytesText}
              onChange={(e) => setBytesText(e.target.value)}
              spellCheck={false}
              className={cn(
                'min-h-[80px] flex-1 resize-none rounded-md border border-border bg-background px-3 py-2 font-mono text-xs outline-none focus:border-primary/50',
                direction === 'encode' && 'bg-secondary/30',
              )}
              readOnly={direction === 'encode'}
            />
          </div>

          {rawView && (
            <div className="rounded-md border border-border bg-card p-2 text-[11px]">
              <div className="mb-1 flex items-center gap-1.5 text-muted-foreground">
                <Info className="h-3 w-3" />
                裸字段视图（按 wire type 解析）
              </div>
              <table className="w-full text-[11px]">
                <thead className="text-muted-foreground">
                  <tr>
                    <th className="text-left font-normal">field</th>
                    <th className="text-left font-normal">wire</th>
                    <th className="text-left font-normal">value</th>
                  </tr>
                </thead>
                <tbody className="font-mono">
                  {rawView.map((r, i) => (
                    <tr key={i} className="align-top">
                      <td className="pr-2">#{r.field}</td>
                      <td className="pr-2">
                        {r.wire}/{r.wireName}
                      </td>
                      <td className="break-all">{formatRawValue(r.value)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>
    </ToolShell>
  )
}

function DirectionToggle({
  direction,
  onChange,
}: {
  direction: Direction
  onChange: (d: Direction) => void
}) {
  return (
    <div className="inline-flex items-center rounded-md border border-border bg-background p-0.5">
      <button
        onClick={() => onChange('encode')}
        className={cn(
          'rounded-sm px-3 py-1 text-xs font-medium',
          direction === 'encode'
            ? 'bg-info text-white'
            : 'text-muted-foreground hover:text-foreground',
        )}
      >
        JSON → 字节
      </button>
      <button
        onClick={() => onChange('decode')}
        className={cn(
          'rounded-sm px-3 py-1 text-xs font-medium',
          direction === 'decode'
            ? 'bg-info text-white'
            : 'text-muted-foreground hover:text-foreground',
        )}
      >
        字节 → JSON
      </button>
    </div>
  )
}

function toBytesSafe(s: string, enc: DataEncoding): number {
  try {
    if (enc === 'hex') return hexToBytes(s).length
    if (enc === 'base64') return base64ToBytes(s).length
    return new TextEncoder().encode(s).length
  } catch {
    return 0
  }
}

function formatRawValue(v: any): string {
  if (v instanceof Uint8Array) {
    // 尝试按 UTF-8 解码展示，失败回退 hex
    try {
      const s = new TextDecoder('utf-8', { fatal: true }).decode(v)
      if (/^[\x09\x0a\x0d\x20-\x7e一-鿿-￿]+$/.test(s)) {
        return `"${s}" (${v.length}B)`
      }
    } catch {
      // ignore
    }
    return 'hex:' + bytesToHex(v)
  }
  return String(v)
}

// 复用导出
void bytesToBase64

function DescriptorDrop({
  bytes,
  fileName,
  filesInPb,
  onLoaded,
  onClear,
}: {
  bytes: Uint8Array | null
  fileName: string
  filesInPb: string[]
  onLoaded: (b: Uint8Array, name: string) => void
  onClear: () => void
}) {
  const [dragOver, setDragOver] = useState(false)

  const handleFile = async (f: File) => {
    const buf = await f.arrayBuffer()
    onLoaded(new Uint8Array(buf), f.name)
  }

  const onDrop = (e: React.DragEvent) => {
    e.preventDefault()
    setDragOver(false)
    const f = e.dataTransfer.files?.[0]
    if (f) void handleFile(f)
  }

  const onClick = () => {
    const input = document.createElement('input')
    input.type = 'file'
    input.accept = '.pb,.desc,.bin,application/octet-stream'
    input.onchange = () => {
      const f = input.files?.[0]
      if (f) void handleFile(f)
    }
    input.click()
  }

  return (
    <div className="flex flex-1 min-h-0 flex-col gap-2">
      {bytes ? (
        <div className="rounded-md border border-border bg-card p-3">
          <div className="flex items-center gap-2">
            <FileUp className="h-4 w-4 text-info" />
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">{fileName}</div>
              <div className="text-[10px] text-muted-foreground">
                {bytes.length.toLocaleString()} 字节
              </div>
            </div>
            <Button size="sm" variant="ghost" onClick={onClear} title="移除">
              <X className="h-3.5 w-3.5" />
            </Button>
          </div>
          {filesInPb.length > 0 && (
            <div className="mt-2 border-t border-border pt-2">
              <div className="mb-1 text-[10px] uppercase tracking-wide text-muted-foreground">
                包含的文件
              </div>
              <ul className="space-y-0.5 font-mono text-[11px]">
                {filesInPb.map((f) => (
                  <li key={f} className="truncate">
                    · {f}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      ) : null}
      <button
        type="button"
        onClick={onClick}
        onDragOver={(e) => {
          e.preventDefault()
          setDragOver(true)
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={onDrop}
        className={cn(
          'flex flex-1 min-h-[160px] flex-col items-center justify-center gap-2 rounded-md border-2 border-dashed bg-secondary/30 px-4 py-6 text-center transition-colors',
          dragOver ? 'border-info bg-info/10' : 'border-border hover:border-info/50',
        )}
      >
        <Upload className="h-6 w-6 text-muted-foreground" />
        <div className="text-sm">{bytes ? '更换文件' : '点击或拖入 .pb / .desc 文件'}</div>
        <div className="max-w-sm text-[10px] text-muted-foreground">
          protoc --descriptor_set_out=xxx.pb --include_imports *.proto
          <br />
          编译出的 FileDescriptorSet 二进制
        </div>
      </button>
    </div>
  )
}
