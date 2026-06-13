// 图片拼接工具的共享类型。
// 渲染/布局只认 image(已按旋转烘焙好的可绘制源)+ width/height,与 EXIF/旋转解耦。

export type LayoutMode = 'vertical' | 'horizontal' | 'grid'
export type AlignMode = 'min' | 'max' | 'custom'
export type GridFit = 'contain' | 'cover'
export type OutputFormat = 'png' | 'jpeg' | 'webp'
export type Rotation = 0 | 90 | 180 | 270

/** 一张参与拼接的图片(运行态,不持久化) */
export interface MergeImage {
  id: string
  name: string
  size: number // 原始文件字节
  rotation: Rotation
  /** EXIF 已纠正的原图(未旋转),旋转时据此重新烘焙 image */
  orig: ImageBitmap
  /** orig 按 rotation 旋转后的可绘制源(ImageBitmap 或 canvas),布局/渲染用这个 */
  image: CanvasImageSource
  width: number // image 的宽(旋转后)
  height: number // image 的高(旋转后)
  thumb: string // 列表缩略图 dataURL
}

/** 拼接设置(持久化) */
export interface MergeSettings {
  layout: LayoutMode
  autoColumns: boolean // grid 列数自动(≈正方形,4 张→2×2)
  columns: number // grid 手动列数
  gap: number // 图片间距 px
  margin: number // 整体外边距 px
  radius: number // 每张图圆角 px
  bg: string // 背景:'transparent' 或 #hex
  align: AlignMode // 尺寸对齐策略
  customSize: number // align=custom 时的目标宽(竖)/高(横)/cell 边(网格)
  gridFit: GridFit // 网格内保比方式
  format: OutputFormat
  quality: number // 0..1,仅 jpeg/webp
  maxSide: number // 输出最长边上限(护栏),超出则整体等比缩小
}
