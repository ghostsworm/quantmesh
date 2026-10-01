import { describe, expect, it } from 'vitest'
import { resolveStrategyTemplateSelection } from './strategyTemplateSelection'
import type { StrategyTemplateFull } from '../services/strategy'

function template(overrides: Partial<StrategyTemplateFull>): StrategyTemplateFull {
  return {
    id: 'test-template',
    name: 'Test',
    description: '',
    category: 'grid',
    strategy_type: 'grid',
    config: {},
    params: {},
    default_weight: 1,
    ...overrides,
  }
}

describe('resolveStrategyTemplateSelection', () => {
  it('selects an actual single strategy ID and routes its defaults to that strategy', () => {
    const selection = resolveStrategyTemplateSelection(template({
      strategy_type: 'grid',
      config: { grid_spacing: 500, grid_levels: 10 },
      params: { price_interval: { name: 'Price interval', description: '', type: 'number', default: 500, required: true } },
    }))

    expect(selection.mode).toBe('single')
    expect(selection.singleStrategyId).toBe('grid')
    expect(selection.strategyParams.grid).toEqual({ grid_spacing: 500, grid_levels: 10, price_interval: 500 })
  })

  it('routes combo defaults only to their declared strategy', () => {
    const selection = resolveStrategyTemplateSelection(template({
      strategy_type: 'combo',
      config: { strategies: [{ type: 'grid', weight: 0.7 }, { type: 'dca', weight: 0.3 }] },
      params: {
        price_interval: { name: 'Grid interval', description: '', strategy_id: 'grid', type: 'number', default: 500, required: true },
        dca_amount: { name: 'DCA amount', description: '', strategy_id: 'dca', type: 'number', default: 100, required: true },
      },
    }))

    expect(selection.mode).toBe('combo')
    expect(selection.strategies).toEqual([{ type: 'grid', weight: 0.7 }, { type: 'dca', weight: 0.3 }])
    expect(selection.strategyParams).toEqual({ grid: { price_interval: 500 }, dca: { dca_amount: 100 } })
  })

  it('rejects a combo template with required parameters lacking a strategy target', () => {
    expect(() => resolveStrategyTemplateSelection(template({
      strategy_type: 'combo',
      config: { strategies: [{ type: 'grid', weight: 1 }] },
      params: { unknown: { name: 'Unknown', description: '', type: 'number', default: 1, required: true } },
    }))).toThrow('template_parameter_target_missing:unknown')
  })
})
