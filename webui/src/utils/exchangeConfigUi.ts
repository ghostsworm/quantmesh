import { EXCHANGES_REQUIRING_PASSPHRASE } from '../constants/exchanges'

/**
 * 交易所 API 區塊是否「視覺上為空」：無憑證、無需展開即可編輯的選項（testnet / 非零手續費）。
 * 用於配置頁預設摺疊未填寫的交易所以節省縱向空間。
 */
export function isExchangeApiSlotVisuallyEmpty(
  config: { exchanges?: Record<string, unknown> } | null | undefined,
  exchange: string
): boolean {
  const slot = config?.exchanges?.[exchange] as Record<string, unknown> | undefined
  const api = String(slot?.api_key ?? '').trim()
  const secret = String(slot?.secret_key ?? '').trim()
  const pass = String(slot?.passphrase ?? '').trim()
  if (api || secret) return false
  if (EXCHANGES_REQUIRING_PASSPHRASE.includes(exchange as (typeof EXCHANGES_REQUIRING_PASSPHRASE)[number]) && pass) {
    return false
  }
  if (slot?.testnet) return false
  const fr = slot?.fee_rate
  const n = fr === undefined || fr === null ? 0 : Number(fr)
  if (Number.isFinite(n) && n !== 0) return false
  return true
}
