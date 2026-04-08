/**
 * 市場情報頁客戶端緩存（sessionStorage）
 * - 按數據源分層：不同來源使用不同 TTL（宏觀事件單獨鍵）
 * - 「全部」視圖下合併各來源緩存，僅對過期來源發起請求
 */

import type { MarketIntelligenceResponse } from '../services/api'

/** 舊版單一鍵緩存 TTL（保留與單測兼容） */
export const MARKET_INTEL_CLIENT_CACHE_MS = 90 * 1000

/** 各數據源客戶端緩存 TTL（毫秒） */
export const MARKET_INTEL_SOURCE_TTL_MS: Record<
  'rss' | 'fear_greed' | 'reddit' | 'polymarket',
  number
> = {
  rss: 2 * 60 * 1000,
  fear_greed: 10 * 60 * 1000,
  reddit: 2 * 60 * 1000,
  polymarket: 3 * 60 * 1000,
}

export const MACRO_INTEL_CACHE_MS = 5 * 60 * 1000

const STORAGE_KEY_INTEL_PREFIX = 'qm_market_intel_v1:'
const STORAGE_KEY_INTEL_SOURCE_PREFIX = 'qm_market_intel_src_v2:'
const STORAGE_KEY_MACRO = 'qm_market_intel_macro_v2'

export type MarketIntelCacheParams = {
  limit: number
  keyword: string
  source: string
}

export type MarketIntelSourceKey = 'rss' | 'fear_greed' | 'reddit' | 'polymarket'

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

/** 單一來源緩存參數（source 必填） */
export function buildMarketIntelSourceParams(params: {
  limit?: number
  keyword?: string
  source: MarketIntelSourceKey
}): MarketIntelCacheParams & { source: MarketIntelSourceKey } {
  return {
    limit: params.limit ?? 50,
    keyword: params.keyword ?? '',
    source: params.source,
  }
}

function intelStorageKey(p: MarketIntelCacheParams): string {
  return `${STORAGE_KEY_INTEL_PREFIX}${JSON.stringify(p)}`
}

function intelSourceStorageKey(source: MarketIntelSourceKey, p: Omit<MarketIntelCacheParams, 'source'>): string {
  return `${STORAGE_KEY_INTEL_SOURCE_PREFIX}${source}:${JSON.stringify({ limit: p.limit, keyword: p.keyword })}`
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

function ttlForSource(source: MarketIntelSourceKey): number {
  return MARKET_INTEL_SOURCE_TTL_MS[source]
}

/** 讀取單一來源的市場情報緩存（按來源 TTL） */
export function readMarketIntelSourceCache(
  source: MarketIntelSourceKey,
  p: Omit<MarketIntelCacheParams, 'source'>,
): MarketIntelligenceResponse | null {
  const raw = safeGetItem(intelSourceStorageKey(source, p))
  if (!raw) return null
  let env: CachedEnvelope<MarketIntelligenceResponse>
  try {
    env = JSON.parse(raw) as CachedEnvelope<MarketIntelligenceResponse>
  } catch {
    return null
  }
  if (!env || typeof env.at !== 'number' || env.data === undefined) return null
  if (Date.now() - env.at > ttlForSource(source)) return null
  return env.data
}

export function writeMarketIntelSourceCache(
  source: MarketIntelSourceKey,
  p: Omit<MarketIntelCacheParams, 'source'>,
  data: MarketIntelligenceResponse,
): void {
  const env: CachedEnvelope<MarketIntelligenceResponse> = { at: Date.now(), data }
  safeSetItem(intelSourceStorageKey(source, p), JSON.stringify(env))
}

export function normalizeMarketIntelResponse(
  partial: Partial<MarketIntelligenceResponse> | undefined,
): MarketIntelligenceResponse {
  return {
    rss_feeds: partial?.rss_feeds ?? [],
    fear_greed: partial?.fear_greed ?? null,
    reddit_posts: partial?.reddit_posts ?? [],
    polymarket: partial?.polymarket ?? [],
  }
}

/** 合併各來源片段為完整響應（後寫入的非空字段覆蓋） */
export function mergeMarketIntelParts(parts: MarketIntelligenceResponse[]): MarketIntelligenceResponse {
  const out = normalizeMarketIntelResponse({})
  for (const p of parts) {
    if (p.rss_feeds?.length) out.rss_feeds = p.rss_feeds
    if (p.fear_greed != null) out.fear_greed = p.fear_greed
    if (p.reddit_posts?.length) out.reddit_posts = p.reddit_posts
    if (p.polymarket?.length) out.polymarket = p.polymarket
  }
  return out
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
  if (Date.now() - env.at > MACRO_INTEL_CACHE_MS) return null
  return env.data
}

export function writeMacroIntelCache(bundle: MacroBundleCache): void {
  const env: CachedEnvelope<MacroBundleCache> = { at: Date.now(), data: bundle }
  safeSetItem(STORAGE_KEY_MACRO, JSON.stringify(env))
}
