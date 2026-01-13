import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'

export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      registerType: 'autoUpdate',
      includeAssets: ['icons/*.png', 'assets/logo.svg'],
      manifest: {
        name: 'QuantMesh 做市商系统',
        short_name: 'QuantMesh',
        description: '专业的加密货币做市交易系统',
        theme_color: '#3182ce',
        background_color: '#1a202c',
        display: 'standalone',
        orientation: 'portrait-primary',
        start_url: '/',
        icons: [
          {
            src: '/icons/icon-72x72.png',
            sizes: '72x72',
            type: 'image/png'
          },
          {
            src: '/icons/icon-96x96.png',
            sizes: '96x96',
            type: 'image/png'
          },
          {
            src: '/icons/icon-128x128.png',
            sizes: '128x128',
            type: 'image/png'
          },
          {
            src: '/icons/icon-144x144.png',
            sizes: '144x144',
            type: 'image/png'
          },
          {
            src: '/icons/icon-152x152.png',
            sizes: '152x152',
            type: 'image/png'
          },
          {
            src: '/icons/icon-192x192.png',
            sizes: '192x192',
            type: 'image/png'
          },
          {
            src: '/icons/icon-384x384.png',
            sizes: '384x384',
            type: 'image/png'
          },
          {
            src: '/icons/icon-512x512.png',
            sizes: '512x512',
            type: 'image/png'
          }
        ]
      },
      workbox: {
        globPatterns: ['**/*.{js,css,html,ico,png,svg,woff,woff2}'],
        runtimeCaching: [
          {
            urlPattern: /^https:\/\/fonts\.googleapis\.com\/.*/i,
            handler: 'CacheFirst',
            options: {
              cacheName: 'google-fonts-cache',
              expiration: {
                maxEntries: 10,
                maxAgeSeconds: 60 * 60 * 24 * 365 // 1 year
              },
              cacheableResponse: {
                statuses: [0, 200]
              }
            }
          },
          {
            urlPattern: /^https:\/\/fonts\.gstatic\.com\/.*/i,
            handler: 'CacheFirst',
            options: {
              cacheName: 'gstatic-fonts-cache',
              expiration: {
                maxEntries: 10,
                maxAgeSeconds: 60 * 60 * 24 * 365 // 1 year
              },
              cacheableResponse: {
                statuses: [0, 200]
              }
            }
          },
          {
            // 所有 API 请求直接走网络，不缓存
            urlPattern: /\/api\/.*/i,
            handler: 'NetworkOnly'
          }
        ]
      },
      devOptions: {
        enabled: true,
        type: 'module'
      }
    })
  ],
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
    // 确保资源路径是相对路径
    base: './',
    rollupOptions: {
      output: {
        manualChunks: (id) => {
          // React 核心库
          if (id.includes('node_modules/react') || id.includes('node_modules/react-dom') || id.includes('node_modules/react-router')) {
            return 'react-vendor'
          }
          
          // Chakra UI 及其依赖
          if (id.includes('node_modules/@chakra-ui') || id.includes('node_modules/@emotion') || id.includes('node_modules/framer-motion')) {
            return 'chakra-vendor'
          }
          
          // Chart.js 相关
          if (id.includes('node_modules/chart.js') || id.includes('node_modules/react-chartjs-2')) {
            return 'chartjs-vendor'
          }
          
          // Recharts
          if (id.includes('node_modules/recharts')) {
            return 'recharts-vendor'
          }
          
          // Lightweight Charts
          if (id.includes('node_modules/lightweight-charts')) {
            return 'lightweight-charts-vendor'
          }
          
          // i18n 国际化
          if (id.includes('node_modules/i18next') || id.includes('node_modules/react-i18next')) {
            return 'i18n-vendor'
          }
          
          // 其他 node_modules 中的大型库
          if (id.includes('node_modules')) {
            return 'vendor'
          }
        },
        // 优化 chunk 文件名
        chunkFileNames: 'assets/[name]-[hash].js',
        entryFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash].[ext]'
      }
    },
    // 增加 chunk 大小警告限制到 600KB（因为我们已经做了分割）
    chunkSizeWarningLimit: 600
  },
  server: {
    port: 15173,  // Vite开发服务器端口（使用10000以上端口，避免常见端口冲突）
    host: true,   // 允许外部访问（用于局域网开发）
    open: false,  // 不自动打开浏览器
    proxy: {
      '/api': {
        target: 'http://localhost:28888',  // 后端API端口
        changeOrigin: true,
        secure: false,  // 如果是 HTTPS，设置为 false
      },
      '/ws': {
        target: 'ws://localhost:28888',  // 后端WebSocket端口
        ws: true,
        changeOrigin: true,
      },
    },
  },
})

