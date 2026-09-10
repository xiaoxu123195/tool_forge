import { useEffect, useRef } from 'react'
import { OnFileDrop, OnFileDropOff } from '../../wailsjs/runtime/runtime'

/**
 * 注册 Wails 原生文件拖放,只在带 --wails-drop-target:drop 的元素上触发(useDropTarget=true)。
 * 拿到的是文件**绝对路径**(HTML5 拖放给不了),供后端流式读取。
 * 组件卸载时 OnFileDropOff 摘掉监听,避免影响其它工具的 HTML5 图片拖拽。
 *
 * 放在 lib 下是因为哈希、MMKV、plist 三个工具都要"拿到路径交给后端读",
 * 各自复制一份的话,哪天 Wails 改了 OnFileDrop 的签名就要改三处。
 */
export function useNativeFileDrop(onPaths: (paths: string[]) => void) {
  const cb = useRef(onPaths)
  cb.current = onPaths
  useEffect(() => {
    try {
      OnFileDrop((_x, _y, paths) => {
        if (paths && paths.length > 0) cb.current(paths)
      }, true)
    } catch {
      // 非 wails 环境(纯浏览器预览)没有 runtime,忽略
    }
    return () => {
      try {
        OnFileDropOff()
      } catch {
        // ignore
      }
    }
  }, [])
}
