import type { StrategyTemplateFull } from '../services/strategy'

export type TemplateSelectionMode = 'single' | 'combo' | 'hedge'

export interface ResolvedTemplateSelection {
  mode: TemplateSelectionMode
  singleStrategyId: string | null
  strategies: Array<{ type: string; weight: number }>
  strategyParams: Record<string, Record<string, unknown>>
}

export function resolveStrategyTemplateSelection(template: StrategyTemplateFull): ResolvedTemplateSelection {
  const config = template.config ?? {}
  const configuredStrategies = Array.isArray(config.strategies) ? config.strategies : []
  const mode: TemplateSelectionMode = template.strategy_type === 'combo'
    ? 'combo'
    : template.strategy_type === 'hedge'
      ? 'hedge'
      : 'single'

  const strategies = configuredStrategies.flatMap((item) => {
    if (typeof item === 'string') return [{ type: item, weight: 1 / Math.max(configuredStrategies.length, 1) }]
    if (typeof item !== 'object' || item === null || typeof item.type !== 'string') return []
    const weight = typeof item.weight === 'number' && Number.isFinite(item.weight) && item.weight > 0
      ? item.weight
      : 1 / Math.max(configuredStrategies.length, 1)
    return [{ type: item.type, weight }]
  })

  const strategyParams: Record<string, Record<string, unknown>> = {}
  if (mode === 'single') {
    const { strategies: _ignored, ...singleConfig } = config
    strategyParams[template.strategy_type] = { ...singleConfig }
  } else {
    for (const item of configuredStrategies) {
      if (typeof item !== 'object' || item === null || typeof item.type !== 'string') continue
      if (typeof item.config === 'object' && item.config !== null && !Array.isArray(item.config)) {
        strategyParams[item.type] = { ...(item.config as Record<string, unknown>) }
      }
    }
  }

  for (const [name, definition] of Object.entries(template.params ?? {})) {
    const targetStrategyId = definition.strategy_id || (mode === 'single' ? template.strategy_type : '')
    if (!targetStrategyId || (mode !== 'single' && !strategies.some(({ type }) => type === targetStrategyId))) {
      if (definition.required) throw new Error(`template_parameter_target_missing:${name}`)
      continue
    }
    strategyParams[targetStrategyId] = {
      ...(strategyParams[targetStrategyId] ?? {}),
      [name]: definition.default,
    }
  }

  return {
    mode,
    singleStrategyId: mode === 'single' ? template.strategy_type : null,
    strategies,
    strategyParams,
  }
}
