import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  MARKET_INTEL_CLIENT_CACHE_MS,
  buildMarketIntelCacheParams,
  readMarketIntelCache,
  writeMarketIntelCache,
  readMacroIntelCache,
  writeMacroIntelCache,
} from './marketIntelligenceCache'

function installMemorySessionStorage() {
  const store: Record<string, string> = {}
  vi.stubGlobal('sessionStorage', {
    getItem: (k: string) => (k in store ? store[k] : null),
    setItem: (k: string, v: string) => {
      store[k] = v
    },
    removeItem: (k: string) => {
      delete store[k]
    },
    clear: () => {
      for (const k of Object.keys(store)) delete store[k]
    },
    get length() {
      return Object.keys(store).length
    },
    key: (i: number) => Object.keys(store)[i] ?? null,
  })
}

describe('marketIntelligenceCache', () => {
  beforeEach(() => {
    installMemorySessionStorage()
    vi.useRealTimers()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('round-trips intel cache within TTL', () => {
    const p = buildMarketIntelCacheParams({ limit: 50, keyword: '', source: '' })
    const data = { rss_feeds: [], fear_greed: null, reddit_posts: [], polymarket: [] }
    writeMarketIntelCache(p, data)
    expect(readMarketIntelCache<typeof data>(p)).toEqual(data)
  })

  it('returns null after TTL', () => {
    vi.useFakeTimers()
    const p = buildMarketIntelCacheParams({ limit: 50, keyword: 'btc', source: 'rss' })
    writeMarketIntelCache(p, { x: 1 })
    vi.advanceTimersByTime(MARKET_INTEL_CLIENT_CACHE_MS + 1)
    expect(readMarketIntelCache(p)).toBeNull()
  })

  it('macro bundle is separate from intel params', () => {
    writeMacroIntelCache({ events: { events: [] }, impact: { enabled: false } })
    expect(readMacroIntelCache()).toEqual({ events: { events: [] }, impact: { enabled: false } })
  })
})
