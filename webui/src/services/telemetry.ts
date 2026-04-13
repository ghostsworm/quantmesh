/** 文檔與 Web 共用的閱讀量/使用量像素（與 Markdown 中 ![](...) 同源） */
export const USAGE_BEACON_URL = 'https://um.facev.app/p/IiDQJEIGM'

const isTelemetryDisabled = () => {
  if (import.meta.env.VITE_DISABLE_TELEMETRY === '1' || import.meta.env.VITE_DISABLE_TELEMETRY === 'true') {
    return true
  }
  if (typeof window === 'undefined') {
    return false
  }
  try {
    return localStorage.getItem('QUANTMESH_DISABLE_TELEMETRY') === '1'
  } catch {
    return false
  }
}

/** 加載 1×1 像素統計 Web 使用量（可通過 VITE_DISABLE_TELEMETRY / localStorage 關閉） */
export const trackUsageBeaconPixel = (): void => {
  if (isTelemetryDisabled() || typeof window === 'undefined') {
    return
  }
  try {
    const img = new Image()
    img.decoding = 'async'
    img.referrerPolicy = 'no-referrer-when-downgrade'
    img.src = USAGE_BEACON_URL
  } catch {
    // 靜默失敗
  }
}

export const trackAppInit = async () => {
  if (isTelemetryDisabled()) {
    return
  }
  trackUsageBeaconPixel()
}

export const trackUserLogin = async (_method: 'password' | 'webauthn') => {}

export const trackConfigSaved = async (_configType?: string) => {}

export const trackTradingStarted = async (_exchange?: string, _symbol?: string) => {}
