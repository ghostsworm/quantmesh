import type { DecimalNumberInputValue } from '../components/DecimalNumberInput'
import type { OptimSearchSpace } from '../services/backtest'
import { optimizerNumber, optimizerRange } from './optimizerInput'

export const REPLAY_MAX_CANDIDATES = 32
export const REPLAY_MAX_FOLDS = 12
const DAY_MS = 86_400_000
const MAX_CANDLES = 100_000
const MAX_CANDLE_EVALUATIONS = 2_000_000
const MIN_TRAIN_BARS = 50
const MIN_TEST_BARS = 10
const INTERVAL_MS: Record<string, number> = {
  '1m': 60_000, '3m': 180_000, '5m': 300_000, '15m': 900_000,
  '30m': 1_800_000, '1h': 3_600_000, '4h': 14_400_000, '1d': DAY_MS,
}

export const REPLAY_FEATURES = [
  'regime_filter', 'adaptive_interval', 'upper_bound_freeze', 'inventory_skew', 'fee_aware_spread',
] as const
export type ReplayFeature = typeof REPLAY_FEATURES[number]
export const REPLAY_NUMERIC_DEFAULTS = {
  trainDays: 3, testDays: 1, leverage: 1,
  spacingMin: 100, spacingMax: 200, spacingStep: 100,
  quantityMin: 100, quantityMax: 200, quantityStep: 100,
  baselineSpacing: 100, baselineQuantity: 100, profitSpread: 0,
  buyWindow: 10, sellWindow: 10,
  makerFee: 0.0002, takerFee: 0.0005, fundingRate: 0,
}
export type ReplayNumericField = keyof typeof REPLAY_NUMERIC_DEFAULTS
export type ReplayFormValues = Record<ReplayNumericField, DecimalNumberInputValue> & {
  direction: 'LONG' | 'SHORT'
  fundingEnabled: boolean
  features: Record<ReplayFeature, boolean>
}

export function createReplayFormValues(): ReplayFormValues {
  return {
    ...REPLAY_NUMERIC_DEFAULTS,
    direction: 'LONG', fundingEnabled: false,
    features: { regime_filter: false, adaptive_interval: false, upper_bound_freeze: false, inventory_skew: false, fee_aware_spread: false },
  }
}

export type ReplayInputError = 'invalidInput' | 'candidateLimit' | 'windowRange' | 'foldLimit' | 'windowAlignment' | 'windowBars' | 'budgetLimit'
type ReplayBuildResult = { ok: false; error: ReplayInputError } | {
  ok: true
  searchSpace: OptimSearchSpace
  candidates: number
  folds: number
  startTime: string
  endTime: string
}

/** Dates denote inclusive UTC calendar days; fold ends in results are exclusive. */
export function buildReplaySearchSpace(
  values: ReplayFormValues, startDate: string, endDate: string, interval: string,
): ReplayBuildResult {
  const invalid = { ok: false, error: 'invalidInput' } as const
  const parsed = {} as Record<ReplayNumericField, number>
  for (const key of Object.keys(REPLAY_NUMERIC_DEFAULTS) as ReplayNumericField[]) {
    const number = optimizerNumber(values[key])
    if (number === undefined) return invalid
    parsed[key] = number
  }
  const spacing = optimizerRange([values.spacingMin, values.spacingMax, values.spacingStep])
  const quantity = optimizerRange([values.quantityMin, values.quantityMax, values.quantityStep])
  if (!spacing || !quantity || parsed.baselineSpacing <= 0 || parsed.baselineQuantity <= 0 || parsed.profitSpread < 0 ||
      !Number.isInteger(parsed.leverage) || parsed.leverage < 1 || parsed.leverage > 125 ||
      [parsed.buyWindow, parsed.sellWindow].some((v) => !Number.isInteger(v) || v < 1 || v > 1000) ||
      [parsed.makerFee, parsed.takerFee].some((v) => v < 0 || v > 1) || Math.abs(parsed.fundingRate) > 1 ||
      !['LONG', 'SHORT'].includes(values.direction)) return invalid

  if ([spacing, quantity].some((r) => r.min !== r.max && (r.min + r.step <= r.min || r.max + r.step <= r.max))) return invalid
  const count = (range: typeof spacing) => Math.floor((range.max - range.min) / range.step) + 1
  const candidates = count(spacing) * count(quantity)
  if (!Number.isSafeInteger(candidates) || candidates > REPLAY_MAX_CANDIDATES) return { ok: false, error: 'candidateLimit' }

  const start = Date.parse(`${startDate}T00:00:00.000Z`)
  const end = Date.parse(`${endDate}T00:00:00.000Z`) + DAY_MS
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start ||
      new Date(start).toISOString().slice(0, 10) !== startDate || new Date(end - DAY_MS).toISOString().slice(0, 10) !== endDate) return invalid
  const trainMs = parsed.trainDays * DAY_MS
  const testMs = parsed.testDays * DAY_MS
  if (trainMs <= 0 || testMs <= 0 || trainMs + testMs > end - start) return { ok: false, error: 'windowRange' }
  const barMs = INTERVAL_MS[interval]
  if (!barMs || !Number.isSafeInteger(trainMs) || !Number.isSafeInteger(testMs) || trainMs % barMs || testMs % barMs) {
    return { ok: false, error: 'windowAlignment' }
  }
  if (trainMs / barMs < MIN_TRAIN_BARS || testMs / barMs < MIN_TEST_BARS) return { ok: false, error: 'windowBars' }
  const folds = 1 + Math.floor((end - start - trainMs - testMs) / testMs)
  if (folds > REPLAY_MAX_FOLDS) return { ok: false, error: 'foldLimit' }
  const workBars = folds * ((candidates + 1) * trainMs / barMs + 2 * testMs / barMs)
  if ((end - start) / barMs > MAX_CANDLES || workBars > MAX_CANDLE_EVALUATIONS) return { ok: false, error: 'budgetLimit' }

  return {
    ok: true, candidates, folds,
    startTime: new Date(start).toISOString(), endTime: new Date(end - 1).toISOString(),
    searchSpace: {
      strategy: 'grid', ranges: { grid_spacing: spacing, order_quantity: quantity },
      replay_walk_forward: {
        train_days: parsed.trainDays, test_days: parsed.testDays, step_days: parsed.testDays, leverage: parsed.leverage,
        base_params: {
          grid_spacing: parsed.baselineSpacing, order_quantity: parsed.baselineQuantity,
          profit_spread: parsed.profitSpread, buy_window_size: parsed.buyWindow, sell_window_size: parsed.sellWindow,
          direction: values.direction, market_type: 'futures',
          maker_fee_rate: parsed.makerFee, taker_fee_rate: parsed.takerFee,
          funding_enabled: values.fundingEnabled, funding_rate: values.fundingEnabled ? parsed.fundingRate : 0,
          participation_rate: 1, queue_factor: 0, fill_on_touch: false,
          intrabar_path: 'auto', intrabar_steps_per_leg: 1, adjust_interval_ms: 50,
          price_decimals: 2, quantity_decimals: 6, enforce_margin: true,
          order_cleaner: true, order_cleanup_interval: 60, post_only_reprice_max_attempts: 3,
          ...Object.fromEntries(REPLAY_FEATURES.map((feature) => [feature, {
            enabled: values.features[feature], ...(feature === 'regime_filter' ? { kline_interval: interval } : {}),
          }])),
        },
      },
    },
  }
}
