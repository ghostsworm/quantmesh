import { describe, expect, it } from 'vitest'
import { botLifecycleState, botsForTradingScope } from './botLifecycle'
import type { BotInfo } from './api'

describe('Bot lifecycle controls', () => {
  it.each([false, true])('pending stop takes precedence over running=%s', (running) => {
    expect(botLifecycleState({ running, stop_pending: true })).toBe('stop_pending')
  })
  it('preserves running and stopped for legacy responses', () => {
    expect(botLifecycleState({ running: true })).toBe('running')
    expect(botLifecycleState({ running: false })).toBe('stopped')
  })
  it('keeps pending owners in the exact symbol market scope without guessing a sole owner', () => {
    const bots = [
      { bot_id: 'pending', exchange: 'BINANCE', symbol: 'BTCUSDT', market_type: 'futures', running: false, stop_pending: true },
      { bot_id: 'peer', exchange: 'binance', symbol: 'BTCUSDT', market_type: 'futures', running: true },
      { bot_id: 'spot', exchange: 'binance', symbol: 'BTCUSDT', market_type: 'spot', running: true },
    ] as BotInfo[]
    const scoped = botsForTradingScope(bots, value => value.toLowerCase(), 'binance', 'BTCUSDT')
    expect(scoped.map(bot => bot.bot_id)).toEqual(['pending', 'peer'])
    expect(scoped.some(bot => bot.stop_pending)).toBe(true)
    expect(botsForTradingScope(bots, value => value.toLowerCase(), 'binance', 'BTCUSDT', 'spot').some(bot => bot.stop_pending)).toBe(false)
  })
})
