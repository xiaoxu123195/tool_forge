import { useState } from 'react'
import { ChevronDown, ChevronRight, Clock, Copy } from 'lucide-react'
import { cn } from '@/lib/utils'
import { copyText } from './lib'
import type { protobuf } from '../../../wailsjs/go/models'

type Kind = 'varint' | 'i64' | 'i32' | 'message' | 'group' | 'string' | 'bytes' | 'packed'

const KIND_BADGE: Record<string, string> = {
  message: 'bg-violet-500/12 text-violet-600 dark:text-violet-300',
  group: 'bg-violet-500/12 text-violet-600 dark:text-violet-300',
  string: 'bg-emerald-500/12 text-emerald-600 dark:text-emerald-300',
  varint: 'bg-sky-500/12 text-sky-600 dark:text-sky-300',
  i64: 'bg-sky-500/12 text-sky-600 dark:text-sky-300',
  i32: 'bg-sky-500/12 text-sky-600 dark:text-sky-300',
  bytes: 'bg-amber-500/12 text-amber-600 dark:text-amber-300',
  packed: 'bg-teal-500/12 text-teal-600 dark:text-teal-300',
}

const KIND_LABEL: Record<string, string> = {
  message: 'message',
  group: 'group',
  string: 'string',
  varint: 'varint',
  i64: 'i64',
  i32: 'i32',
  bytes: 'bytes',
  packed: 'packed',
}

export function RawTree({ nodes }: { nodes: protobuf.Node[] }) {
  if (!nodes || nodes.length === 0) {
    return <div className="text-xs text-muted-foreground">(空消息)</div>
  }
  return (
    <div className="font-mono text-xs">
      {nodes.map((n, i) => (
        <NodeRow key={i} node={n} depth={0} />
      ))}
    </div>
  )
}

function NodeRow({ node, depth }: { node: protobuf.Node; depth: number }) {
  const isLen = node.wire === 2
  const alts = altKinds(node)
  const [view, setView] = useState<Kind>((node.type as Kind) || 'bytes')
  const [open, setOpen] = useState(depth < 6)

  const kind: Kind = isLen ? view : ((node.type as Kind) || 'bytes')
  const isMsg = kind === 'message' || kind === 'group'

  return (
    <div className="border-l border-border/60 pl-3">
      <div className="group flex flex-wrap items-baseline gap-x-2 gap-y-0.5 py-[3px]">
        {/* 展开箭头(仅消息) */}
        {isMsg ? (
          <button
            onClick={() => setOpen((o) => !o)}
            className="-ml-1 inline-flex h-4 w-4 items-center justify-center rounded text-muted-foreground hover:text-foreground"
          >
            {open ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
          </button>
        ) : (
          <span className="inline-block w-3" />
        )}

        <span className="font-semibold text-foreground">{node.field}</span>
        <span className={cn('rounded px-1.5 py-0.5 text-[10px] font-medium', KIND_BADGE[kind])}>
          {KIND_LABEL[kind] ?? kind}
        </span>

        {/* 值 */}
        {isMsg ? (
          <span className="text-muted-foreground">
            {'{'} {node.message?.length ?? 0} 字段 {open ? '' : '…}'}
          </span>
        ) : (
          <NodeValue node={node} kind={kind} />
        )}

        {/* 时间戳标注 */}
        {node.time && (
          <span className="inline-flex items-center gap-1 rounded bg-orange-500/10 px-1.5 py-0.5 text-[10px] text-orange-600 dark:text-orange-300">
            <Clock className="h-2.5 w-2.5" />
            {node.time}
            <span className="opacity-60">({node.timeUnit})</span>
          </span>
        )}

        {/* LEN 多解读切换 */}
        {isLen && alts.length > 1 && (
          <span className="ml-auto inline-flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
            {alts.map((a) => (
              <button
                key={a}
                onClick={() => setView(a)}
                className={cn(
                  'rounded px-1 py-0.5 text-[9px] font-medium',
                  view === a
                    ? 'bg-primary text-primary-foreground'
                    : 'text-muted-foreground hover:bg-secondary',
                )}
              >
                {a}
              </button>
            ))}
          </span>
        )}
      </div>

      {isMsg && open && node.message && (
        <div className="ml-1">
          {node.message.map((c, i) => (
            <NodeRow key={i} node={c} depth={depth + 1} />
          ))}
        </div>
      )}
    </div>
  )
}

function NodeValue({ node, kind }: { node: protobuf.Node; kind: Kind }) {
  switch (kind) {
    case 'string':
      return (
        <>
          <span className="whitespace-pre-wrap break-all text-emerald-700 dark:text-emerald-300">
            "{node.str}"
          </span>
          <CopyBtn text={node.str ?? ''} />
        </>
      )
    case 'bytes':
      return (
        <>
          <span className="break-all text-amber-700/90 dark:text-amber-300/90">{node.hex}</span>
          <span className="text-[10px] text-muted-foreground">({(node.hex?.length ?? 0) / 2}B)</span>
          <CopyBtn text={node.hex ?? ''} />
        </>
      )
    case 'packed':
      return (
        <span className="break-all text-teal-700 dark:text-teal-300">
          [{(node.packed ?? []).map((p) => p.uint).join(', ')}]
        </span>
      )
    case 'varint':
      return (
        <span className="break-all">
          <span className="text-foreground">{node.uint}</span>
          <Alt label="signed" value={node.sint} />
          <Alt label="zigzag" value={node.zigzag} />
          {node.bool !== undefined && <Alt label="bool" value={String(node.bool)} />}
        </span>
      )
    case 'i64':
      return (
        <span className="break-all">
          <span className="text-foreground">{node.uint}</span>
          <Alt label="signed" value={node.sint} />
          <Alt label="double" value={node.double} />
          <Alt label="hex" value={node.hex} />
        </span>
      )
    case 'i32':
      return (
        <span className="break-all">
          <span className="text-foreground">{node.uint}</span>
          <Alt label="signed" value={node.sint} />
          <Alt label="float" value={node.float} />
          <Alt label="hex" value={node.hex} />
        </span>
      )
    default:
      return null
  }
}

function Alt({ label, value }: { label: string; value?: string }) {
  if (value === undefined || value === '') return null
  return (
    <span className="ml-2 text-[10px] text-muted-foreground">
      {label}=<span className="text-foreground/70">{value}</span>
    </span>
  )
}

function CopyBtn({ text }: { text: string }) {
  if (!text) return null
  return (
    <button
      onClick={() => copyText(text)}
      title="复制"
      className="opacity-0 transition-opacity hover:text-foreground group-hover:opacity-60"
    >
      <Copy className="h-3 w-3" />
    </button>
  )
}

/** LEN 字段可切换的解读种类。 */
function altKinds(node: protobuf.Node): Kind[] {
  if (node.wire !== 2) return []
  const out: Kind[] = []
  if (node.msgOk) out.push('message')
  if (node.strOk) out.push('string')
  out.push('bytes')
  if (node.packedOk) out.push('packed')
  return out
}
