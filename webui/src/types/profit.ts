// 盈利管理類型定义

export type WithdrawFrequency = 'immediate' | 'daily' | 'weekly'
export type WithdrawDestination = 'account' | 'wallet'
export type WithdrawStatus = 'pending' | 'processing' | 'completed' | 'failed' | 'cancelled'
export type WithdrawType = 'auto' | 'manual'

export interface ProfitSummary {
  exchangeId?: string
  totalProfit: number // 淨利潤（毛利 - 手續費 + 資金費淨額）
  grossProfit?: number // 毛利（價差盈虧，未扣手續費）
  totalFee?: number // 手續費合計
  fundingNet?: number // 資金費淨額（正=淨收入，負=淨支出）
  todayProfit: number // 今日盈利
  weekProfit: number // 本周盈利
  monthProfit: number // 本月盈利
  unrealizedProfit: number // 未實現盈利
  withdrawnProfit: number // 已提取盈利
  availableToWithdraw: number // 可提取盈利
  lastUpdated: string
}

export interface StrategyProfit {
  exchangeId: string
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
  exchangeId: string
  strategyId: string
  strategyName?: string
  enabled: boolean
  triggerAmount: number // 触发金額
  withdrawRatio: number // 提取比例 (0-1)
  frequency: WithdrawFrequency
  destination: WithdrawDestination
  walletAddress?: string
  minWithdrawAmount: number // 最小提取金額
  maxWithdrawAmount?: number // 最大提取金額
  createdAt: string
  updatedAt: string
}

export interface WithdrawRecord {
  id: string
  exchangeId: string
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
  strategyId?: string // 如果為空，從所有策略提取
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
  exchangeId?: string
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

export interface FundingPaymentItem {
  id: number
  exchange: string
  symbol: string
  incomeType: string
  income: number // 正=收入，負=支出
  asset: string
  transactionId: number
  tradeTime: string
  createdAt: string
}

export interface ProfitTrendItem {
  date: string
  profit: number
  cumulativeProfit: number
}

export interface PriceChangeItem {
  date: string
  open: number
  close: number
  priceChange: number // close - open
}

export interface ProfitTrendResponse {
  trend: ProfitTrendItem[]
  period: string
}
