// 策略类型定义

export type StrategyType = 'grid' | 'dca' | 'martingale' | 'trend' | 'mean_reversion' | 'combo'

export type RiskLevel = 'low' | 'medium' | 'high'

export interface StrategyInfo {
  id: string
  name: string
  type: StrategyType
  description: string
  riskLevel: RiskLevel
  isPremium: boolean
  isEnabled: boolean
  features: string[]
  minCapital: number
  recommendedCapital: number
  icon?: string
  version?: string
  author?: string
  createdAt?: string
  updatedAt?: string
}

export interface StrategyDetailInfo extends StrategyInfo {
  longDescription: string
  parameters: StrategyParameter[]
  historicalPerformance?: StrategyPerformance
  riskMetrics?: RiskMetrics
  documentation?: string
}

export interface StrategyParameter {
  name: string
  key: string
  type: 'number' | 'boolean' | 'string' | 'select'
  defaultValue: any
  min?: number
  max?: number
  step?: number
  options?: { label: string; value: any }[]
  description: string
  required: boolean
}

export interface StrategyPerformance {
  totalPnL: number
  winRate: number
  sharpeRatio: number
  maxDrawdown: number
  tradeCount: number
  period: string // e.g., "30d", "90d", "1y"
}

export interface RiskMetrics {
  volatility: number
  var95: number // Value at Risk 95%
  cvar95: number // Conditional VaR
  beta: number
}

export interface StrategyConfig {
  strategyId: string
  enabled: boolean
  params: Record<string, any>
  capitalConfig: StrategyCapitalConfig
  riskConfig: StrategyRiskConfig
}

export interface StrategyCapitalConfig {
  strategyId: string
  maxCapital: number // 最大可用资金 (USDT)
  maxPercentage: number // 最大占用比例 (0-100)
  reserveRatio: number // 预留保证金比例
  autoRebalance: boolean // 自动再平衡
  priority: number // 优先级 (资金紧张时按此分配)
}

export interface StrategyRiskConfig {
  maxDrawdown: number
  stopLossRatio: number
  takeProfitRatio: number
  maxLeverage: number
  trendFilterEnabled: boolean
}

export interface StrategyLicense {
  strategyId: string
  licensed: boolean
  expiresAt?: string
  purchasedAt?: string
  price?: number
  tier?: 'basic' | 'pro' | 'enterprise'
}

export interface StrategiesResponse {
  strategies: StrategyInfo[]
}

export interface StrategyDetailResponse {
  strategy: StrategyDetailInfo
}

export interface StrategyEnableResponse {
  success: boolean
  message: string
}

export interface StrategyLicenseResponse {
  license: StrategyLicense
}

export interface StrategyConfigsResponse {
  configs: StrategyConfig[]
}
