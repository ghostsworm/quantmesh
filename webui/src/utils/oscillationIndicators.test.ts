import { describe, expect, it } from 'vitest'
import { computeShakeStrength, computeGridFriendly } from './oscillationIndicators'

describe('computeShakeStrength', () => {
  it('returns 0 for empty array', () => {
    expect(computeShakeStrength([])).toBe(0)
  })

  it('returns 0 when mid is 0', () => {
    expect(computeShakeStrength([-1, 1])).toBe(0)
  })

  it('computes correct value for symmetric oscillation', () => {
    // mid=100, closes [90,110,90,110] -> abs diff [10,10,10,10], mean=10, 10/100*100=10%
    const closes = [90, 110, 90, 110]
    expect(computeShakeStrength(closes)).toBe(10)
  })

  it('computes correct value for flat price', () => {
    const closes = [100, 100, 100, 100]
    expect(computeShakeStrength(closes)).toBe(0)
  })

  it('handles single value', () => {
    expect(computeShakeStrength([100])).toBe(0)
  })
})

describe('computeGridFriendly', () => {
  it('returns 0 for empty array', () => {
    expect(computeGridFriendly([])).toBe(0)
  })

  it('returns 1 for perfect symmetric oscillation', () => {
    // mid=100, [90,110,90,110] -> upper area = 10+10=20, lower area = 10+10=20, ratio=1
    const closes = [90, 110, 90, 110]
    expect(computeGridFriendly(closes)).toBe(1)
  })

  it('returns low value for one-sided trend', () => {
    // mid=150, [100,100,100,200] -> lower area 150, upper area 50 -> 50/150 ≈ 0.33
    const closes = [100, 100, 100, 200]
    const v = computeGridFriendly(closes)
    expect(v).toBeLessThan(0.5)
    expect(v).toBeGreaterThan(0)
  })

  it('returns 1 when all same (no area)', () => {
    const closes = [100, 100, 100]
    expect(computeGridFriendly(closes)).toBe(1)
  })

  it('returns ~0.5 for asymmetric but both sides', () => {
    // mid=100, [90,110,110,110] -> lower 10, upper 10+10+10=30, min/max = 10/30 = 0.333
    const closes = [90, 110, 110, 110]
    const v = computeGridFriendly(closes)
    expect(v).toBeGreaterThan(0)
    expect(v).toBeLessThan(1)
  })
})
