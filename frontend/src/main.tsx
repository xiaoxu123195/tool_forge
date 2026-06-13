import React from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import { applyAppearance, useLayoutStore } from './stores/layout'
import './styles/globals.css'

// 渲染前同步把主题打到根元素,保证 webview 首帧就是正确主题
// (配合 main.go 的 StartHidden + App 里的 WindowShow,启动不再黑屏/闪主题)
{
  const ls = useLayoutStore.getState()
  applyAppearance(ls.theme, ls.styleId, ls.glassWallpaperLight, ls.glassWallpaperDark)
}

const container = document.getElementById('root')!
const root = createRoot(container)

root.render(
  <React.StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </React.StrictMode>
)
