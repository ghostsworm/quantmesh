// 盈利管理类型定义

export type WithdrawFrequency = 'immediate' | 'daily' | 'weekly'
export type WithdrawDestination = 'account' | 'wallet'
export type WithdrawStatus = 'pending' | 'processing' | 'completed' | 'failed' | 'cancelled'
export type WithdrawType = 'auto' | 'manual'

export interface ProfitSummary {
  totalProfit: number // 累计盈利
  todayProfit: number // 今日盈利
  weekProfit: number // 本周盈利
  monthProfit: number // 本月盈利
  unrealizedProfit: number // 未实现盈利
  withdrawnProfit: number // 已提取盈利
  availableToWithdraw: number // 可提取盈利
  lastUpdated: string
}

export interface StrategyProfit {
  strategyId: string
  strategyName: string
  strategyType: string
  totalProfit: number
  todayProfit: number
  unrealizedProfit: number
  realizedProfit: number
  withdrawnProfit: number
  availableToWithdraw: number
  tradeCount: number
  winRate: number
  avgProfitPerTrade: number
  lastTradeAt?: string
}

export interface ProfitWithdrawRule {
  id: string
  strategyId: string
  strategyName?: string
  enabled: boolean
  triggerAmount: number // 触发金额
  withdrawRatio: number // 提取比例 (0-1)
  frequency: WithdrawFrequency
  destination: WithdrawDestination
  walletAddress?: string
  minWithdrawAmount: number // 最小提取金额
  maxWithdrawAmount?: number // 最大提取金额
  createdAt: string
  updatedAt: string
}

export interface WithdrawRecord {
  id: string
  strategyId: string
  strategyName: string
  amount: number
  fee: number
  netAmount: number
  type: WithdrawType
  status: WithdrawStatus
  destination: WithdrawDestination
  walletAddress?: string
  txHash?: string
  createdAt: string
  completedAt?: string
  failedReason?: string
}

export interface ManualWithdrawRequest {
  strategyId?: string // 如果为空，从所有策略提取
  amount: number
  destination: WithdrawDestination
  walletAddress?: string
}

export interface WithdrawResponse {
  success: boolean
  message: string
  withdrawId?: string
  estimatedFee?: number
  estimatedArrival?: string
}

export interface ProfitSummaryResponse {
  summary: ProfitSummary
}

export interface StrategyProfitsResponse {
  profits: StrategyProfit[]
}

export interface WithdrawRulesResponse {
  rules: ProfitWithdrawRule[]
}

export interface UpdateWithdrawRuleRequest {
  rules: ProfitWithdrawRule[]
}

export interface UpdateWithdrawRuleResponse {
  success: boolean
  message: string
}

export interface WithdrawHistoryParams {
  strategyId?: string
  status?: WithdrawStatus
  type?: WithdrawType
  startTime?: string
  endTime?: string
  limit?: number
  offset?: number
}

export interface WithdrawHistoryResponse {
  records: WithdrawRecord[]
  total: number
}

export interface ProfitTrendItem {
  date: string
  profit: number
  cumulativeProfit: number
}

export interface ProfitTrendResponse {
  trend: ProfitTrendItem[]
  period: string
}
