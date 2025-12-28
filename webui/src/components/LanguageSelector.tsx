import React from 'react'
import { useTranslation } from 'react-i18next'
import {
  Select,
} from '@chakra-ui/react'

const LanguageSelector: React.FC = () => {
  const { i18n } = useTranslation()

  const languages = [
    { code: 'zh-CN', name: '中文' },
    { code: 'en-US', name: 'English' },
    { code: 'fr-FR', name: 'Français' },
    { code: 'es-ES', name: 'Español' },
    { code: 'ru-RU', name: 'Русский' },
    { code: 'hi-IN', name: 'हिन्दी' },
    { code: 'pt-BR', name: 'Português' },
    { code: 'de-DE', name: 'Deutsch' },
    { code: 'ja-JP', name: '日本語' },
    { code: 'ko-KR', name: '한국어' },
    { code: 'ar-SA', name: 'العربية' },
    { code: 'tr-TR', name: 'Türkçe' },
    { code: 'vi-VN', name: 'Tiếng Việt' },
    { code: 'it-IT', name: 'Italiano' },
    { code: 'id-ID', name: 'Bahasa Indonesia' },
    { code: 'th-TH', name: 'ไทย' },
    { code: 'nl-NL', name: 'Nederlands' },
  ]

  const handleLanguageChange = (event: React.ChangeEvent<HTMLSelectElement>) => {
    const newLanguage = event.target.value
    i18n.changeLanguage(newLanguage)
  }

  const bgColor = 'white'
  const borderColor = 'gray.200'
  const hoverBg = 'gray.50'

  return (
    <Select
      value={i18n.language}
      onChange={handleLanguageChange}
      size="xs"
      borderRadius="full"
      borderColor={borderColor}
      bg={bgColor}
      fontSize="12px"
      fontWeight="600"
      cursor="pointer"
      _hover={{
        bg: hoverBg,
      }}
      minW="100px"
      maxW="140px"
    >
      {languages.map((lang) => (
        <option key={lang.code} value={lang.code}>
          {lang.name}
        </option>
      ))}
    </Select>
  )
}

export default LanguageSelector

