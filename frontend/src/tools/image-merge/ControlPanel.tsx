import { useRef, useState, type ReactNode } from 'react'
import { Upload } from 'lucide-react'
import { cn } from '@/lib/utils'
import { ModeToggle } from '@/components/tool/ModeToggle'
import type { MergeSettings } from './types'

const BG_SWATCHES = ['#ffffff', '#000000', '#f5f5f5', '#333333', '#667eea', '#4ecdc4']

interface Props {
  settings: MergeSettings
  set: <K extends keyof MergeSettings>(key: K, value: MergeSettings[K]) => void
  onAddFiles: (files: FileList | File[]) => void
}

export function ControlPanel({ settings: s, set, onAddFiles }: Props) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [dragover, setDragover] = useState(false)

  return (
    <div className="space-y-5">
      {/* 上传 */}
      <div
        onClick={() => inputRef.current?.click()}
        onDragOver={(e) => {
          e.preventDefault()
          setDragover(true)
        }}
        onDragLeave={() => setDragover(false)}
        onDrop={(e) => {
          e.preventDefault()
          setDragover(false)
          if (e.dataTransfer.files.length) onAddFiles(e.dataTransfer.files)
        }}
        className={cn(
          'flex cursor-pointer flex-col items-center gap-1.5 rounded-lg border-2 border-dashed px-4 py-6 text-center transition-colors',
          dragover ? 'border-info bg-info/5' : 'border-border hover:border-info/60 hover:bg-secondary/40',
        )}
      >
        <Upload className="h-6 w-6 text-info" />
        <div className="text-sm">点击 / 拖拽 / 粘贴图片</div>
        <div className="text-[11px] text-muted-foreground">支持 JPG · PNG · GIF · WebP（多选）</div>
        <input
          ref={inputRef}
          type="file"
          accept="image/*"
          multiple
          className="hidden"
          onChange={(e) => {
            if (e.target.files?.length) onAddFiles(e.target.files)
            e.target.value = ''
          }}
        />
      </div>

      {/* 布局 */}
      <Field label="布局方式">
        <ModeToggle
          value={s.layout}
          onChange={(v) => set('layout', v)}
          options={[
            { value: 'vertical', label: '竖排' },
            { value: 'horizontal', label: '横排' },
            { value: 'grid', label: '网格' },
          ]}
        />
      </Field>

      {s.layout === 'grid' && (
        <>
          <Field label="网格列数" hint={s.autoColumns ? '自动取近正方形(如 4 张→2×2),最后一排居中' : undefined}>
            <ModeToggle
              value={s.autoColumns ? 'auto' : 'manual'}
              onChange={(v) => set('autoColumns', v === 'auto')}
              options={[
                { value: 'auto', label: '自动' },
                { value: 'manual', label: '手动' },
              ]}
            />
          </Field>
          {!s.autoColumns && (
            <Field label={`列数：${s.columns}`}>
              <input
                type="range"
                min={1}
                max={8}
                value={s.columns}
                onChange={(e) => set('columns', parseInt(e.target.value))}
                className="w-full accent-info"
              />
            </Field>
          )}
          <Field label="格内缩放">
            <ModeToggle
              value={s.gridFit}
              onChange={(v) => set('gridFit', v)}
              options={[
                { value: 'contain', label: '保比留白' },
                { value: 'cover', label: '裁切铺满' },
              ]}
            />
          </Field>
        </>
      )}

      {/* 尺寸对齐 */}
      <Field
        label="尺寸对齐"
        hint={
          s.align === 'min'
            ? '对齐到最小尺寸，绝不放大（最清晰）'
            : s.align === 'max'
              ? '对齐到最大尺寸，小图会被放大（可能变糊）'
              : '指定目标尺寸'
        }
      >
        <ModeToggle
          value={s.align}
          onChange={(v) => set('align', v)}
          options={[
            { value: 'min', label: '最小' },
            { value: 'max', label: '最大' },
            { value: 'custom', label: '自定义' },
          ]}
        />
      </Field>
      {s.align === 'custom' && (
        <Field label={s.layout === 'horizontal' ? '目标高度 (px)' : s.layout === 'grid' ? '格子边长 (px)' : '目标宽度 (px)'}>
          <NumberInput value={s.customSize} min={1} max={20000} onChange={(v) => set('customSize', v)} />
        </Field>
      )}

      {/* 间距 / 外边距 / 圆角 */}
      <Field label={`图片间距：${s.gap} px`}>
        <input type="range" min={0} max={80} value={s.gap} onChange={(e) => set('gap', parseInt(e.target.value))} className="w-full accent-info" />
      </Field>
      <Field label={`整体外边距：${s.margin} px`}>
        <input type="range" min={0} max={120} value={s.margin} onChange={(e) => set('margin', parseInt(e.target.value))} className="w-full accent-info" />
      </Field>
      <Field label={`圆角：${s.radius} px`}>
        <input type="range" min={0} max={100} value={s.radius} onChange={(e) => set('radius', parseInt(e.target.value))} className="w-full accent-info" />
      </Field>

      {/* 背景色 */}
      <Field label="背景颜色">
        <div className="flex flex-wrap items-center gap-2">
          {BG_SWATCHES.map((c) => (
            <button
              key={c}
              type="button"
              onClick={() => set('bg', c)}
              title={c}
              className={cn(
                'h-7 w-7 rounded-md border transition-transform hover:scale-110',
                s.bg === c ? 'border-foreground ring-2 ring-info/40' : 'border-border',
              )}
              style={{ background: c }}
            />
          ))}
          {/* 透明 */}
          <button
            type="button"
            onClick={() => set('bg', 'transparent')}
            title="透明背景（仅 PNG/WebP 生效）"
            className={cn(
              'flex h-7 w-7 items-center justify-center rounded-md border bg-[conic-gradient(#ccc_90deg,#fff_90deg_180deg,#ccc_180deg_270deg,#fff_270deg)] bg-[length:10px_10px] text-[9px] font-medium text-foreground/70 transition-transform hover:scale-110',
              s.bg === 'transparent' ? 'border-foreground ring-2 ring-info/40' : 'border-border',
            )}
          >
            透
          </button>
          {/* 自定义颜色 */}
          <label
            className={cn(
              'relative h-7 w-7 cursor-pointer overflow-hidden rounded-md border transition-transform hover:scale-110',
              !BG_SWATCHES.includes(s.bg) && s.bg !== 'transparent'
                ? 'border-foreground ring-2 ring-info/40'
                : 'border-border',
            )}
            title="自定义颜色"
            style={{ background: !BG_SWATCHES.includes(s.bg) && s.bg !== 'transparent' ? s.bg : undefined }}
          >
            <input
              type="color"
              value={!BG_SWATCHES.includes(s.bg) && s.bg !== 'transparent' ? s.bg : '#888888'}
              onChange={(e) => set('bg', e.target.value)}
              className="absolute inset-0 h-full w-full cursor-pointer opacity-0"
            />
            {(BG_SWATCHES.includes(s.bg) || s.bg === 'transparent') && (
              <span className="pointer-events-none absolute inset-0 flex items-center justify-center text-[13px] text-muted-foreground">
                +
              </span>
            )}
          </label>
        </div>
      </Field>

      {/* 输出格式 */}
      <Field label="输出格式" hint={s.bg === 'transparent' && s.format === 'jpeg' ? 'JPEG 不支持透明，将填白底' : undefined}>
        <ModeToggle
          value={s.format}
          onChange={(v) => set('format', v)}
          options={[
            { value: 'png', label: 'PNG' },
            { value: 'jpeg', label: 'JPEG' },
            { value: 'webp', label: 'WebP' },
          ]}
        />
      </Field>
      {s.format !== 'png' && (
        <Field label={`画质：${Math.round(s.quality * 100)}%`}>
          <input
            type="range"
            min={10}
            max={100}
            value={Math.round(s.quality * 100)}
            onChange={(e) => set('quality', parseInt(e.target.value) / 100)}
            className="w-full accent-info"
          />
        </Field>
      )}

      {/* 输出最长边上限 */}
      <Field label="输出最长边上限 (px)" hint="成品超过此值会整体等比缩小，防止画布过大失败">
        <NumberInput value={s.maxSide} min={512} max={16384} step={256} onChange={(v) => set('maxSide', v)} />
      </Field>
    </div>
  )
}

function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="space-y-1.5">
      <div className="text-xs font-medium text-foreground/80">{label}</div>
      {children}
      {hint && <div className="text-[11px] text-muted-foreground">{hint}</div>}
    </div>
  )
}

function NumberInput({
  value,
  min,
  max,
  step,
  onChange,
}: {
  value: number
  min: number
  max: number
  step?: number
  onChange: (v: number) => void
}) {
  return (
    <input
      type="number"
      value={value}
      min={min}
      max={max}
      step={step ?? 1}
      onChange={(e) => {
        const v = parseInt(e.target.value)
        if (Number.isFinite(v)) onChange(Math.min(max, Math.max(min, v)))
      }}
      className="w-full rounded-md border border-border bg-background px-2.5 py-1.5 text-sm tabular-nums outline-none focus:border-info/60"
    />
  )
}
