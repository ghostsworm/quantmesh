import type { CapitalHistoryItem } from '../types/capital'

export interface EquityCurvePoint {
  date: string
  balance: number
}

// Backward-compatible mapping: accept both old and new backend fields.
export function mapCapitalHistoryToEquityCurve(history: CapitalHistoryItem[]): EquityCurvePoint[] {
  return history
    .map((item) => ({
      date: item.timestamp?.slice(0, 10) || '',
      balance: item.total ?? item.totalBalance ?? 0,
    }))
    .sort((a, b) => a.date.localeCompare(b.date))
}
