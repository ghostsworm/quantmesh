/**
 * 支持的交易所列表，与后端 exchange/factory.go 保持一致
 * 用于配置界面、交易对选择等
 */
export const SUPPORTED_EXCHANGES = [
  'binance',
  'bitget',
  'bybit',
  'gate',
  'okx',
  'huobi',
  'kucoin',
  'kraken',
  'bitfinex',
  'mexc',
  'bingx',
  'deribit',
  'bitmex',
  'phemex',
  'woox',
  'coinex',
  'bitrue',
  'xtcom',
  'btcc',
  'ascendex',
  'poloniex',
  'cryptocom',
  'whitebit',
  'bitkub',
  'coinsph',
] as const

export type SupportedExchange = (typeof SUPPORTED_EXCHANGES)[number]

/** 需要 passphrase 的交易所 */
export const EXCHANGES_REQUIRING_PASSPHRASE = ['bitget', 'okx', 'kucoin'] as const

/** 支持现货交易的交易所 */
export const SPOT_SUPPORTED_EXCHANGES = [
  'binance',
  'bitget',
  'bybit',
  'gate',
  'okx',
  'bitkub',
  'coinsph',
] as const
