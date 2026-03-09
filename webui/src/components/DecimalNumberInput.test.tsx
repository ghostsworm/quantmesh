import { describe, expect, it } from 'vitest'

import { parseDecimalValue } from './DecimalNumberInput'

describe('parseDecimalValue', () => {
  it('preserves trailing dot for decimal input (e.g. "3.")', () => {
    expect(parseDecimalValue('3.', 3)).toBe('3.')
  })

  it('returns number when input is complete decimal', () => {
    expect(parseDecimalValue('3.5', 3.5)).toBe(3.5)
  })

  it('returns number for integer input', () => {
    expect(parseDecimalValue('10', 10)).toBe(10)
  })

  it('returns undefined for empty string', () => {
    expect(parseDecimalValue('', NaN)).toBeUndefined()
  })

  it('preserves partial input like "0."', () => {
    expect(parseDecimalValue('0.', 0)).toBe('0.')
  })
})
