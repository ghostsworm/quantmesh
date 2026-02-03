const API_BASE_URL = `${window.location.origin}/api`

async function fetchWithAuth(url: string, options: RequestInit = {}) {
  const currentLang = localStorage.getItem('i18nextLng') || 'zh-CN'
  const headers: HeadersInit = {
    'Content-Type': 'application/json',
    'Accept-Language': currentLang,
    ...(options.headers as Record<string, string>),
  }
  const response = await fetch(url, { ...options, headers, credentials: 'include' })
  if (!response.ok) {
    const errorText = await response.text()
    throw new Error(`HTTP ${response.status}: ${errorText}`)
  }
  return response.json()
}

async function fetchText(url: string) {
  const currentLang = localStorage.getItem('i18nextLng') || 'zh-CN'
  const response = await fetch(url, {
    headers: { 'Accept-Language': currentLang },
    credentials: 'include',
  })
  if (!response.ok) throw new Error(`HTTP ${response.status}`)
  return response.text()
}

// 策略参數定义
export interface ParamField {
  name: string
  label: string
  type: string
  required: boolean
  default?: unknown
  min?: number
  max?: number
  step?: number // 步長，如 0.1 表示允許小數
  options?: { value: unknown; label: string }[]
  unit?: string
  hint?: string
}

export interface StrategyParamDefinition {
  strategy_type: string
  name: string
  description: string
  params: ParamField[]
}

// 交易對預設
export interface SymbolBacktestPreset {
  symbol: string
  volatility_type: string
  recommended_days: number[]
  recommended_interval: string
  grid_gap_range: string
  interval_options: string[]
}

// 回测任務
export interface BacktestTask {
  id: string
  status: string
  strategy: string
  symbol: string
  interval: string
  start_time: string
  end_time: string
  params: Record<string, unknown>
  total_capital: number
  progress: number
  created_at: string
  started_at?: string
  completed_at?: string
  error?: string
  result_path?: string
  report_path?: string
}

// 缓存信息
export interface CacheInfo {
  name: string
  symbol: string
  interval: string
  start: string
  end: string
  candles: number
  size_mb: number
  created: string
}

export async function getBacktestStrategies(): Promise<{ success: boolean; strategies: StrategyParamDefinition[] }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/strategies`)
}

export async function getBacktestPreset(symbol: string): Promise<{ success: boolean; preset: SymbolBacktestPreset }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/presets/${encodeURIComponent(symbol)}`)
}

export async function postCacheGenerate(params: {
  symbol: string
  interval: string
  start_date: string
  end_date: string
}): Promise<{ success: boolean; message: string; cache_key: string }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/cache/generate`, {
    method: 'POST',
    body: JSON.stringify(params),
  })
}

export async function getCacheStatus(params: {
  symbol: string
  interval: string
  start_date: string
  end_date: string
}): Promise<{ success: boolean; cache_key: string; exists: boolean }> {
  const q = new URLSearchParams(params)
  return fetchWithAuth(`${API_BASE_URL}/backtest/cache/status?${q}`)
}

export async function getCacheStats(): Promise<{ success: boolean; stats: { file_count: number; total_size: number; size_mb: number } }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/cache/stats`)
}

export async function listCache(): Promise<{ success: boolean; caches: CacheInfo[] }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/cache/list`)
}

export async function deleteCache(cacheKey: string): Promise<{ success: boolean; message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/cache/${encodeURIComponent(cacheKey)}`, { method: 'DELETE' })
}

export async function postBacktestTask(params: {
  strategy: string
  symbol: string
  interval: string
  start_time: string
  end_time: string
  params?: Record<string, unknown>
  total_capital: number
}): Promise<{ success: boolean; message: string; task_id: string }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/tasks`, {
    method: 'POST',
    body: JSON.stringify(params),
  })
}

export async function getBacktestTasks(limit?: number, offset?: number): Promise<{ success: boolean; tasks: BacktestTask[] }> {
  const q = new URLSearchParams()
  if (limit != null) q.set('limit', String(limit))
  if (offset != null) q.set('offset', String(offset))
  const query = q.toString()
  return fetchWithAuth(`${API_BASE_URL}/backtest/tasks${query ? '?' + query : ''}`)
}

export async function getBacktestTask(id: string): Promise<{ success: boolean; task: BacktestTask }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/tasks/${encodeURIComponent(id)}`)
}

export async function getBacktestTaskResult(id: string): Promise<unknown> {
  const url = `${API_BASE_URL}/backtest/tasks/${encodeURIComponent(id)}/result`
  return fetchWithAuth(url)
}

