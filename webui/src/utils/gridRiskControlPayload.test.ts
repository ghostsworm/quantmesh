import { describe, it, expect } from 'vitest'
import { toRatio, normalizeGridRiskControlPayload } from './gridRiskControlPayload'

describe('toRatio', () => {
  it('converts percentage string to 0-1 ratio', () => {
    expect(toRatio('15')).toBe(0.15)
    expect(toRatio('10')).toBe(0.1)
    expect(toRatio('100')).toBe(1)
  })

  it('keeps 0-1 number as-is', () => {
    expect(toRatio(0.15)).toBe(0.15)
    expect(toRatio(0.1)).toBe(0.1)
    expect(toRatio(1)).toBe(1)
  })

  it('converts percentage number > 1 to 0-1', () => {
    expect(toRatio(15)).toBe(0.15)
    expect(toRatio(10)).toBe(0.1)
  })

  it('handles null/undefined', () => {
    expect(toRatio(null)).toBe(0)
    expect(toRatio(undefined)).toBe(0)
  })

  it('handles invalid string', () => {
    expect(toRatio('')).toBe(0)
    expect(toRatio('abc')).toBe(0)
  })
})

describe('normalizeGridRiskControlPayload', () => {
  it('converts string ratio fields to number for backend', () => {
    const input = {
      enabled: true,
      stop_loss_ratio: '15',
      take_profit_trigger_ratio: '8',
      trailing_take_profit_ratio: '3',
    }
    const out = normalizeGridRiskControlPayload(input)
    expect(out.stop_loss_ratio).toBe(0.15)
    expect(out.take_profit_trigger_ratio).toBe(0.08)
    expect(out.trailing_take_profit_ratio).toBe(0.03)
  })

  it('handles number inputs', () => {
    const input = {
      stop_loss_ratio: 0.15,
      take_profit_trigger_ratio: 0.08,
      trailing_take_profit_ratio: 0.03,
    }
    const out = normalizeGridRiskControlPayload(input)
    expect(out.stop_loss_ratio).toBe(0.15)
    expect(out.take_profit_trigger_ratio).toBe(0.08)
    expect(out.trailing_take_profit_ratio).toBe(0.03)
  })

  it('converts max_grid_layers string to number', () => {
    const input = { max_grid_layers: '10' }
    const out = normalizeGridRiskControlPayload(input)
    expect(out.max_grid_layers).toBe(10)
  })

  it('preserves non-string max_grid_layers', () => {
    const input = { max_grid_layers: 5 }
    const out = normalizeGridRiskControlPayload(input)
    expect(out.max_grid_layers).toBe(5)
  })

  it('preserves trend_filter_enabled independently of enabled', () => {
    const input = { enabled: false, trend_filter_enabled: true }
    const out = normalizeGridRiskControlPayload(input)
    expect(out.trend_filter_enabled).toBe(true)
    expect(out.enabled).toBe(false)
  })

  it('normalizes close_condition_profit_target and close_condition_loss_limit', () => {
    const input = {
      close_condition_enabled: true,
      close_condition_profit_target: 20,
      close_condition_loss_limit: '10',
    }
    const out = normalizeGridRiskControlPayload(input)
    expect(out.close_condition_profit_target).toBe(0.2)
    expect(out.close_condition_loss_limit).toBe(0.1)
    expect(out.close_condition_enabled).toBe(true)
  })
})
