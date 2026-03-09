import { describe, it, expect } from 'vitest'
import { buildBacktestUrl } from './backtestUrl'

describe('buildBacktestUrl', () => {
  it('returns /backtest when bot has no exchange or symbol', () => {
    expect(buildBacktestUrl(null)).toBe('/backtest')
    expect(buildBacktestUrl({})).toBe('/backtest')
    expect(buildBacktestUrl({ exchange: 'binance' })).toBe('/backtest')
    expect(buildBacktestUrl({ symbol: 'ETHUSDT' })).toBe('/backtest')
  })

  it('includes grid_spacing, order_quantity, grid_count from bot config', () => {
    const bot = {
      exchange: 'binance',
      symbol: 'ETHUSDT',
      market_type: 'futures',
      bot_id: 'test-bot-id',
      config: {
        strategies: [{ type: 'grid', weight: 1, config: {} }],
        price_interval: 3,
        order_quantity: 30,
        open_position_control: { max_position_value: 5000, max_position_layers: 20 },
      },
    }
    const url = buildBacktestUrl(bot)
    const params = new URLSearchParams(url.replace('/backtest?', ''))
    expect(params.get('grid_spacing')).toBe('3')
    expect(params.get('order_quantity')).toBe('30')
    expect(params.get('grid_count')).toBe('20')
    expect(params.get('total_capital')).toBe('5000')
    expect(params.get('symbol')).toBe('ETHUSDT')
  })

  it('uses buy_window_size + sell_window_size for grid_count when max_position_layers is 0', () => {
    const bot = {
      exchange: 'binance',
      symbol: 'BTCUSDT',
      config: {
        strategies: [{ type: 'grid', weight: 1 }],
        price_interval: 200,
        order_quantity: 100,
        open_position_control: { max_position_layers: 0 },
        buy_window_size: 10,
        sell_window_size: 10,
      },
    }
    const url = buildBacktestUrl(bot)
    const params = new URLSearchParams(url.replace('/backtest?', ''))
    expect(params.get('grid_count')).toBe('20')
    expect(params.get('grid_spacing')).toBe('200')
    expect(params.get('order_quantity')).toBe('100')
  })

  it('includes profit_spread from bot config', () => {
    const bot = {
      exchange: 'binance',
      symbol: 'BTCUSDT',
      config: {
        strategies: [{ type: 'grid', weight: 1, config: {} }],
        price_interval: 160,
        profit_spread: 200,
        order_quantity: 260,
        open_position_control: { max_position_value: 10000, max_position_layers: 15 },
      },
    }
    const url = buildBacktestUrl(bot)
    const params = new URLSearchParams(url.replace('/backtest?', ''))
    expect(params.get('grid_spacing')).toBe('160')
    expect(params.get('profit_spread')).toBe('200')
    expect(params.get('order_quantity')).toBe('260')
    expect(params.get('grid_count')).toBe('15')
  })

  it('merges bot top-level grid params into grid strategy config for multi-strategy', () => {
    const bot = {
      exchange: 'binance',
      symbol: 'BTCUSDT',
      config: {
        strategies: [
          { type: 'grid', weight: 0.6, config: {} },
          { type: 'trend_following', weight: 0.4, config: { fast_period: 10, slow_period: 30 } },
        ],
        price_interval: 160,
        profit_spread: 200,
        order_quantity: 260,
        direction: 'LONG',
        open_position_control: { max_position_value: 10000, max_position_layers: 15 },
      },
    }
    const url = buildBacktestUrl(bot)
    const params = new URLSearchParams(url.replace('/backtest?', ''))

    expect(params.get('mode')).toBe('bot_strategies')
    const strategies = JSON.parse(params.get('strategies') || '[]')
    expect(strategies).toHaveLength(2)

    // Grid strategy should have merged top-level params
    const gridStrategy = strategies[0]
    expect(gridStrategy.type).toBe('grid')
    expect(gridStrategy.weight).toBe(0.6)
    expect(gridStrategy.config.grid_spacing).toBe(160)
    expect(gridStrategy.config.profit_spread).toBe(200)
    expect(gridStrategy.config.order_quantity).toBe(260)
    expect(gridStrategy.config.grid_count).toBe(15)
    expect(gridStrategy.config.direction).toBe('LONG')

    // Trend following should keep its own config without grid params
    const trendStrategy = strategies[1]
    expect(trendStrategy.type).toBe('trend_following')
    expect(trendStrategy.weight).toBe(0.4)
    expect(trendStrategy.config.fast_period).toBe(10)
    expect(trendStrategy.config.slow_period).toBe(30)
    expect(trendStrategy.config.grid_spacing).toBeUndefined()
    expect(trendStrategy.config.order_quantity).toBeUndefined()
  })

  it('does not overwrite strategy-specific config with top-level params', () => {
    const bot = {
      exchange: 'binance',
      symbol: 'BTCUSDT',
      config: {
        strategies: [
          { type: 'grid', weight: 1, config: { grid_spacing: 300, order_quantity: 500 } },
        ],
        price_interval: 160,
        order_quantity: 260,
        open_position_control: { max_position_value: 10000 },
      },
    }
    const url = buildBacktestUrl(bot)
    const params = new URLSearchParams(url.replace('/backtest?', ''))
    const strategies = JSON.parse(params.get('strategies') || '[]')

    // Strategy-specific config should take precedence over top-level
    expect(strategies[0].config.grid_spacing).toBe(300)
    expect(strategies[0].config.order_quantity).toBe(500)
  })

  it('passes trend_following strategy params through', () => {
    const bot = {
      exchange: 'binance',
      symbol: 'BTCUSDT',
      config: {
        strategies: [
          { type: 'trend_following', weight: 1, config: { fast_period: 5, slow_period: 20 } },
        ],
        open_position_control: { max_position_value: 5000 },
      },
    }
    const url = buildBacktestUrl(bot)
    const params = new URLSearchParams(url.replace('/backtest?', ''))
    const strategies = JSON.parse(params.get('strategies') || '[]')

    expect(strategies[0].type).toBe('trend_following')
    expect(strategies[0].config.fast_period).toBe(5)
    expect(strategies[0].config.slow_period).toBe(20)
    // Non-grid strategy should not have grid params injected
    expect(strategies[0].config.grid_spacing).toBeUndefined()
  })
})
