import enUS from './locales/en-US.json'

const localeCodes = [
  'zh-CN', 'zh-TW', 'en-US', 'fr-FR', 'es-ES', 'ru-RU', 'hi-IN', 'pt-BR', 'de-DE', 'ja-JP',
  'ko-KR', 'ar-SA', 'tr-TR', 'vi-VN', 'it-IT', 'id-ID', 'nl-NL', 'pl-PL', 'th-TH', 'uk-UA',
  'bn-BD', 'ur-PK', 'tl-PH', 'fa-IR',
] as const

const englishOrderReconciliation = enUS.configuration.orderReconciliation

export function withOrderReconciliation(language: string, bundle: Record<string, unknown>) {
  const configuration = bundle.configuration && typeof bundle.configuration === 'object'
    ? bundle.configuration as Record<string, unknown>
    : {}
  const existing = configuration.orderReconciliation
  const resource = existing && typeof existing === 'object'
    ? existing
    : englishOrderReconciliation
  return {
    ...bundle,
    configuration: { ...configuration, orderReconciliation: resource },
  }
}

export function orderReconciliationLocaleCodes(): readonly string[] {
  return localeCodes
}
