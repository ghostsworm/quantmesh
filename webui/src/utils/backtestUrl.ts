/**
 * 构建从 Bot 详情跳转到全局回测页的 URL，预填参数
 */
export interface BotConfigForBacktest {
  exchange?: string
  symbol?: string
  market_type?: string
  bot_id?: string
  config?: Record<string, unknown>
}

export function buildBacktestUrl(bot: BotConfigForBacktest | null): string {
  if (!bot?.exchange || !bot?.symbol) return '/backtest'
  const cfg = bot.config as Record<string, unknown> | undefined
  const openCtrl = cfg?.open_position_control as Record<string, unknown> | undefined
  const strategies = cfg?.strategies as Array<{ type?: string; weight?: number; config?: Record<string, unknown> }> | undefined
  const strategyType = strategies?.[0]?.type || 'grid'
  const params = new URLSearchParams()
  params.set('exchange', bot.exchange)
  params.set('market_type', bot.market_type || 'futures')
  params.set('symbol', bot.symbol)
  params.set('strategy', strategyType)
  if (bot.bot_id) params.set('bot_id', bot.bot_id)
  if (strategies && strategies.length > 0) {
    params.set('mode', 'bot_strategies')
    params.set(
      'strategies',
      JSON.stringify(
        strategies.map((strategy) => ({
          type: strategy.type || 'grid',
          weight: typeof strategy.weight === 'number' ? strategy.weight : 0,
          config: strategy.config || {},
        }))
      )
    )
  }
  params.set('days', '7')
  const maxVal = openCtrl?.max_position_value
  if (typeof maxVal === 'number' && maxVal > 0) params.set('total_capital', String(maxVal))
  const priceInterval = cfg?.price_interval
  if (typeof priceInterval === 'number' && priceInterval > 0) params.set('grid_spacing', String(priceInterval))
  const orderQty = cfg?.order_quantity
  if (typeof orderQty === 'number' && orderQty > 0) params.set('order_quantity', String(orderQty))
  const profitSpread = cfg?.profit_spread
  if (typeof profitSpread === 'number' && profitSpread > 0) params.set('profit_spread', String(profitSpread))
  // 格子数量：优先用 max_position_layers，否则用 buy_window_size + sell_window_size
  const maxLayers = openCtrl?.max_position_layers
  if (typeof maxLayers === 'number' && maxLayers > 0) {
    params.set('grid_count', String(maxLayers))
  } else {
    const buyWin = cfg?.buy_window_size as number | undefined
    const sellWin = cfg?.sell_window_size as number | undefined
    const totalGrids = (typeof buyWin === 'number' ? buyWin : 0) + (typeof sellWin === 'number' ? sellWin : 0)
    if (totalGrids > 0) params.set('grid_count', String(totalGrids))
  }
  return `/backtest?${params.toString()}`
}
