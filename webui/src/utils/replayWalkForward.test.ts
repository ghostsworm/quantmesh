import { describe, expect, it } from 'vitest'
import { buildReplaySearchSpace, createReplayFormValues, type ReplayFormValues, type ReplayNumericField } from './replayWalkForward'

const build = (changes: Partial<ReplayFormValues> = {}, end = '2026-01-07', interval = '1h') =>
  buildReplaySearchSpace({ ...createReplayFormValues(), ...changes }, '2026-01-01', end, interval)

describe('grid replay request', () => {
  it('uses four candidates and an independently declared deterministic baseline', () => {
    const result = build()
    expect(result.ok).toBe(true)
    if (!result.ok) throw new Error(result.error)
    expect(result.candidates).toBe(4)
    expect(result.folds).toBe(4)
    expect(result.startTime).toBe('2026-01-01T00:00:00.000Z')
    expect(result.endTime).toBe('2026-01-07T23:59:59.999Z')
    expect(result.searchSpace).toMatchObject({
      strategy: 'grid', ranges: { grid_spacing: { min: 100, max: 200, step: 100 }, order_quantity: { min: 100, max: 200, step: 100 } },
      replay_walk_forward: {
        train_days: 3, test_days: 1, step_days: 1, leverage: 1,
        base_params: { grid_spacing: 100, order_quantity: 100, maker_fee_rate: 0.0002, taker_fee_rate: 0.0005, funding_enabled: false, market_type: 'futures', direction: 'LONG' },
      },
    })
    expect(Object.keys(result.searchSpace.ranges).sort()).toEqual(['grid_spacing', 'order_quantity'])
    expect(JSON.stringify(result.searchSpace)).not.toMatch(/risk_volume_multiplier|risk_average_window|price_low_range/)
    const changed = build({ spacingMin: 300, spacingMax: 400, quantityMin: 200, quantityMax: 300 })
    if (!changed.ok) throw new Error(changed.error)
    expect(changed.searchSpace.replay_walk_forward?.base_params).toEqual(result.searchSpace.replay_walk_forward?.base_params)
  })

  it('preserves supplied baseline decimals, object feature controls, and signed settlement funding', () => {
    const values = createReplayFormValues()
    values.features.adaptive_interval = true
    values.features.regime_filter = true
    const result = build({ baselineSpacing: '12.345', baselineQuantity: '80.05', makerFee: '0.00015', fundingEnabled: true, fundingRate: '-0.0001', testDays: 2, features: values.features })
    if (!result.ok) throw new Error(result.error)
    expect(result.searchSpace.replay_walk_forward).toMatchObject({
      test_days: 2, step_days: 2,
      base_params: {
        grid_spacing: 12.345, order_quantity: 80.05, maker_fee_rate: 0.00015,
        funding_enabled: true, funding_rate: -0.0001,
        adaptive_interval: { enabled: true }, regime_filter: { enabled: true, kline_interval: '1h' },
      },
    })
  })

  it('freezes a fresh request object independent of later form changes', () => {
    const values = createReplayFormValues()
    const result = buildReplaySearchSpace(values, '2026-01-01', '2026-01-07', '1h')
    if (!result.ok) throw new Error(result.error)
    values.features.inventory_skew = true
    values.baselineSpacing = 900
    expect(result.searchSpace.replay_walk_forward?.base_params).toMatchObject({ grid_spacing: 100, inventory_skew: { enabled: false } })
  })

  it('allows exactly 32 candidates and rejects larger searches before submission', () => {
    expect(build({ spacingMax: 800, quantityMax: 400 })).toMatchObject({ ok: true, candidates: 32 })
    expect(build({ spacingMax: 900, quantityMax: 400 })).toEqual({ ok: false, error: 'candidateLimit' })
    expect(build({ spacingStep: 0.000001 })).toEqual({ ok: false, error: 'candidateLimit' })
  })

  it('allows exactly 12 folds and rejects more', () => {
    expect(build({}, '2026-01-15')).toMatchObject({ ok: true, folds: 12 })
    expect(build({}, '2026-01-16')).toEqual({ ok: false, error: 'foldLimit' })
  })

  it.each<[ReplayNumericField, string | number | undefined]>([
    ['baselineSpacing', ''], ['spacingMin', '-'], ['spacingStep', 0], ['quantityMin', undefined],
    ['leverage', 1.5], ['leverage', 126], ['makerFee', 1.1], ['takerFee', -0.1],
    ['fundingRate', -1.01], ['buyWindow', 0], ['sellWindow', 2.5], ['baselineQuantity', Infinity],
  ])('rejects invalid %s input %s', (field, value) => {
    expect(build({ [field]: value })).toMatchObject({ ok: false })
  })

  it('validates complete windows, candle alignment, and minimum warm-up coverage', () => {
    expect(build({ trainDays: 8 })).toEqual({ ok: false, error: 'windowRange' })
    expect(build({ testDays: 0 })).toEqual({ ok: false, error: 'windowRange' })
    expect(build({ testDays: 0.2 })).toEqual({ ok: false, error: 'windowAlignment' })
    expect(build({}, '2026-01-07', '1d')).toEqual({ ok: false, error: 'windowBars' })
  })

  it('enforces data and candle evaluation budgets', () => {
    expect(build({ trainDays: 80, testDays: 10 }, '2026-03-31', '1m')).toEqual({ ok: false, error: 'budgetLimit' })
    expect(build({ trainDays: 10, testDays: 1, spacingMax: 800, quantityMax: 400 }, '2026-01-20', '1m')).toEqual({ ok: false, error: 'budgetLimit' })
  })

  it('rejects malformed, normalized-away, and reversed dates', () => {
    for (const [start, end] of [['', '2026-01-07'], ['2026-02-30', '2026-03-07'], ['2026-01-07', '2026-01-01']]) {
      expect(buildReplaySearchSpace(createReplayFormValues(), start, end, '1h')).toEqual({ ok: false, error: 'invalidInput' })
    }
  })

  it('counts decimal ranges using the same floor rule as the backend', () => {
    expect(build({ spacingMin: 0.1, spacingMax: 0.3, spacingStep: 0.1, quantityMax: 100 })).toMatchObject({ ok: true, candidates: 2 })
  })
})
