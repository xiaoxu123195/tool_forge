import { useState } from 'react'
import {
  fileToFileBlock,
  fileToImageBlock,
  isImageFile,
  MAX_FILES_PER_MESSAGE,
  MAX_IMAGE_BYTES,
} from './file-parsers'
import type { FileBlock, ImageBlock, ModelSpec } from './types'

/**
 * 待发送附件的收集逻辑:选文件 / 粘贴 / 拖入三条入口,统一走 ingestFiles。
 *
 * 抽成 hook 是因为这里的规则跟 UI 没关系,而且不止一处会碰到:数量上限、单文件大小、
 * 图片要模型支持 vision 才收。和渲染代码混在一起时很难看清到底拦了什么。
 */
export function useAttachments({
  spec,
  modelId,
  onError,
}: {
  /** 当前模型能力画像;为 null(还没拉到)时不做 vision 拦截 */
  spec: ModelSpec | null
  modelId: string
  /** 收不下的文件统一在这里报给用户 */
  onError: (title: string, message: string) => Promise<unknown> | void
}) {
  const [pendingImages, setPendingImages] = useState<ImageBlock[]>([])
  const [pendingFiles, setPendingFiles] = useState<FileBlock[]>([])

  const ingestFiles = async (files: FileList | File[]) => {
    const list = Array.from(files)
    if (list.length === 0) return
    if (pendingImages.length + pendingFiles.length + list.length > MAX_FILES_PER_MESSAGE) {
      await onError(
        '附件数量超限',
        `单条消息最多 ${MAX_FILES_PER_MESSAGE} 个附件(图片/文件合计)`,
      )
      return
    }
    const newImages: ImageBlock[] = []
    const newFiles: FileBlock[] = []
    const errors: string[] = []
    // 图片要模型本身支持才有意义;文本类附件最终是拼进 prompt 的纯文本,任何模型都吃得下。
    // spec 还没拉到时不拦 —— 宁可让请求去撞错误,也别因为一次没拉到能力就挡住用户。
    const visionOK = !spec || spec.capabilities.includes('vision')
    for (const f of list) {
      try {
        if (isImageFile(f)) {
          if (!visionOK) {
            errors.push(`「${f.name}」当前模型 ${modelId} 不支持图片输入`)
            continue
          }
          if (f.size > MAX_IMAGE_BYTES) {
            errors.push(`「${f.name}」超过 5 MB`)
            continue
          }
          newImages.push(await fileToImageBlock(f))
        } else {
          newFiles.push(await fileToFileBlock(f))
        }
      } catch (e: any) {
        errors.push(`「${f.name}」${e?.message ?? e}`)
      }
    }
    if (newImages.length > 0) {
      setPendingImages((prev) => [...prev, ...newImages])
    }
    if (newFiles.length > 0) {
      setPendingFiles((prev) => [...prev, ...newFiles])
    }
    if (errors.length > 0) {
      await onError('部分文件无法上传', errors.join('\n'))
    }
  }

  const onPickFiles = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files) void ingestFiles(e.target.files)
    e.target.value = ''
  }

  const onPaste = (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const items = e.clipboardData?.items
    if (!items) return
    // 任意 kind=file 都收(图/PDF/docx/...);text 走默认粘贴
    const files: File[] = []
    for (let i = 0; i < items.length; i++) {
      const it = items[i]
      if (it.kind === 'file') {
        const f = it.getAsFile()
        if (f) files.push(f)
      }
    }
    if (files.length > 0) {
      e.preventDefault()
      void ingestFiles(files)
    }
  }

  const onDrop = (e: React.DragEvent<HTMLDivElement>) => {
    if (e.dataTransfer.files.length > 0) {
      e.preventDefault()
      void ingestFiles(e.dataTransfer.files)
    }
  }

  const removePendingImage = (idx: number) => {
    setPendingImages((prev) => prev.filter((_, i) => i !== idx))
  }

  const removePendingFile = (idx: number) => {
    setPendingFiles((prev) => prev.filter((_, i) => i !== idx))
  }

  return {
    pendingImages,
    pendingFiles,
    /** 发送失败时把附件塞回去用 */
    restore: (images: ImageBlock[], files: FileBlock[]) => {
      setPendingImages(images)
      setPendingFiles(files)
    },
    /** 发送成功后清空 */
    clear: () => {
      setPendingImages([])
      setPendingFiles([])
    },
    onPickFiles,
    onPaste,
    onDrop,
    removePendingImage,
    removePendingFile,
    count: pendingImages.length + pendingFiles.length,
  }
}
