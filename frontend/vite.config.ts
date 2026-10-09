import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  // iOS 投屏用的 VNC 客户端(noVNC)模块顶层就有 await。
  // 默认目标会报「不支持顶层 await」;Edge WebView2、Safari 15 起的 WebKit 都支持
  build: { target: 'es2022' },
  optimizeDeps: { esbuildOptions: { target: 'es2022' } },
})
