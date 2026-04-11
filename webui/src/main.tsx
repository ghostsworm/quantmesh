import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './index.css'
import './i18n/config'
import i18n from 'i18next'
import { trackAppInit } from './services/telemetry'
import { disableServiceWorkersForAuthFlow } from './utils/appRuntimeGuards'

// ============================================
// PWA Service Worker 注册与更新处理
// ============================================
function registerServiceWorker() {
  if (typeof window === 'undefined' || !('serviceWorker' in navigator)) {
    return
  }

  // Vite 開發服務不產出 /sw.js，請求會被回退成 index.html（MIME 為 text/html），註冊必失敗且刷屏
  if (import.meta.env.DEV) {
    return
  }

  // 检查是否在认证流程页面，如果是则禁用 Service Worker
  const pathname = window.location.pathname
  if (pathname === '/login' || pathname === '/register' || pathname === '/setup') {
    disableServiceWorkersForAuthFlow(pathname)
    return
  }

  window.addEventListener('load', async () => {
    try {
      const registration = await navigator.serviceWorker.register('/sw.js', {
        scope: '/'
      })

      console.log('✅ Service Worker registered:', registration.scope)

      // 监听 Service Worker 更新
      registration.addEventListener('updatefound', () => {
        const newWorker = registration.installing
        if (!newWorker) return

        newWorker.addEventListener('statechange', () => {
          // 新的 Service Worker 已安装完成，等待激活
          if (newWorker.state === 'installed' && navigator.serviceWorker.controller) {
            console.log('🔄 New version available, refreshing...')

            // 直接刷新页面（skipWaiting 已经在 workbox 配置中启用）
            // 这样可以避免旧的 Service Worker 缓存导致的 404 错误
            window.location.reload()
          }
        })
      })

      // 监听新的 Service Worker 接管页面
      navigator.serviceWorker.addEventListener('controllerchange', () => {
        console.log('🔄 Service Worker controller changed, reloading page...')
        window.location.reload()
      })

    } catch (error) {
      console.error('❌ Service Worker registration failed:', error)
    }
  })

  // 监听来自 Service Worker 的消息
  navigator.serviceWorker.addEventListener('message', (event) => {
    if (event.data && event.data.type === 'SKIP_WAITING') {
      // Service Worker 要求立即刷新
      window.location.reload()
    }
  })
}

// 注册 Service Worker
registerServiceWorker()

// PWA 安装提示
let deferredPrompt: any
window.addEventListener('beforeinstallprompt', (e) => {
  // 阻止預設的安装提示
  e.preventDefault()
  // 保存事件以便稍后触发
  deferredPrompt = e
  console.log('💡', i18n.t('pwa.canInstall'))
  
  // 可以在这里显示自定义的安装按钮
  // 例如：显示一個"添加到主屏幕"的提示横幅
})

window.addEventListener('appinstalled', () => {
  console.log('✅', i18n.t('pwa.installed'))
  deferredPrompt = null
})

void disableServiceWorkersForAuthFlow(window.location.pathname)

// 追踪应用初始化
trackAppInit()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <App />
)

