import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './index.css'
import './i18n/config'
import i18n from 'i18next'

// PWA Service Worker 注册
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js', { scope: '/' })
      .then(registration => {
        console.log('✅', i18n.t('pwa.serviceWorkerRegistered'), ':', registration.scope)
        
        // 检查更新
        registration.addEventListener('updatefound', () => {
          const newWorker = registration.installing
          if (newWorker) {
            newWorker.addEventListener('statechange', () => {
              if (newWorker.state === 'installed' && navigator.serviceWorker.controller) {
                // 新版本可用
                console.log('🆕', i18n.t('pwa.newVersionDetected'))
                // 可以在这里显示更新提示
                if (confirm(i18n.t('pwa.updateNow'))) {
                  window.location.reload()
                }
              }
            })
          }
        })
      })
      .catch(error => {
        console.warn('⚠️', i18n.t('pwa.serviceWorkerFailed'), ':', error)
      })
  })
}

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

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)

