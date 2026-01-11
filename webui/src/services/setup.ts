// 配置状态响应
export interface SetupStatus {
  needs_setup: boolean
  config_path: string
  exchanges?: Record<string, {
    api_key: string
    secret_key: string
    passphrase?: string
    fee_rate?: number
    testnet?: boolean
  }>
  symbols?: Array<{
    exchange: string
    symbol: string
    price_interval: number
    order_quantity: number
    min_order_value: number
    buy_window_size: number
    sell_window_size: number
  }>
}

// 配置初始化请求
export interface SetupInitRequest {
  exchange: string
  api_key: string
  secret_key: string
  passphrase?: string
  symbol?: string // 保持向后兼容，但优先使用 symbols
  symbols?: string[] // 多交易对支持
  price_interval: number
  order_quantity: number
  min_order_value?: number
  buy_window_size: number
  sell_window_size?: number
  testnet?: boolean
  fee_rate?: number
}

// 配置初始化响应
export interface SetupInitResponse {
  success: boolean
  message: string
  requires_restart: boolean
  backup_path?: string // 备份文件路径（如果存在）
}

// 检查配置状态
export async function checkSetupStatus(): Promise<SetupStatus> {
  const response = await fetch(`${window.location.origin}/api/setup/status`, {
    credentials: 'include',
  })
  if (!response.ok) {
    throw new Error('检查配置状态失败')
  }
  return await response.json()
}

// 保存初始配置
export async function saveInitialConfig(config: SetupInitRequest): Promise<SetupInitResponse> {
  const response = await fetch(`${window.location.origin}/api/setup/init`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    credentials: 'include',
    body: JSON.stringify(config),
  })
  if (!response.ok) {
    const error = await response.json()
    throw new Error(error.message || '保存配置失败')
  }
  return await response.json()
}

// 获取交易所的所有交易对
export interface ExchangeSymbolsRequest {
  exchange: string
  api_key: string
  secret_key: string
  passphrase?: string
  testnet?: boolean
}

export interface ExchangeSymbolsResponse {
  success: boolean
  message?: string
  symbols: string[]
}

export async function getExchangeSymbols(request: ExchangeSymbolsRequest): Promise<ExchangeSymbolsResponse> {
  const response = await fetch(`${window.location.origin}/api/setup/exchange-symbols`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    credentials: 'include',
    body: JSON.stringify(request),
  })
  if (!response.ok) {
    const error = await response.json()
    throw new Error(error.message || '获取交易对列表失败')
  }
  return await response.json()
}
