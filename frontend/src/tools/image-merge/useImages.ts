import { useCallback, useState } from 'react'
import type { MergeImage, Rotation } from './types'

const THUMB_MAX = 120

function makeId(): string {
  try {
    if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) return crypto.randomUUID()
  } catch {
    // ignore
  }
  return `img-${Date.now()}-${Math.round(Math.random() * 1e9)}`
}

/** 把(已纠正 EXIF 的)原图按 rotation 烘焙成可绘制源,返回旋转后的源与尺寸。 */
function bake(orig: ImageBitmap, rotation: Rotation): {
  image: CanvasImageSource
  width: number
  height: number
} {
  if (rotation === 0) return { image: orig, width: orig.width, height: orig.height }
  const swap = rotation === 90 || rotation === 270
  const w = swap ? orig.height : orig.width
  const h = swap ? orig.width : orig.height
  const canvas = document.createElement('canvas')
  canvas.width = w
  canvas.height = h
  const ctx = canvas.getContext('2d')!
  ctx.translate(w / 2, h / 2)
  ctx.rotate((rotation * Math.PI) / 180)
  ctx.drawImage(orig, -orig.width / 2, -orig.height / 2)
  return { image: canvas, width: w, height: h }
}

/** 由旋转后的源生成列表缩略图 dataURL */
function makeThumb(image: CanvasImageSource, w: number, h: number): string {
  const scale = Math.min(1, THUMB_MAX / Math.max(w, h))
  const tw = Math.max(1, Math.round(w * scale))
  const th = Math.max(1, Math.round(h * scale))
  const canvas = document.createElement('canvas')
  canvas.width = tw
  canvas.height = th
  const ctx = canvas.getContext('2d')!
  ctx.imageSmoothingEnabled = true
  ctx.imageSmoothingQuality = 'high'
  ctx.drawImage(image, 0, 0, w, h, 0, 0, tw, th)
  return canvas.toDataURL('image/jpeg', 0.7)
}

async function decode(file: File): Promise<MergeImage> {
  let orig: ImageBitmap
  try {
    orig = await createImageBitmap(file, { imageOrientation: 'from-image' })
  } catch {
    // 兜底:个别格式/环境不支持 imageOrientation 选项,退回默认
    orig = await createImageBitmap(file)
  }
  const { image, width, height } = bake(orig, 0)
  return {
    id: makeId(),
    name: file.name,
    size: file.size,
    rotation: 0,
    orig,
    image,
    width,
    height,
    thumb: makeThumb(image, width, height),
  }
}

export function useImages() {
  const [images, setImages] = useState<MergeImage[]>([])

  /** 加图,返回失败文件的错误信息(成功为空数组) */
  const addFiles = useCallback(async (input: FileList | File[]): Promise<string[]> => {
    const files = Array.from(input).filter((f) => f.type.startsWith('image/'))
    const errors: string[] = []
    const decoded: MergeImage[] = []
    await Promise.all(
      files.map(async (f) => {
        try {
          decoded.push(await decode(f))
        } catch {
          errors.push(`${f.name} 解码失败`)
        }
      }),
    )
    if (decoded.length) setImages((prev) => [...prev, ...decoded])
    return errors
  }, [])

  const remove = useCallback((id: string) => {
    setImages((prev) => {
      const target = prev.find((i) => i.id === id)
      target?.orig.close?.()
      return prev.filter((i) => i.id !== id)
    })
  }, [])

  const clear = useCallback(() => {
    setImages((prev) => {
      prev.forEach((i) => i.orig.close?.())
      return []
    })
  }, [])

  const reorder = useCallback((from: number, to: number) => {
    setImages((prev) => {
      if (from === to || from < 0 || to < 0 || from >= prev.length || to >= prev.length) return prev
      const next = prev.slice()
      const [moved] = next.splice(from, 1)
      next.splice(to, 0, moved)
      return next
    })
  }, [])

  const rotate = useCallback((id: string) => {
    setImages((prev) =>
      prev.map((i) => {
        if (i.id !== id) return i
        const rotation = (((i.rotation + 90) % 360) as Rotation)
        const { image, width, height } = bake(i.orig, rotation)
        return { ...i, rotation, image, width, height, thumb: makeThumb(image, width, height) }
      }),
    )
  }, [])

  return { images, addFiles, remove, clear, reorder, rotate }
}
