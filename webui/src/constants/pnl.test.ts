import { describe, it, expect } from 'vitest'
import { PNL_EXCHANGE_MAX_RANGE_DAYS, getPnLExchangeDefaultRangeISO } from './pnl'

describe('pnl range helpers', () => {
  it('uses 90 days max', () => {
    expect(PNL_EXCHANGE_MAX_RANGE_DAYS).toBe(90)
  })

  it('getPnLExchangeDefaultRangeISO spans about 90 days', () => {
    const { startTime, endTime } = getPnLExchangeDefaultRangeISO()
    const start = new Date(startTime).getTime()
    const end = new Date(endTime).getTime()
    const days = (end - start) / (24 * 60 * 60 * 1000)
    expect(days).toBeGreaterThan(89)
    expect(days).toBeLessThan(91)
  })
})
