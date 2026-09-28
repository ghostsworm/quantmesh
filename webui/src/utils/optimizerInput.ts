import type { DecimalNumberInputValue } from '../components/DecimalNumberInput'
import type { OptimSearchSpace, Range } from '../services/optimizer'

/** Reject empty, partial and non-finite input rather than silently substituting a trading parameter. */
export function optimizerNumber(value: DecimalNumberInputValue): number | undefined {
  if (typeof value === 'number') return Number.isFinite(value) ? value : undefined
  if (typeof value !== 'string' || !/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)$/.test(value)) return undefined
  const number = Number(value)
  return Number.isFinite(number) ? number : undefined
}

export function optimizerRange(
  values: readonly [DecimalNumberInputValue, DecimalNumberInputValue, DecimalNumberInputValue],
  integer = false,
): Range | undefined {
  const [min, max, step] = values.map(optimizerNumber)
  if (min === undefined || max === undefined || step === undefined || min <= 0 || max < min || step <= 0) return undefined
  if (integer && ![min, max, step].every(Number.isSafeInteger)) return undefined
  return { min, max, step }
}

export function optimizerSearchSpace(
  low: Parameters<typeof optimizerRange>[0],
  high: Parameters<typeof optimizerRange>[0],
  grids: Parameters<typeof optimizerRange>[0],
  quantity: Parameters<typeof optimizerRange>[0],
): OptimSearchSpace | undefined {
  const price_low_range = optimizerRange(low)
  const price_high_range = optimizerRange(high)
  const grid_count_range = optimizerRange(grids, true)
  const order_qty_range = optimizerRange(quantity)
  if (!price_low_range || !price_high_range || !grid_count_range || !order_qty_range) return undefined
  // Match the backend's non-degenerate price ranges. Overlapping ranges are valid:
  // the optimizer filters individual candidates whose high price is not above low.
  if (price_low_range.min === price_low_range.max || price_high_range.min === price_high_range.max ||
      price_low_range.min >= price_high_range.max) return undefined
  return { price_low_range, price_high_range, grid_count_range, order_qty_range }
}
