// 盈利管理 API 服务
import { fetchWithAuth } from './api'
import type {
  ProfitSummary,
  StrategyProfit,
  ProfitWithdrawRule,
  WithdrawRecord,
  ManualWithdrawRequest,
  WithdrawResponse,
  ProfitSummaryResponse,
  StrategyProfitsResponse,
  WithdrawRulesResponse,
  UpdateWithdrawRuleRequest,
  UpdateWithdrawRuleResponse,
  WithdrawHistoryParams,
  WithdrawHistoryResponse,
  ProfitTrendResponse,
} from '../types/profit'

const API_BASE_URL = `${window.location.origin}/api`

// 获取盈利汇总
export async function getProfitSummary(exchangeId?: string): Promise<ProfitSummaryResponse> {
  const url = exchangeId ? `${API_BASE_URL}/profit/summary?exchange_id=${exchangeId}` : `${API_BASE_URL}/profit/summary`
  return fetchWithAuth(url)
}

// 按策略获取盈利
export async function getStrategyProfits(exchangeId?: string): Promise<StrategyProfitsResponse> {
  const url = exchangeId ? `${API_BASE_URL}/profit/by-strategy?exchange_id=${exchangeId}` : `${API_BASE_URL}/profit/by-strategy`
  return fetchWithAuth(url)
}

// 获取单个策略的盈利详情
export async function getStrategyProfitDetail(
  strategyId: string
): Promise<{ profit: StrategyProfit }> {
  return fetchWithAuth(`${API_BASE_URL}/profit/by-strategy/${strategyId}`)
}

// 获取提取规则
export async function getWithdrawRules(): Promise<WithdrawRulesResponse> {
  return fetchWithAuth(`${API_BASE_URL}/profit/withdraw-rules`)
}

// 更新提取规则
export async function updateWithdrawRules(
  request: UpdateWithdrawRuleRequest
): Promise<UpdateWithdrawRuleResponse> {
  return fetchWithAuth(`${API_BASE_URL}/profit/withdraw-rules`, {
    method: 'PUT',
    body: JSON.stringify(request),
  })
}

// 创建或更新单个提取规则
export async function upsertWithdrawRule(
  rule: Partial<ProfitWithdrawRule>
): Promise<{ success: boolean; message: string; rule?: ProfitWithdrawRule }> {
  return fetchWithAuth(`${API_BASE_URL}/profit/withdraw-rules/upsert`, {
    method: 'POST',
    body: JSON.stringify(rule),
  })
}

// 删除提取规则
export async function deleteWithdrawRule(
  ruleId: string
): Promise<{ success: boolean; message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/profit/withdraw-rules/${ruleId}`, {
    method: 'DELETE',
  })
}

// 手动提取
export async function withdrawProfit(
  request: ManualWithdrawRequest
): Promise<WithdrawResponse> {
  return fetchWithAuth(`${API_BASE_URL}/profit/withdraw`, {
    method: 'POST',
    body: JSON.stringify(request),
  })
}

// 获取提取历史
export async function getWithdrawHistory(
  params?: WithdrawHistoryParams
): Promise<WithdrawHistoryResponse> {
  const queryParams = new URLSearchParams()
  if (params?.exchangeId) queryParams.append('exchange_id', params.exchangeId)
  if (params?.strategyId) queryParams.append('strategy_id', params.strategyId)
  if (params?.status) queryParams.append('status', params.status)
  if (params?.type) queryParams.append('type', params.type)
  if (params?.startTime) queryParams.append('start_time', params.startTime)
  if (params?.endTime) queryParams.append('end_time', params.endTime)
  if (params?.limit) queryParams.append('limit', params.limit.toString())
  if (params?.offset) queryParams.append('offset', params.offset.toString())

  const url = `${API_BASE_URL}/profit/history${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

// 获取盈利趋势
export async function getProfitTrend(
  period: '7d' | '30d' | '90d' | '1y' = '30d',
  exchangeId?: string,
  strategyId?: string
): Promise<ProfitTrendResponse> {
  const queryParams = new URLSearchParams({ period })
  if (exchangeId) queryParams.append('exchange_id', exchangeId)
  if (strategyId) queryParams.append('strategy_id', strategyId)

  return fetchWithAuth(`${API_BASE_URL}/profit/trend?${queryParams.toString()}`)
}

// 取消待处理的提取
export async function cancelWithdraw(
  withdrawId: string
): Promise<{ success: boolean; message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/profit/withdraw/${withdrawId}/cancel`, {
    method: 'POST',
  })
}

// 获取提取详情
export async function getWithdrawDetail(
  withdrawId: string
): Promise<{ record: WithdrawRecord }> {
  return fetchWithAuth(`${API_BASE_URL}/profit/withdraw/${withdrawId}`)
}

// 估算提取费用
export async function estimateWithdrawFee(
  request: ManualWithdrawRequest
): Promise<{ fee: number; netAmount: number; estimatedArrival: string }> {
  return fetchWithAuth(`${API_BASE_URL}/profit/withdraw/estimate`, {
    method: 'POST',
    body: JSON.stringify(request),
  })
}
