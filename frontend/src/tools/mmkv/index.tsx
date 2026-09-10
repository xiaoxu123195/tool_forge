import { useMemo, useState } from 'react'
import { AlertCircle, Database, Download, FolderOpen, Lock, Search } from 'lucide-react'
import { ToolShell } from '@/components/tool/ToolShell'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { downloadText } from '@/lib/download'
import { useNativeFileDrop } from '@/lib/useNativeFileDrop'
import { useMmkvStore, type TypesMap } from '@/stores/mmkv'
import {
  ParseMMKVFile,
  PickLocalFile,
  ReadMMKVValueHex,
} from '../../../wailsjs/go/main/App'
import type { mmkv } from '../../../wailsjs/go/models'
import { meta } from './meta'
import { defaultTypeOf, displayOf, nextTypeOf } from './valueTypes'
import { ValueCell } from './components/ValueCell'
import { DetailModal, type DetailContext } from './components/DetailModal'
import { DecryptPanel } from './components/DecryptPanel'

// 解析、解密、类型推断全在 Go 里(backend/tools/mmkv)。这个页面以前自带一整套
// TypeScript 实现,和后端那份是两套代码 —— 同一个文件两边读出不一样的值时
// 谁也说不清该信哪个。现在页面只负责显示,和 MCP 工具走同一条代码路径。

