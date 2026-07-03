import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { AlertCircle, Binary, Copy, FileUp, Upload, X } from 'lucide-react'
import { ToolShell } from '@/components/tool/ToolShell'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import {
  ProtobufDecodeRaw,
  ProtobufDecodeSchema,
  ProtobufEncodeSchema,
  ProtobufInspectSchema,
} from '../../../wailsjs/go/main/App'
import { protobuf } from '../../../wailsjs/go/models'
import { meta } from './meta'
import { RawTree } from './RawTree'
import {
  EXAMPLE_HEX,
  EXAMPLE_JSON,
  EXAMPLE_PROTO,
  copyText,
  fileToBase64,
  fmtBytes,
  inputPlaceholder,
  type Direction,
  type InputEncoding,
  type RawView,
  type SchemaKind,
} from './lib'

type Mode = 'raw' | 'schema'

export default function ProtobufTool() {
  const [mode, setMode] = useState<Mode>('raw')

  // —— 裸解析 ——
  const [inData, setInData] = useState(EXAMPLE_HEX)
  const [inEnc, setInEnc] = useState<InputEncoding>('hex')
  const [raw, setRaw] = useState<protobuf.RawResult | null>(null)
  const [rawErr, setRawErr] = useState('')
  const [rawView, setRawView] = useState<RawView>('tree')

  // —— Schema ——
  const [kind, setKind] = useState<SchemaKind>('proto')
  const [proto, setProto] = useState(EXAMPLE_PROTO)
  const [descB64, setDescB64] = useState('')
  const [descName, setDescName] = useState('')
  const [info, setInfo] = useState<protobuf.SchemaInfo | null>(null)
  const [typeName, setTypeName] = useState('demo.Person')
  const [dir, setDir] = useState<Direction>('decode')
  const [sBytes, setSBytes] = useState('')
  const [sBytesEnc, setSBytesEnc] = useState<InputEncoding>('hex')
  const [json, setJson] = useState(EXAMPLE_JSON)
  const [outEnc, setOutEnc] = useState<'hex' | 'base64'>('hex')
  const [encOut, setEncOut] = useState<protobuf.SchemaEncodeResult | null>(null)
  const [dec, setDec] = useState<protobuf.SchemaDecodeResult | null>(null)
  const [schemaErr, setSchemaErr] = useState('')

  // ============ 裸解析:输入变化自动解析 ============
  useEffect(() => {
    if (mode !== 'raw') return
    if (!inData.trim()) {
      setRaw(null)
      setRawErr('')
      return
    }
    const t = window.setTimeout(async () => {
      try {
        const r = await ProtobufDecodeRaw({ data: inData, encoding: inEnc })
        setRaw(r)
        setRawErr('')
      } catch (e) {
        setRaw(null)
        setRawErr(errMsg(e))
      }
    }, 250)
    return () => window.clearTimeout(t)
  }, [mode, inData, inEnc])

  // ============ Schema:来源变化自动解析类型列表 ============
  const src = useMemo<protobuf.SchemaSource>(
    () => ({ kind, proto, desc: descB64 }) as protobuf.SchemaSource,
    [kind, proto, descB64],
  )

  useEffect(() => {
    if (mode !== 'schema') return
    if (kind === 'proto' && !proto.trim()) {
      setInfo(null)
      return
    }
    if (kind === 'descriptor' && !descB64) {
      setInfo(null)
      return
    }
    const t = window.setTimeout(async () => {
      const i = await ProtobufInspectSchema(src)
      setInfo(i)
    }, 300)
    return () => window.clearTimeout(t)
  }, [mode, kind, proto, descB64, src])

  // schema 解析成功后自动选第一个类型
  useEffect(() => {
    if (info?.types && info.types.length > 0 && !info.types.includes(typeName)) {
      setTypeName(info.types[0])
    }
  }, [info, typeName])

  const runSchema = useCallback(async () => {
    setSchemaErr('')
    setDec(null)
    setEncOut(null)
    try {
      if (dir === 'decode') {
        const r = await ProtobufDecodeSchema(
          protobuf.SchemaDecodeInput.createFrom({ src, type: typeName, data: sBytes, encoding: sBytesEnc }),
        )
        setDec(r)
      } else {
        const r = await ProtobufEncodeSchema(
          protobuf.SchemaEncodeInput.createFrom({ src, type: typeName, json, encoding: outEnc }),
        )
        setEncOut(r)
      }
    } catch (e) {
      setSchemaErr(errMsg(e))
    }
  }, [dir, src, typeName, sBytes, sBytesEnc, json, outEnc])

  // ============ 顶栏动作 ============
  const loadExample = () => {
    if (mode === 'raw') {
      setInEnc('hex')
      setInData(EXAMPLE_HEX)
    } else {
      setKind('proto')
      setProto(EXAMPLE_PROTO)
      setJson(EXAMPLE_JSON)
      setTypeName('demo.Person')
    }
  }
  const clear = () => {
    if (mode === 'raw') {
      setInData('')
      setRaw(null)
      setRawErr('')
    } else {
      setSBytes('')
      setJson('')
      setDec(null)
      setEncOut(null)
      setSchemaErr('')
    }
  }

  return (
    <ToolShell title={meta.title} description={meta.description} onLoadExample={loadExample} onClear={clear}>
      <div className="mx-auto max-w-6xl space-y-4">
        {/* 模式切换 */}
        <Seg
          value={mode}
          onChange={(v) => setMode(v as Mode)}
          options={[
            { value: 'raw', label: '裸解析 (decode_raw)' },
            { value: 'schema', label: 'Schema 编解码' },
          ]}
          size="md"
        />

        {mode === 'raw' ? (
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            {/* 输入 */}
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium text-muted-foreground">输入</span>
                {raw && <span className="text-[11px] text-muted-foreground">{fmtBytes(raw.size)}</span>}
              </div>
              <BytesInput
                value={inData}
                enc={inEnc}
                onData={setInData}
                onEnc={setInEnc}
              />
            </div>

            {/* 输出 */}
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Seg
                  value={rawView}
                  onChange={(v) => setRawView(v as RawView)}
                  options={[
                    { value: 'tree', label: '树' },
                    { value: 'text', label: 'protoc 文本' },
                    { value: 'proto', label: '.proto 草稿' },
                  ]}
                />
                {raw && (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-7 px-2"
                    onClick={() =>
                      copyText(rawView === 'text' ? raw.protoText : rawView === 'proto' ? raw.protoDef : raw.protoText)
                    }
                  >
                    <Copy className="h-3.5 w-3.5" /> 复制
                  </Button>
                )}
              </div>

              {rawErr ? (
                <ErrorBox msg={rawErr} />
              ) : !raw ? (
                <Placeholder text="粘贴或拖入 Protobuf 二进制,自动解析" />
              ) : rawView === 'tree' ? (
                <Card>
                  <RawTree nodes={raw.nodes} />
                </Card>
              ) : (
                <Pre text={rawView === 'text' ? raw.protoText : raw.protoDef} />
              )}
            </div>
          </div>
        ) : (
          <SchemaPane
            kind={kind}
            setKind={setKind}
            proto={proto}
            setProto={setProto}
            descB64={descB64}
            descName={descName}
            onDesc={(b, name) => {
              setDescB64(b)
              setDescName(name)
            }}
            onClearDesc={() => {
              setDescB64('')
              setDescName('')
            }}
            info={info}
            typeName={typeName}
            setTypeName={setTypeName}
            dir={dir}
            setDir={setDir}
            sBytes={sBytes}
            setSBytes={setSBytes}
            sBytesEnc={sBytesEnc}
            setSBytesEnc={setSBytesEnc}
            json={json}
            setJson={setJson}
            outEnc={outEnc}
            setOutEnc={setOutEnc}
            dec={dec}
            encOut={encOut}
            err={schemaErr}
            onRun={runSchema}
          />
        )}
      </div>
    </ToolShell>
  )
}

