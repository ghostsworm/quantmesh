import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './index.css'
import './i18n/config'

// PWA Service Worker 注册
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js', { scope: '/' })
      .then(registration => {
        console.log('✅ Service Worker 注册成功:', registration.scope)
        
        // 检查更新
        registration.addEventListener('updatefound', () => {
          const newWorker = registration.installing
          if (newWorker) {
            newWorker.addEventListener('statechange', () => {
              if (newWorker.state === 'installed' && navigator.serviceWorker.controller) {
                // 新版本可用
                console.log('🆕 检测到新版本，刷新页面以更新')
                // 可以在这里显示更新提示
                if (confirm('检测到新版本，是否立即更新？')) {
                  window.location.reload()
                }
              }
            })
          }
        })
      })
      .catch(error => {
        console.warn('⚠️ Service Worker 注册失败:', error)
      })
  })
}

// PWA 安装提示
let deferredPrompt: any
window.addEventListener('beforeinstallprompt', (e) => {
  // 阻止默认的安装提示
  e.preventDefault()
  // 保存事件以便稍后触发
  deferredPrompt = e
  console.log('💡 PWA 可以安装')
  
  // 可以在这里显示自定义的安装按钮
  // 例如：显示一个"添加到主屏幕"的提示横幅
})

window.addEventListener('appinstalled', () => {
  console.log('✅ PWA 已安装')
  deferredPrompt = null
})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)

