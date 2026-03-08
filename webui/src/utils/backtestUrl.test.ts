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
})
