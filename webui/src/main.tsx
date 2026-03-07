import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './index.css'
import './i18n/config'
import i18n from 'i18next'
import { trackAppInit } from './services/telemetry'
import { disableServiceWorkersForAuthFlow } from './utils/appRuntimeGuards'

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

void disableServiceWorkersForAuthFlow(window.location.pathname)

// 追踪应用初始化
trackAppInit()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <App />
)

