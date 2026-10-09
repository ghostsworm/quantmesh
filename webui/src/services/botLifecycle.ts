import type { BotInfo } from './api'

export type BotLifecycleState = 'stop_pending' | 'running' | 'stopped'

export function botLifecycleState(bot: Pick<BotInfo, 'running' | 'stop_pending'>): BotLifecycleState {
  if (bot.stop_pending) return 'stop_pending'
  return bot.running ? 'running' : 'stopped'
}

export function botsForTradingScope(bots: BotInfo[], normalize: (exchange: string) => string, exchange: string, symbol: string, market = 'futures'): BotInfo[] {
  return bots.filter(bot => normalize(bot.exchange) === normalize(exchange) && bot.symbol === symbol && (bot.market_type || 'futures') === market)
}