export default function MmkvTool() {
  const file = useMmkvStore((s) => s.file)
  const typesByKey = useMmkvStore((s) => s.typesByKey)
  const search = useMmkvStore((s) => s.search)
  const keyColWidth = useMmkvStore((s) => s.keyColWidth)
  const setFile = useMmkvStore((s) => s.setFile)
  const setTypesByKey = useMmkvStore((s) => s.setTypesByKey)
  const cycleTypeAction = useMmkvStore((s) => s.cycleType)
  const setSearch = useMmkvStore((s) => s.setSearch)
  const setKeyColWidth = useMmkvStore((s) => s.setKeyColWidth)
  const reset = useMmkvStore((s) => s.reset)

  const [error, setError] = useState('')
  const [detail, setDetail] = useState<DetailContext | null>(null)
  const [decryptOpen, setDecryptOpen] = useState(false)
  const [busy, setBusy] = useState(false)

  const applyResult = (
    path: string,
    result: mmkv.FileResult,
    crcPath = '',
    keyHex = ''
  ) => {
    // 每个值的初始类型用后端猜的那个,而不是一律显示原始十六进制 ——
    // 后端本来就把"最可能是什么"算出来了,让人再一个个点回去没有意义
    const initial: TypesMap = {}
    for (const e of result.entries) {
      initial[e.key] = e.values.map(defaultTypeOf)
    }
    setFile({ path, crcPath, keyHex, result })
    setTypesByKey(initial)
    setError('')
  }

  const openPath = async (path: string) => {
    setBusy(true)
    try {
      applyResult(path, await ParseMMKVFile(path, '', ''))
    } catch (e) {
      setFile(null)
      setError(String(e))
    } finally {
      setBusy(false)
    }
  }

  const decryptAndLoad = async (mmkvPath: string, crcPath: string, keyHex: string) => {
    const res = await ParseMMKVFile(mmkvPath, crcPath, keyHex)
    applyResult(mmkvPath, res, crcPath, keyHex)
    setDecryptOpen(false)
  }

  // 原生拖放给的是绝对路径(HTML5 拖放给不了),后端直接读文件,
  // 50MB 的 MMKV 不用先在 JS 里读一遍再传过桥
  useNativeFileDrop((paths) => {
    if (paths.length > 0) openPath(paths[0])
  })

  const pickFile = async () => {
    const p = await PickLocalFile('选择 MMKV 文件')
    if (p) openPath(p)
  }

  const entries = file?.result.entries ?? []

  const filteredEntries = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) return entries
    return entries.filter((e) => e.key.toLowerCase().includes(q))
  }, [entries, search])

  const getType = (key: string, index: number, value: mmkv.Value): string =>
    typesByKey[key]?.[index] ?? defaultTypeOf(value)

  const cycleType = (key: string, index: number, value: mmkv.Value) => {
    cycleTypeAction(key, index, nextTypeOf(value, getType(key, index, value)))
  }

  const exportJson = () => {
    if (!file) return
    const out: Record<string, unknown> = {}
    for (const entry of entries) {
      out[entry.key] = entry.values.map((v, i) => {
        const t = getType(entry.key, i, v)
        return { type: t, value: displayOf(v, t) }
      })
    }
    downloadText(
      JSON.stringify(out, null, 2),
      `${file.result.name}.json`,
      'application/json;charset=utf-8'
    )
  }

  const startResize = (e: React.MouseEvent) => {
    e.preventDefault()
    const startX = e.clientX
    const startWidth = keyColWidth
    const onMove = (ev: MouseEvent) => {
      const next = Math.max(100, Math.min(700, startWidth + ev.clientX - startX))
      setKeyColWidth(next)
    }
    const onUp = () => {
      document.removeEventListener('mousemove', onMove)
      document.removeEventListener('mouseup', onUp)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
    document.addEventListener('mousemove', onMove)
    document.addEventListener('mouseup', onUp)
  }

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      onClear={() => {
        reset()
        setError('')
        setDetail(null)
        setDecryptOpen(false)
      }}
      actions={
        <div className="flex items-center gap-1.5">
          <Button variant="outline" size="sm" onClick={pickFile} disabled={busy}>
            <FolderOpen className="h-3.5 w-3.5" />
            打开
          </Button>
          <Button
            variant={decryptOpen ? 'default' : 'outline'}
            size="sm"
            onClick={() => setDecryptOpen((v) => !v)}
          >
            <Lock className="h-3.5 w-3.5" />
            加密打开
          </Button>
          <Button variant="ghost" size="sm" onClick={exportJson} disabled={!file}>
            <Download className="h-3.5 w-3.5" />
            导出 JSON
          </Button>
        </div>
      }
    >
      <div
        style={{ ['--wails-drop-target' as never]: 'drop' }}
        className="relative flex h-full flex-col gap-3 rounded-lg"
      >
        {decryptOpen && (
          <DecryptPanel
            initialMmkvPath={file?.path}
            onCancel={() => setDecryptOpen(false)}
            onSubmit={decryptAndLoad}
          />
        )}

        {file ? (
          <>
            <div className="flex flex-wrap items-center justify-between gap-3 text-xs">
              <div className="flex items-center gap-3">
                <span className="font-medium">{file.result.name}</span>
                {file.result.encrypted && (
                  <span className="inline-flex items-center gap-1 rounded-sm bg-emerald-100 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
                    <Lock className="h-3 w-3" />
                    已解密
                  </span>
                )}
                <span className="text-muted-foreground">
                  共 {entries.length} 个 key
                  {file.result.removedCount > 0 && (
                    <> · {file.result.removedCount} 个删除标记</>
                  )}
                  {' · '}
                  {(file.result.size / 1024).toFixed(1)} KB
                </span>
              </div>
              <div className="flex items-center gap-2">
                <div className="relative">
                  <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                  <input
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    placeholder="搜索 key"
                    spellCheck={false}
                    className="h-7 w-56 rounded-md border border-input bg-background pl-7 pr-2 text-xs outline-none focus:ring-1 focus:ring-ring"
                  />
                </div>
              </div>
            </div>

            {error && (
              <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
                {error}
              </div>
            )}

            <div className="flex-1 overflow-auto rounded-lg border border-border bg-card">
              <table className="w-full table-fixed border-separate border-spacing-0">
                <colgroup>
                  <col style={{ width: `${keyColWidth}px` }} />
                  <col />
                </colgroup>
                <thead className="sticky top-0 z-10 bg-muted/60 text-left text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                  <tr>
                    <th className="relative border-b border-border px-3 py-2">
                      Key
                      {/* 拖拽句柄：宽 5px 更容易命中，hover 时显色 */}
                      <div
                        onMouseDown={startResize}
                        title="拖动调整 Key 列宽"
                        className="absolute right-[-2px] top-0 z-20 h-full w-[5px] cursor-col-resize bg-transparent hover:bg-primary/60"
                      />
                    </th>
                    <th className="border-b border-border px-3 py-2">Values</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredEntries.length === 0 ? (
                    <tr>
                      <td
                        colSpan={2}
                        className="px-4 py-8 text-center text-xs text-muted-foreground"
                      >
                        {search ? '没有匹配的 key' : '空 MMKV 文件'}
                      </td>
                    </tr>
                  ) : (
                    filteredEntries.map((entry) => (
                      <tr key={entry.key} className="align-top">
                        <td className="border-b border-border px-3 py-2 font-mono text-[12.5px]">
                          <div className="truncate" title={entry.key}>
                            {entry.key}
                          </div>
                          {entry.values.length > 1 && (
                            <div className="mt-0.5 text-[10px] text-muted-foreground">
                              {entry.values.length} 个历史值
                            </div>
                          )}
                        </td>
                        <td className="min-w-0 border-b border-border px-2 py-1.5">
                          <div className="space-y-1">
                            {entry.values.map((v, i) => (
                              <ValueCell
                                key={i}
                                value={v}
                                type={getType(entry.key, i, v)}
                                onCycle={() => cycleType(entry.key, i, v)}
                                onExpand={() =>
                                  setDetail({
                                    key: entry.key,
                                    index: i,
                                    total: entry.values.length,
                                    value: v,
                                    type: getType(entry.key, i, v),
                                    // 表格里的十六进制是截断过的。要完整的就回后端再读一次,
                                    // 而不是让每一行都背着一份完整字节过桥
                                    fullHex: () =>
                                      ReadMMKVValueHex(
                                        file.path,
                                        file.crcPath,
                                        file.keyHex,
                                        entry.key,
                                        i
                                      ),
                                  })
                                }
                              />
                            ))}
                          </div>
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </>
        ) : (
          <EmptyState onPick={pickFile} error={error} />
        )}
      </div>

      {detail && <DetailModal ctx={detail} onClose={() => setDetail(null)} />}
    </ToolShell>
  )
}

function EmptyState({ onPick, error }: { onPick: () => void; error: string }) {
  return (
    <div className="flex flex-1 items-center justify-center">
      <div className="flex flex-col items-center gap-4 text-center">
        <Database className="h-12 w-12 text-muted-foreground" />
        <div className="space-y-1">
          <div className="text-sm font-medium">打开一个 MMKV 文件</div>
          <div className="text-xs text-muted-foreground">
            将 MMKV 文件拖到此处，或点击下方按钮选择（最大 50 MB）
          </div>
        </div>
        <Button onClick={onPick} size="sm">
          <FolderOpen className="h-3.5 w-3.5" />
          选择文件
        </Button>
        {error && (
          <div
            className={cn(
              'mt-2 flex max-w-md items-start gap-1.5 rounded-md border border-destructive/30',
              'bg-destructive/10 px-3 py-1.5 text-left text-xs text-destructive'
            )}
          >
            <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            <span>{error}</span>
          </div>
        )}
        <div className="mt-4 max-w-md space-y-1 text-left text-[11px] text-muted-foreground">
          <div className="font-medium text-foreground/80">基本使用</div>
          <div>· 支持未加密的 MMKV 文件（拖放 / 点击「打开」）</div>
          <div>· 同一个 key 的历史值会各自一行展示</div>
          <div>· 每个值默认按最可能的类型显示，点类型徽章可在解得通的类型间循环</div>
          <div>· 点击右侧 expand 图标弹出详情，会列出这串字节所有说得通的读法</div>
          <div>· 拖动 Key / Values 列之间的分隔线可调整列宽</div>
          <div>· 切换到别的工具再回来，文件不会丢（刷新页面会丢）</div>

          <div className="mt-3 font-medium text-foreground/80">加密文件（AES-128-CFB）</div>
          <div>
            · 点击工具栏「<Lock className="inline h-3 w-3" /> 加密打开」展开解密面板
          </div>
          <div>· 同时选择加密的 MMKV 文件和配对的 .crc 文件</div>
          <div>· 输入 AES key 的十六进制（不足 16 字节自动补 0，多余截断）</div>
          <div>
            · IV 取自 .crc 文件的第 12~27 字节，工具会自动处理；头 4 字节不加密
          </div>
          <div>· 解密后按正常 MMKV 流程解析，成功后状态栏会有「已解密」标记</div>
        </div>
      </div>
    </div>
  )
}
