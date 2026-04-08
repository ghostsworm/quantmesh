/** 日统计行（与 api DailyStatistics / Statistics 页内联类型对齐的最小字段） */
export interface DailyEquityStatRow {
  date: string
  total_pnl: number
  cumulative_pnl?: number
  funding_fee?: number
  /** 交易所 API 帳戶權益（USDT） */
  account_equity?: number
}

export function filterDailyStatsByRecentDays<T extends { date: string }>(items: T[], days: number): T[] {
  if (!items.length) return []
  const sorted = [...items].sort((a, b) => new Date(a.date).getTime() - new Date(b.date).getTime())
  const cutoff = new Date()
  cutoff.setHours(0, 0, 0, 0)
  cutoff.setDate(cutoff.getDate() - days)
  return sorted.filter((r) => {
    const d = new Date(r.date)
    if (Number.isNaN(d.getTime())) return false
    d.setHours(0, 0, 0, 0)
    return d >= cutoff
  })
}

export interface DailyEquityChartPoint {
  dateKey: string
  label: string
  cumulativePnl: number
  /** 当日综合：网格/策略盈亏 + 资金费（用于观察与「累计盈亏」列是否因资金费产生体感偏差） */
  dailyNetWithFunding: number
  /** 交易所帳戶權益（USDT），無採樣時為 undefined */
  accountEquity?: number
}

export function buildDailyEquityChartPoints(items: DailyEquityStatRow[]): DailyEquityChartPoint[] {
  return items.map((r) => {
    const d = new Date(r.date)
    const label = Number.isNaN(d.getTime())
      ? r.date
      : `${d.getMonth() + 1}/${d.getDate()}`
    const ae = r.account_equity
    return {
      dateKey: r.date,
      label,
      cumulativePnl: r.cumulative_pnl ?? 0,
      dailyNetWithFunding: r.total_pnl + (r.funding_fee ?? 0),
      accountEquity:
        ae !== undefined && ae !== null && !Number.isNaN(Number(ae)) ? Number(ae) : undefined,
    }
  })
}
