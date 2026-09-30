import { describe, expect, it } from 'vitest'
import { buildDailyEquityChartPoints, filterDailyStatsByRecentDays } from './dailyEquityChartData'

describe('filterDailyStatsByRecentDays', () => {
  it('sorts by date ascending', () => {
    const rows = [
      { date: '2026-04-10', x: 1 },
      { date: '2026-04-02', x: 2 },
    ]
    const out = filterDailyStatsByRecentDays(rows, 365)
    expect(out.map((r) => r.date)).toEqual(['2026-04-02', '2026-04-10'])
  })

  it('drops rows before the rolling cutoff', () => {
    const rows = [
      { date: '2099-01-08', x: 1 },
      { date: '2000-01-01', x: 2 },
    ]
    const out = filterDailyStatsByRecentDays(rows, 7)
    expect(out).toHaveLength(1)
    expect(out[0].date).toBe('2099-01-08')
  })
})

describe('buildDailyEquityChartPoints', () => {
  it('sums total_pnl and funding_fee for dailyNetWithFunding', () => {
    const pts = buildDailyEquityChartPoints([
      { date: '2026-04-04', total_pnl: 2.2, cumulative_pnl: 100, pnl_verified: true, funding_fee: -8.94, funding_fee_verified: true },
    ])
    expect(pts).toHaveLength(1)
    expect(pts[0].dailyNetWithFunding).toBeCloseTo(2.2 - 8.94)
    expect(pts[0].cumulativePnl).toBe(100)
  })

  it('maps account_equity from API when present', () => {
    const pts = buildDailyEquityChartPoints([
      { date: '2026-04-04', total_pnl: 0, cumulative_pnl: 10, pnl_verified: true, account_equity: 12345.67 },
    ])
    expect(pts[0].accountEquity).toBeCloseTo(12345.67)
  })

  it('does not chart unverified PnL or combine account-wide funding as symbol PnL', () => {
    const pts = buildDailyEquityChartPoints([
      { date: '2026-04-04', total_pnl: 9, cumulative_pnl: 9, funding_fee: 3 },
    ])
    expect(pts[0].cumulativePnl).toBeNull()
    expect(pts[0].dailyNetWithFunding).toBeNull()
  })
})
