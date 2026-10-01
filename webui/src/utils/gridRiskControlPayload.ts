import type { BotRiskControl, GridRiskControl } from '../services/botApi'

type NumericDraft<T> = { [K in keyof T]: NonNullable<T[K]> extends number ? T[K] | string : T[K] }
export type GridRiskControlPayload = NumericDraft<GridRiskControl>
export type BotRiskControlDraft = Omit<NumericDraft<BotRiskControl>, 'grid_risk_control'> & {
  grid_risk_control?: GridRiskControlPayload
}

function finiteNonnegative(value: unknown): number {
  if (typeof value !== 'number' && (typeof value !== 'string' || !/^(?:[0-9]+(?:[.][0-9]*)?|[.][0-9]+)$/.test(value))) {
    throw new Error('Invalid risk-control number')
  }
  const number = Number(value)
  if (!Number.isFinite(number) || number < 0) throw new Error('Invalid risk-control number')
  return number
}

/** Numbers are API ratios; strings are percent editor text, never guessed by magnitude. */
export function toRatio(value: unknown): number {
  const number = finiteNonnegative(value)
  const ratio = typeof value === 'string' ? number / 100 : number
  if (ratio > 1) throw new Error('Risk-control ratio exceeds 100%')
  return ratio
}

export function riskPercentDisplay(value: number | string | undefined): number | string | undefined {
  return typeof value === 'number' ? value * 100 : value
}

function read<T, K extends keyof T>(draft: T, key: K, ratio = false, integer = false): number | undefined {
  if (!(key in Object(draft))) return undefined
  const value = ratio ? toRatio(draft[key]) : finiteNonnegative(draft[key])
  if (integer && !Number.isSafeInteger(value)) throw new Error('Risk-control count must be an integer')
  return value
}

export function normalizeGridRiskControlPayload(draft: GridRiskControlPayload): GridRiskControl {
  return {
    ...draft,
    stop_loss_ratio: read(draft, 'stop_loss_ratio', true),
    take_profit_trigger_ratio: read(draft, 'take_profit_trigger_ratio', true),
    trailing_take_profit_ratio: read(draft, 'trailing_take_profit_ratio', true),
    close_condition_profit_target: read(draft, 'close_condition_profit_target', true),
    close_condition_loss_limit: read(draft, 'close_condition_loss_limit', true),
    max_grid_layers: read(draft, 'max_grid_layers', false, true),
    max_open_orders_at_cap: read(draft, 'max_open_orders_at_cap', false, true),
  }
}

export function normalizeBotRiskControlPayload(draft: BotRiskControlDraft): BotRiskControl {
  const {
    stop_loss_ratio: legacyStopLossRatio,
    take_profit_ratio: legacyTakeProfitRatio,
    trailing_stop_ratio: legacyTrailingStopRatio,
    ...supportedDraft
  } = draft
  // Legacy Bot-level ratios have no runtime consumer. Keep stored values for
  // compatibility, but never submit them as if they enabled protection.
  void legacyStopLossRatio
  void legacyTakeProfitRatio
  void legacyTrailingStopRatio
  return {
    ...supportedDraft,
    max_position_quantity: read(draft, 'max_position_quantity'),
    max_position_qty: read(draft, 'max_position_qty'),
    max_position_value: read(draft, 'max_position_value'),
    max_position_layers: read(draft, 'max_position_layers', false, true),
    max_open_orders: read(draft, 'max_open_orders', false, true),
    open_order_distance: read(draft, 'open_order_distance'),
    auto_resume_after: read(draft, 'auto_resume_after', false, true),
    grid_risk_control: draft.grid_risk_control === undefined ? undefined : normalizeGridRiskControlPayload(draft.grid_risk_control),
  }
}
