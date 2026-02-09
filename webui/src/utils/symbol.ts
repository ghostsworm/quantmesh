/**
 * 從交易對符號中提取计价幣種（quote asset）
 * 例如: "BTCUSDT" -> "USDT", "BTCU" -> "U", "ETHFDUSD" -> "FDUSD"
 */
const KNOWN_QUOTE_ASSETS = ['USDT', 'USDC', 'BUSD', 'FDUSD', 'DAI', 'U'] as const

export function getQuoteAsset(symbol: string | null | undefined): string {
  if (!symbol) return 'USDT'
  const upper = symbol.toUpperCase()
  // 優先匹配較長的後綴，避免 "BTCUSDT" 被錯誤匹配為 "U"
  const sorted = [...KNOWN_QUOTE_ASSETS].sort((a, b) => b.length - a.length)
  for (const quote of sorted) {
    if (upper.endsWith(quote) && upper.length > quote.length) {
      return quote
    }
  }
  return 'USDT'
}

/**
 * 從交易對符號中提取基础幣種（base asset）
 * 例如: "BTCUSDT" -> "BTC", "BTCU" -> "BTC", "ETHFDUSD" -> "ETH"
 */
export function getBaseAsset(symbol: string | null | undefined): string {
  if (!symbol) return ''
  const upper = symbol.toUpperCase()
  const sorted = [...KNOWN_QUOTE_ASSETS].sort((a, b) => b.length - a.length)
  for (const quote of sorted) {
    if (upper.endsWith(quote) && upper.length > quote.length) {
      return upper.slice(0, upper.length - quote.length)
    }
  }
  return upper
}
