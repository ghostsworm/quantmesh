import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './index.css'
import './i18n/config'
import i18n from 'i18next'

// PWA Service Worker 註冊已由 vite-plugin-pwa 自動處理（通過 registerSW.js）
// 不需要手動註冊，避免雙重註冊導致的衝突

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

