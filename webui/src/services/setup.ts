// 配置状态响应
export interface SetupStatus {
  needs_setup: boolean
  config_path: string
}

// 配置初始化请求
export interface SetupInitRequest {
  exchange: string
  api_key: string
  secret_key: string
  passphrase?: string
  symbol: string
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

