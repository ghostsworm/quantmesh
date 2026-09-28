import { describe, expect, it } from 'vitest'
import { optimizerNumber, optimizerRange, optimizerSearchSpace } from './optimizerInput'

describe('optimizer numeric submission boundary', () => {
  it.each([undefined, '', ' ', '-', '.', '1e', '10oops', 'Infinity', Infinity, NaN])('rejects invalid input %s without a default', (value) => {
    expect(optimizerNumber(value)).toBeUndefined()
  })

  it.each([['3.', 3], ['0.10', 0.1], ['.5', 0.5], [12, 12]] as const)('accepts complete decimal value %s', (input, expected) => {
    expect(optimizerNumber(input)).toBe(expected)
  })

  it('preserves decimal ranges and rejects empty, reversed or zero-step ranges', () => {
    expect(optimizerRange(['0.10', '0.30', '0.01'])).toEqual({ min: 0.1, max: 0.3, step: 0.01 })
    expect(optimizerRange(['', 3, 1])).toBeUndefined()
    expect(optimizerRange([3, 2, 1])).toBeUndefined()
    expect(optimizerRange([1, 3, 0])).toBeUndefined()
    expect(optimizerRange([0, 3, 1])).toBeUndefined()
  })

  it('never rounds fractional grid counts or accepts unsafe integer counts', () => {
    expect(optimizerRange([1.5, 3, 1], true)).toBeUndefined()
    expect(optimizerRange([1, 3, 0.1], true)).toBeUndefined()
    expect(optimizerRange([1, Number.MAX_SAFE_INTEGER + 1, 1], true)).toBeUndefined()
    expect(optimizerRange(['10', '30', '5'], true)).toEqual({ min: 10, max: 30, step: 5 })
  })

  it('produces numeric payloads, allows overlapping search ranges, and rejects impossible spans', () => {
    const space = optimizerSearchSpace(['1', '2', '.1'], ['3', '4', '.1'], ['10', '30', '5'], ['50', '200', '50'])
    expect(space?.grid_count_range.min).toBe(10)
    expect(space?.price_low_range.step).toBe(0.1)
    expect(optimizerSearchSpace([1, 3, 1], [2, 4, 1], [10, 30, 5], [50, 200, 50])).toBeDefined()
    expect(optimizerSearchSpace([4, 5, 1], [3, 4, 1], [10, 30, 5], [50, 200, 50])).toBeUndefined()
    expect(optimizerSearchSpace([1, 1, 1], [3, 4, 1], [10, 30, 5], [50, 200, 50])).toBeUndefined()
    expect(optimizerSearchSpace([1, 2, 1], [3, 4, 1], [10, 30, ''], [50, 200, 50])).toBeUndefined()
  })
})
