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

    // Bot 顶层网格参数 → 注入到每个 grid 策略的 config 中
    const topLevelGridConfig: Record<string, unknown> = {}
    const pi = cfg?.price_interval
    if (typeof pi === 'number' && pi > 0) topLevelGridConfig.grid_spacing = pi
    const oq = cfg?.order_quantity
    if (typeof oq === 'number' && oq > 0) topLevelGridConfig.order_quantity = oq
    const ps = cfg?.profit_spread
    if (typeof ps === 'number' && ps > 0) topLevelGridConfig.profit_spread = ps
    const ml = openCtrl?.max_position_layers
    if (typeof ml === 'number' && ml > 0) {
      topLevelGridConfig.grid_count = ml
    } else {
      const bw = cfg?.buy_window_size as number | undefined
      const sw = cfg?.sell_window_size as number | undefined
      const total = (typeof bw === 'number' ? bw : 0) + (typeof sw === 'number' ? sw : 0)
      if (total > 0) topLevelGridConfig.grid_count = total
    }
    const dir = cfg?.direction as string | undefined
    if (dir) topLevelGridConfig.direction = dir

    params.set(
      'strategies',
      JSON.stringify(
        strategies.map((strategy) => {
          const sType = strategy.type || 'grid'
          const isGridLike = sType === 'grid' || sType === 'grid+trend'
          const mergedConfig = isGridLike
            ? { ...topLevelGridConfig, ...(strategy.config || {}) }
            : { ...(strategy.config || {}) }
          return {
            type: sType,
            weight: typeof strategy.weight === 'number' ? strategy.weight : 0,
            config: mergedConfig,
          }
        })
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
