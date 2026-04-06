import { fetchWithAuth } from './api'
import type {
  FundingCarryDashboardResponse,
  FundingIncomeHistoryResponse,
  BatchCreateFundingRequest,
  BatchCreateFundingResponse,
} from '../types/fundingCarry'

const API_BASE_URL = `${window.location.origin}/api`

export async function getFundingCarryDashboard(): Promise<FundingCarryDashboardResponse> {
  return fetchWithAuth(`${API_BASE_URL}/funding-carry/dashboard`)
}

export async function getFundingIncomeHistory(
  symbol?: string,
  period: string = '30d'
): Promise<FundingIncomeHistoryResponse> {
  const params = new URLSearchParams({ period })
  if (symbol) params.append('symbol', symbol)
  return fetchWithAuth(`${API_BASE_URL}/funding-carry/income-history?${params.toString()}`)
}

export async function batchCreateFundingBots(
  req: BatchCreateFundingRequest
): Promise<BatchCreateFundingResponse> {
  return fetchWithAuth(`${API_BASE_URL}/funding-carry/batch-create`, {
    method: 'POST',
    body: JSON.stringify(req),
  })
}
