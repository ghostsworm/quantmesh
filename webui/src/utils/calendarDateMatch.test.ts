import { describe, expect, it } from 'vitest'
import { calendarMonthMatchesDateStr } from './calendarDateMatch'

describe('calendarMonthMatchesDateStr', () => {
  it('matches year and month from plain date string', () => {
    expect(calendarMonthMatchesDateStr('2026-04-07', 2026, 4)).toBe(true)
    expect(calendarMonthMatchesDateStr('2026-04-07', 2026, 3)).toBe(false)
  })

  it('rejects invalid format', () => {
    expect(calendarMonthMatchesDateStr('2026-4-7', 2026, 4)).toBe(false)
  })
})
