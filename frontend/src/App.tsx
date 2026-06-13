import { useEffect } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { MainLayout } from '@/layouts/MainLayout'
import { Home } from '@/pages/Home'
import { Profile } from '@/profile'
import { ToolRouter } from '@/tools/ToolRouter'
import { WindowShow } from '../wailsjs/runtime/runtime'

function App() {
  // 首帧渲染完成后再显示窗口:此时 webview 已画好(含主题),不会露出原生黑底。
  // 放在根组件、跨路由只跑一次;rAF 多等一帧确保 paint 已发生。包 try/catch 兼容浏览器预览。
  useEffect(() => {
    const id = requestAnimationFrame(() => {
      try {
        WindowShow()
      } catch {
        // 非 wails 运行环境(纯浏览器)没有 runtime,忽略
      }
    })
    return () => cancelAnimationFrame(id)
  }, [])

  return (
    <Routes>
      <Route element={<MainLayout />}>
        <Route index element={<Home />} />
        <Route path="/tools/:toolId" element={<ToolRouter />} />
        <Route path="/profile" element={<Profile />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

export default App
