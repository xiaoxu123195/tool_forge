// 纯几何计算:根据图片尺寸 + 设置,算出画布尺寸与每张图的源/目标矩形。
// 不碰 Canvas,方便单测与复用(预览/导出共用)。

import type { MergeImage, MergeSettings } from './types'

/** 一张图在画布上的摆放:src=从源图裁剪的区域,dst=画到画布的区域(自然坐标,未乘 exportScale) */
export interface Placement {
  index: number
  sx: number
  sy: number
  sw: number
  sh: number
  dx: number
  dy: number
  dw: number
  dh: number
}

export interface LayoutResult {
  /** 自然画布尺寸(未受 maxSide 护栏缩放) */
  width: number
  height: number
  placements: Placement[]
  /** 护栏:导出时建议的整体缩放系数(<=1),超过 maxSide / 面积上限才 <1 */
  exportScale: number
}

// Chromium canvas 单边与总面积硬上限(留一点余量)
const HARD_MAX_SIDE = 16384
const HARD_MAX_AREA = 16384 * 16384

function fullSrc(img: MergeImage) {
  return { sx: 0, sy: 0, sw: img.width, sh: img.height }
}

function clampPositive(n: number): number {
  return Number.isFinite(n) && n > 0 ? n : 1
}

/** 计算布局。images 为空时返回 0 尺寸。 */
export function computeLayout(images: MergeImage[], s: MergeSettings): LayoutResult {
  const n = images.length
  if (n === 0) return { width: 0, height: 0, placements: [], exportScale: 1 }

  const margin = Math.max(0, s.margin)
  const gap = Math.max(0, s.gap)
  const placements: Placement[] = []
  let width = 0
  let height = 0

  if (s.layout === 'vertical') {
    const widths = images.map((i) => i.width)
    const target =
      s.align === 'custom'
        ? clampPositive(s.customSize)
        : s.align === 'max'
          ? Math.max(...widths)
          : Math.min(...widths)
    let y = margin
    images.forEach((img, index) => {
      const dw = target
      const dh = (img.height * target) / img.width
      placements.push({ index, ...fullSrc(img), dx: margin, dy: y, dw, dh })
      y += dh + gap
    })
    width = target + margin * 2
    height = y - gap + margin
  } else if (s.layout === 'horizontal') {
    const heights = images.map((i) => i.height)
    const target =
      s.align === 'custom'
        ? clampPositive(s.customSize)
        : s.align === 'max'
          ? Math.max(...heights)
          : Math.min(...heights)
    let x = margin
    images.forEach((img, index) => {
      const dh = target
      const dw = (img.width * target) / img.height
      placements.push({ index, ...fullSrc(img), dx: x, dy: margin, dw, dh })
      x += dw + gap
    })
    width = x - gap + margin
    height = target + margin * 2
  } else {
    // grid
    // 自动列数:取 ≈正方形(4 张→2×2、9 张→3×3),减少最后一排的大片空白
    const cols = s.autoColumns
      ? Math.max(1, Math.ceil(Math.sqrt(n)))
      : Math.max(1, Math.floor(s.columns))
    const rows = Math.ceil(n / cols)
    // 最后一排不满时,把这几张居中,避免右侧一大块空白
    const lastRowCount = n - (rows - 1) * cols
    const widths = images.map((i) => i.width)
    const heights = images.map((i) => i.height)
    const cellW =
      s.align === 'custom'
        ? clampPositive(s.customSize)
        : s.align === 'max'
          ? Math.max(...widths)
          : Math.min(...widths)
    const cellH =
      s.align === 'custom'
        ? clampPositive(s.customSize)
        : s.align === 'max'
          ? Math.max(...heights)
          : Math.min(...heights)

    images.forEach((img, index) => {
      const col = index % cols
      const row = Math.floor(index / cols)
      const inLastRow = row === rows - 1 && lastRowCount < cols
      const rowOffset = inLastRow ? ((cols - lastRowCount) * (cellW + gap)) / 2 : 0
      const cellX = margin + col * (cellW + gap) + rowOffset
      const cellY = margin + row * (cellH + gap)

      if (s.gridFit === 'cover') {
        // 裁剪源图到 cell 宽高比,铺满 cell
        const cellAspect = cellW / cellH
        const imgAspect = img.width / img.height
        let sw = img.width
        let sh = img.height
        if (imgAspect > cellAspect) {
          sw = img.height * cellAspect
        } else {
          sh = img.width / cellAspect
        }
        const sx = (img.width - sw) / 2
        const sy = (img.height - sh) / 2
        placements.push({ index, sx, sy, sw, sh, dx: cellX, dy: cellY, dw: cellW, dh: cellH })
      } else {
        // contain:保比缩到 cell 内,居中留白(背景色透出)
        const scale = Math.min(cellW / img.width, cellH / img.height)
        const dw = img.width * scale
        const dh = img.height * scale
        const dx = cellX + (cellW - dw) / 2
        const dy = cellY + (cellH - dh) / 2
        placements.push({ index, ...fullSrc(img), dx, dy, dw, dh })
      }
    })

    width = margin * 2 + cols * cellW + (cols - 1) * gap
    height = margin * 2 + rows * cellH + (rows - 1) * gap
  }

  width = Math.max(1, Math.round(width))
  height = Math.max(1, Math.round(height))

  // 护栏:超过用户设的 maxSide、或硬上限单边/面积,整体等比缩小
  const maxSide = Math.min(clampPositive(s.maxSide), HARD_MAX_SIDE)
  const sideScale = Math.min(1, maxSide / Math.max(width, height))
  const areaScale = Math.min(1, Math.sqrt(HARD_MAX_AREA / (width * height)))
  const exportScale = Math.min(sideScale, areaScale)

  return { width, height, placements, exportScale }
}