// ================= 子组件 =================

function SchemaPane(props: {
  kind: SchemaKind
  setKind: (k: SchemaKind) => void
  proto: string
  setProto: (s: string) => void
  descB64: string
  descName: string
  onDesc: (b64: string, name: string) => void
  onClearDesc: () => void
  info: protobuf.SchemaInfo | null
  typeName: string
  setTypeName: (s: string) => void
  dir: Direction
  setDir: (d: Direction) => void
  sBytes: string
  setSBytes: (s: string) => void
  sBytesEnc: InputEncoding
  setSBytesEnc: (e: InputEncoding) => void
  json: string
  setJson: (s: string) => void
  outEnc: 'hex' | 'base64'
  setOutEnc: (e: 'hex' | 'base64') => void
  dec: protobuf.SchemaDecodeResult | null
  encOut: protobuf.SchemaEncodeResult | null
  err: string
  onRun: () => void
}) {
  const p = props
  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      {/* 左:schema 来源 */}
      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <Seg
            value={p.kind}
            onChange={(v) => p.setKind(v as SchemaKind)}
            options={[
              { value: 'proto', label: '.proto 文本' },
              { value: 'descriptor', label: '.pb / .desc' },
            ]}
          />
          <span className="text-[10px] text-muted-foreground">
            {p.info?.error ? (
              <span className="text-red-500">{p.info.error}</span>
            ) : p.info?.types?.length ? (
              <span className="text-emerald-600 dark:text-emerald-400">
                {p.info.files?.length ? `${p.info.files.length} 文件 · ` : ''}
                {p.info.types.length} message
              </span>
            ) : null}
          </span>
        </div>
        {p.kind === 'proto' ? (
          <textarea
            value={p.proto}
            onChange={(e) => p.setProto(e.target.value)}
            spellCheck={false}
            className="h-64 w-full resize-y rounded-lg border border-border bg-background px-3 py-2 font-mono text-xs outline-none focus:border-primary/50"
            placeholder="粘贴 .proto(支持 import google/protobuf/*;自定义 import 请改用 .pb)"
          />
        ) : (
          <DescDrop b64={p.descB64} name={p.descName} onDesc={p.onDesc} onClear={p.onClearDesc} />
        )}
      </div>

      {/* 右:操作 + IO */}
      <div className="space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <Seg
            value={p.dir}
            onChange={(v) => p.setDir(v as Direction)}
            options={[
              { value: 'decode', label: '字节 → JSON' },
              { value: 'encode', label: 'JSON → 字节' },
            ]}
          />
          <select
            value={p.typeName}
            onChange={(e) => p.setTypeName(e.target.value)}
            disabled={!p.info?.types?.length}
            className="min-w-0 flex-1 rounded-md border border-border bg-background px-2 py-1 font-mono text-xs outline-none"
          >
            {p.info?.types?.map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
          <Button size="sm" onClick={p.onRun} disabled={!p.info?.types?.length}>
            执行
          </Button>
        </div>

        {p.err && <ErrorBox msg={p.err} />}

        {p.dir === 'decode' ? (
          <>
            <BytesInput value={p.sBytes} enc={p.sBytesEnc} onData={p.setSBytes} onEnc={p.setSBytesEnc} small />
            {p.dec && (
              <>
                <Pre text={p.dec.json} label="JSON" />
                {p.dec.unknown?.length > 0 && (
                  <Card label="未知字段(schema 未覆盖,已裸解析)">
                    <RawTree nodes={p.dec.unknown} />
                  </Card>
                )}
              </>
            )}
          </>
        ) : (
          <>
            <textarea
              value={p.json}
              onChange={(e) => p.setJson(e.target.value)}
              spellCheck={false}
              className="h-40 w-full resize-y rounded-lg border border-border bg-background px-3 py-2 font-mono text-xs outline-none focus:border-primary/50"
              placeholder="JSON 输入"
            />
            {p.encOut && (
              <div className="space-y-1">
                <div className="flex items-center justify-between">
                  <Seg
                    value={p.outEnc}
                    onChange={(v) => p.setOutEnc(v as 'hex' | 'base64')}
                    options={[
                      { value: 'hex', label: 'hex' },
                      { value: 'base64', label: 'base64' },
                    ]}
                  />
                  <div className="flex items-center gap-2">
                    <span className="text-[11px] text-muted-foreground">{fmtBytes(p.encOut.size)}</span>
                    <Button variant="ghost" size="sm" className="h-7 px-2" onClick={() => copyText(p.encOut!.data)}>
                      <Copy className="h-3.5 w-3.5" /> 复制
                    </Button>
                  </div>
                </div>
                <Pre text={p.encOut.data} />
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}

function BytesInput({
  value,
  enc,
  onData,
  onEnc,
  small,
}: {
  value: string
  enc: InputEncoding
  onData: (s: string) => void
  onEnc: (e: InputEncoding) => void
  small?: boolean
}) {
  const [drag, setDrag] = useState(false)
  const [fileHint, setFileHint] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  const loadFile = async (f: File) => {
    const b64 = await fileToBase64(f)
    onEnc('base64')
    onData(b64)
    setFileHint(`${f.name} · ${fmtBytes(f.size)}`)
  }

  return (
    <div
      onDragOver={(e) => {
        e.preventDefault()
        setDrag(true)
      }}
      onDragLeave={() => setDrag(false)}
      onDrop={(e) => {
        e.preventDefault()
        setDrag(false)
        const f = e.dataTransfer.files?.[0]
        if (f) void loadFile(f)
      }}
      className={cn('space-y-1.5 rounded-lg border p-1.5', drag ? 'border-primary bg-primary/5' : 'border-border')}
    >
      <div className="flex items-center gap-2">
        <Seg
          value={enc}
          onChange={(v) => onEnc(v as InputEncoding)}
          options={[
            { value: 'hex', label: 'hex' },
            { value: 'base64', label: 'base64' },
            { value: 'blob', label: "X'..'" },
          ]}
        />
        <input
          ref={inputRef}
          type="file"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0]
            if (f) void loadFile(f)
          }}
        />
        <Button
          variant="ghost"
          size="sm"
          className="ml-auto h-7 px-2"
          onClick={() => inputRef.current?.click()}
        >
          <FileUp className="h-3.5 w-3.5" /> 导入 .bin
        </Button>
      </div>
      <textarea
        value={value}
        onChange={(e) => {
          onData(e.target.value)
          setFileHint('')
        }}
        spellCheck={false}
        className={cn(
          'w-full resize-y rounded-md border border-border bg-background px-3 py-2 font-mono text-xs outline-none focus:border-primary/50',
          small ? 'h-24' : 'h-56',
        )}
        placeholder={inputPlaceholder(enc)}
      />
      {fileHint && (
        <div className="flex items-center gap-1.5 px-1 text-[10px] text-muted-foreground">
          <Binary className="h-3 w-3" /> {fileHint}
        </div>
      )}
    </div>
  )
}

function DescDrop({
  b64,
  name,
  onDesc,
  onClear,
}: {
  b64: string
  name: string
  onDesc: (b64: string, name: string) => void
  onClear: () => void
}) {
  const [drag, setDrag] = useState(false)
  const load = async (f: File) => onDesc(await fileToBase64(f), f.name)

  return (
    <div className="space-y-2">
      {b64 && (
        <div className="flex items-center gap-2 rounded-lg border border-border bg-card p-2.5">
          <FileUp className="h-4 w-4 text-primary" />
          <span className="min-w-0 flex-1 truncate text-sm">{name}</span>
          <Button size="sm" variant="ghost" className="h-7 w-7 px-0" onClick={onClear}>
            <X className="h-3.5 w-3.5" />
          </Button>
        </div>
      )}
      <button
        type="button"
        onClick={() => {
          const el = document.createElement('input')
          el.type = 'file'
          el.accept = '.pb,.desc,.bin,application/octet-stream'
          el.onchange = () => {
            const f = el.files?.[0]
            if (f) void load(f)
          }
          el.click()
        }}
        onDragOver={(e) => {
          e.preventDefault()
          setDrag(true)
        }}
        onDragLeave={() => setDrag(false)}
        onDrop={(e) => {
          e.preventDefault()
          setDrag(false)
          const f = e.dataTransfer.files?.[0]
          if (f) void load(f)
        }}
        className={cn(
          'flex h-56 w-full flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed px-4 text-center transition-colors',
          drag ? 'border-primary bg-primary/10' : 'border-border hover:border-primary/50',
        )}
      >
        <Upload className="h-6 w-6 text-muted-foreground" />
        <div className="text-sm">{b64 ? '更换 .pb / .desc' : '点击或拖入 .pb / .desc'}</div>
        <div className="max-w-sm text-[10px] text-muted-foreground">
          protoc --descriptor_set_out=x.pb --include_imports *.proto 的产物
        </div>
      </button>
    </div>
  )
}

// —— 小组件 ——
function Seg<T extends string>({
  value,
  onChange,
  options,
  size = 'sm',
}: {
  value: T
  onChange: (v: T) => void
  options: { value: T; label: string }[]
  size?: 'sm' | 'md'
}) {
  return (
    <div className="inline-flex items-center rounded-md border border-border bg-background p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          onClick={() => onChange(o.value)}
          className={cn(
            'rounded-sm font-medium transition-colors',
            size === 'md' ? 'px-3 py-1.5 text-xs' : 'px-2.5 py-1 text-[11px]',
            value === o.value ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

function Card({ children, label }: { children: React.ReactNode; label?: string }) {
  return (
    <div className="rounded-lg border border-border bg-card p-3">
      {label && <div className="mb-2 text-[10px] uppercase tracking-wide text-muted-foreground">{label}</div>}
      <div className="max-h-[60vh] overflow-auto">{children}</div>
    </div>
  )
}

function Pre({ text, label }: { text: string; label?: string }) {
  return (
    <div className="rounded-lg border border-border bg-card p-3">
      {label && <div className="mb-2 text-[10px] uppercase tracking-wide text-muted-foreground">{label}</div>}
      <pre className="max-h-[60vh] overflow-auto whitespace-pre-wrap break-all font-mono text-xs">{text}</pre>
    </div>
  )
}

function Placeholder({ text }: { text: string }) {
  return (
    <div className="flex h-56 items-center justify-center rounded-lg border border-dashed border-border text-xs text-muted-foreground">
      {text}
    </div>
  )
}

function ErrorBox({ msg }: { msg: string }) {
  return (
    <div className="flex items-start gap-2 rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">
      <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
      <span className="break-all font-mono">{msg}</span>
    </div>
  )
}

function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}
