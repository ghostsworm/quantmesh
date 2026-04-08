import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  MARKET_INTEL_CLIENT_CACHE_MS,
  MARKET_INTEL_SOURCE_TTL_MS,
  buildMarketIntelCacheParams,
  readMarketIntelCache,
  writeMarketIntelCache,
  readMarketIntelSourceCache,
  writeMarketIntelSourceCache,
  readMacroIntelCache,
  writeMacroIntelCache,
  mergeMarketIntelParts,
  normalizeMarketIntelResponse,
} from './marketIntelligenceCache'
import type { MarketIntelligenceResponse } from '../services/api'

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

  it('round-trips legacy intel cache within TTL', () => {
    const p = buildMarketIntelCacheParams({ limit: 50, keyword: '', source: '' })
    const data = { rss_feeds: [], fear_greed: null, reddit_posts: [], polymarket: [] }
    writeMarketIntelCache(p, data)
    expect(readMarketIntelCache<typeof data>(p)).toEqual(data)
  })

  it('returns null after legacy TTL', () => {
    vi.useFakeTimers()
    const p = buildMarketIntelCacheParams({ limit: 50, keyword: 'btc', source: 'rss' })
    writeMarketIntelCache(p, { x: 1 })
    vi.advanceTimersByTime(MARKET_INTEL_CLIENT_CACHE_MS + 1)
    expect(readMarketIntelCache(p)).toBeNull()
  })

  it('per-source cache expires after source TTL', () => {
    vi.useFakeTimers()
    const base = { limit: 50, keyword: '' }
    const data: MarketIntelligenceResponse = normalizeMarketIntelResponse({
      fear_greed: { value: 50, classification: 'Neutral', timestamp: '2020-01-01' },
    })
    writeMarketIntelSourceCache('fear_greed', base, data)
    expect(readMarketIntelSourceCache('fear_greed', base)).toEqual(data)
    vi.advanceTimersByTime(MARKET_INTEL_SOURCE_TTL_MS.fear_greed + 1)
    expect(readMarketIntelSourceCache('fear_greed', base)).toBeNull()
  })

  it('mergeMarketIntelParts combines slices', () => {
    const a: MarketIntelligenceResponse = normalizeMarketIntelResponse({
      rss_feeds: [{ title: 'F', description: '', url: 'http://x', items: [], last_update: '' }],
    })
    const b: MarketIntelligenceResponse = normalizeMarketIntelResponse({
      fear_greed: { value: 20, classification: 'Fear', timestamp: 't' },
    })
    const m = mergeMarketIntelParts([a, b])
    expect(m.rss_feeds.length).toBe(1)
    expect(m.fear_greed?.value).toBe(20)
  })

  it('macro bundle uses separate storage key from intel', () => {
    writeMacroIntelCache({ events: { events: [] }, impact: { enabled: false } })
    expect(readMacroIntelCache()).toEqual({ events: { events: [] }, impact: { enabled: false } })
  })
})
