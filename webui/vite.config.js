import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
    // 确保资源路径是相对路径
    base: './',
  },
  server: {
    port: 15173,  // Vite开发服务器端口（使用10000以上端口，避免常见端口冲突）
    proxy: {
      '/api': {
        target: 'http://localhost:28888',  // 后端API端口
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://localhost:28888',  // 后端WebSocket端口
        ws: true,
      },
    },
  },
})

