import type { TFunction } from 'i18next'

/** 後端原文或別名 → pauseReason 子鍵 */
const PAUSE_REASON_CODE: Record<string, string> = {
  single_leg_running: 'single_leg_running',
  manual: 'manual',
  position_limit: 'position_limit',
  schedule: 'schedule',
  periodic: 'periodic',
  circuit_breaker: 'circuit_breaker',
  手动暂停: 'manual',
  紧急停止: 'emergency_stop',
  紧急暂停: 'emergency_pause',
}

/**
 * 將暫停開倉原因轉為可讀文案（優先使用 i18n，否則返回後端原文）
 */
export function displayPauseOpeningReason(raw: string | undefined, t: TFunction): string {
  if (raw == null || !String(raw).trim()) return ''
  const norm = String(raw).trim()
  const code = PAUSE_REASON_CODE[norm] ?? PAUSE_REASON_CODE[norm.toLowerCase()] ?? norm
  const key = `botRiskControl.pauseReason.${code}`
  const translated = t(key)
  if (translated !== key) return translated
  return norm
}
