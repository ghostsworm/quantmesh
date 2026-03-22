import type { BotInfo } from '../services/api'

/**
 * 从 Bot 列表中提取唯一的交易所和币种集合（用于筛选下拉选项）
 */
export function getUniqueExchangesAndSymbols(bots: BotInfo[]) {
  const exchanges = new Set<string>()
  const symbols = new Set<string>()
  for (const b of bots) {
    if (b.exchange) exchanges.add(b.exchange)
    if (b.symbol) symbols.add(b.symbol)
  }
  return {
    uniqueExchanges: Array.from(exchanges).sort(),
    uniqueSymbols: Array.from(symbols).sort(),
  }
}

/**
 * 按交易所和币种筛选 Bot 列表
 */
export function filterBotsByExchangeAndSymbol(
  bots: BotInfo[],
  filterExchange: string,
  filterSymbol: string
): BotInfo[] {
  return bots.filter((b) => {
    if (filterExchange && b.exchange !== filterExchange) return false
    if (filterSymbol && b.symbol !== filterSymbol) return false
    return true
  })
}
