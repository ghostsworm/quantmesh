// 使用页面同源，避免相对路径被代理/扩展劫持
const API_BASE_URL = `${window.location.origin}/api`

// Helper function to make authenticated requests
export async function fetchWithAuth(url: string, options: RequestInit = {}) {
  // 获取当前语言设置
  const currentLang = localStorage.getItem('i18nextLng') || 'zh-CN'
  
  const headers = {
    'Content-Type': 'application/json',
    'Accept-Language': currentLang,
    ...options.headers,
  }

  const response = await fetch(url, {
    ...options,
    headers,
    credentials: 'include', // 包含 cookies
  })

  if (!response.ok) {
    const errorText = await response.text()
    throw new Error(`HTTP ${response.status}: ${errorText}`)
  }

  return response.json()
}

// System Status
export interface SystemStatus {
  running: boolean
  exchange: string
  symbol: string
  current_price: number
  total_pnl: number
  total_trades: number
  risk_triggered: boolean
  uptime: number
}

export async function getSystemStatus(exchange?: string, symbol?: string): Promise<SystemStatus> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  const url = `${API_BASE_URL}/status${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

// Alias for backward compatibility
export const getStatus = getSystemStatus

// Symbols and Exchanges
export interface SymbolInfo {
  exchange: string
  symbol: string
  is_active: boolean
  current_price: number
}

export interface SymbolsResponse {
  symbols: SymbolInfo[]
}

export async function getSymbols(): Promise<SymbolsResponse> {
  return fetchWithAuth(`${API_BASE_URL}/symbols`)
}

export interface ExchangesResponse {
  exchanges: string[]
}

export async function getExchanges(): Promise<ExchangesResponse> {
  return fetchWithAuth(`${API_BASE_URL}/exchanges`)
}

// Positions
// 旧的 PositionInfo 接口（用于其他API，保留以兼容）
export interface ExchangePositionInfo {
  symbol: string
  size: number
  entry_price: number
  mark_price: number
  unrealized_pnl: number
  leverage: number
}

// 新的 PositionInfo 接口（用于持仓页面）
export interface PositionInfo {
  price: number
  quantity: number
  value: number
  unrealized_pnl: number
}

// 持仓汇总接口（用于持仓页面）
export interface PositionSummary {
  total_quantity: number
  total_value: number
  position_count: number
  average_price: number
  current_price: number
  unrealized_pnl: number
  pnl_percentage: number
  positions: PositionInfo[]
}

export interface PositionsResponse {
  summary: PositionSummary
}

export async function getPositions(exchange?: string, symbol?: string): Promise<PositionsResponse> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  const url = `${API_BASE_URL}/positions${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

// 旧的 PositionSummary 接口（用于其他API，保留以兼容）
export interface PositionSummaryLegacy {
  total_position: number
  total_unrealized_pnl: number
  total_value: number
  position_count: number
}

export interface PositionsSummaryResponse {
  summary: PositionSummaryLegacy
}

export async function getPositionsSummary(exchange?: string, symbol?: string): Promise<PositionsSummaryResponse> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  const url = `${API_BASE_URL}/positions/summary${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

// Orders
export interface OrderInfo {
  order_id: number
  client_order_id: string
  symbol: string
  side: string
  type: string
  price: number
  quantity: number
  filled_quantity: number
  status: string
  created_at: string
  updated_at: string
}

export interface OrdersResponse {
  orders: OrderInfo[]
}

export async function getOrders(exchange?: string, symbol?: string): Promise<OrdersResponse> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  const url = `${API_BASE_URL}/orders${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

export interface OrderHistoryParams {
  limit?: number
  offset?: number
  start_time?: string
  end_time?: string
  exchange?: string
  symbol?: string
}

export async function getOrderHistory(params?: OrderHistoryParams): Promise<OrdersResponse> {
  const queryParams = new URLSearchParams()
  if (params?.limit) queryParams.append('limit', params.limit.toString())
  if (params?.offset) queryParams.append('offset', params.offset.toString())
  if (params?.start_time) queryParams.append('start_time', params.start_time)
  if (params?.end_time) queryParams.append('end_time', params.end_time)
  if (params?.exchange) queryParams.append('exchange', params.exchange)
  if (params?.symbol) queryParams.append('symbol', params.symbol)
  
  const url = `${API_BASE_URL}/orders/history${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

// Pending Orders
export interface PendingOrderInfo {
  order_id: number
  client_order_id: string
  symbol: string
  side: string
  price: number
  quantity: number
  filled_quantity: number
  status: string
  created_at: string
  slot_price: number
}

export interface PendingOrdersResponse {
  orders: PendingOrderInfo[]
}

export async function getPendingOrders(exchange?: string, symbol?: string): Promise<PendingOrdersResponse> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  const url = `${API_BASE_URL}/orders/pending${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

// Statistics
export interface StatisticsSummary {
  total_trades: number
  total_volume: number
  total_pnl: number
  win_rate: number
  average_pnl: number
  max_profit: number
  max_loss: number
}

export interface StatisticsSummaryResponse {
  summary: StatisticsSummary
}

export async function getStatistics(exchange?: string, symbol?: string): Promise<StatisticsSummaryResponse> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  const url = `${API_BASE_URL}/statistics${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

export interface DailyStatistics {
  date: string
  total_trades: number
  total_volume: number
  total_pnl: number
  win_rate: number
  winning_trades?: number
  losing_trades?: number
}

export interface DailyStatisticsResponse {
  daily_statistics: DailyStatistics[]
}

export async function getDailyStatistics(exchange?: string, symbol?: string): Promise<DailyStatisticsResponse> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  const url = `${API_BASE_URL}/statistics/daily${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

export interface TradeStatistics {
  symbol: string
  trades: number
  volume: number
  pnl: number
  win_rate: number
}

export interface TradeStatisticsResponse {
  trade_statistics: TradeStatistics[]
}

export async function getTradeStatistics(): Promise<TradeStatisticsResponse> {
  return fetchWithAuth(`${API_BASE_URL}/statistics/trades`)
}

// PnL Statistics
export interface PnLSummary {
  symbol: string
  total_pnl: number
  total_trades: number
  total_volume: number
  win_rate: number
  win_trades: number
  loss_trades: number
}

export interface PnLSummaryResponse {
  summary: PnLSummary
}

export async function getPnLBySymbol(symbol: string, startTime?: string, endTime?: string): Promise<PnLSummaryResponse> {
  const queryParams = new URLSearchParams({ symbol })
  if (startTime) queryParams.append('start_time', startTime)
  if (endTime) queryParams.append('end_time', endTime)
  
  return fetchWithAuth(`${API_BASE_URL}/statistics/pnl/symbol?${queryParams.toString()}`)
}

export interface PnLBySymbol {
  symbol: string
  total_pnl: number
  total_trades: number
  total_volume: number
  win_rate: number
}

export interface PnLBySymbolResponse {
  pnl_by_symbol: PnLBySymbol[]
}

export async function getPnLByTimeRange(startTime: string, endTime: string): Promise<PnLBySymbolResponse> {
  const queryParams = new URLSearchParams({ start_time: startTime, end_time: endTime })
  return fetchWithAuth(`${API_BASE_URL}/statistics/pnl/time-range?${queryParams.toString()}`)
}

export interface SymbolPnLInfo {
  symbol: string
  total_pnl: number
  total_trades: number
  total_volume: number
  win_rate: number
}

export interface ExchangePnLResponse {
  exchange: string
  total_pnl: number
  total_trades: number
  total_volume: number
  win_rate: number
  symbols: SymbolPnLInfo[]
}

export interface ExchangePnLResponseData {
  exchanges: ExchangePnLResponse[]
}

export async function getPnLByExchange(startTime?: string, endTime?: string): Promise<ExchangePnLResponseData> {
  const queryParams = new URLSearchParams()
  if (startTime) queryParams.append('start_time', startTime)
  if (endTime) queryParams.append('end_time', endTime)
  const url = `${API_BASE_URL}/statistics/pnl/exchange${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

// System Metrics
export interface SystemMetrics {
  timestamp: string
  cpu_percent: number
  memory_mb: number
  memory_percent: number
  process_id: number
}

export interface SystemMetricsResponse {
  metrics: SystemMetrics[]
  granularity?: string
}

export interface SystemMetricsParams {
  start_time?: string
  end_time?: string
  granularity?: string
}

export async function getSystemMetrics(params?: SystemMetricsParams): Promise<SystemMetricsResponse> {
  const queryParams = new URLSearchParams()
  if (params?.start_time) queryParams.append('start_time', params.start_time)
  if (params?.end_time) queryParams.append('end_time', params.end_time)
  if (params?.granularity) queryParams.append('granularity', params.granularity)
  
  const url = `${API_BASE_URL}/system/metrics${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

export interface CurrentSystemMetricsResponse extends SystemMetrics {
}

export async function getCurrentSystemMetrics(): Promise<CurrentSystemMetricsResponse> {
  return fetchWithAuth(`${API_BASE_URL}/system/metrics/current`)
}

export interface DailySystemMetric {
  date: string
  avg_cpu_percent: number
  max_cpu_percent: number
  min_cpu_percent: number
  avg_memory_mb: number
  max_memory_mb: number
  min_memory_mb: number
  sample_count: number
}

export interface DailySystemMetricsResponse {
  metrics: DailySystemMetric[]
}

export async function getDailySystemMetrics(days?: number): Promise<DailySystemMetricsResponse> {
  const queryParams = new URLSearchParams()
  if (days) queryParams.append('days', days.toString())
  
  const url = `${API_BASE_URL}/system/metrics/daily${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

// Logs
export interface LogEntry {
  id: number
  level: string
  message: string
  timestamp: string
}

export interface LogsParams {
  limit?: number
  offset?: number
  level?: string
  start_time?: string
  end_time?: string
}

export interface LogsResponse {
  logs: LogEntry[]
  total: number
}

export async function getLogs(params?: LogsParams): Promise<LogsResponse> {
  const queryParams = new URLSearchParams()
  if (params?.limit) queryParams.append('limit', params.limit.toString())
  if (params?.offset) queryParams.append('offset', params.offset.toString())
  if (params?.level) queryParams.append('level', params.level)
  if (params?.start_time) queryParams.append('start_time', params.start_time)
  if (params?.end_time) queryParams.append('end_time', params.end_time)
  
  const url = `${API_BASE_URL}/logs${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

export type LogSubscribeHandler = (log: LogEntry) => void
export type LogSubscribeErrorHandler = (event: Event) => void

export function subscribeLogs(onLog: LogSubscribeHandler, onError?: LogSubscribeErrorHandler) {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const host = window.location.host
  const wsUrl = `${protocol}//${host}/ws?subscribe_logs=true`
  const socket = new WebSocket(wsUrl)

  const handleMessage = (event: MessageEvent) => {
    try {
      const payload = JSON.parse(event.data)
      if (payload?.type === 'log' && payload.data) {
        onLog({
          id: payload.data.id,
          timestamp: payload.data.timestamp,
          level: payload.data.level,
          message: payload.data.message,
        })
      }
    } catch (err) {
      console.error('解析日志消息失败:', err)
    }
  }

  const handleError = (event: Event) => {
    if (onError) {
      onError(event)
    }
  }

  const handleClose = (event: CloseEvent) => {
    if (onError && !event.wasClean) {
      onError(event)
    }
  }

  socket.addEventListener('message', handleMessage)
  socket.addEventListener('error', handleError)
  socket.addEventListener('close', handleClose)

  return () => {
    socket.removeEventListener('message', handleMessage)
    socket.removeEventListener('error', handleError)
    socket.removeEventListener('close', handleClose)
    socket.close()
  }
}

// 清理日志
export interface CleanLogsRequest {
  days: number
  levels?: string[]
}

export interface CleanLogsResponse {
  success: boolean
  rows_affected: number
  message: string
}

export async function cleanLogs(request: CleanLogsRequest): Promise<CleanLogsResponse> {
  return fetchWithAuth(`${API_BASE_URL}/logs/clean`, {
    method: 'POST',
    body: JSON.stringify(request),
  })
}

// 获取日志统计信息
export interface LogStats {
  total: number
  by_level: Record<string, number>
  oldest_time?: string
  newest_time?: string
}

export async function getLogStats(): Promise<LogStats> {
  return fetchWithAuth(`${API_BASE_URL}/logs/stats`)
}

// 优化日志数据库
export async function vacuumLogs(): Promise<{ success: boolean; message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/logs/vacuum`, {
    method: 'POST',
  })
}

// Slots
export interface SlotInfo {
  price: number
  position_status: string  // EMPTY/FILLED
  position_qty: number
  order_id: number
  client_order_id: string
  order_side: string  // BUY/SELL
  order_status: string  // NOT_PLACED/PLACED/CONFIRMED/PARTIALLY_FILLED/FILLED/CANCELED
  order_price: number
  order_filled_qty: number
  order_created_at: string
  slot_status: string  // FREE/PENDING/LOCKED
}

export interface SlotsResponse {
  slots: SlotInfo[]
}

export async function getSlots(exchange?: string, symbol?: string): Promise<SlotsResponse> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  const url = `${API_BASE_URL}/slots${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

// Strategy Allocation
export interface StrategyCapitalInfo {
  strategy_name: string
  allocated_capital: number
  used_capital: number
  available_capital: number
  utilization_rate: number
}

export interface StrategyAllocationResponse {
  strategies: StrategyCapitalInfo[]
}

export async function getStrategyAllocation(): Promise<StrategyAllocationResponse> {
  return fetchWithAuth(`${API_BASE_URL}/strategies/allocation`)
}

// Reconciliation
export interface ReconciliationStatus {
  reconcile_count: number
  last_reconcile_time: string | Date
  local_position: number
  total_buy_qty: number
  total_sell_qty: number
  estimated_profit: number
}

export interface ReconciliationStatusResponse {
  status: ReconciliationStatus
}

export async function getReconciliationStatus(): Promise<ReconciliationStatusResponse> {
  return fetchWithAuth(`${API_BASE_URL}/reconciliation/status`)
}

export interface ReconciliationHistory {
  id: number
  symbol: string
  reconcile_time: string | Date
  local_position: number
  exchange_position: number
  position_diff: number
  active_buy_orders: number
  active_sell_orders: number
  pending_sell_qty: number
  total_buy_qty: number
  total_sell_qty: number
  estimated_profit: number
  created_at: string | Date
}

export interface ReconciliationHistoryParams {
  limit?: number
  offset?: number
  start_time?: string
  end_time?: string
}

export interface ReconciliationHistoryResponse {
  history: ReconciliationHistory[]
}

export async function getReconciliationHistory(params?: ReconciliationHistoryParams): Promise<ReconciliationHistoryResponse> {
  const queryParams = new URLSearchParams()
  if (params?.limit) queryParams.append('limit', params.limit.toString())
  if (params?.offset) queryParams.append('offset', params.offset.toString())
  if (params?.start_time) queryParams.append('start_time', params.start_time)
  if (params?.end_time) queryParams.append('end_time', params.end_time)
  
  const url = `${API_BASE_URL}/reconciliation/history${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

// Risk Monitor
export interface RiskStatus {
  triggered: boolean
  triggered_time: string | Date
  recovered_time: string | Date
  monitor_symbols: string[]
}

export interface RiskStatusResponse {
  triggered: boolean
  triggered_time: string | Date
  recovered_time: string | Date
  monitor_symbols: string[]
}

export async function getRiskStatus(): Promise<RiskStatusResponse> {
  return fetchWithAuth(`${API_BASE_URL}/risk/status`)
}

export interface SymbolMonitorData {
  symbol: string
  current_price: number
  average_price: number
  price_deviation: number
  current_volume: number
  average_volume: number
  volume_ratio: number
  is_abnormal: boolean
  last_update: string | Date
}

export interface RiskMonitorDataResponse {
  symbols: SymbolMonitorData[]
}

export async function getRiskMonitorData(): Promise<RiskMonitorDataResponse> {
  return fetchWithAuth(`${API_BASE_URL}/risk/monitor`)
}

export interface RiskCheckSymbolInfo {
  symbol: string
  is_healthy: boolean
  price_deviation: number
  volume_ratio: number
  reason: string
}

export interface RiskCheckHistoryItem {
  check_time: string | Date
  symbols: RiskCheckSymbolInfo[]
  healthy_count: number
  total_count: number
}

export interface RiskCheckHistoryResponse {
  history: RiskCheckHistoryItem[]
}

export interface RiskCheckHistoryParams {
  start_time?: string
  end_time?: string
  limit?: number
}

export async function getRiskCheckHistory(params?: RiskCheckHistoryParams): Promise<RiskCheckHistoryResponse> {
  const queryParams = new URLSearchParams()
  if (params?.start_time) queryParams.append('start_time', params.start_time)
  if (params?.end_time) queryParams.append('end_time', params.end_time)
  if (params?.limit) queryParams.append('limit', String(params.limit))
  
  const url = `${API_BASE_URL}/risk/history${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

// Config
export interface Config {
  symbol: string
  interval: string
  order_quantity: number
  // ... other config fields
}

export interface ConfigResponse {
  config: Config
}

export async function getConfig(): Promise<ConfigResponse> {
  return fetchWithAuth(`${API_BASE_URL}/config`)
}

export async function updateConfig(config: Partial<Config>): Promise<{ message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/config/update`, {
    method: 'POST',
    body: JSON.stringify(config),
  })
}

// Trading Control
export async function startTrading(exchange?: string, symbol?: string): Promise<{ message: string }> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  const url = `${API_BASE_URL}/trading/start${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url, {
    method: 'POST',
  })
}

export async function stopTrading(exchange?: string, symbol?: string): Promise<{ message: string }> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  const url = `${API_BASE_URL}/trading/stop${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url, {
    method: 'POST',
  })
}

export interface ClosePositionsResponse {
  success_count: number
  fail_count: number
  message: string
}

export async function closeAllPositions(exchange?: string, symbol?: string): Promise<ClosePositionsResponse> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  const url = `${API_BASE_URL}/trading/close-positions${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url, {
    method: 'POST',
  })
}

// K线数据
export interface KlineData {
  time: number    // 时间戳（秒）
  open: number
  high: number
  low: number
  close: number
  volume: number
}

export interface KlinesResponse {
  klines: KlineData[]
  symbol: string
  interval: string
}

export async function getKlines(interval: string = '1m', limit: number = 500, exchange?: string, symbol?: string, signal?: AbortSignal): Promise<KlinesResponse> {
  const queryParams = new URLSearchParams({
    interval,
    limit: limit.toString(),
  })
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  return fetchWithAuth(`${API_BASE_URL}/klines?${queryParams.toString()}`, {
    signal,
  })
}

// Funding Rate
export interface FundingRateInfo {
  rate: number
  rate_pct: number
  timestamp: string
}

export interface FundingRateCurrentResponse {
  rates: Record<string, FundingRateInfo>
}

export async function getFundingRateCurrent(exchange?: string, symbol?: string): Promise<FundingRateCurrentResponse> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  const url = `${API_BASE_URL}/funding/current${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

export interface FundingRateHistoryItem {
  id: number
  symbol: string
  exchange: string
  rate: number
  rate_pct: number
  timestamp: string
  created_at: string
}

export interface FundingRateHistoryResponse {
  history: FundingRateHistoryItem[]
}

export async function getFundingRateHistory(symbol?: string, limit: number = 100): Promise<FundingRateHistoryResponse> {
  const queryParams = new URLSearchParams()
  if (symbol) {
    queryParams.append('symbol', symbol)
  }
  queryParams.append('limit', limit.toString())
  return fetchWithAuth(`${API_BASE_URL}/funding/history?${queryParams.toString()}`)
}

// AI Analysis API
export interface AIAnalysisStatus {
  enabled: boolean
  modules: {
    [key: string]: {
      enabled: boolean
      last_update: string | null
      has_data: boolean
    }
  }
}

export async function getAIAnalysisStatus(): Promise<AIAnalysisStatus> {
  return fetchWithAuth(`${API_BASE_URL}/ai/status`)
}

export interface AIMarketAnalysis {
  trend: string
  confidence: number
  signal: string
  reasoning: string
  price_prediction?: {
    short_term: number
    long_term: number
    timeframe: string
  }
}

export interface AIMarketAnalysisResponse {
  analysis: AIMarketAnalysis
  last_update: string
}

export async function getAIMarketAnalysis(): Promise<AIMarketAnalysisResponse> {
  return fetchWithAuth(`${API_BASE_URL}/ai/analysis/market`)
}

export interface AIParameterOptimization {
  recommended_params: {
    price_interval: number
    buy_window_size: number
    sell_window_size: number
    order_quantity: number
  }
  expected_improvement: number
  confidence: number
  reasoning: string
}

export interface AIParameterOptimizationResponse {
  optimization: AIParameterOptimization
  last_update: string
}

export async function getAIParameterOptimization(): Promise<AIParameterOptimizationResponse> {
  return fetchWithAuth(`${API_BASE_URL}/ai/analysis/parameter`)
}

export interface AIRiskAnalysis {
  risk_score: number
  risk_level: string
  warnings: string[]
  recommendations: string[]
  reasoning: string
}

export interface AIRiskAnalysisResponse {
  analysis: AIRiskAnalysis
  last_update: string
}

export async function getAIRiskAnalysis(): Promise<AIRiskAnalysisResponse> {
  return fetchWithAuth(`${API_BASE_URL}/ai/analysis/risk`)
}

export interface AISentimentAnalysis {
  sentiment_score: number
  trend: string
  key_factors: string[]
  news_summary: string
  reasoning: string
}

export interface AISentimentAnalysisResponse {
  analysis: AISentimentAnalysis
  last_update: string
}

export async function getAISentimentAnalysis(): Promise<AISentimentAnalysisResponse> {
  return fetchWithAuth(`${API_BASE_URL}/ai/analysis/sentiment`)
}

export interface AIPolymarketSignal {
  signal: string
  strength: number
  confidence: number
  reasoning: string
  signals?: Array<{
    question: string
    signal: string
    probability: number
    strength: number
    confidence: number
    reasoning: string
    relevance: string
  }>
}

export interface AIPolymarketSignalResponse {
  analysis: AIPolymarketSignal
  last_update: string
}

export async function getAIPolymarketSignal(): Promise<AIPolymarketSignalResponse> {
  return fetchWithAuth(`${API_BASE_URL}/ai/analysis/polymarket`)
}

export async function triggerAIAnalysis(module: string): Promise<{ message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/ai/analysis/trigger/${module}`, {
    method: 'POST',
  })
}

export interface AIPromptTemplate {
  module: string
  template: string
  system_prompt: string
}

export interface AIPromptsResponse {
  prompts: Record<string, AIPromptTemplate>
}

export async function getAIPrompts(): Promise<AIPromptsResponse> {
  return fetchWithAuth(`${API_BASE_URL}/ai/prompts`)
}

export async function updateAIPrompt(module: string, template: string, systemPrompt?: string): Promise<{ message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/ai/prompts`, {
    method: 'POST',
    body: JSON.stringify({
      module,
      template,
      system_prompt: systemPrompt || '',
    }),
  })
}

// Market Intelligence
export interface RSSItemInfo {
  title: string
  description: string
  link: string
  pub_date: string
  source: string
}

export interface RSSFeedInfo {
  title: string
  description: string
  url: string
  items: RSSItemInfo[]
  last_update: string
}

export interface FearGreedIndexInfo {
  value: number
  classification: string
  timestamp: string
}

export interface RedditPostInfo {
  title: string
  content: string
  url: string
  subreddit: string
  score: number
  upvote_ratio: number
  created_at: string
  author: string
}

export interface PolymarketMarketInfo {
  id: string
  question: string
  description: string
  end_date: string
  outcomes: string[]
  volume: number
  liquidity: number
}

export interface MarketIntelligenceResponse {
  rss_feeds: RSSFeedInfo[]
  fear_greed: FearGreedIndexInfo | null
  reddit_posts: RedditPostInfo[]
  polymarket: PolymarketMarketInfo[]
}

export interface MarketIntelligenceParams {
  source?: 'rss' | 'fear_greed' | 'reddit' | 'polymarket'
  keyword?: string
  limit?: number
}

export async function getMarketIntelligence(params?: MarketIntelligenceParams): Promise<MarketIntelligenceResponse> {
  const queryParams = new URLSearchParams()
  if (params?.source) queryParams.append('source', params.source)
  if (params?.keyword) queryParams.append('keyword', params.keyword)
  if (params?.limit) queryParams.append('limit', params.limit.toString())
  
  const url = `${API_BASE_URL}/market-intelligence${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

// ==================== 价差监控 ====================

export interface BasisData {
  symbol: string
  exchange: string
  spot_price: number
  futures_price: number
  basis: number
  basis_percent: number
  funding_rate: number
  timestamp: string
}

export interface BasisStats {
  symbol: string
  exchange: string
  avg_basis: number
  max_basis: number
  min_basis: number
  std_dev: number
  data_points: number
  hours: number
}

export async function getBasisCurrent(symbol?: string): Promise<BasisData[]> {
  const queryParams = new URLSearchParams()
  if (symbol) queryParams.append('symbol', symbol)
  
  const url = `${API_BASE_URL}/basis/current${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  const response = await fetchWithAuth(url)
  return response.data
}

export async function getBasisHistory(symbol: string, limit: number = 100): Promise<BasisData[]> {
  const queryParams = new URLSearchParams()
  queryParams.append('symbol', symbol)
  queryParams.append('limit', limit.toString())
  
  const url = `${API_BASE_URL}/basis/history?${queryParams.toString()}`
  const response = await fetchWithAuth(url)
  return response.data
}

export async function getBasisStatistics(symbol: string, hours: number = 24): Promise<BasisStats> {
  const queryParams = new URLSearchParams()
  queryParams.append('symbol', symbol)
  queryParams.append('hours', hours.toString())
  
  const url = `${API_BASE_URL}/basis/statistics?${queryParams.toString()}`
  const response = await fetchWithAuth(url)
  return response.data
}

// AI 配置助手

// 按币种分配的资金配置
export interface SymbolCapitalConfig {
  symbol: string
  capital: number
}

// 并行策略实例 (从 config.ts 复制或引用)
export interface StrategyInstance {
  type: string
  weight: number
  config: Record<string, any>
}

// 提现策略 - 从 config.ts 导入
export { 
  type WithdrawalPolicy, 
  type TieredWithdrawRule, 
  type PrincipalProtection, 
  type WithdrawSchedule 
} from './config'

export interface AIGenerateConfigRequest {
  exchange: string
  symbols: string[]
  total_capital?: number  // 总金额模式时使用
  symbol_capitals?: SymbolCapitalConfig[]  // 按币种分配模式时使用
  capital_mode: 'total' | 'per_symbol'  // 资金配置模式
  risk_profile: 'conservative' | 'balanced' | 'aggressive'
  gemini_api_key?: string  // 可选的 Gemini API Key，如果提供则临时使用
  access_mode?: 'native' | 'proxy'  // 可选的访问模式
  proxy_base_url?: string  // 可选的代理服务地址
  proxy_username?: string  // 可选的 Basic Auth 用户名
  proxy_password?: string  // 可选的 Basic Auth 密码
  
  // 资产优先重构新增字段
  symbol_allocations?: Record<string, number> // 币种比例分配 symbol -> weight (0-1)
  strategy_splits?: Record<string, StrategyInstance[]> // 每个币种的策略分配
  withdrawal_policy?: WithdrawalPolicy // 提现策略
}

export interface AIGridConfig {
  exchange: string
  symbol: string
  price_interval: number
  order_quantity: number
  buy_window_size: number
  sell_window_size: number
  grid_risk_control?: {
    enabled: boolean
    max_grid_layers: number
    stop_loss_ratio: number
    take_profit_trigger_ratio: number
    trailing_take_profit_ratio: number
    trend_filter_enabled: boolean
  }
}

export interface AIAllocationConfig {
  exchange: string
  symbol: string
  max_amount_usdt: number
  max_percentage: number
}

// 对应后端 SymbolConfig
export interface AISymbolConfig {
  exchange: string
  symbol: string
  total_allocated_capital: number
  strategies: StrategyInstance[]
  withdrawal_policy: WithdrawalPolicy
  price_interval: number
  order_quantity: number
  buy_window_size: number
  sell_window_size: number
  grid_risk_control?: any
}

export interface AIGenerateConfigResponse {
  explanation: string
  grid_config: AIGridConfig[]
  allocation: AIAllocationConfig[]
  symbols_config?: AISymbolConfig[] // 新增：分级资产配置结果
}

export async function generateAIConfig(request: AIGenerateConfigRequest): Promise<AIGenerateConfigResponse> {
  return fetchWithAuth(`${API_BASE_URL}/ai/generate-config`, {
    method: 'POST',
    body: JSON.stringify(request),
  })
}

export async function applyAIConfig(config: AIGenerateConfigResponse): Promise<{ message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/ai/apply-config`, {
    method: 'POST',
    body: JSON.stringify(config),
  })
}

// ==================== 事件中心 ====================

export interface EventRecord {
  id: number
  type: string
  severity: 'critical' | 'warning' | 'info'
  source: 'exchange' | 'network' | 'system' | 'strategy' | 'risk' | 'api'
  exchange?: string
  symbol?: string
  title: string
  message: string
  details: string
  created_at: string
}

export interface EventStats {
  total_count: number
  critical_count: number
  warning_count: number
  info_count: number
  count_by_type: Record<string, number>
  count_by_source: Record<string, number>
  last_24_hours_count: number
}

export interface EventFilter {
  type?: string
  severity?: string
  source?: string
  exchange?: string
  symbol?: string
  start_time?: string
  end_time?: string
  limit?: number
  offset?: number
}

export interface EventsResponse {
  events: EventRecord[]
  count: number
}

export async function getEvents(filter?: EventFilter): Promise<EventsResponse> {
  const queryParams = new URLSearchParams()
  if (filter?.type) queryParams.append('type', filter.type)
  if (filter?.severity) queryParams.append('severity', filter.severity)
  if (filter?.source) queryParams.append('source', filter.source)
  if (filter?.exchange) queryParams.append('exchange', filter.exchange)
  if (filter?.symbol) queryParams.append('symbol', filter.symbol)
  if (filter?.start_time) queryParams.append('start_time', filter.start_time)
  if (filter?.end_time) queryParams.append('end_time', filter.end_time)
  if (filter?.limit) queryParams.append('limit', filter.limit.toString())
  if (filter?.offset) queryParams.append('offset', filter.offset.toString())
  
  const url = `${API_BASE_URL}/events${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

export async function getEventDetail(id: number): Promise<EventRecord> {
  return fetchWithAuth(`${API_BASE_URL}/events/${id}`)
}

export async function getEventStats(): Promise<EventStats> {
  return fetchWithAuth(`${API_BASE_URL}/events/stats`)
}
