import type { Config } from '../services/config'

/** 与 config.example.yaml / 后端 ApplyGammaRelatedDefaults 对齐 */
export const DEFAULT_GAMMA_API_URL = 'https://gamma-api.polymarket.com'

export const DEFAULT_POLYMARKET_ANALYSIS_INTERVAL_SEC = 300

const DEFAULT_KEYWORDS = ['bitcoin', 'btc', 'ethereum', 'eth', 'crypto', 'regulation'] as const

/** 开启 Polymarket 信号时写入表单的默认模块（与 config.example.yaml 一致） */
export function mergePolymarketSignalOnEnable(existing: Record<string, unknown> | undefined): Record<string, unknown> {
  const e = existing || {}
  const em = (e.markets || {}) as Record<string, unknown>
  const esg = (e.signal_generation || {}) as Record<string, unknown>

  const keywords = Array.isArray(em.keywords) && em.keywords.length > 0 ? em.keywords : [...DEFAULT_KEYWORDS]

  const pickNum = (v: unknown, def: number) => (typeof v === 'number' && !Number.isNaN(v) ? v : def)
  const pickStr = (v: unknown, def: string) => (typeof v === 'string' && v.trim() !== '' ? v.trim() : def)

  return {
    enabled: true,
    upstream_ref: e.upstream_ref,
    api_url: pickStr(e.api_url, DEFAULT_GAMMA_API_URL),
    analysis_interval:
      typeof e.analysis_interval === 'number' && e.analysis_interval > 0
        ? e.analysis_interval
        : DEFAULT_POLYMARKET_ANALYSIS_INTERVAL_SEC,
    markets: {
      keywords,
      min_liquidity: em.min_liquidity != null ? pickNum(em.min_liquidity, 5000) : 5000,
      min_volume_24h: em.min_volume_24h != null ? pickNum(em.min_volume_24h, 0) : 0,
      min_days_to_expiry: em.min_days_to_expiry != null ? pickNum(em.min_days_to_expiry, 1) : 1,
      max_days_to_expiry: em.max_days_to_expiry != null ? pickNum(em.max_days_to_expiry, 90) : 90,
    },
    signal_generation: {
      buy_threshold: esg.buy_threshold != null ? pickNum(esg.buy_threshold, 0.65) : 0.65,
      sell_threshold: esg.sell_threshold != null ? pickNum(esg.sell_threshold, 0.35) : 0.35,
      min_signal_strength: esg.min_signal_strength != null ? pickNum(esg.min_signal_strength, 0.3) : 0.3,
      min_confidence: esg.min_confidence != null ? pickNum(esg.min_confidence, 0.5) : 0.5,
    },
  }
}

/** 在配置界面打开 Polymarket 时合并默认项；关闭时仅关开关 */
export function applyPolymarketEnabledToConfig(config: Config, enabled: boolean): Config {
  const next = JSON.parse(JSON.stringify(config)) as Config
  if (!next.ai) (next as Record<string, unknown>).ai = {}
  const ai = next.ai as Record<string, unknown>
  if (!ai.modules) ai.modules = {}
  const modules = ai.modules as Record<string, unknown>

  if (enabled) {
    modules.polymarket_signal = mergePolymarketSignalOnEnable(modules.polymarket_signal as Record<string, unknown>)

    const root = next as Record<string, unknown>
    if (!root.macro_event) root.macro_event = {}
    const me = root.macro_event as Record<string, unknown>
    if (!me.gamma_api_url || String(me.gamma_api_url).trim() === '') {
      me.gamma_api_url = DEFAULT_GAMMA_API_URL
    }
    if (typeof me.fetch_interval !== 'number' || me.fetch_interval <= 0) {
      me.fetch_interval = 300
    }
  } else {
    const cur = (modules.polymarket_signal || {}) as Record<string, unknown>
    modules.polymarket_signal = { ...cur, enabled: false }
  }
  return next
}
