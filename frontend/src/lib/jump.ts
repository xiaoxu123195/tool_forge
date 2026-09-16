import { useEffect, useRef } from 'react'
import { useLocation, type NavigateFunction } from 'react-router-dom'

/**
 * 工具之间带参数跳转。
 *
 * 现场的顺序常常是:真机浏览翻到一个目录 → 用移动取证把它整个取下来 → 拿 SQLite 搜索翻内容。
 * 以前这三步之间要手抄路径;抄错一层就是别的目录。
 *
 * 路由是 keep-alive 的:目标工具可能早就挂着,只是被 hidden 了。所以不能只在挂载时
 * 读一次 location.state —— 要在每次 location.key 变化时看一眼是不是发给自己的;
 * 处理过的 key 记下来,免得同一条跳转在重渲染时被吃两遍。
 */
export type Jump =
  | { to: 'mobile-forensic'; platform: 'android' | 'ios'; paths: string[] }
  | { to: 'sqlite-search'; root: string }

export function jumpTo(navigate: NavigateFunction, jump: Jump) {
  navigate(`/tools/${jump.to}`, { state: { jump } })
}

export function useJump<T extends Jump['to']>(
  to: T,
  handler: (j: Extract<Jump, { to: T }>) => void,
) {
  const location = useLocation()
  const handled = useRef('')
  const fn = useRef(handler)
  fn.current = handler
  useEffect(() => {
    const j = (location.state as { jump?: Jump } | null)?.jump
    if (!j || j.to !== to || handled.current === location.key) return
    handled.current = location.key
    fn.current(j as Extract<Jump, { to: T }>)
  }, [location.key, location.state, to])
}
