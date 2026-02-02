import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'
import { readFileSync } from 'fs'
import { resolve } from 'path'

// 读取 package.json 獲取版本号
const packageJson = JSON.parse(readFileSync(resolve(__dirname, 'package.json'), 'utf-8'))
const buildTime = new Date().toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })

export default defineConfig({
  define: {
    __APP_VERSION__: JSON.stringify(packageJson.version),
    __BUILD_TIME__: JSON.stringify(buildTime),
  },
  plugins: [
    react(),
    VitePWA({
      registerType: 'autoUpdate',
      includeAssets: ['icons/*.png', 'icons/*.svg'],
      // 添加錯誤處理，避免 Service Worker 註冊失敗導致頁面卡住
      injectRegister: 'auto',
      manifest: {
        name: 'QuantMesh 做市商系統',
        short_name: 'QuantMesh',
        description: '专业的加密貨幣做市交易系统',
        theme_color: '#3182ce',
        background_color: '#1a202c',
        display: 'standalone',
        orientation: 'portrait-primary',
        start_url: '/',
        icons: [
          {
            src: './icons/icon.svg',
            sizes: 'any',
            type: 'image/svg+xml',
            purpose: 'any'
          },
          {
            src: './icons/icon.svg',
            sizes: '512x512',
            type: 'image/svg+xml',
            purpose: 'maskable'
          }
        ]
      },
      workbox: {
        globPatterns: ['**/*.{js,css,html,ico,png,svg,woff,woff2}'],
        // 添加版本号，确保 Service Worker 更新
        skipWaiting: true,
        clientsClaim: true,
        // 添加導航回退策略，確保頁面能正常加載
        navigateFallback: '/index.html',
        navigateFallbackDenylist: [/^\/api/, /^\/ws/],
        // 重要：絕不為 /api、/ws 註冊 runtimeCaching。
        // 原因：一經 SW 攔截，請求會先進入 SW → 再轉發網絡，多一跳 + JS 執行；
        // 若 SW 處於冷啟動（未運行），還需先載入 sw.js 和 workbox（體積大），
        // 可能需 1～3 秒甚至更久，用戶就會看到「請求要 3 秒」。
        // 插件自帶的 cachePreset 含 /api（NetworkFirst + 10s 超時），我們不引用它，
        // 只保留下面字體規則，讓 /api、/ws 完全不經 SW，直接走瀏覽器網絡。
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
    // 确保资源路径是相對路径
    base: './',
    // 开啟 sourcemap 方便調試
    sourcemap: true,
    rollupOptions: {
      output: {
        manualChunks: (id) => {
          // React 核心库及其必需依赖（包括 react-is、scheduler、hoist-non-react-statics 等）
          if (
            id.includes('node_modules/react/') || 
            id.includes('node_modules/react-dom/') || 
            id.includes('node_modules/react-router') ||
            id.includes('node_modules/react-is/') ||
            id.includes('node_modules/scheduler/') ||
            id.includes('node_modules/hoist-non-react-statics/') ||
            id.includes('node_modules/prop-types/')
          ) {
            return 'react-vendor'
          }
          
          // Chakra UI 及其依赖
          if (id.includes('node_modules/@chakra-ui') || id.includes('node_modules/@emotion') || id.includes('node_modules/framer-motion')) {
            return 'chakra-vendor'
          }
          
          // Chart.js 相关（不包括 react-chartjs-2，因為它依赖 React）
          if (id.includes('node_modules/chart.js/')) {
            return 'chartjs-vendor'
          }
          
          // Recharts（放入 vendor，因為它有複杂依赖）
          if (id.includes('node_modules/recharts')) {
            return 'recharts-vendor'
          }
          
          // Lightweight Charts
          if (id.includes('node_modules/lightweight-charts')) {
            return 'lightweight-charts-vendor'
          }
          
          // i18n 国際化
          if (id.includes('node_modules/i18next') || id.includes('node_modules/react-i18next')) {
            return 'i18n-vendor'
          }
        },
        // 优化 chunk 文件名
        chunkFileNames: 'assets/[name]-[hash].js',
        entryFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash].[ext]'
      }
    },
    // 增加 chunk 大小警告限制到 600KB（因為我们已經做了分割）
    chunkSizeWarningLimit: 600
  },
  server: {
    port: 15173,  // Vite开发服務器端口（使用10000以上端口，避免常见端口冲突）
    host: true,   // 允許外部访问（用於局域网开发）
    open: false,  // 不自动打开浏览器
    proxy: {
      '/api': {
        target: 'http://localhost:28888',  // 后端API端口
        changeOrigin: true,
        secure: false,  // 如果是 HTTPS，設置為 false
      },
      '/ws': {
        target: 'ws://localhost:28888',  // 后端WebSocket端口
        ws: true,
        changeOrigin: true,
      },
    },
  },
})

