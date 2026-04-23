import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from 'react'
import CodeMirror, {
  EditorView,
  type ReactCodeMirrorRef,
} from '@uiw/react-codemirror'
import type { Extension } from '@codemirror/state'
import { foldAll, unfoldAll } from '@codemirror/language'
import { oneDark } from '@codemirror/theme-one-dark'
import { githubLight } from '@uiw/codemirror-theme-github'
import { json } from '@codemirror/lang-json'
import { xml } from '@codemirror/lang-xml'
import { markdown } from '@codemirror/lang-markdown'
import { javascript } from '@codemirror/lang-javascript'
import { yaml } from '@codemirror/lang-yaml'
import { useLayoutStore } from '@/stores/layout'
import { cn } from '@/lib/utils'

export type EditorLanguage =
  | 'json'
  | 'xml'
  | 'markdown'
  | 'javascript'
  | 'typescript'
  | 'ini'
  | 'yaml'
  | 'plaintext'

interface CodeEditorProps {
  value: string
  onChange?: (v: string) => void
  /** 语言高亮。不传则为 plaintext。 */
  language?: EditorLanguage
  readOnly?: boolean
  placeholder?: string
  /** 传 "100%" 时编辑器会填满父级高度；其他值会作为最小高度。 */
  minHeight?: string
  className?: string
  /**
   * 兼容占位——早前 CodeMirror / Monaco 时期传 extensions,
   * 保留字段避免误触发 any-import 错误。
   */
  extensions?: unknown
}

export interface CodeEditorHandle {
  foldAll(): void
  unfoldAll(): void
  focus(): void
}

function resolveDark(theme: string): boolean {
  if (theme === 'dark') return true
  if (theme === 'light') return false
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

function languageExtension(lang: EditorLanguage): Extension | null {
  switch (lang) {
    case 'json':
      return json()
    case 'xml':
      return xml()
    case 'markdown':
      return markdown()
    case 'javascript':
      return javascript({ jsx: true })
    case 'typescript':
      return javascript({ typescript: true, jsx: true })
    case 'yaml':
      return yaml()
    case 'ini':
    case 'plaintext':
    default:
      return null
  }
}

// 统一字体栈 + 小号字;参考 VS Code 的 `fontSize=13, lineHeight=20`。
const fontExtension = EditorView.theme({
  '&': { fontSize: '13px' },
  '.cm-content': {
    fontFamily:
      "'JetBrains Mono', 'Fira Code', 'Cascadia Code', 'SF Mono', 'Menlo', 'Consolas', monospace",
    lineHeight: '1.55',
  },
  '.cm-gutters': {
    fontFamily:
      "'JetBrains Mono', 'Fira Code', 'Cascadia Code', 'SF Mono', 'Menlo', 'Consolas', monospace",
  },
  '.cm-scroller': { overflow: 'auto' },
})

export const CodeEditor = forwardRef<CodeEditorHandle, CodeEditorProps>(
  function CodeEditor(
    {
      value,
      onChange,
      language = 'plaintext',
      readOnly,
      placeholder,
      minHeight = '200px',
      className,
    },
    ref,
  ) {
    const theme = useLayoutStore((s) => s.theme)
    const [dark, setDark] = useState(() => resolveDark(theme))
    const cmRef = useRef<ReactCodeMirrorRef>(null)

    useEffect(() => {
      if (theme !== 'system') {
        setDark(theme === 'dark')
        return
      }
      const mq = window.matchMedia('(prefers-color-scheme: dark)')
      const update = () => setDark(mq.matches)
      update()
      mq.addEventListener('change', update)
      return () => mq.removeEventListener('change', update)
    }, [theme])

    const fill = minHeight === '100%'

    const extensions = useMemo(() => {
      const exts: Extension[] = [EditorView.lineWrapping, fontExtension]
      if (fill) {
        exts.push(
          EditorView.theme({
            '&': { height: '100%' },
            '.cm-scroller': { overflow: 'auto' },
          }),
        )
      }
      const lang = languageExtension(language)
      if (lang) exts.push(lang)
      return exts
    }, [language, fill])

    useImperativeHandle(
      ref,
      () => ({
        foldAll() {
          const view = cmRef.current?.view
          if (!view) return
          view.focus()
          foldAll(view)
        },
        unfoldAll() {
          const view = cmRef.current?.view
          if (!view) return
          view.focus()
          unfoldAll(view)
        },
        focus() {
          cmRef.current?.view?.focus()
        },
      }),
      [],
    )

    return (
      <div
        className={cn(fill && 'flex min-h-0 flex-col', className)}
        style={!fill ? { minHeight } : undefined}
      >
        <CodeMirror
          ref={cmRef}
          value={value}
          onChange={(v) => onChange?.(v)}
          readOnly={readOnly}
          placeholder={placeholder}
          height={fill ? '100%' : undefined}
          minHeight={fill ? undefined : minHeight}
          theme={dark ? oneDark : githubLight}
          className={fill ? 'flex-1 min-h-0 overflow-hidden' : undefined}
          extensions={extensions}
          basicSetup={{
            lineNumbers: true,
            highlightActiveLine: !readOnly,
            highlightActiveLineGutter: !readOnly,
            foldGutter: true,
            bracketMatching: true,
            closeBrackets: !readOnly,
            autocompletion: !readOnly,
            indentOnInput: !readOnly,
            highlightSelectionMatches: true,
            searchKeymap: true,
            drawSelection: true,
            rectangularSelection: true,
            crosshairCursor: true,
          }}
        />
      </div>
    )
  },
)
