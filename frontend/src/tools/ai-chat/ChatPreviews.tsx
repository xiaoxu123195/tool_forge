import { X, Download } from 'lucide-react'
import { createPortal } from 'react-dom'
import { blobURL, imageSrc, formatFileSize } from './file-parsers'
import { MarkdownPreview } from '@/components/tool/MarkdownPreview'
import type { FileBlock, ImageBlock } from './types'
import { fileIcon } from './chat-utils'
import { cn } from '@/lib/utils'

// 图片 / 文件的全屏预览弹窗

export function ImagePreviewModal({ img, onClose }: { img: ImageBlock; onClose: () => void }) {
  const src = imageSrc(img)
  const onDownload = () => {
    const a = document.createElement('a')
    a.href = src
    const ext = (img.mimeType ?? 'image/png').split('/')[1] ?? 'png'
    a.download = `image-${Date.now()}.${ext}`
    a.click()
  }
  return (
    <div
      className="fixed inset-0 z-[80] flex items-center justify-center bg-black/80 p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div className="relative max-h-full max-w-full">
        <img src={src} alt="" className="max-h-[88vh] max-w-[88vw] rounded-md object-contain" />
        <div className="absolute right-2 top-2 flex gap-1">
          <button
            type="button"
            onClick={onDownload}
            className="flex h-8 w-8 items-center justify-center rounded-md bg-black/60 text-white transition-colors hover:bg-black/80"
            title="下载"
          >
            <Download className="h-4 w-4" />
          </button>
          <button
            type="button"
            onClick={onClose}
            className="flex h-8 w-8 items-center justify-center rounded-md bg-black/60 text-white transition-colors hover:bg-black/80"
            title="关闭"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      </div>
    </div>
  )
}

export function FilePreviewModal({ file, onClose }: { file: FileBlock; onClose: () => void }) {
  // 落盘后二进制只剩 ref。直接指到 /aiblob/ 让 webview 自己下,
  // 比先取回 base64、再 atob、再拼 Blob 少了两次整份拷贝
  const onDownload = () => {
    if (file.ref && !file.data) {
      const a = document.createElement('a')
      a.href = blobURL(file.ref)
      a.download = file.name
      a.click()
      return
    }
    let blob: Blob
    if (file.data) {
      const bin = atob(file.data)
      const arr = new Uint8Array(bin.length)
      for (let i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i)
      blob = new Blob([arr], { type: file.mimeType ?? 'application/octet-stream' })
    } else {
      blob = new Blob([file.text ?? ''], { type: 'text/plain;charset=utf-8' })
    }
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = file.name
    a.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
  const Icon = fileIcon(file.name)
  return (
    <div
      className="fixed inset-0 z-[80] flex items-center justify-center bg-black/40 p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div className="flex h-[80vh] w-[760px] max-w-full flex-col overflow-hidden rounded-xl border border-border bg-card shadow-2xl">
        <header className="flex h-12 shrink-0 items-center justify-between gap-3 border-b border-border px-4">
          <div className="flex min-w-0 items-center gap-2">
            <Icon className="h-4 w-4 shrink-0 text-info" />
            <span className="truncate text-sm font-semibold" title={file.name}>
              {file.name}
            </span>
            <span className="shrink-0 text-[11px] text-muted-foreground">
              {file.sizeBytes ? formatFileSize(file.sizeBytes) : ''}
              {file.text ? ` · ${file.text.length} 字` : ''}
              {file.data || file.ref ? ' · 二进制' : ''}
            </span>
          </div>
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={onDownload}
              className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              title="下载"
            >
              <Download className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              onClick={onClose}
              className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              title="关闭"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        </header>
        <div className="min-h-0 flex-1 overflow-auto p-4">
          {file.text ? (
            <pre className="whitespace-pre-wrap break-words font-mono text-xs leading-relaxed text-foreground">
              {file.text}
            </pre>
          ) : file.data || file.ref ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              <div className="text-center">
                <Icon className="mx-auto h-12 w-12 text-info/60" />
                <p className="mt-3">这是二进制文件,无法直接预览。</p>
                <p className="mt-1 text-xs">点击右上角下载查看。</p>
              </div>
            </div>
          ) : (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              (无内容)
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
