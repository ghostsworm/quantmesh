import { describe, expect, it } from 'vitest'
import {
  DEFAULT_GAMMA_API_URL,
  mergePolymarketSignalOnEnable,
  applyPolymarketEnabledToConfig,
} from './polymarketConfigDefaults'
import type { Config } from '../services/config'

describe('mergePolymarketSignalOnEnable', () => {
  it('fills example defaults when empty', () => {
    const m = mergePolymarketSignalOnEnable(undefined)
    expect(m.enabled).toBe(true)
    expect(m.api_url).toBe(DEFAULT_GAMMA_API_URL)
    expect(m.analysis_interval).toBe(300)
    expect((m.markets as { keywords: string[] }).keywords).toContain('bitcoin')
    expect((m.signal_generation as { buy_threshold: number }).buy_threshold).toBe(0.65)
  })

  it('preserves custom api_url', () => {
    const m = mergePolymarketSignalOnEnable({ api_url: 'https://example.com/gamma' })
    expect(m.api_url).toBe('https://example.com/gamma')
  })

  it('preserves non-empty keywords', () => {
    const m = mergePolymarketSignalOnEnable({ markets: { keywords: ['sol'] } })
    expect((m.markets as { keywords: string[] }).keywords).toEqual(['sol'])
  })
})

describe('applyPolymarketEnabledToConfig', () => {
  it('merges macro_event gamma url when enabling', () => {
    const cfg = { ai: { modules: {} } } as unknown as Config
    const out = applyPolymarketEnabledToConfig(cfg, true)
    const me = (out as Record<string, unknown>).macro_event as Record<string, unknown>
    expect(me.gamma_api_url).toBe(DEFAULT_GAMMA_API_URL)
    expect(me.fetch_interval).toBe(300)
  })

  it('only disables flag when turning off', () => {
    const cfg = {
      ai: {
        modules: {
          polymarket_signal: {
            enabled: true,
            api_url: 'https://keep.me',
            analysis_interval: 120,
          },
        },
      },
    } as unknown as Config
    const out = applyPolymarketEnabledToConfig(cfg, false)
    const ps = (out as Config).ai?.modules?.polymarket_signal as Record<string, unknown>
    expect(ps.enabled).toBe(false)
    expect(ps.api_url).toBe('https://keep.me')
  })
})
