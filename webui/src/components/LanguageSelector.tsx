import React from 'react'
import { useTranslation } from 'react-i18next'
import {
  Select,
} from '@chakra-ui/react'

const LanguageSelector: React.FC = () => {
  const { i18n } = useTranslation()

  const languages = [
    { code: 'zh-CN', name: '简体中文' },
    { code: 'zh-TW', name: '繁體中文' },
    { code: 'en-US', name: 'English' },
    { code: 'fr-FR', name: 'Français' },
    { code: 'es-ES', name: 'Español' },
    { code: 'ru-RU', name: 'Русский' },
    { code: 'hi-IN', name: 'हिन्दी' },
    { code: 'pt-BR', name: 'Português' },
    { code: 'de-DE', name: 'Deutsch' },
    { code: 'ko-KR', name: '한국어' },
    { code: 'ar-SA', name: 'العربية' },
    { code: 'tr-TR', name: 'Türkçe' },
    { code: 'vi-VN', name: 'Tiếng Việt' },
    { code: 'it-IT', name: 'Italiano' },
    { code: 'id-ID', name: 'Bahasa Indonesia' },
    { code: 'nl-NL', name: 'Nederlands' },
    { code: 'uk-UA', name: 'Українська' },
    { code: 'bn-BD', name: 'বাংলা' },
    { code: 'ur-PK', name: 'اردو' },
    { code: 'tl-PH', name: 'Filipino' },
    { code: 'fa-IR', name: 'فارسی' },
  ]

  const handleLanguageChange = (event: React.ChangeEvent<HTMLSelectElement>) => {
    const newLanguage = event.target.value
    i18n.changeLanguage(newLanguage)
  }

  /** 瀏覽器可能回傳 `en` 等簡碼，與選項 `en-US` 不一致時 Chakra Select 可能異常，需對齊到已列舉值 */
  const selectValue =
    languages.find((l) => l.code === i18n.language)?.code ??
    languages.find((l) => i18n.language?.toLowerCase().startsWith(l.code.split('-')[0].toLowerCase()))?.code ??
    'zh-CN'

  const bgColor = 'white'
  const borderColor = 'gray.200'
  const hoverBg = 'gray.50'

  return (
    <Select
      value={selectValue}
      onChange={handleLanguageChange}
      size="xs"
      borderRadius="full"
      borderColor={borderColor}
      bg={bgColor}
      fontSize="xs"
      fontWeight="normal"
      lineHeight="short"
      fontFamily="body"
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

