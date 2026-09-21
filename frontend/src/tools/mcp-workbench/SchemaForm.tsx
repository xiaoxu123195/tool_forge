import { cn } from '@/lib/utils'
import type { Field } from './schema'

/**
 * 按 schema 画出来的参数表单。
 *
 * 每个字段下面都标着它在 schema 里的类型 —— 调试时"我以为它是字符串"这种误会
 * 占了一半的失败调用,而服务器返回的错误往往只说"参数无效"。
 */
export function SchemaForm({
  fields,
  values,
  errors,
  onChange,
}: {
  fields: Field[]
  values: Record<string, string>
  errors: Record<string, string>
  onChange: (name: string, value: string) => void
}) {
  if (fields.length === 0) {
    return (
      <p className="rounded-md border border-dashed border-border px-3 py-4 text-center text-[11px] text-muted-foreground">
        这个工具不需要参数，直接调用就行。
      </p>
    )
  }
  return (
    <div className="space-y-3">
      {fields.map((f) => (
        <div key={f.name}>
          <div className="mb-1 flex items-baseline gap-1.5">
            <label className="font-mono text-xs font-medium">{f.name}</label>
            {f.required && <span className="text-[10px] text-destructive">必填</span>}
            <span className="ml-auto rounded-sm bg-muted px-1 text-[10px] text-muted-foreground">
              {KIND_LABEL[f.kind]}
            </span>
          </div>
          {f.description && (
            <p className="mb-1 text-[11px] leading-relaxed text-muted-foreground">{f.description}</p>
          )}
          <Control field={f} value={values[f.name] ?? ''} onChange={(v) => onChange(f.name, v)} />
          {errors[f.name] && (
            <p className="mt-0.5 text-[11px] text-destructive">{errors[f.name]}</p>
          )}
        </div>
      ))}
    </div>
  )
}

const KIND_LABEL: Record<Field['kind'], string> = {
  string: '字符串',
  number: '数字',
  integer: '整数',
  boolean: '布尔',
  enum: '枚举',
  stringArray: '数组',
  json: 'JSON',
}

const inputCls =
  'w-full rounded-md border border-input bg-background px-2 py-1.5 font-mono text-xs outline-none focus:ring-1 focus:ring-ring'

function Control({
  field,
  value,
  onChange,
}: {
  field: Field
  value: string
  onChange: (v: string) => void
}) {
  switch (field.kind) {
    case 'boolean':
      return (
        <label className="flex cursor-pointer items-center gap-2 text-xs">
          <input
            type="checkbox"
            name={field.name}
            checked={value === 'true'}
            onChange={(e) => onChange(e.target.checked ? 'true' : 'false')}
            className="h-3.5 w-3.5"
          />
          {value === 'true' ? 'true' : 'false'}
        </label>
      )
    case 'enum':
      return (
        <select
          name={field.name}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className={cn(inputCls, 'h-8')}
        >
          <option value="">（不传）</option>
          {field.options.map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </select>
      )
    case 'stringArray':
      return (
        <textarea
          name={field.name}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          rows={3}
          spellCheck={false}
          placeholder="一行一个"
          className={cn(inputCls, 'resize-y leading-relaxed')}
        />
      )
    case 'json':
      return (
        <textarea
          name={field.name}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          rows={4}
          spellCheck={false}
          placeholder={jsonPlaceholder(field.raw)}
          className={cn(inputCls, 'resize-y leading-relaxed')}
        />
      )
    default:
      return (
        <input
          name={field.name}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          spellCheck={false}
          className={cn(inputCls, 'h-8')}
        />
      )
  }
}

/** 认不出类型的字段,把它在 schema 里的原样贴成 placeholder,省得人再去翻文档 */
function jsonPlaceholder(raw: unknown): string {
  try {
    const s = JSON.stringify(raw)
    return s && s.length <= 120 ? `schema: ${s}` : '合法的 JSON'
  } catch {
    return '合法的 JSON'
  }
}
