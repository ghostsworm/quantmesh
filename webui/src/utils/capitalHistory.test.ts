import { describe, expect, it } from 'vitest'

import { mapCapitalHistoryToEquityCurve } from './capitalHistory'

describe('mapCapitalHistoryToEquityCurve', () => {
  it('uses total when backend returns capital history with total field', () => {
    const points = mapCapitalHistoryToEquityCurve([
      { timestamp: '2026-03-07T08:00:00Z', total: 12345.67 },
    ])

    expect(points).toEqual([{ date: '2026-03-07', balance: 12345.67 }])
  })

  it('falls back to totalBalance for compatibility', () => {
    const points = mapCapitalHistoryToEquityCurve([
      { timestamp: '2026-03-08T08:00:00Z', totalBalance: 8888.88 },
    ])

    expect(points).toEqual([{ date: '2026-03-08', balance: 8888.88 }])
  })

  it('falls back to 0 when no total fields are present', () => {
    const points = mapCapitalHistoryToEquityCurve([
      { timestamp: '2026-03-09T08:00:00Z' },
    ])

    expect(points).toEqual([{ date: '2026-03-09', balance: 0 }])
  })
})
