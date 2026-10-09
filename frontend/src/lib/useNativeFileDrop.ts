import { useEffect, useRef } from 'react'
import { OnFileDrop, OnFileDropOff } from '../../wailsjs/runtime/runtime'

interface Target {
  el: () => HTMLElement | null
  cb: (paths: string[]) => void
}

// Wails 的原生拖放全局只认一个回调:第二次注册直接被忽略,摘掉又是全摘。
// 而工具页切走只是藏起来、不卸载 —— 先打开过哈希,再到 MMKV 里拖文件,
// 文件就会落到哈希的回调里。所以全局只注册一次,松手时看落在谁的拖放区里再分给谁
const targets = new Set<Target>()

function dispatch(x: number, y: number, paths: string[]) {
  if (!paths || paths.length === 0) return
  // 从落点往外找,最近的那个拖放区说了算(拖放区套着拖放区时,里面的优先)
  for (let n = document.elementFromPoint(x, y); n; n = n.parentElement) {
    for (const t of targets) {
      if (t.el() === n) {
        t.cb(paths)
        return
      }
    }
  }
}

function register(t: Target) {
  if (targets.size === 0) {
    try {
      OnFileDrop(dispatch, true)
    } catch {
      // 非 wails 环境(纯浏览器预览)没有 runtime,忽略
    }
  }
  targets.add(t)
  return () => {
    targets.delete(t)
    if (targets.size > 0) return
    // 最后一个拖放区没了才摘,摘掉之后别的工具的 HTML5 图片拖拽不受影响
    try {
      OnFileDropOff()
    } catch {
      // ignore
    }
  }
}

/**
 * 接 Wails 原生文件拖放,拿到的是文件**绝对路径**(HTML5 拖放给不了),供后端流式读取。
 *
 * 把返回的 ref 挂到拖放区上,那个元素还要带 --wails-drop-target:drop 样式(后代会继承),
 * 文件在它里面松手才算数。哈希、MMKV、plist、投屏都要「拿到路径交给后端」,
 * 各自复制一份的话,哪天 Wails 改了 OnFileDrop 的签名就要改好几处
 */
export function useNativeFileDrop<T extends HTMLElement = HTMLDivElement>(onPaths: (paths: string[]) => void) {
  const ref = useRef<T>(null)
  const cb = useRef(onPaths)
  cb.current = onPaths
  useEffect(
    () =>
      register({
        el: () => ref.current,
        cb: (paths) => cb.current(paths),
      }),
    [],
  )
  return ref
}
