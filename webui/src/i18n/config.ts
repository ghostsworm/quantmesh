import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import resourcesToBackend from 'i18next-resources-to-backend'

// 仅静态导入 fallback 语言（zh-CN），确保首屏无延迟
import zhCN from './locales/zh-CN.json'

const supportedLngs = [
  'zh-CN', 'zh-TW', 'en-US', 'fr-FR', 'es-ES', 'ru-RU',
  'hi-IN', 'pt-BR', 'de-DE', 'ja-JP', 'ko-KR', 'ar-SA', 'tr-TR',
  'vi-VN', 'it-IT', 'id-ID', 'nl-NL', 'pl-PL', 'th-TH',
  'uk-UA', 'bn-BD', 'ur-PK', 'tl-PH', 'fa-IR',
]

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  // 动态加载语言包：Vite 会为每个 locale JSON 生成独立 chunk，按需下载
  .use(resourcesToBackend((language: string, namespace: string) => {
    // zh-CN 已静态导入，不需要动态加载
    if (language === 'zh-CN' && namespace === 'translation') {
      return Promise.resolve(zhCN)
    }
    return import(`./locales/${language}.json`)
  }))
  .init({
    fallbackLng: 'zh-CN',
    supportedLngs,
    // 预加载 fallback 语言，其他语言按需加载
    partialBundledLanguages: true,
    resources: {
      'zh-CN': { translation: zhCN },
    },
    interpolation: {
      escapeValue: false,
    },
    detection: {
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
      lookupLocalStorage: 'i18nextLng',
    },
  })
  .then(() => {
    // 初始化完成后設置 HTML lang 属性和方向
    const currentLang = i18n.language
    document.documentElement.lang = currentLang
    // 設置文本方向：阿拉伯语、乌尔都语、波斯语為 RTL，其他為 LTR
    document.documentElement.dir = ['ar-SA', 'ur-PK', 'fa-IR'].includes(currentLang) ? 'rtl' : 'ltr'
  })

// 當语言改变時，更新 HTML lang 属性和方向
i18n.on('languageChanged', (lng) => {
  document.documentElement.lang = lng
  // 設置文本方向：阿拉伯语、乌尔都语、波斯语為 RTL，其他為 LTR
  document.documentElement.dir = ['ar-SA', 'ur-PK', 'fa-IR'].includes(lng) ? 'rtl' : 'ltr'
})

export default i18n
