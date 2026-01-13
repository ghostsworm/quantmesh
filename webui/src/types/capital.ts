// 资金管理类型定义

export interface CapitalOverview {
  totalBalance: number // 账户总权益
  allocatedCapital: number // 已分配给策略的资金
  usedCapital: number // 实际已占用保证金
  availableCapital: number // 交易所可用余额
  reservedCapital: number // 用户预留资金
  unrealizedPnL: number // 未实现盈亏
  marginRatio: number // 保证金占用率
  exchanges?: ExchangeCapitalSummary[] // 各交易所摘要
  lastUpdated: string
}

export interface ExchangeCapitalSummary {
  exchangeId: string
  exchangeName: string
  totalBalance: number
  available: number
  used: number
  pnl: number
  status: 'online' | 'offline' | 'error'
  isTestnet?: boolean // 是否使用测试网
}

export interface ExchangeCapitalDetail {
  exchangeId: string
  exchangeName: string
  assets: AssetAllocation[]
  isTestnet?: boolean // 是否使用测试网
}

export interface AssetAllocation {
  asset: string // 资产名称，如 USDT
  totalBalance: number
  availableBalance: number
  allocatedToStrategies: number
  unallocated: number
  strategies: StrategyCapitalInfo[]
}

export interface StrategyCapitalInfo {
  strategyId: string
  strategyName: string
  strategyType: string
  exchangeId: string
  asset: string
  allocated: number // 分配金额
  used: number // 已使用
  available: number // 可用配额
  weight: number // 权重
  maxCapital?: number // 最大限额
  maxPercentage?: number // 最大占比
  reserveRatio?: number // 预留保证金比例
  autoRebalance: boolean
  priority: number
  utilizationRate: number // 使用率
  status: 'active' | 'paused' | 'error'
}

export interface CapitalAllocationConfig {
  strategyId: string
  maxCapital: number
  maxPercentage: number
  reserveRatio: number
  autoRebalance: boolean
  priority: number
}

export interface RebalanceRequest {
  mode: 'equal' | 'weighted' | 'priority'
  dryRun?: boolean
}

export interface RebalanceResult {
  success: boolean
  message: string
  changes: RebalanceChange[]
  timestamp: string
}

export interface RebalanceChange {
  strategyId: string
  previousAllocation: number
  newAllocation: number
  difference: number
}

export interface CapitalOverviewResponse {
  success: boolean
  overview: CapitalOverview
}

export interface CapitalAllocationResponse {
  success: boolean
  exchanges: ExchangeCapitalDetail[]
}

export interface UpdateAllocationRequest {
  allocations: CapitalAllocationConfig[]
}

export interface UpdateAllocationResponse {
  success: boolean
  message: string
}

export interface CapitalHistoryItem {
  timestamp: string
  totalBalance: number
  allocatedCapital: number
  usedCapital: number
  unrealizedPnL: number
}

export interface CapitalHistoryResponse {
  history: CapitalHistoryItem[]
}
