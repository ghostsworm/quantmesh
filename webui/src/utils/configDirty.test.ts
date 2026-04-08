import { describe, it, expect } from 'vitest'
import { mergeFeeRateInputsIntoConfig } from './configDirty'
import type { Config } from '../services/config'

describe('mergeFeeRateInputsIntoConfig', () => {
  it('merges fee_rate from inputs into exchanges', () => {
    const cfg = {
      exchanges: {
        binance: { api_key: '', secret_key: '', fee_rate: 0.0004 },
      },
    } as unknown as Config
    const merged = mergeFeeRateInputsIntoConfig(cfg, { binance: '0.0005' })
    expect(merged.exchanges?.binance?.fee_rate).toBe(0.0005)
  })

  it('treats empty string as 0', () => {
    const cfg = {
      exchanges: {
        binance: { fee_rate: 0.0004 },
      },
    } as unknown as Config
    const merged = mergeFeeRateInputsIntoConfig(cfg, { binance: '' })
    expect(merged.exchanges?.binance?.fee_rate).toBe(0)
  })

  it('does not mutate original config', () => {
    const cfg = {
      exchanges: {
        binance: { fee_rate: 0.0004 },
      },
    } as unknown as Config
    mergeFeeRateInputsIntoConfig(cfg, { binance: '0.001' })
    expect(cfg.exchanges?.binance?.fee_rate).toBe(0.0004)
  })
})
