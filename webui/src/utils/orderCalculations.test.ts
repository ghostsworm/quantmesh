import { describe, expect, it } from 'vitest'
import { computeOrderTotalPrice, computeOrderCapitalUsage } from './orderCalculations'

describe('computeOrderTotalPrice', () => {
  it('returns price × quantity', () => {
    expect(computeOrderTotalPrice(100, 0.5)).toBe(50)
    expect(computeOrderTotalPrice(50000, 0.01)).toBe(500)
  })

  it('handles null/undefined as 0', () => {
    expect(computeOrderTotalPrice(null, 1)).toBe(0)
    expect(computeOrderTotalPrice(100, undefined)).toBe(0)
    expect(computeOrderTotalPrice(null, null)).toBe(0)
  })
})

describe('computeOrderCapitalUsage', () => {
  it('returns totalPrice / leverage when leverage > 0', () => {
    expect(computeOrderCapitalUsage(1000, 10)).toBe(100)
    expect(computeOrderCapitalUsage(500, 5)).toBe(100)
  })

  it('returns totalPrice when leverage <= 0 (spot or invalid)', () => {
    expect(computeOrderCapitalUsage(1000, 0)).toBe(1000)
    expect(computeOrderCapitalUsage(1000, -1)).toBe(1000)
  })
})
