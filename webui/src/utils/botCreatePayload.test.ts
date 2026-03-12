import { describe, it, expect } from 'vitest'

/**
 * 与 BotCreateWizard handleSubmit 中使用的 toNum 逻辑一致
 */
function toNum(v: number | string | undefined, def: number): number {
  if (typeof v === 'number' && !Number.isNaN(v)) return v
  if (v == null || v === '') return def
  const n = parseFloat(String(v))
  return Number.isNaN(n) ? def : n
}

describe('botCreatePayload', () => {
  it('toNum returns 0 for empty profit_spread when default is 0', () => {
    expect(toNum('', 0)).toBe(0)
    expect(toNum(undefined, 0)).toBe(0)
  })

  it('toNum returns user value for profit_spread when set', () => {
    expect(toNum(100, 0)).toBe(100)
    expect(toNum('80.5', 0)).toBe(80.5)
  })

  it('profit_spread in baseReq is 0 when form.profit_spread is empty', () => {
    const form = { profit_spread: '' as const }
    const profit_spread = toNum(form.profit_spread, 0)
    expect(profit_spread).toBe(0)
  })

  it('profit_spread in baseReq equals user value when set', () => {
    const form = { profit_spread: 120 }
    const profit_spread = toNum(form.profit_spread, 0)
    expect(profit_spread).toBe(120)
  })
})
