import type { BotInfo } from '../services/api'

/** 首页交易所卡片仅有 Symbol，需用 bots 列表解析 bot_id 才能跳转详情 */
export function findBotIdForSymbol(
  bots: BotInfo[],
  normalizeExchange: (name: string | undefined) => string,
  exchange: string,
  symbol: string,
  marketType?: 'spot' | 'futures'
): string | undefined {
  const ex = normalizeExchange(exchange)
  const mt = (marketType || 'futures').toLowerCase()
  const exact = bots.find((b) => {
    if (normalizeExchange(b.exchange) !== ex) return false
    if (b.symbol.toUpperCase() !== symbol.toUpperCase()) return false
    return (b.market_type || 'futures').toLowerCase() === mt
  })
  if (exact) return exact.bot_id
  return bots.find(
    (b) =>
      normalizeExchange(b.exchange) === ex &&
      b.symbol.toUpperCase() === symbol.toUpperCase()
  )?.bot_id
}
