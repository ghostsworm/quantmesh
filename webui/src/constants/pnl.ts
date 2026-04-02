/** 与后端 `maxPnLExchangeQueryRange` 一致：按交易所聚合盈亏的最大查询跨度（天） */
export const PNL_EXCHANGE_MAX_RANGE_DAYS = 90

export function getPnLExchangeDefaultRangeISO(): { startTime: string; endTime: string } {
  const end = new Date()
  const start = new Date(end.getTime() - PNL_EXCHANGE_MAX_RANGE_DAYS * 24 * 60 * 60 * 1000)
  return { startTime: start.toISOString(), endTime: end.toISOString() }
}
