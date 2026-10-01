import type { StrategyTemplateFull } from '../services/strategy'

export type TemplateSelectionMode = 'single' | 'combo' | 'hedge'

export interface ResolvedTemplateSelection {
  mode: TemplateSelectionMode
  singleStrategyId: string | null
  strategies: Array<{ type: string; weight: number }>
  strategyParams: Record<string, Record<string, unknown>>
  tradingParameters: { price_interval?: number; order_quantity?: number }
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
  const tradingParameters: { price_interval?: number; order_quantity?: number } = {}
  const hasGridStrategy = mode === 'single'
    ? template.strategy_type === 'grid'
    : strategies.some(({ type }) => type === 'grid')

  const applyTradingParameter = (name: string, value: unknown) => {
    if (!hasGridStrategy || typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return
    if (name === 'price_interval' || name === 'grid_spacing') tradingParameters.price_interval = value
    if (name === 'order_quantity' || name === 'grid_order_qty') tradingParameters.order_quantity = value
  }

  if (mode === 'single') {
    const { strategies: _ignored, ...singleConfig } = config
    if (template.strategy_type !== 'grid') strategyParams[template.strategy_type] = { ...singleConfig }
  } else {
    for (const item of configuredStrategies) {
      if (typeof item !== 'object' || item === null || typeof item.type !== 'string') continue
      if (item.type === 'grid') continue
      if (typeof item.config === 'object' && item.config !== null && !Array.isArray(item.config)) {
        strategyParams[item.type] = { ...(item.config as Record<string, unknown>) }
      }
    }
  }

  for (const [name, value] of Object.entries(config)) applyTradingParameter(name, value)

  for (const [name, definition] of Object.entries(template.params ?? {})) {
    applyTradingParameter(name, definition.default)
    if (hasGridStrategy && ['price_interval', 'grid_spacing', 'order_quantity', 'grid_order_qty'].includes(name)) continue
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
    tradingParameters,
  }
}
