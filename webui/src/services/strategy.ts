// 策略 API 服務
import { fetchWithAuth } from './api'
import type {
  StrategyInfo,
  StrategyDetailInfo,
  StrategyConfig,
  StrategyLicense,
  StrategiesResponse,
  StrategyDetailResponse,
  StrategyEnableResponse,
  StrategyLicenseResponse,
  StrategyConfigsResponse,
} from '../types/strategy'

const API_BASE_URL = `${window.location.origin}/api`

// 獲取所有策略列表
export async function getStrategies(): Promise<StrategiesResponse> {
  return fetchWithAuth(`${API_BASE_URL}/strategies`)
}

// 獲取策略详情
export async function getStrategyDetail(strategyId: string): Promise<StrategyDetailResponse> {
  return fetchWithAuth(`${API_BASE_URL}/strategies/${strategyId}`)
}

// 啟用策略
export async function enableStrategy(strategyId: string): Promise<StrategyEnableResponse> {
  return fetchWithAuth(`${API_BASE_URL}/strategies/${strategyId}/enable`, {
    method: 'POST',
  })
}

// 禁用策略
export async function disableStrategy(strategyId: string): Promise<StrategyEnableResponse> {
  return fetchWithAuth(`${API_BASE_URL}/strategies/${strategyId}/disable`, {
    method: 'POST',
  })
}

// 检查策略授权状態
export async function getStrategyLicense(strategyId: string): Promise<StrategyLicenseResponse> {
  return fetchWithAuth(`${API_BASE_URL}/strategies/${strategyId}/license`)
}

// 獲取策略配置
export async function getStrategyConfigs(): Promise<StrategyConfigsResponse> {
  return fetchWithAuth(`${API_BASE_URL}/strategies/configs`)
}

// 更新策略配置
export async function updateStrategyConfig(
  strategyId: string,
  config: Partial<StrategyConfig>
): Promise<{ success: boolean; message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/strategies/${strategyId}/config`, {
    method: 'PUT',
    body: JSON.stringify(config),
  })
}

// 獲取策略類型分類
export async function getStrategyTypes(): Promise<{ types: string[] }> {
  return fetchWithAuth(`${API_BASE_URL}/strategies/types`)
}

// 购買付费策略
export async function purchaseStrategy(
  strategyId: string,
  tier: 'basic' | 'pro' | 'enterprise'
): Promise<{ success: boolean; message: string; license?: StrategyLicense }> {
  return fetchWithAuth(`${API_BASE_URL}/strategies/${strategyId}/purchase`, {
    method: 'POST',
    body: JSON.stringify({ tier }),
  })
}

// 獲取已啟用的策略列表
export async function getEnabledStrategies(): Promise<{ strategies: StrategyInfo[] }> {
  return fetchWithAuth(`${API_BASE_URL}/strategies/enabled`)
}

// 批量更新策略状態
export async function batchUpdateStrategies(
  updates: { strategyId: string; enabled: boolean }[]
): Promise<{ success: boolean; message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/strategies/batch-update`, {
    method: 'POST',
    body: JSON.stringify({ updates }),
  })
}
