/**
 * 首次配置向导：常用交易对与推荐网格参数（USDT 本位；可按交易所再调）
 */
export type SymbolTradingPreset = {
  price_interval: number
  order_quantity: number
  min_order_value: number
  buy_window_size: number
  sell_window_size: number
}

/** 预设键顺序即下拉里展示顺序；默认项为 BTCUSDT */
export const CONFIG_SETUP_SYMBOL_ORDER: string[] = [
  'BTCUSDT',
  'ETHUSDT',
  'SOLUSDT',
  'BNBUSDT',
  'XRPUSDT',
  'DOGEUSDT',
  'ADAUSDT',
  'LINKUSDT',
  'AVAXUSDT',
  'TONUSDT',
]

export const CONFIG_SETUP_SYMBOL_PRESETS: Record<string, SymbolTradingPreset> = {
  BTCUSDT: {
    price_interval: 10,
    order_quantity: 30,
    min_order_value: 5,
    buy_window_size: 10,
    sell_window_size: 10,
  },
  ETHUSDT: {
    price_interval: 2,
    order_quantity: 30,
    min_order_value: 5,
    buy_window_size: 10,
    sell_window_size: 10,
  },
  SOLUSDT: {
    price_interval: 0.5,
    order_quantity: 30,
    min_order_value: 5,
    buy_window_size: 10,
    sell_window_size: 10,
  },
  BNBUSDT: {
    price_interval: 2,
    order_quantity: 30,
    min_order_value: 5,
    buy_window_size: 10,
    sell_window_size: 10,
  },
  XRPUSDT: {
    price_interval: 0.005,
    order_quantity: 30,
    min_order_value: 5,
    buy_window_size: 10,
    sell_window_size: 10,
  },
  DOGEUSDT: {
    price_interval: 0.001,
    order_quantity: 30,
    min_order_value: 5,
    buy_window_size: 10,
    sell_window_size: 10,
  },
  ADAUSDT: {
    price_interval: 0.002,
    order_quantity: 30,
    min_order_value: 5,
    buy_window_size: 10,
    sell_window_size: 10,
  },
  LINKUSDT: {
    price_interval: 0.1,
    order_quantity: 30,
    min_order_value: 5,
    buy_window_size: 10,
    sell_window_size: 10,
  },
  AVAXUSDT: {
    price_interval: 0.5,
    order_quantity: 30,
    min_order_value: 5,
    buy_window_size: 10,
    sell_window_size: 10,
  },
  TONUSDT: {
    price_interval: 0.05,
    order_quantity: 30,
    min_order_value: 5,
    buy_window_size: 10,
    sell_window_size: 10,
  },
}

export function getPresetForSymbol(symbol: string): SymbolTradingPreset | undefined {
  return CONFIG_SETUP_SYMBOL_PRESETS[symbol]
}
