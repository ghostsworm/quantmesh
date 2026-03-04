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

// 策略組合模板
export interface StrategyTemplate {
  id: string
  name: string
  description: string
  type: 'combo' | 'hedge'
  strategies: string[]
  weights?: number[]
  tags: string[]
}

export async function getStrategyTemplates(): Promise<{
  success: boolean
  templates: StrategyTemplate[]
  total: number
}> {
  return fetchWithAuth(`${API_BASE_URL}/strategies/templates`)
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

// ========== 策略運行狀態 API ==========

// DCA策略可视化数据
export interface DCAVisualizationData {
  layers?: Array<{
    index: number
    price: number
    quantity: number
    cost: number
    pnl: number
    pnlPercent: number
    status: string
    filledAt: number
  }>
  atr?: number
  dynamicInterval?: number
  baseInterval?: number
  minPriceStep?: number
  maxPriceStep?: number
  currentPrice?: number
  avgEntryPrice?: number
  totalCost?: number
  totalQty?: number
  firstOrderTakeProfit?: number
  lastOrderTakeProfit?: number
  totalTakeProfit?: number
  stopLoss?: number
  nextBuyPrice?: number
  requiredDrop?: number
  distanceToNextBuy?: number
  isPaused?: boolean
  pauseUntil?: number
  cascadeProtection?: boolean
  trendFilterEnabled?: boolean
  isTrendUp?: boolean
  takeProfitTriggered?: boolean
  highestProfit?: number
  currentLayer?: number
  maxLayers?: number
}

// 趋势跟踪策略可视化数据
export interface TrendFollowingVisualizationData {
  fastMA?: number
  slowMA?: number
  method?: string
  shortPeriod?: number
  longPeriod?: number
  currentPrice?: number
  trend?: 'up' | 'down' | 'side'
  maDiff?: number
  maDiffAbs?: number
  hasPosition?: boolean
  entryPrice?: number
  pnlPercent?: number
  stopLoss?: number
  takeProfit?: number
  isGoldenCross?: boolean
  isDeathCross?: boolean
}

// 均值回归策略可视化数据
export interface MeanReversionVisualizationData {
  upperBand?: number
  middleBand?: number
  lowerBand?: number
  currentPrice?: number
  positionInBand?: number
  touchesUpperBand?: boolean
  touchesLowerBand?: boolean
  hasPosition?: boolean
  entryPrice?: number
  pnlPercent?: number
  period?: number
  stdMultiplier?: number
  reversionThreshold?: number
  buySignal?: boolean
  sellSignal?: boolean
  distanceToBuy?: number
  distanceToSell?: number
}

// 网格策略可视化数据
export interface GridVisualizationData {
  slots?: Array<{
    price: number
    positionStatus: string
    positionQty: number
    orderID: number
    orderSide: string
    orderStatus: string
    orderPrice: number
    slotStatus: string
  }>
  slotCount?: number
  filledCount?: number
  emptyCount?: number
  minPrice?: number
  maxPrice?: number
  priceRange?: number
  priceInterval?: number
}

// 策略可视化数据联合类型
export type StrategyVisualizationData = 
  | DCAVisualizationData 
  | TrendFollowingVisualizationData 
  | MeanReversionVisualizationData 
  | GridVisualizationData
  | Record<string, any>

// 策略運行狀態類型
export interface StrategyRuntimeStatus {
  name: string
  type: string
  isEnabled: boolean
  isRunning: boolean
  weight: number
  allocatedFunds: number
  usedFunds: number
  availableFunds: number
  positionCount: number
  orderCount: number
  statistics: {
    totalTrades: number
    winRate: number
    totalPnL: number
    totalVolume: number
  } | null
  positions?: Array<{
    symbol: string
    size: number
    entryPrice: number
    currentPrice: number
    pnl: number
  }>
  orders?: Array<{
    orderId: number
    symbol: string
    side: string
    price: number
    quantity: number
    status: string
  }>
  visualizationData?: StrategyVisualizationData // 新增：策略可视化數據
}

export interface StrategyRuntimeResponse {
  success: boolean
  strategies: StrategyRuntimeStatus[]
  exchange?: string
  symbol?: string
  message?: string
}

/** 單個幣種下的策略運行狀態聚合（GET /api/strategies/runtime/all） */
export interface SymbolStrategyRuntimeItem {
  exchange: string
  symbol: string
  marketType: string
  strategies: StrategyRuntimeStatus[]
}

export interface StrategyRuntimeAllResponse {
  success: boolean
  data: SymbolStrategyRuntimeItem[]
  message?: string
}

export interface SingleStrategyRuntimeResponse {
  success: boolean
  strategy: StrategyRuntimeStatus | null
  message?: string
}

// 單個幣種下的策略運行狀態聚合（用於 GET /api/strategies/runtime/all）
export interface SymbolStrategyRuntimeItem {
  exchange: string
  symbol: string
  marketType: string
  strategies: StrategyRuntimeStatus[]
}

export interface StrategyRuntimeAllResponse {
  success: boolean
  data: SymbolStrategyRuntimeItem[]
  message?: string
}

// 獲取所有幣種下所有策略的運行狀態（聚合）
export async function getStrategyRuntimeStatusAll(): Promise<StrategyRuntimeAllResponse> {
  const url = `${API_BASE_URL}/strategies/runtime/all`
  return fetchWithAuth(url)
}

// 獲取所有策略的運行狀態
export async function getStrategyRuntimeStatus(
  exchange?: string,
  symbol?: string
): Promise<StrategyRuntimeResponse> {
  const params = new URLSearchParams()
  if (exchange) params.append('exchange', exchange)
  if (symbol) params.append('symbol', symbol)
  const queryString = params.toString()
  const url = `${API_BASE_URL}/strategies/runtime${queryString ? `?${queryString}` : ''}`
  return fetchWithAuth(url)
}

// 獲取單個策略的運行狀態
export async function getStrategyRuntimeStatusById(
  strategyId: string,
  exchange?: string,
  symbol?: string
): Promise<SingleStrategyRuntimeResponse> {
  const params = new URLSearchParams()
  if (exchange) params.append('exchange', exchange)
  if (symbol) params.append('symbol', symbol)
  const queryString = params.toString()
  const url = `${API_BASE_URL}/strategies/runtime/${strategyId}${queryString ? `?${queryString}` : ''}`
  return fetchWithAuth(url)
}
