import { describe, it, expect } from 'vitest'
import { toRatio, normalizeGridRiskControlPayload, normalizeBotRiskControlPayload, riskPercentDisplay } from './gridRiskControlPayload'

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

  it('rejects out-of-range API ratios instead of guessing units', () => {
    expect(() => toRatio(15)).toThrow()
    expect(() => toRatio(10)).toThrow()
  })

  it('handles null/undefined', () => {
    expect(() => toRatio(null)).toThrow()
    expect(() => toRatio(undefined)).toThrow()
  })

  it('handles invalid string', () => {
    expect(() => toRatio('')).toThrow()
    expect(() => toRatio('abc')).toThrow()
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
      close_condition_profit_target: 0.2,
      close_condition_loss_limit: '10',
    }
    const out = normalizeGridRiskControlPayload(input)
    expect(out.close_condition_profit_target).toBe(0.2)
    expect(out.close_condition_loss_limit).toBe(0.1)
    expect(out.close_condition_enabled).toBe(true)
  })
})

describe('explicit risk editor units', () => {
  it('preserves sub-one percentages and partially typed decimal display', () => {
    expect(toRatio('0.5')).toBe(0.005)
    expect(toRatio('1.')).toBe(0.01)
    expect(riskPercentDisplay('0.')).toBe('0.')
    expect(riskPercentDisplay(0.005)).toBe(0.5)
  })

  it('does not create zero limits for absent fields and preserves explicit zero', () => {
    expect(JSON.parse(JSON.stringify(normalizeBotRiskControlPayload({ enabled: true })))).toEqual({ enabled: true })
    const payload = normalizeBotRiskControlPayload({
      max_position_quantity: '0',
      stop_loss_ratio: '0',
      take_profit_ratio: '0.2',
      trailing_stop_ratio: '0.1',
    })
    expect(payload.max_position_quantity).toBe(0)
    expect(payload).not.toHaveProperty('stop_loss_ratio')
    expect(payload).not.toHaveProperty('take_profit_ratio')
    expect(payload).not.toHaveProperty('trailing_stop_ratio')
  })

  it.each(['', ' ', '2oops', 'Infinity', '-1', Infinity, NaN])('rejects malformed limits %s', (value) => {
    expect(() => normalizeBotRiskControlPayload({ max_position_value: value })).toThrow()
  })

  it('rejects fractional grid limits and percent overflow', () => {
    expect(() => normalizeGridRiskControlPayload({ max_grid_layers: '2.5' })).toThrow()
    expect(() => normalizeGridRiskControlPayload({ stop_loss_ratio: '101' })).toThrow()
    expect(() => normalizeBotRiskControlPayload({ max_open_orders: NaN })).toThrow()
  })

  it('omits unsupported root ratios and normalizes active nested grid ratios', () => {
    const draft = { stop_loss_ratio: '0.5', grid_risk_control: { stop_loss_ratio: '0.5', max_grid_layers: '12' } }
    const payload = normalizeBotRiskControlPayload(draft)
    expect(payload).not.toHaveProperty('stop_loss_ratio')
    expect(payload.grid_risk_control).toMatchObject({ stop_loss_ratio: 0.005, max_grid_layers: 12 })
    expect(draft.grid_risk_control.stop_loss_ratio).toBe('0.5')
  })
})
