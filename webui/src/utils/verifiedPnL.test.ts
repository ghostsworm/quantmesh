import { describe, expect, it } from 'vitest'
import { getVerifiedPnL } from './verifiedPnL'

describe('getVerifiedPnL', () => {
  it('returns the amount and normalized asset only with explicit verification', () => {
    expect(getVerifiedPnL({ total_pnl: 12.5, total_pnl_asset: ' usdc ', total_pnl_verified: true }))
      .toEqual({ amount: 12.5, asset: 'USDC' })
  })

  it('hides estimated, unlabelled, and non-finite values', () => {
    expect(getVerifiedPnL({ total_pnl: 999, total_pnl_asset: 'USDT' })).toBeNull()
    expect(getVerifiedPnL({ total_pnl: 999, total_pnl_asset: 'USDT', total_pnl_verified: false })).toBeNull()
    expect(getVerifiedPnL({ total_pnl: 999, total_pnl_verified: true })).toBeNull()
    expect(getVerifiedPnL({ total_pnl: Number.NaN, total_pnl_asset: 'USDT', total_pnl_verified: true })).toBeNull()
  })
})
