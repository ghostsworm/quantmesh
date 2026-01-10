// 资金管理 API 服务
import { fetchWithAuth } from './api'
import type {
  CapitalOverview,
  StrategyCapitalInfo,
  CapitalAllocationConfig,
  RebalanceRequest,
  RebalanceResult,
  CapitalOverviewResponse,
  CapitalAllocationResponse,
  UpdateAllocationRequest,
  UpdateAllocationResponse,
  CapitalHistoryResponse,
} from '../types/capital'

const API_BASE_URL = `${window.location.origin}/api`

// 获取资金概览
export async function getCapitalOverview(): Promise<CapitalOverviewResponse> {
  return fetchWithAuth(`${API_BASE_URL}/capital/overview`)
}

// 获取资金分配配置
export async function getCapitalAllocation(): Promise<CapitalAllocationResponse> {
  return fetchWithAuth(`${API_BASE_URL}/capital/allocation`)
}

// 更新资金分配
export async function updateCapitalAllocation(
  request: UpdateAllocationRequest
): Promise<UpdateAllocationResponse> {
  return fetchWithAuth(`${API_BASE_URL}/capital/allocation`, {
    method: 'PUT',
    body: JSON.stringify(request),
  })
}

// 更新单个策略的资金配置
export async function updateStrategyCapital(
  strategyId: string,
  config: Partial<CapitalAllocationConfig>
): Promise<UpdateAllocationResponse> {
  return fetchWithAuth(`${API_BASE_URL}/capital/allocation/${strategyId}`, {
    method: 'PUT',
    body: JSON.stringify(config),
  })
}

// 触发资金再平衡
export async function rebalanceCapital(request: RebalanceRequest): Promise<RebalanceResult> {
  return fetchWithAuth(`${API_BASE_URL}/capital/rebalance`, {
    method: 'POST',
    body: JSON.stringify(request),
  })
}

// 获取资金历史记录
export async function getCapitalHistory(
  days: number = 30
): Promise<CapitalHistoryResponse> {
  return fetchWithAuth(`${API_BASE_URL}/capital/history?days=${days}`)
}

// 获取单个策略的资金详情
export async function getStrategyCapitalDetail(
  strategyId: string
): Promise<{ capital: StrategyCapitalInfo }> {
  return fetchWithAuth(`${API_BASE_URL}/capital/allocation/${strategyId}`)
}

// 设置预留保证金
export async function setReserveCapital(
  amount: number
): Promise<{ success: boolean; message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/capital/reserve`, {
    method: 'PUT',
    body: JSON.stringify({ amount }),
  })
}

// 锁定/解锁策略资金
export async function lockStrategyCapital(
  strategyId: string,
  locked: boolean
): Promise<{ success: boolean; message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/capital/allocation/${strategyId}/lock`, {
    method: 'POST',
    body: JSON.stringify({ locked }),
  })
}
