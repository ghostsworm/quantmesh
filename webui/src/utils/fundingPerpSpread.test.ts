import { describe, expect, it } from 'vitest'
import { parseFundingPerpSpread } from './fundingPerpSpread'
import type { BotDetailInfo } from '../services/api'

function baseBot(overrides: Partial<BotDetailInfo>): BotDetailInfo {
  return {
    bot_id: 'b1',
    name: 't',
    exchange: 'binance',
    symbol: 'BTCUSDT',
    market_type: 'futures',
    running: false,
    ...overrides,
  } as BotDetailInfo
}

describe('parseFundingPerpSpread', () => {
  it('returns null when market_type is not funding_perp_spread', () => {
    expect(parseFundingPerpSpread(baseBot({ market_type: 'futures' }))).toBeNull()
  })

  it('returns null when legs are incomplete', () => {
    expect(
      parseFundingPerpSpread(
        baseBot({
          market_type: 'funding_perp_spread',
          config: { funding_perp_spread: { leg_a: { exchange: 'a', symbol: 'x' }, leg_b: { exchange: '', symbol: 'y' } } },
        })
      )
    ).toBeNull()
  })

  it('returns config when valid', () => {
    const fp = {
      leg_a: { exchange: 'binance', symbol: 'BTCUSDT' },
      leg_b: { exchange: 'okx', symbol: 'BTC-USDT-SWAP' },
    }
    const r = parseFundingPerpSpread(
      baseBot({
        market_type: 'funding_perp_spread',
        config: { funding_perp_spread: fp },
      })
    )
    expect(r).toEqual(fp)
  })
})
