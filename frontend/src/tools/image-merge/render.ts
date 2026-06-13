// Canvas 绘制:把 layout 的摆放画到画布,内置清晰度护栏。
//   - imageSmoothingQuality='high' + 大幅缩小时离屏「逐次减半」抗锯齿
//   - 背景/透明、圆角、外边距
//   - 边缘对齐取整,避免无缝模式出现 1px 缝隙
// 预览与导出共用,差别只是传入的 scale 不同。

import type { LayoutResult, Placement } from './layout'
import type { MergeImage, MergeSettings, OutputFormat } from './types'

export const PREVIEW_MAX_SIDE = 1400

export interface RenderOutput {
  canvas: HTMLCanvasElement
  width: number
  height: number
}

/**
 * 把布局画到画布。scale 把自然坐标整体缩放(导出用 exportScale,预览再叠加降采样)。
 * 传入 target 时画到该画布(预览用 React 托管的 <canvas>,避免手动 DOM 与 React 协调打架);
 * 不传则新建离屏画布(导出用)。
 */
export function renderToCanvas(
  images: MergeImage[],
  s: MergeSettings,
  layout: LayoutResult,
  scale: number,
  target?: HTMLCanvasElement,
): RenderOutput {
  const width = Math.max(1, Math.round(layout.width * scale))
  const height = Math.max(1, Math.round(layout.height * scale))

  const canvas = target ?? document.createElement('canvas')
  canvas.width = width
  canvas.height = height
  const ctx = canvas.getContext('2d')!
  ctx.clearRect(0, 0, width, height)
  ctx.imageSmoothingEnabled = true
  ctx.imageSmoothingQuality = 'high'

  // 背景:透明时不填(仅 png/webp 保留 alpha);jpeg 无 alpha,透明也得填白
  const transparent = s.bg === 'transparent'
  if (!transparent) {
    ctx.fillStyle = s.bg
    ctx.fillRect(0, 0, width, height)
  } else if (s.format === 'jpeg') {
    ctx.fillStyle = '#ffffff'
    ctx.fillRect(0, 0, width, height)
  }

  const radius = Math.max(0, s.radius * scale)

  for (const p of layout.placements) {
    const img = images[p.index]
    if (!img) continue

    // 边缘取整:用左上/右下取整再求宽高,相邻图共享边界值 → 无缝
    const left = Math.round(p.dx * scale)
    const top = Math.round(p.dy * scale)
    const right = Math.round((p.dx + p.dw) * scale)
    const bottom = Math.round((p.dy + p.dh) * scale)
    const dw = Math.max(1, right - left)
    const dh = Math.max(1, bottom - top)

    if (radius > 0) {
      ctx.save()
      roundRectPath(ctx, left, top, dw, dh, Math.min(radius, dw / 2, dh / 2))
      ctx.clip()
      drawHQ(ctx, img.image, p.sx, p.sy, p.sw, p.sh, left, top, dw, dh)
      ctx.restore()
    } else {
      drawHQ(ctx, img.image, p.sx, p.sy, p.sw, p.sh, left, top, dw, dh)
    }
  }

  return { canvas, width, height }
}

/** 高质量绘制:目标远小于源时,先离屏逐次减半再画,避免一次性大比例缩小的锯齿。 */
function drawHQ(
  ctx: CanvasRenderingContext2D,
  src: CanvasImageSource,
  sx: number,
  sy: number,
  sw: number,
  sh: number,
  dx: number,
  dy: number,
  dw: number,
  dh: number,
): void {
  if (sw <= dw * 2 && sh <= dh * 2) {
    ctx.drawImage(src, sx, sy, sw, sh, dx, dy, dw, dh)
    return
  }

  // 先把源裁剪区按 1:1 落到临时画布
  let cur = document.createElement('canvas')
  cur.width = Math.max(1, Math.round(sw))
  cur.height = Math.max(1, Math.round(sh))
  let cctx = cur.getContext('2d')!
  cctx.imageSmoothingEnabled = true
  cctx.imageSmoothingQuality = 'high'
  cctx.drawImage(src, sx, sy, sw, sh, 0, 0, cur.width, cur.height)

  // 逐次减半,直到接近目标(不小于目标)
  while (cur.width > dw * 2 && cur.height > dh * 2) {
    const nw = Math.max(Math.round(dw), Math.round(cur.width / 2))
    const nh = Math.max(Math.round(dh), Math.round(cur.height / 2))
    const next = document.createElement('canvas')
    next.width = nw
    next.height = nh
    const nctx = next.getContext('2d')!
    nctx.imageSmoothingEnabled = true
    nctx.imageSmoothingQuality = 'high'
    nctx.drawImage(cur, 0, 0, cur.width, cur.height, 0, 0, nw, nh)
    cur = next
    cctx = nctx
  }

  ctx.drawImage(cur, 0, 0, cur.width, cur.height, dx, dy, dw, dh)
}

function roundRectPath(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  w: number,
  h: number,
  r: number,
): void {
  // 新版 Chromium 有原生 roundRect;兜底手动画
  const c = ctx as CanvasRenderingContext2D & {
    roundRect?: (x: number, y: number, w: number, h: number, r: number) => void
  }
  ctx.beginPath()
  if (typeof c.roundRect === 'function') {
    c.roundRect(x, y, w, h, r)
    return
  }
  ctx.moveTo(x + r, y)
  ctx.arcTo(x + w, y, x + w, y + h, r)
  ctx.arcTo(x + w, y + h, x, y + h, r)
  ctx.arcTo(x, y + h, x, y, r)
  ctx.arcTo(x, y, x + w, y, r)
  ctx.closePath()
}

export function formatToMime(f: OutputFormat): string {
  return f === 'jpeg' ? 'image/jpeg' : f === 'webp' ? 'image/webp' : 'image/png'
}

export function formatToExt(f: OutputFormat): string {
  return f === 'jpeg' ? 'jpg' : f
}

export function canvasToBlob(
  canvas: HTMLCanvasElement,
  format: OutputFormat,
  quality: number,
): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (blob) => (blob ? resolve(blob) : reject(new Error('导出失败:画布转换为图片失败'))),
      formatToMime(format),
      format === 'png' ? undefined : quality,
    )
  })
}

/** Blob → base64(无 data: 前缀),用于交给后端原生保存 */
export function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const dataUrl = reader.result as string
      const comma = dataUrl.indexOf(',')
      resolve(comma >= 0 ? dataUrl.slice(comma + 1) : '')
    }
    reader.onerror = () => reject(new Error('读取导出数据失败'))
    reader.readAsDataURL(blob)
  })
}

/** 给定布局,算出预览要叠加的缩放(在 exportScale 之上再降采样,避免预览过大卡顿) */
export function previewScale(layout: LayoutResult): number {
  const natural = Math.max(layout.width, layout.height)
  const previewCap = natural > 0 ? Math.min(1, PREVIEW_MAX_SIDE / natural) : 1
  return Math.min(layout.exportScale, previewCap)
}
