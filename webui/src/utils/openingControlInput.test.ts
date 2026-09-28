import { describe, expect, it } from 'vitest'
import { openingControlConfig, type OpeningControlDraft } from './openingControlInput'

const draft = (): OpeningControlDraft => ({ maxPositionValue: '100.50', maxPositionLayers: '3',
  periodicEnabled: true, openDurationMin: '60', closeDurationMin: '30',
  scheduleRules: [{ enabled: true, action: 'pause', time: '22:00', weekdays: [0, 6] }] })

describe('opening control submission', () => {
  it('preserves finite amounts, integers and explicit unlimited zero', () => {
    expect(openingControlConfig(draft())).toMatchObject({ max_position_value: 100.5, max_position_layers: 3 })
    expect(openingControlConfig({ ...draft(), maxPositionValue: '0', maxPositionLayers: '0' }))
      .toMatchObject({ max_position_value: 0, max_position_layers: 0 })
  })

  it('does not turn cleared, partial or invalid amounts into unlimited exposure', () => {
    for (const value of ['', ' ', '.', '-1', 'NaN', 'Infinity', '1e309', '12junk', '0x10']) {
      expect(openingControlConfig({ ...draft(), maxPositionValue: value }), value).toBeUndefined()
    }
  })

  it('rejects fractional and unsafe layer counts instead of truncating', () => {
    for (const value of ['', '2.5', '-1', '9007199254740992', '3x']) {
      expect(openingControlConfig({ ...draft(), maxPositionLayers: value }), value).toBeUndefined()
    }
  })

  it('requires positive integer durations without fallback', () => {
    for (const field of ['openDurationMin', 'closeDurationMin'] as const) {
      for (const value of ['', '0', '-1', '1.5', '30abc', '9007199254740992']) {
        expect(openingControlConfig({ ...draft(), [field]: value }), `${field}:${value}`).toBeUndefined()
      }
    }
    expect(openingControlConfig({ ...draft(), periodicEnabled: false, openDurationMin: '', closeDurationMin: '' }))
      .toMatchObject({ periodic_rule: { enabled: false } })
  })

  it('validates UTC times and weekdays even for disabled saved rules', () => {
    for (const time of ['', '24:00', '22:60', '7:00', '12:00junk']) {
      expect(openingControlConfig({ ...draft(), scheduleRules: [{ enabled: false, action: 'pause', time }] })).toBeUndefined()
    }
    for (const day of [-1, 7, 1.5, NaN]) {
      const input = draft()
      input.scheduleRules[0].weekdays = [day]
      expect(openingControlConfig(input)).toBeUndefined()
    }
  })

  it('returns independent schedule objects without mutating the draft', () => {
    const input = draft()
    const output = openingControlConfig(input)!
    output.schedule_rules![0].time = '01:00'
    output.schedule_rules![0].weekdays!.push(2)
    expect(input.scheduleRules[0]).toEqual({ enabled: true, action: 'pause', time: '22:00', weekdays: [0, 6] })
  })
})
