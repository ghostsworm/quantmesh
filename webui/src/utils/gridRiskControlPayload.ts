/**
 * 網格風控 payload 正規化：前端用 % 顯示，後端用 0-1；DecimalNumberInput 可能傳 string，需統一轉 number
 */

export interface GridRiskControlPayload {
  enabled?: boolean
  stop_loss_ratio?: number | string
  take_profit_trigger_ratio?: number | string
  trailing_take_profit_ratio?: number | string
  max_grid_layers?: number | string
  trend_filter_enabled?: boolean
  [key: string]: unknown
}

/** 將比例值轉為 0-1：15 -> 0.15，0.15 -> 0.15；支援 string 輸入 */
export function toRatio(v: number | string | undefined): number {
  if (v == null) return 0
  const n = typeof v === 'string' ? parseFloat(v) : v
  if (Number.isNaN(n)) return 0
  return n > 1 ? n / 100 : n
}

/** 正規化 grid_risk_control payload，確保所有數值欄位為正確類型供後端解析 */
export function normalizeGridRiskControlPayload(
  grc: GridRiskControlPayload
): Record<string, unknown> {
  const out = { ...grc }
  out.stop_loss_ratio = toRatio(grc.stop_loss_ratio)
  out.take_profit_trigger_ratio = toRatio(grc.take_profit_trigger_ratio)
  out.trailing_take_profit_ratio = toRatio(grc.trailing_take_profit_ratio)
  if (typeof grc.max_grid_layers === 'string') {
    out.max_grid_layers = parseInt(String(grc.max_grid_layers), 10) || 0
  }
  return out
}
