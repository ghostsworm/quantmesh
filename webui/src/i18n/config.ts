import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import LanguageDetector from 'i18next-browser-languagedetector'

import zhCN from './locales/zh-CN.json'
import zhTW from './locales/zh-TW.json'
import enUS from './locales/en-US.json'
import frFR from './locales/fr-FR.json'
import esES from './locales/es-ES.json'
import ruRU from './locales/ru-RU.json'
import hiIN from './locales/hi-IN.json'
import ptBR from './locales/pt-BR.json'
import deDE from './locales/de-DE.json'
import koKR from './locales/ko-KR.json'
import arSA from './locales/ar-SA.json'
import trTR from './locales/tr-TR.json'
import viVN from './locales/vi-VN.json'
import itIT from './locales/it-IT.json'
import idID from './locales/id-ID.json'
import nlNL from './locales/nl-NL.json'

const resources = {
  'zh-CN': { translation: zhCN },
  'zh-TW': { translation: zhTW },
  'en-US': { translation: enUS },
  'fr-FR': { translation: frFR },
  'es-ES': { translation: esES },
  'ru-RU': { translation: ruRU },
  'hi-IN': { translation: hiIN },
  'pt-BR': { translation: ptBR },
  'de-DE': { translation: deDE },
  'ko-KR': { translation: koKR },
  'ar-SA': { translation: arSA },
  'tr-TR': { translation: trTR },
  'vi-VN': { translation: viVN },
  'it-IT': { translation: itIT },
  'id-ID': { translation: idID },
  'nl-NL': { translation: nlNL },
}

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    fallbackLng: 'zh-CN',
    supportedLngs: ['zh-CN', 'zh-TW', 'en-US', 'fr-FR', 'es-ES', 'ru-RU', 'hi-IN', 'pt-BR', 'de-DE', 'ko-KR', 'ar-SA', 'tr-TR', 'vi-VN', 'it-IT', 'id-ID', 'nl-NL'],
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
    // 初始化完成后设置 HTML lang 属性和方向
    const currentLang = i18n.language
    document.documentElement.lang = currentLang
    // 设置文本方向：阿拉伯语为 RTL，其他为 LTR
    document.documentElement.dir = currentLang === 'ar-SA' ? 'rtl' : 'ltr'
  })

// 当语言改变时，更新 HTML lang 属性和方向
i18n.on('languageChanged', (lng) => {
  document.documentElement.lang = lng
  // 设置文本方向：阿拉伯语为 RTL，其他为 LTR
  document.documentElement.dir = lng === 'ar-SA' ? 'rtl' : 'ltr'
})

export default i18n

