import { describe, it, expect } from 'vitest'
import {
  SUPPORTED_EXCHANGES,
  EXCHANGES_REQUIRING_PASSPHRASE,
  SPOT_SUPPORTED_EXCHANGES,
} from './exchanges'

describe('exchanges constants', () => {
  it('SUPPORTED_EXCHANGES includes OKX and major exchanges', () => {
    expect(SUPPORTED_EXCHANGES).toContain('okx')
    expect(SUPPORTED_EXCHANGES).toContain('binance')
    expect(SUPPORTED_EXCHANGES).toContain('bitget')
    expect(SUPPORTED_EXCHANGES).toContain('bybit')
    expect(SUPPORTED_EXCHANGES).toContain('gate')
  })

  it('EXCHANGES_REQUIRING_PASSPHRASE includes okx, bitget, kucoin', () => {
    expect(EXCHANGES_REQUIRING_PASSPHRASE).toContain('okx')
    expect(EXCHANGES_REQUIRING_PASSPHRASE).toContain('bitget')
    expect(EXCHANGES_REQUIRING_PASSPHRASE).toContain('kucoin')
  })

  it('SPOT_SUPPORTED_EXCHANGES includes okx', () => {
    expect(SPOT_SUPPORTED_EXCHANGES).toContain('okx')
    expect(SPOT_SUPPORTED_EXCHANGES).toContain('binance')
  })

  it('SPOT_SUPPORTED_EXCHANGES is subset of SUPPORTED_EXCHANGES', () => {
    for (const ex of SPOT_SUPPORTED_EXCHANGES) {
      expect(SUPPORTED_EXCHANGES).toContain(ex)
    }
  })
})