export async function getBacktestTaskReport(id: string, download = false): Promise<string> {
  const url = `${API_BASE_URL}/backtest/tasks/${encodeURIComponent(id)}/report${download ? '?download=1' : ''}`
  return fetchText(url)
}

export interface KlinePoint {
  time: number
  open: number
  high: number
  low: number
  close: number
  volume: number
}

export async function getBacktestTaskKlines(id: string): Promise<{ klines: KlinePoint[]; symbol: string; interval: string }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/tasks/${encodeURIComponent(id)}/klines`)
}

export async function deleteBacktestTask(id: string): Promise<{ success: boolean; message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/tasks/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

// 回测交易所信息
export interface BacktestExchangeInfo {
  exchange: string
  market_types: string[] // 支援的市場類型：spot, futures
  is_configured: boolean // 是否已在 config 中配置
}

// 回测交易對信息
export interface BacktestSymbolInfo {
  symbol: string
  exchange: string
  market_type: string // spot 或 futures
  is_configured: boolean // 是否已在 config 中配置
}

// 獲取可用於回测的交易所列表
export async function getBacktestExchanges(): Promise<{ success: boolean; exchanges: BacktestExchangeInfo[] }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/exchanges`)
}

// 獲取可用於回测的交易對列表（按交易所+市場類型過濾）
export async function getBacktestSymbols(exchange: string, marketType: string = 'futures'): Promise<{ success: boolean; symbols: BacktestSymbolInfo[] }> {
  const params = new URLSearchParams({ exchange, market_type: marketType })
  return fetchWithAuth(`${API_BASE_URL}/backtest/symbols?${params}`)
}

// 獲取已配置交易對的策略参數（用於預填）
export async function getBacktestConfigParams(params: {
  exchange?: string
  symbol: string
  strategy: string
}): Promise<{ success: boolean; found: boolean; params: Record<string, unknown> }> {
  const q = new URLSearchParams()
  if (params.exchange) q.set('exchange', params.exchange)
  q.set('symbol', params.symbol)
  q.set('strategy', params.strategy)
  return fetchWithAuth(`${API_BASE_URL}/backtest/config-params?${q}`)
}

// ========== 智能參數推薦 API ==========

// 波動率信息
export interface VolatilityInfo {
  symbol: string
  volatility_7d: number
  volatility_30d: number
  average_range: number
  trend_direction: 'up' | 'down' | 'sideways'
  updated_at: string
}

// 智能參數推薦結果
export interface SmartParamsRecommendation {
  symbol: string
  exchange: string
  market_type: string
  strategy: string
  current_price: number
  volatility: VolatilityInfo
  params: Record<string, unknown>
  reasoning: string
  confidence: number
  generated_at: string
}

// 獲取智能參數推薦
export async function getSmartParamsRecommendation(params: {
  exchange?: string
  market_type?: string
  symbol: string
  strategy: string
  total_capital?: number
}): Promise<{ success: boolean; recommendation: SmartParamsRecommendation }> {
  const q = new URLSearchParams()
  if (params.exchange) q.set('exchange', params.exchange)
  if (params.market_type) q.set('market_type', params.market_type)
  q.set('symbol', params.symbol)
  q.set('strategy', params.strategy)
  if (params.total_capital) q.set('total_capital', String(params.total_capital))
  return fetchWithAuth(`${API_BASE_URL}/backtest/smart-params?${q}`)
}

// 獲取多個策略的智能推薦
export async function getMultipleSmartParams(params: {
  exchange?: string
  market_type?: string
  symbol: string
  total_capital?: number
}): Promise<{ success: boolean; recommendations: SmartParamsRecommendation[] }> {
  const q = new URLSearchParams()
  if (params.exchange) q.set('exchange', params.exchange)
  if (params.market_type) q.set('market_type', params.market_type)
  q.set('symbol', params.symbol)
  if (params.total_capital) q.set('total_capital', String(params.total_capital))
  return fetchWithAuth(`${API_BASE_URL}/backtest/smart-params/multiple?${q}`)
}

// ========== 預計算回測 API ==========

// 回測指標
export interface BacktestMetrics {
  total_return: number
  max_drawdown: number
  sharpe_ratio: number
  total_trades: number
  win_rate: number
  profit_factor?: number
  avg_trade_return?: number
}

// 回測結果
export interface BacktestResult {
  metrics: BacktestMetrics
  trades?: unknown[]
  equity_curve?: unknown[]
}

