import { describe, expect, it } from 'vitest'
import { formatWithdrawAmount } from './withdrawalDisplay'

describe('formatWithdrawAmount', () => {
  it('uses the record currency instead of assuming USDT', () => {
    expect(formatWithdrawAmount(12.5, 'btc')).toBe('12.50 BTC')
    expect(formatWithdrawAmount(12.5, 'USD')).toBe('12.50 USD')
  })

  it('marks a missing or malformed currency as unknown', () => {
    expect(formatWithdrawAmount(12.5)).toBe('12.50 —')
    expect(formatWithdrawAmount(12.5, 'USDT;USD')).toBe('12.50 —')
  })

  it('does not display a non-finite amount as a financial value', () => {
    expect(formatWithdrawAmount(Number.NaN, 'USDT')).toBe('—')
    expect(formatWithdrawAmount(Number.POSITIVE_INFINITY, 'USDT')).toBe('—')
  })
})
