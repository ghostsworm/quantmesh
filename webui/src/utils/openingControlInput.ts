import type { OpenPositionControlConfig, ScheduleRule } from '../services/api'

export interface OpeningControlDraft {
  maxPositionValue: string
  maxPositionLayers: string
  periodicEnabled: boolean
  openDurationMin: string
  closeDurationMin: string
  scheduleRules: ScheduleRule[]
}

function numberInput(text: string, integer: boolean, minimum: number): number | undefined {
  if (!/^(?:\d+(?:\.\d*)?|\.\d+)$/.test(text)) return undefined
  const value = Number(text)
  if (!Number.isFinite(value) || value < minimum || (integer && !Number.isSafeInteger(value))) return undefined
  return value
}

/** Blank inputs must not silently remove a financial limit or substitute a duration. */
export function openingControlConfig(draft: OpeningControlDraft): OpenPositionControlConfig | undefined {
  const max_position_value = numberInput(draft.maxPositionValue, false, 0)
  const max_position_layers = numberInput(draft.maxPositionLayers, true, 0)
  const open = draft.periodicEnabled ? numberInput(draft.openDurationMin, true, 1) : 60
  const close = draft.periodicEnabled ? numberInput(draft.closeDurationMin, true, 1) : 30
  if (max_position_value === undefined || max_position_layers === undefined || open === undefined || close === undefined) return undefined
  if (draft.scheduleRules.some(rule => typeof rule.enabled !== 'boolean' ||
    !['pause', 'resume'].includes(rule.action) || !/^(?:[01]\d|2[0-3]):[0-5]\d$/.test(rule.time) ||
    rule.weekdays?.some(day => !Number.isInteger(day) || day < 0 || day > 6))) return undefined
  return {
    max_position_value, max_position_layers,
    periodic_rule: { enabled: draft.periodicEnabled, open_duration_min: open, close_duration_min: close },
    schedule_rules: draft.scheduleRules.map(rule => ({ ...rule, weekdays: rule.weekdays?.slice() })),
  }
}
