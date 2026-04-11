import { describe, it, expect } from 'vitest'
import { findBotIdForSymbol } from './botLookup'
import type { BotInfo } from '../services/api'

const norm = (name: string | undefined) =>
  (name ?? 'unknown').toLowerCase().replace(/\s*\[dryrun\]\s*/gi, '').trim()

function bot(partial: Partial<BotInfo> & Pick<BotInfo, 'bot_id' | 'exchange' | 'symbol'>): BotInfo {
  return {
    name: 'x',
    market_type: 'futures',
    running: false,
    ...partial,
  } as BotInfo
}

describe('findBotIdForSymbol', () => {
  it('matches exchange + symbol + market_type', () => {
    const bots = [
      bot({ bot_id: 'a', exchange: 'binance', symbol: 'BTCUSDT', market_type: 'futures' }),
      bot({ bot_id: 'b', exchange: 'binance', symbol: 'BTCUSDT', market_type: 'spot' }),
    ]
    expect(findBotIdForSymbol(bots, norm, 'binance', 'BTCUSDT', 'futures')).toBe('a')
    expect(findBotIdForSymbol(bots, norm, 'binance', 'btcusdt', 'spot')).toBe('b')
  })

  it('falls back to exchange + symbol when market_type differs', () => {
    const bots = [bot({ bot_id: 'only', exchange: 'binance', symbol: 'ETHUSDT', market_type: 'futures' })]
    expect(findBotIdForSymbol(bots, norm, 'binance', 'ETHUSDT', 'spot')).toBe('only')
  })
})