// 預計算結果
export interface PrecomputedResult {
  symbol: string
  exchange: string
  market_type: string
  strategy: string
  recommendation: SmartParamsRecommendation
  task_id: string
  task_status: string
  result?: BacktestResult
  generated_at: string
  completed_at?: string
  is_ready: boolean
  reasoning_report?: string
}

// 獲取預計算結果列表
export async function getPrecomputedResults(params?: {
  symbol?: string
  only_ready?: boolean
}): Promise<{ success: boolean; results: PrecomputedResult[]; count: number }> {
  const q = new URLSearchParams()
  if (params?.symbol) q.set('symbol', params.symbol)
  if (params?.only_ready) q.set('only_ready', '1')
  const query = q.toString()
  return fetchWithAuth(`${API_BASE_URL}/backtest/precomputed${query ? '?' + query : ''}`)
}

// 獲取特定預計算結果
export async function getPrecomputedResult(
  symbol: string,
  strategy: string,
  exchange?: string
): Promise<{ success: boolean; result: PrecomputedResult }> {
  const q = new URLSearchParams()
  if (exchange) q.set('exchange', exchange)
  return fetchWithAuth(`${API_BASE_URL}/backtest/precomputed/${encodeURIComponent(symbol)}/${encodeURIComponent(strategy)}?${q}`)
}

// 觸發預計算
export async function triggerPrecompute(params: {
  symbol: string
  exchange?: string
  market_type?: string
  strategy: string
}): Promise<{ success: boolean; message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/precomputed/trigger`, {
    method: 'POST',
    body: JSON.stringify(params),
  })
}

// 自動調度器配置
export interface SchedulerSymbolConfig {
  symbol: string
  exchange: string
  market_type: string
  strategies: string[]
}

// 調度器狀態
export interface AutoSchedulerStatus {
  enabled: boolean
  running: boolean
  schedule_interval: string
  total_tasks: number
  ready_count: number
  running_count: number
  symbols: SchedulerSymbolConfig[]
}

// 獲取自動調度器狀態
export async function getAutoSchedulerStatus(): Promise<{ success: boolean } & AutoSchedulerStatus> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/scheduler/status`)
}

// ========== 參數優化 API ==========

export interface ParamRange {
  min: number
  max: number
  step: number
}

export interface OptimSearchSpace {
  strategy: string
  ranges: Record<string, ParamRange>
}

export interface OptimTask {
  id: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  strategy: string
  symbol: string
  interval: string
  start_time: string
  end_time: string
  total_capital: number
  search_space: OptimSearchSpace
  progress: number
  total_combos: number
  completed_combos: number
  created_at: string
  completed_at?: string
  error?: string
}

export interface OptimParamResult {
  params: Record<string, number>
  total_return: number
  max_drawdown: number
  sharpe_ratio: number
  win_rate: number
  total_trades: number
}

export interface OptimResult {
  task_id: string
  strategy: string
  all_results: OptimParamResult[]
  best_by_return?: OptimParamResult
  best_by_sharpe?: OptimParamResult
  elapsed: number
  total_combos: number
  completed: number
}

export async function postOptimTask(params: {
  strategy: string
  symbol: string
  interval: string
  start_time: string
  end_time: string
  total_capital: number
  search_space?: OptimSearchSpace
}): Promise<{ success: boolean; message: string; task_id: string }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/optim/tasks`, {
    method: 'POST',
    body: JSON.stringify(params),
  })
}

export async function getOptimTasks(limit?: number, offset?: number): Promise<{ success: boolean; tasks: OptimTask[] }> {
  const q = new URLSearchParams()
  if (limit != null) q.set('limit', String(limit))
  if (offset != null) q.set('offset', String(offset))
  const query = q.toString()
  return fetchWithAuth(`${API_BASE_URL}/backtest/optim/tasks${query ? '?' + query : ''}`)
}

export async function getOptimTask(id: string): Promise<{ success: boolean; task: OptimTask }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/optim/tasks/${encodeURIComponent(id)}`)
}

export async function getOptimTaskResult(id: string): Promise<OptimResult> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/optim/tasks/${encodeURIComponent(id)}/result`)
}

export async function deleteOptimTask(id: string): Promise<{ success: boolean; message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/optim/tasks/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export async function getOptimSearchSpace(strategy: string): Promise<{ success: boolean; search_space: OptimSearchSpace }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/optim/space/${encodeURIComponent(strategy)}`)
}
