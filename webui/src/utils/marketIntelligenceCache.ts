/**
 * 市場情報頁短期客戶端緩存（sessionStorage），減輕刷新/返回頁面時的重複請求。
 * TTL 介於 1～2 分鐘之間（默認 90s）；手動刷新與搜索仍強制拉取。
 */

export const MARKET_INTEL_CLIENT_CACHE_MS = 90 * 1000

const STORAGE_KEY_INTEL_PREFIX = 'qm_market_intel_v1:'
const STORAGE_KEY_MACRO = 'qm_market_intel_macro_v1'

export type MarketIntelCacheParams = {
  limit: number
  keyword: string
  source: string
}

export function buildMarketIntelCacheParams(params: {
  limit?: number
  keyword?: string
  source?: string
}): MarketIntelCacheParams {
  return {
    limit: params.limit ?? 50,
    keyword: params.keyword ?? '',
    source: params.source ?? '',
  }
}

function intelStorageKey(p: MarketIntelCacheParams): string {
  return `${STORAGE_KEY_INTEL_PREFIX}${JSON.stringify(p)}`
}

type CachedEnvelope<T> = { at: number; data: T }

function safeGetItem(key: string): string | null {
  try {
    return sessionStorage.getItem(key)
  } catch {
    return null
  }
}

function safeSetItem(key: string, value: string): void {
  try {
    sessionStorage.setItem(key, value)
  } catch {
    // 私密模式或配額滿：跳過緩存
  }
}

export function readMarketIntelCache<T>(params: MarketIntelCacheParams): T | null {
  const raw = safeGetItem(intelStorageKey(params))
  if (!raw) return null
  let env: CachedEnvelope<T>
  try {
    env = JSON.parse(raw) as CachedEnvelope<T>
  } catch {
    return null
  }
  if (!env || typeof env.at !== 'number' || env.data === undefined) return null
  if (Date.now() - env.at > MARKET_INTEL_CLIENT_CACHE_MS) return null
  return env.data
}

export function writeMarketIntelCache<T>(params: MarketIntelCacheParams, data: T): void {
  const env: CachedEnvelope<T> = { at: Date.now(), data }
  safeSetItem(intelStorageKey(params), JSON.stringify(env))
}

export type MacroBundleCache = {
  events: unknown
  impact: unknown
}

export function readMacroIntelCache(): MacroBundleCache | null {
  const raw = safeGetItem(STORAGE_KEY_MACRO)
  if (!raw) return null
  let env: CachedEnvelope<MacroBundleCache>
  try {
    env = JSON.parse(raw) as CachedEnvelope<MacroBundleCache>
  } catch {
    return null
  }
  if (!env || typeof env.at !== 'number' || !env.data) return null
  if (Date.now() - env.at > MARKET_INTEL_CLIENT_CACHE_MS) return null
  return env.data
}

export function writeMacroIntelCache(bundle: MacroBundleCache): void {
  const env: CachedEnvelope<MacroBundleCache> = { at: Date.now(), data: bundle }
  safeSetItem(STORAGE_KEY_MACRO, JSON.stringify(env))
}
