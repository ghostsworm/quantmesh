// 倉位计划 API 服務
import { fetchWithAuth } from './api'
import type {
  CreatePositionPlanRequest,
  CreatePositionPlanResponse,
  UpdatePositionPlanRequest,
  UpdatePositionPlanResponse,
  GetPositionPlansResponse,
  GetPositionPlanResponse,
  CheckPositionPlanResponse,
} from '../types/positionPlan'

const API_BASE_URL = `${window.location.origin}/api`

// 獲取倉位计划列表
export async function getPositionPlans(params?: {
  exchange?: string
  symbol?: string
  status?: string
  limit?: number
  offset?: number
}): Promise<GetPositionPlansResponse> {
  const queryParams = new URLSearchParams()
  if (params?.exchange) queryParams.append('exchange', params.exchange)
  if (params?.symbol) queryParams.append('symbol', params.symbol)
  if (params?.status) queryParams.append('status', params.status)
  if (params?.limit) queryParams.append('limit', params.limit.toString())
  if (params?.offset) queryParams.append('offset', params.offset.toString())
  const url = `${API_BASE_URL}/position-plans${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

// 獲取單個倉位计划
export async function getPositionPlan(id: number): Promise<GetPositionPlanResponse> {
  return fetchWithAuth(`${API_BASE_URL}/position-plans/${id}`)
}

// 創建倉位计划
export async function createPositionPlan(request: CreatePositionPlanRequest): Promise<CreatePositionPlanResponse> {
  return fetchWithAuth(`${API_BASE_URL}/position-plans`, {
    method: 'POST',
    body: JSON.stringify(request),
  })
}

// 更新倉位计划
export async function updatePositionPlan(
  id: number,
  request: UpdatePositionPlanRequest
): Promise<UpdatePositionPlanResponse> {
  return fetchWithAuth(`${API_BASE_URL}/position-plans/${id}`, {
    method: 'PUT',
    body: JSON.stringify(request),
  })
}

// 取消倉位计划
export async function cancelPositionPlan(
  id: number,
  restoreLimit: boolean = false
): Promise<UpdatePositionPlanResponse> {
  return fetchWithAuth(`${API_BASE_URL}/position-plans/${id}?restoreLimit=${restoreLimit}`, {
    method: 'DELETE',
  })
}

// 检查當前倉位與目標差异
export async function checkPositionPlan(
  exchange: string,
  symbol: string
): Promise<CheckPositionPlanResponse> {
  const queryParams = new URLSearchParams()
  queryParams.append('exchange', exchange)
  queryParams.append('symbol', symbol)
  return fetchWithAuth(`${API_BASE_URL}/position-plans/check?${queryParams.toString()}`)
}
