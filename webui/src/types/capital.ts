// 资金管理类型定义

export interface CapitalOverview {
  totalBalance: number // 账户总余额
  allocatedCapital: number // 已分配资金
  usedCapital: number // 已使用资金
  availableCapital: number // 可用资金
  reservedCapital: number // 预留保证金
  unrealizedPnL: number // 未实现盈亏
  marginRatio: number // 保证金使用率
  lastUpdated: string
}

export interface StrategyCapitalInfo {
  strategyId: string
  strategyName: string
  strategyType: string
  allocated: number // 分配金额
  used: number // 已使用
  available: number // 可用
  weight: number // 权重
  maxCapital: number // 最大限额
  maxPercentage: number // 最大占比
  reserveRatio: number // 预留保证金比例
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
  overview: CapitalOverview
}

export interface CapitalAllocationResponse {
  strategies: StrategyCapitalInfo[]
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
