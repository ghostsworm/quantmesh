import { describe, expect, it } from 'vitest'
import {
  CONFIG_SETUP_SYMBOL_ORDER,
  CONFIG_SETUP_SYMBOL_PRESETS,
  getPresetForSymbol,
} from './configSetupSymbolPresets'

describe('configSetupSymbolPresets', () => {
  it('defaults first symbol is BTCUSDT with expected grid spacing', () => {
    expect(CONFIG_SETUP_SYMBOL_ORDER[0]).toBe('BTCUSDT')
    const p = CONFIG_SETUP_SYMBOL_PRESETS.BTCUSDT
    expect(p.price_interval).toBe(10)
    expect(p.min_order_value).toBe(5)
  })

  it('getPresetForSymbol returns preset for known symbols', () => {
    expect(getPresetForSymbol('ETHUSDT')?.price_interval).toBe(2)
    expect(getPresetForSymbol('XRPUSDT')?.price_interval).toBe(0.005)
    expect(getPresetForSymbol('UNKNOWN')).toBeUndefined()
  })

  it('every ordered symbol has a preset', () => {
    for (const sym of CONFIG_SETUP_SYMBOL_ORDER) {
      expect(CONFIG_SETUP_SYMBOL_PRESETS[sym]).toBeDefined()
      expect(CONFIG_SETUP_SYMBOL_PRESETS[sym].price_interval).toBeGreaterThan(0)
    }
  })
})
