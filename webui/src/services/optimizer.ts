import { fetchWithAuth } from './api'

const API_BASE_URL = `${window.location.origin}/api`

// 参數範圍
export interface Range {
  min: number
  max: number
  step: number
}

export interface IntRange {
  min: number
  max: number
  step: number
}

// 搜索空间
export interface OptimSearchSpace {
  price_low_range: Range
  price_high_range: Range
  grid_count_range: IntRange
  order_qty_range: Range
}

// 优化配置
export interface OptimConfig {
  method: 'grid' | 'bayesian' | 'genetic'
  lambda: number
  max_iterations: number
  tolerance: number
  parallelism: number
}

// 优化任務状態
export interface OptimizerTaskStatus {
  success: boolean
  task_id: string
  status: string
  progress: number
  error: string
  created_at: string
  updated_at: string
}

// 网格参數結果
export interface GridBacktestParams {
  price_low: number
  price_high: number
  grid_count: number
  order_quantity: number
  total_capital: number
  fee_rate: number
  slippage_ratio: number
}

// 性能指標
export interface Metrics {
  total_return: number
  annualized_return: number
  max_drawdown: number
  max_drawdown_duration: number
  volatility: number
  sharpe_ratio: number
  sortino_ratio: number
  calmar_ratio: number
  total_trades: number
  win_rate: number
  profit_factor: number
  avg_win: number
  avg_loss: number
  largest_win: number
  largest_loss: number
  max_consecutive_wins: number
  max_consecutive_losses: number
}

// 單组参數結果
export interface ParamResult {
  params: GridBacktestParams
  score: number
  metrics: Metrics
}

// 热力图數據
export interface HeatmapData {
  x_axis: (number | string)[]
  y_axis: (number | string)[]
  data: number[][]
}

// 优化結果
export interface OptimResult {
  best_params: GridBacktestParams
  best_score: number
  best_metrics: Metrics
  all_results: ParamResult[]
  heatmap_data: HeatmapData | null
  elapsed: number
  iterations: number
  method: string
}

// 啟动优化任務
export async function postOptimizerRun(params: {
  exchange?: string
  symbol: string
  interval: string
  start_time: string
  end_time: string
  initial_capital: number
  search_space: OptimSearchSpace
  config: OptimConfig
}): Promise<{ success: boolean; message: string; task_id: string }> {
  return fetchWithAuth(`${API_BASE_URL}/optimizer/run`, {
    method: 'POST',
    body: JSON.stringify(params),
  })
}

// 查詢任務状態
export async function getOptimizerStatus(id: string): Promise<OptimizerTaskStatus> {
  return fetchWithAuth(`${API_BASE_URL}/optimizer/status/${encodeURIComponent(id)}`)
}

// 獲取优化結果
export async function getOptimizerResult(id: string): Promise<{ success: boolean; status: string; result: OptimResult | null }> {
  return fetchWithAuth(`${API_BASE_URL}/optimizer/result/${encodeURIComponent(id)}`)
}

// 獲取交易對當前價格（用於自动初始化搜索空间）
export async function getOptimizerPrice(exchange: string, symbol: string): Promise<{ price: number; symbol: string; exchange: string }> {
  const params = new URLSearchParams({ exchange, symbol })
  return fetchWithAuth(`${API_BASE_URL}/optimizer/price?${params}`)
}

// 停止优化任務
export async function postOptimizerStop(id: string): Promise<{ success: boolean; message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/optimizer/stop/${encodeURIComponent(id)}`, { method: 'POST' })
}
