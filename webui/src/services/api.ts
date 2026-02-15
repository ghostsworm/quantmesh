// 使用页面同源，避免相對路径被代理/扩展劫持
const API_BASE_URL = `${window.location.origin}/api`

// Helper function to make authenticated requests
export async function fetchWithAuth(url: string, options: RequestInit = {}) {
  // 獲取當前语言設置
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
    if (response.status === 401) {
      window.location.replace('/login')
    }
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
  market_type?: string       // 市場類型：spot/futures，用於區分現貨與合約
  current_price: number
  total_pnl: number
  total_trades: number
  risk_triggered: boolean
  uptime: number
  opening_paused?: boolean   // 是否暂停开仓
  pause_reason?: string      // 暂停原因：manual / schedule / periodic / position_limit
}

export async function getSystemStatus(exchange?: string, symbol?: string, marketType?: string): Promise<SystemStatus> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  if (marketType) queryParams.append('market_type', marketType)
  const url = `${API_BASE_URL}/status${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

export interface SystemStatusesResponse {
  statuses: SystemStatus[]
}

// 批量獲取所有交易對状態（用於概览页一次拉取）
export async function getSystemStatuses(): Promise<SystemStatusesResponse> {
  return fetchWithAuth(`${API_BASE_URL}/statuses`)
}

// 後台服務狀態（存儲、回測等）
export interface ServiceStatusItem {
  id: string
  name?: string
  ok: boolean
  message?: string
  message_key?: string
  message_params?: Record<string, string>
}

export interface ServicesStatusResponse {
  services: ServiceStatusItem[]
}

export async function getServicesStatus(): Promise<ServicesStatusResponse> {
  return fetchWithAuth(`${API_BASE_URL}/services/status`)
}

// Alias for backward compatibility
export const getStatus = getSystemStatus

// Symbols and Exchanges
export interface SymbolInfo {
  exchange: string
  symbol: string
  is_active: boolean
  current_price: number
  market_type?: 'spot' | 'futures' // 市場類型
  direction?: 'LONG' | 'SHORT' // 交易方向，預設 LONG
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

// Bots
export interface BotInfo {
  bot_id: string
  name: string
  exchange: string
  symbol: string
  market_type: string
  running: boolean
  current_price?: number
  total_pnl?: number
  total_trades?: number
  risk_triggered?: boolean
  uptime?: number
}

export interface BotsResponse {
  bots: BotInfo[]
}

export async function getBots(): Promise<BotsResponse> {
  return fetchWithAuth(`${API_BASE_URL}/bots`)
}

export interface BotDetailInfo extends BotInfo {
  config?: Record<string, unknown>
}

export async function getBotById(botId: string): Promise<BotDetailInfo> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}`)
}

export async function startBot(botId: string): Promise<{ ok: boolean; bot_id: string }> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/start`, {
    method: 'POST',
  })
}

export async function stopBot(botId: string): Promise<{ ok: boolean; bot_id: string }> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/stop`, {
    method: 'POST',
  })
}

// Positions
// 舊的 PositionInfo 介面（用於其他API，保留以兼容）
export interface ExchangePositionInfo {
  symbol: string
  size: number
  entry_price: number
  mark_price: number
  unrealized_pnl: number
  leverage: number
}

// 新的 PositionInfo 介面（用於持倉页面）
export interface PositionInfo {
  price: number
  quantity: number
  value: number
  unrealized_pnl: number
}

// 持倉彙總介面（用於持倉页面）
export interface PositionSummary {
  total_quantity: number
  total_value: number
  position_count: number
  average_price: number
  current_price: number
  unrealized_pnl: number
  pnl_percentage: number
  actual_margin: number  // 實際资金占用（實際保证金）
  leverage: number       // 杠杆倍數
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

// positions/summary 返回的是“扁平”結構（非 {summary: ...} 包装）
export interface PositionsSummary {
  exchange?: string
  symbol?: string
  strategy?: string
  total_quantity: number
  total_value: number
  position_count: number
  average_price: number
  current_price: number
  unrealized_pnl: number
  pnl_percentage: number
  actual_margin?: number
  leverage?: number
  // 槽位计算數據
  slot_data?: {
    quantity: number
    average_price: number
    unrealized_pnl: number
    ws_price: number
  }
  // 交易所數據
  exchange_data?: {
    has_data: boolean
    quantity: number
    entry_price: number
    mark_price: number
    unrealized_pnl: number
    leverage: number
  }
  // 差异分析（结构化原因供前端 i18n 格式化；旧 API 可能返回 string[]）
  discrepancy?: {
    pnl_diff: number
    reasons: Array<
      | {
          type: 'quantity_diff' | 'entry_price_diff' | 'price_diff' | 'pnl_diff_other'
          exchange?: number
          slot?: number
          slot_avg?: number
          diff?: number
          diff_pct?: number
          mark_price?: number
          ws_price?: number
          pnl_diff?: number
        }
      | string
    >
  }
}

export async function getPositionsSummary(exchange?: string, symbol?: string): Promise<PositionsSummary> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  const url = `${API_BASE_URL}/positions/summary${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

// 按交易所、币种、策略列出的所有持倉彙總
export interface PositionSummaryItem {
  exchange: string
  symbol: string
  strategy: string
  total_quantity: number
  total_value: number
  position_count: number
  average_price: number
  current_price: number
  unrealized_pnl: number
  pnl_percentage: number
  actual_margin: number
  leverage: number
  exchange_data?: {
    has_data: boolean
    quantity: number
    entry_price: number
    mark_price: number
    unrealized_pnl: number
    leverage: number
  }
}

export interface PositionsSummaryAllResponse {
  positions: PositionSummaryItem[]
}

export async function getPositionsSummaryAll(): Promise<PositionsSummaryAllResponse> {
  return fetchWithAuth(`${API_BASE_URL}/positions/summary/all`)
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
  total_count?: number  // 数据库中的真实订单总数
  today_count?: number  // 今日订单数
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
  strategy_name: string  // 策略名称
  strategy_type: string  // 策略類型
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

// 🔥 Trade Details - 查詢賣單的成交明細（部分成交記錄）
export interface TradeFill {
  id: number
  buy_price: number
  sell_price: number
  quantity: number
  pnl: number
  fee: number
  fee_asset: string
  buy_price_deviation: number
  sell_price_deviation: number
  created_at: string
}

export interface TradeDetailResponse {
  order: {
    order_id: number
    client_order_id: string
    symbol: string
    side: string
    price: number
    quantity: number
    filled_qty: number
    status: string
    exchange: string
    type: string
    created_at: string
    updated_at: string
  } | null
  fills: TradeFill[]
  fill_count: number
  summary: {
    total_quantity: number
    total_pnl: number
    total_fee: number
    net_pnl: number
  }
}

export async function getTradeDetails(orderID: number): Promise<TradeDetailResponse> {
  return fetchWithAuth(`${API_BASE_URL}/trades/by-order/${orderID}`)
}

// Sync Orders (Binance only)
export interface SyncOrdersResponse {
  success: boolean
  message: string
}

export async function syncOrders(exchange: string, symbol: string): Promise<SyncOrdersResponse> {
  const queryParams = new URLSearchParams()
  queryParams.append('exchange', exchange)
  queryParams.append('symbol', symbol)
  const url = `${API_BASE_URL}/orders/sync?${queryParams.toString()}`
  return fetchWithAuth(url, {
    method: 'POST',
  })
}

// Cancel Order
export interface CancelOrderResponse {
  success: boolean
  message: string
  order_id?: number
}

export async function cancelOrder(orderId: number, exchange: string, symbol: string): Promise<CancelOrderResponse> {
  return fetchWithAuth(`${API_BASE_URL}/orders/${orderId}/cancel?exchange=${exchange}&symbol=${symbol}`, {
    method: 'POST',
  })
}

export interface BatchCancelOrdersResponse {
  success: boolean
  message: string
  count?: number
}

export async function batchCancelOrders(orderIds: number[], exchange: string, symbol: string): Promise<BatchCancelOrdersResponse> {
  return fetchWithAuth(`${API_BASE_URL}/orders/cancel`, {
    method: 'POST',
    body: JSON.stringify({ order_ids: orderIds, exchange, symbol }),
  })
}

// Statistics
export interface StatisticsSummary {
  total_trades: number
  total_volume: number
  total_pnl: number // 淨利潤（已扣手續費）
  gross_pnl?: number // 毛利（未扣手續費）
  total_fee?: number // 手續費合計
  win_rate: number
  average_pnl: number
  max_profit: number
  max_loss: number
  total_buy_deviation?: number // 🔥 買入價格偏差總和（USDT）
  total_sell_deviation?: number // 🔥 賣出價格偏差總和（USDT）
}

// /statistics 直接返回彙總字段（非 {summary: ...} 包装）
export async function getStatistics(exchange?: string, symbol?: string): Promise<StatisticsSummary> {
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
  open_price?: number      // 當日开盘價
  close_price?: number     // 當日收盘價
  price_change?: number    // 價格變化（收盘價-开盘價）
  price_change_pct?: number // 價格變化百分比
  cumulative_pnl?: number  // 累计盈亏
  unrealized_pnl?: number  // 當日收盤未實現盈虧（來自每日快照）
  book_value_pnl?: number  // 賬面盈虧 = 已平倉 + 未實現
  intraday_max_drawdown?: number    // 日內最大回撤金額
  intraday_max_drawdown_pct?: number // 日內最大回撤百分比
}

export interface DailyStatisticsResponse {
  statistics: DailyStatistics[]
  max_drawdown?: number     // 最大回撤金額
  max_drawdown_pct?: number // 最大回撤百分比
}

export async function getDailyStatistics(exchange?: string, symbol?: string, days?: number): Promise<DailyStatisticsResponse> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  // 默认查詢365天（1年）的历史數據，确保显示所有交易記錄
  if (days !== undefined) {
    queryParams.append('days', days.toString())
  } else {
    queryParams.append('days', '365')
  }
  const url = `${API_BASE_URL}/statistics/daily${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

/** 日盈虧拆解：摘要 */
export interface DailyPnLBreakdownSummary {
  total_buy_orders: number
  total_buy_qty: number
  total_buy_value: number
  total_sell_orders: number
  total_sell_qty: number
  total_sell_value: number
  net_cash_flow: number
  net_qty_change: number
  start_position_qty: number
  end_position_qty: number
  start_position_value: number
  end_position_value: number
  position_value_change: number
  net_trading_pnl: number
  grid_profit: number
  grid_trades: number
  total_fee: number
  funding_fee: number
  exchange_pnl: number
  unrealized_pnl_start: number
  unrealized_pnl_end: number
  open_price: number
  close_price: number
}

/** 日盈虧拆解：小時權益點 */
export interface HourlyEquityPoint {
  timestamp: number
  equity: number
}

/** 日盈虧拆解：單筆成交（網格計算，盈利/虧損 Top） */
export interface TopTradeItem {
  sell_order_id: number
  buy_price: number
  sell_price: number
  quantity: number
  pnl: number
  fee: number
}

/** 日盈虧拆解：交易所已實現盈虧單筆（交易所計算 Top） */
export interface ExchangeOrderItem {
  order_id: number
  side: string
  price: number
  filled_qty: number
  realized_pnl: number
}

/** 日盈虧拆解 API 響應 */
export interface DailyPnLBreakdownResponse {
  date: string
  summary: DailyPnLBreakdownSummary
  hourly_equity: HourlyEquityPoint[]
  grid_profit_trades: TopTradeItem[]
  grid_loss_trades: TopTradeItem[]
  exchange_profit_orders: ExchangeOrderItem[]
  exchange_loss_orders: ExchangeOrderItem[]
}

export async function getDailyPnLBreakdown(
  date: string,
  exchange?: string,
  symbol?: string
): Promise<DailyPnLBreakdownResponse> {
  const queryParams = new URLSearchParams({ date })
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  return fetchWithAuth(`${API_BASE_URL}/statistics/daily/breakdown?${queryParams.toString()}`)
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
  unrealized_pnl?: number // 時段最後一天收盤未實現盈虧（來自每日快照）
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
  keyword?: string
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
  if (params?.keyword) queryParams.append('keyword', params.keyword)
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

  // 心跳计時器
  let heartbeatInterval: NodeJS.Timeout | null = null

  const handleOpen = () => {
    console.log('WebSocket 连接已建立')
    // 啟动心跳：每 2 秒发送一次 ping（服務端 3 秒超時）
    heartbeatInterval = setInterval(() => {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: 'ping' }))
      }
    }, 2000)
  }

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
      } else if (payload?.type === 'pong') {
        // 收到服務端的 pong 响应
        console.debug('收到心跳响应')
      }
    } catch (err) {
      console.error('解析日志消息失败:', err)
    }
  }

  const handleError = (event: Event) => {
    console.error('WebSocket 錯误:', event)
    if (onError) {
      onError(event)
    }
  }

  const handleClose = (event: CloseEvent) => {
    console.log('WebSocket 连接已关闭:', event.code, event.reason)
    if (heartbeatInterval) {
      clearInterval(heartbeatInterval)
      heartbeatInterval = null
    }
    if (onError && !event.wasClean) {
      onError(event)
    }
  }

  socket.addEventListener('open', handleOpen)
  socket.addEventListener('message', handleMessage)
  socket.addEventListener('error', handleError)
  socket.addEventListener('close', handleClose)

  return () => {
    if (heartbeatInterval) {
      clearInterval(heartbeatInterval)
      heartbeatInterval = null
    }
    socket.removeEventListener('open', handleOpen)
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

// 獲取日志统计信息
export interface LogStats {
  total: number
  by_level: Record<string, number>
  oldest_time?: string
  newest_time?: string
}

export async function getLogStats(): Promise<LogStats> {
  return fetchWithAuth(`${API_BASE_URL}/logs/stats`)
}

// 优化日志數據库
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
  allocated: number   // 分配的资金
  used: number        // 已使用的资金（保证金）
  available: number   // 可用资金
  weight: number      // 权重
  fixed_pool: number  // 固定资金池（如果指定）
}

export interface StrategyAllocationResponse {
  allocation: Record<string, StrategyCapitalInfo>
}

export async function getStrategyAllocation(): Promise<StrategyAllocationResponse> {
  return fetchWithAuth(`${API_BASE_URL}/strategies/allocation`)
}

// Release Locked Capital
export interface ReleaseCapitalResponse {
  success: boolean
  message: string
  released: number
  strategy?: string
}

export interface ReleaseAllCapitalResponse {
  success: boolean
  message: string
  released: Record<string, number>
  total_released: number
}

export async function releaseStrategyCapital(strategyName: string): Promise<ReleaseCapitalResponse> {
  return fetchWithAuth(`${API_BASE_URL}/strategies/${encodeURIComponent(strategyName)}/release-capital`, {
    method: 'POST',
  })
}

export async function releaseAllStrategiesCapital(): Promise<ReleaseAllCapitalResponse> {
  return fetchWithAuth(`${API_BASE_URL}/strategies/release-all-capital`, {
    method: 'POST',
  })
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
  exchange?: string
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
  actual_profit?: number
  created_at: string | Date
}

export interface ReconciliationHistoryParams {
  exchange?: string
  symbol?: string
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
  if (params?.exchange) queryParams.append('exchange', params.exchange)
  if (params?.symbol) queryParams.append('symbol', params.symbol)
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

// ==================== News Analysis ====================

export interface NewsPriceScenario {
  direction: string
  change_percent: number
  probability: number
}

export interface NewsPricePrediction {
  timeframe: string
  scenarios: NewsPriceScenario[]
  target_price_range: { min: number; max: number }
}

export interface NewsRiskAssessment {
  overall_risk_score: number
  crash_probability: number
  recommendation: 'normal' | 'caution' | 'reduce_position' | 'stop_trading'
  price_predictions: NewsPricePrediction[]
  current_price_analysis?: { current_price: number; price_trend: string; support_level: number; resistance_level: number }
  risk_factors?: string[]
  analysis_summary?: string
  last_updated: string
}

export interface NewsAnalysisResponse {
  assessment: NewsRiskAssessment | null
  is_analyzing: boolean
  last_updated: string | null
}

export interface NewsItem {
  title: string
  content: string
  source: string
  url: string
  published_at: string
}

export async function getNewsAnalysis(assetType?: string): Promise<NewsAnalysisResponse> {
  const url = assetType ? `${API_BASE_URL}/news/analysis?asset_type=${encodeURIComponent(assetType)}` : `${API_BASE_URL}/news/analysis`
  return fetchWithAuth(url)
}

export async function getNewsPredictions(): Promise<{ price_predictions: NewsPricePrediction[]; recommendation: string; last_updated: string }> {
  return fetchWithAuth(`${API_BASE_URL}/news/predictions`)
}

export async function triggerNewsAnalyze(symbol?: string, focusEvent?: string, assetType?: string): Promise<{ success: boolean; message: string }> {
  const body: Record<string, string> = {
    symbol: symbol || 'BTCUSDT',
    focus_event: focusEvent || '',
  }
  if (assetType) body.asset_type = assetType
  return fetchWithAuth(`${API_BASE_URL}/news/analyze`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export async function getNewsCollected(): Promise<{ news: NewsItem[] }> {
  return fetchWithAuth(`${API_BASE_URL}/news/collected`)
}

export async function getNewsKeywords(): Promise<{ keywords: string[] }> {
  return fetchWithAuth(`${API_BASE_URL}/news/keywords`)
}

export async function updateNewsKeywords(keywords: string[]): Promise<{ success: boolean; keywords: string[] }> {
  return fetchWithAuth(`${API_BASE_URL}/news/keywords`, {
    method: 'PUT',
    body: JSON.stringify({ keywords }),
  })
}

export interface NewsHistoryItem {
  id: number
  analysis_time: string
  symbol: string
  current_price: number
  recommendation: string
  overall_risk_score?: number
  crash_probability?: number
}

export interface NewsHistoryResponse {
  total: number
  items: NewsHistoryItem[]
}

export async function getNewsHistory(params?: { symbol?: string; start_time?: string; end_time?: string; limit?: number; offset?: number }): Promise<NewsHistoryResponse> {
  const q = new URLSearchParams()
  if (params?.symbol) q.append('symbol', params.symbol)
  if (params?.start_time) q.append('start_time', params.start_time)
  if (params?.end_time) q.append('end_time', params.end_time)
  if (params?.limit) q.append('limit', String(params.limit))
  if (params?.offset) q.append('offset', String(params.offset))
  const url = `${API_BASE_URL}/news/history${q.toString() ? '?' + q.toString() : ''}`
  return fetchWithAuth(url)
}

export async function getNewsHistoryById(id: number): Promise<Record<string, unknown>> {
  return fetchWithAuth(`${API_BASE_URL}/news/history/${id}`)
}

export interface PredictionAccuracyResponse {
  total: number
  correct: number
  accuracy: number
  timeframe_breakdown?: Record<string, {
    total: number
    correct: number
    accuracy: number
    directions?: Record<string, {
      total: number
      correct: number
      accuracy: number
    }>
  }>
  asset_type?: string
}

export async function getPredictionsAccuracy(assetType?: string, sinceDays?: number): Promise<PredictionAccuracyResponse> {
  const q = new URLSearchParams()
  if (assetType) q.append('asset_type', assetType)
  if (sinceDays) q.append('since_days', String(sinceDays))
  const url = `${API_BASE_URL}/predictions/accuracy${q.toString() ? '?' + q.toString() : ''}`
  return fetchWithAuth(url)
}

export interface PredictionHistoryItem {
  id: number
  asset_type: string
  symbol: string
  prediction_time: string
  timeframe: string
  predicted_direction: string
  actual_direction: string
  is_correct: boolean
  status: string
  verified_at?: string
}

export interface PredictionHistoryResponse {
  total: number
  items: PredictionHistoryItem[]
}

export async function getPredictionsHistory(params?: { asset_type?: string; symbol?: string; start_time?: string; end_time?: string; limit?: number; offset?: number }): Promise<PredictionHistoryResponse> {
  const q = new URLSearchParams()
  if (params?.asset_type) q.append('asset_type', params.asset_type)
  if (params?.symbol) q.append('symbol', params.symbol)
  if (params?.start_time) q.append('start_time', params.start_time)
  if (params?.end_time) q.append('end_time', params.end_time)
  if (params?.limit) q.append('limit', String(params.limit))
  if (params?.offset) q.append('offset', String(params.offset))
  const url = `${API_BASE_URL}/predictions/history${q.toString() ? '?' + q.toString() : ''}`
  return fetchWithAuth(url)
}

// ==================== Newbie Risk Check ====================

export interface NewbieRiskCheckItem {
  item: string
  score: number
  level: 'safe' | 'warning' | 'danger'
  message: string
  advice: string
}

export interface NewbieRiskReport {
  overallScore: number
  results: NewbieRiskCheckItem[]
}

export async function getNewbieRiskCheck(): Promise<NewbieRiskReport> {
  return fetchWithAuth(`${API_BASE_URL}/risk/newbie-check`)
}

export async function applyNewbieSecurityConfig(): Promise<{ success: boolean; message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/risk/newbie-check/apply`, {
    method: 'POST',
  })
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

// ==================== 参数建议 (Param Advisor) ====================

export interface RangeAdvice {
  min: number
  recommended: number
  max: number
  reason?: string
}

export interface ParamSuggestion {
  price_interval: RangeAdvice
  order_quantity: RangeAdvice
  min_profitable_interval: number
  breakeven_fee_rate: number
}

export interface ParamAdvisorResponse {
  current_price: number
  maker_fee: number
  taker_fee: number
  fee_source: string // "exchange_api" | "config" | "default" | "user_input"
  exchange: string
  symbol: string
  suggestions: ParamSuggestion
}

export interface ExchangeFeesResponse {
  maker_fee: number
  taker_fee: number
  fee_source: string
  exchange: string
  symbol: string
}

export async function getParamAdvisor(
  exchange: string,
  symbol: string,
  makerFee?: number,
  takerFee?: number
): Promise<ParamAdvisorResponse> {
  const queryParams = new URLSearchParams({ exchange, symbol })
  if (makerFee !== undefined) queryParams.append('maker_fee', makerFee.toString())
  if (takerFee !== undefined) queryParams.append('taker_fee', takerFee.toString())
  return fetchWithAuth(`${API_BASE_URL}/config/param-advisor?${queryParams.toString()}`)
}

export async function getExchangeFees(
  exchange: string,
  symbol: string
): Promise<ExchangeFeesResponse> {
  const queryParams = new URLSearchParams({ exchange, symbol })
  return fetchWithAuth(`${API_BASE_URL}/config/exchange-fees?${queryParams.toString()}`)
}

// Price Range (运行时价格范围计算)
export interface PriceRangeData {
  price_interval: number
  order_quantity: number
  buy_window_size: number
  sell_window_size: number
  anchor_price: number
  current_price: number
  direction: string
  grid_price?: number
  buy_price_low?: number
  buy_price_high?: number
  sell_price_low?: number
  sell_price_high?: number
  message?: string
}

export interface PriceRangeResponse {
  success: boolean
  source: string // "runtime" | "config"
  data: PriceRangeData
}

export async function getPriceRange(
  exchange: string,
  symbol: string
): Promise<PriceRangeResponse> {
  const queryParams = new URLSearchParams({ exchange, symbol })
  return fetchWithAuth(`${API_BASE_URL}/config/price-range?${queryParams.toString()}`)
}

// Trading Control
export async function startTrading(exchange?: string, symbol?: string, marketType?: string): Promise<{ message: string }> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  if (marketType) queryParams.append('market_type', marketType)
  const url = `${API_BASE_URL}/trading/start${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url, {
    method: 'POST',
  })
}

export async function stopTrading(exchange?: string, symbol?: string, marketType?: string): Promise<{ message: string }> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  if (marketType) queryParams.append('market_type', marketType)
  const url = `${API_BASE_URL}/trading/stop${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url, {
    method: 'POST',
  })
}

// Opening Control (開倉管理)
export interface ScheduleRule {
  enabled: boolean
  action: 'pause' | 'resume'
  time: string // "HH:MM" UTC
  weekdays?: number[] // 0=周日..6=周六
}

export interface PeriodicRule {
  enabled: boolean
  open_duration_min: number
  close_duration_min: number
}

export interface OpenPositionControlConfig {
  pause_opening?: boolean
  max_position_value: number
  max_position_layers: number
  schedule_rules?: ScheduleRule[]
  periodic_rule?: PeriodicRule | null
}

export interface OpeningControlStatus {
  exchange: string
  symbol: string
  opening_paused: boolean
  pause_reason: string
  current_position_value_usdt: number
  current_layers: number
  config: {
    max_position_value: number
    max_position_layers: number
    schedule_rules: ScheduleRule[]
    periodic_rule: PeriodicRule | null
  }
}

function openingControlQueryParams(exchange: string, symbol: string, marketType?: string | null): URLSearchParams {
  const queryParams = new URLSearchParams()
  queryParams.append('exchange', exchange)
  queryParams.append('symbol', symbol)
  if (marketType === 'spot' || marketType === 'futures') {
    queryParams.append('market_type', marketType)
  }
  return queryParams
}

export async function getOpeningControlStatus(exchange: string, symbol: string, marketType?: string | null): Promise<OpeningControlStatus> {
  return fetchWithAuth(`${API_BASE_URL}/opening-control/status?${openingControlQueryParams(exchange, symbol, marketType).toString()}`)
}

export async function pauseOpening(exchange: string, symbol: string, marketType?: string | null): Promise<{ message: string; opening_paused: boolean }> {
  return fetchWithAuth(`${API_BASE_URL}/opening-control/pause?${openingControlQueryParams(exchange, symbol, marketType).toString()}`, {
    method: 'POST',
  })
}

export async function resumeOpening(exchange: string, symbol: string, marketType?: string | null): Promise<{ message: string; opening_paused: boolean }> {
  return fetchWithAuth(`${API_BASE_URL}/opening-control/resume?${openingControlQueryParams(exchange, symbol, marketType).toString()}`, {
    method: 'POST',
  })
}

export async function getOpeningControlConfig(exchange: string, symbol: string, marketType?: string | null): Promise<OpenPositionControlConfig> {
  return fetchWithAuth(`${API_BASE_URL}/opening-control/config?${openingControlQueryParams(exchange, symbol, marketType).toString()}`)
}

export async function updateOpeningControlConfig(
  exchange: string,
  symbol: string,
  config: Partial<OpenPositionControlConfig>,
  marketType?: string | null
): Promise<{ message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/opening-control/config?${openingControlQueryParams(exchange, symbol, marketType).toString()}`, {
    method: 'PUT',
    body: JSON.stringify(config),
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

// 網格上移/下移
export async function gridShiftUp(exchange: string, symbol: string, step?: number, marketType?: string | null): Promise<{ message: string }> {
  const queryParams = new URLSearchParams({ exchange, symbol })
  if (step != null && step > 0) queryParams.append('step', String(step))
  if (marketType === 'spot' || marketType === 'futures') queryParams.append('market_type', marketType)
  return fetchWithAuth(`${API_BASE_URL}/grid/shift-up?${queryParams.toString()}`, { method: 'POST' })
}
export async function gridShiftDown(exchange: string, symbol: string, step?: number, marketType?: string | null): Promise<{ message: string }> {
  const queryParams = new URLSearchParams({ exchange, symbol })
  if (step != null && step > 0) queryParams.append('step', String(step))
  if (marketType === 'spot' || marketType === 'futures') queryParams.append('market_type', marketType)
  return fetchWithAuth(`${API_BASE_URL}/grid/shift-down?${queryParams.toString()}`, { method: 'POST' })
}

// K線數據
export interface KlineData {
  time: number    // 時间戳（秒）
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

// ==================== 價差監控 ====================

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

// 並行策略實例 (從 config.ts 複制或引用)
export interface StrategyInstance {
  type: string
  weight: number
  config: Record<string, any>
}

// 提現策略 - 從 config.ts 導入
export { 
  type WithdrawalPolicy, 
  type TieredWithdrawRule, 
  type PrincipalProtection, 
  type WithdrawSchedule 
} from './config'

export interface AIGenerateConfigRequest {
  exchange: string
  symbols: string[]
  total_capital?: number  // 總金額模式時使用
  symbol_capitals?: SymbolCapitalConfig[]  // 按币种分配模式時使用
  capital_mode: 'total' | 'per_symbol'  // 资金配置模式
  risk_profile: 'conservative' | 'balanced' | 'aggressive'
  gemini_api_key?: string  // 可選的 Gemini API Key，如果提供则临時使用
  
  // 资產优先重構新增字段
  symbol_allocations?: Record<string, number> // 币种比例分配 symbol -> weight (0-1)
  strategy_splits?: Record<string, StrategyInstance[]> // 每個币种的策略分配
  withdrawal_policy?: WithdrawalPolicy // 提現策略
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
    max_open_orders_at_cap?: number  // 達到最大持倉預警時最多允許的開倉單數；超出則撤單。做多先撤高價買單，做空先撤低價賣單。0=僅不新開倉不撤單
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

// 對应后端 SymbolConfig
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
  symbols_config?: AISymbolConfig[] // 新增：分级资產配置結果
}

export interface AITaskResponse {
  task_id: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  message?: string
}

export interface AITaskStatusResponse {
  task_id: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  progress: number
  created_at: string
  updated_at: string
  result?: AIGenerateConfigResponse
  error?: string
}

// 創建 AI 配置生成任務（异步）
export async function createAIConfigTask(request: AIGenerateConfigRequest): Promise<AITaskResponse> {
  return fetchWithAuth(`${API_BASE_URL}/ai/generate-config`, {
    method: 'POST',
    body: JSON.stringify(request),
  })
}

// 查詢任務状態
export async function getAITaskStatus(taskId: string): Promise<AITaskStatusResponse> {
  return fetchWithAuth(`${API_BASE_URL}/ai/task/${taskId}`)
}

// 輪詢任務直到完成
export async function pollAITaskUntilComplete(
  taskId: string,
  onProgress?: (progress: number, status: string) => void,
  maxAttempts: number = 600, // 最多輪詢 600 次（约 10 分钟，每次 1 秒）
  interval: number = 1000 // 1 秒輪詢一次
): Promise<AIGenerateConfigResponse> {
  let attempts = 0
  
  while (attempts < maxAttempts) {
    try {
      const status = await getAITaskStatus(taskId)
      
      if (onProgress) {
        onProgress(status.progress, status.status)
      }
      
      if (status.status === 'completed' && status.result) {
        console.log(`✅ [AI任務] ${taskId} 已完成，獲取到結果`)
        return status.result
      }
      
      if (status.status === 'failed') {
        console.error(`❌ [AI任務] ${taskId} 失败:`, status.error)
        throw new Error(status.error || '任務執行失败')
      }
      
      // 如果任務还在运行中，記錄日志（每 10 次記錄一次）
      if (attempts % 10 === 0 && status.status === 'running') {
        console.log(`🔄 [AI任務] ${taskId} 运行中，進度: ${status.progress}%, 已輪詢 ${attempts}/${maxAttempts} 次`)
      }
    } catch (err) {
      // 网络錯误時继续重試，但記錄日志
      if (attempts % 10 === 0) {
        console.warn(`⚠️ [AI任務] ${taskId} 輪詢出錯 (${attempts}/${maxAttempts}):`, err)
      }
    }
    
    // 等待后继续輪詢
    await new Promise(resolve => setTimeout(resolve, interval))
    attempts++
  }
  
  console.error(`⏱️ [AI任務] ${taskId} 輪詢超時，已尝試 ${maxAttempts} 次`)
  throw new Error(`任務超時（已輪詢 ${maxAttempts} 次），请稍后重試或检查后端日志`)
}

// 兼容舊接口：同步等待（内部使用輪詢）
export async function generateAIConfig(request: AIGenerateConfigRequest): Promise<AIGenerateConfigResponse> {
  const taskResponse = await createAIConfigTask(request)
  return pollAITaskUntilComplete(taskResponse.task_id)
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

// ==================== 事件中心状態管理 ====================

export interface EventCenterStatus {
  enabled: boolean
}

export async function getEventCenterStatus(): Promise<EventCenterStatus> {
  return fetchWithAuth(`${API_BASE_URL}/events/center/status`)
}

export async function setEventCenterStatus(enabled: boolean): Promise<{ success: boolean; enabled: boolean; message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/events/center/status`, {
    method: 'POST',
    body: JSON.stringify({ enabled }),
  })
}

// ==================== AI 异步任務管理 ====================

export interface AITask {
  id: string
  task_type: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'timeout'
  request_data: string
  result?: string
  error_message?: string
  model?: string
  ai_input?: string
  ai_output?: string
  input_tokens: number
  output_tokens: number
  processing_time_ms: number
  used_api_key?: string
  retry_count: number
  max_retries: number
  timeout_seconds: number
  created_at: string
  started_at?: string
  completed_at?: string
  expires_at?: string
}

export interface AITaskFilter {
  status?: string
  task_type?: string
  start_time?: string
  end_time?: string
  limit?: number
  offset?: number
}

export interface AITasksResponse {
  tasks: AITask[]
  count: number
}

export interface DailyTokenStat {
  date: string
  input_tokens: number
  output_tokens: number
  total_tokens: number
  task_count: number
}

export interface AITaskStats {
  total_tasks: number
  total_input_tokens: number
  total_output_tokens: number
  total_tokens: number
  today_input_tokens: number
  today_output_tokens: number
  today_tokens: number
  daily_stats: DailyTokenStat[]
}

export async function getAITasks(filter?: AITaskFilter): Promise<AITasksResponse> {
  const queryParams = new URLSearchParams()
  if (filter?.status) queryParams.append('status', filter.status)
  if (filter?.task_type) queryParams.append('task_type', filter.task_type)
  if (filter?.start_time) queryParams.append('start_time', filter.start_time)
  if (filter?.end_time) queryParams.append('end_time', filter.end_time)
  if (filter?.limit) queryParams.append('limit', filter.limit.toString())
  if (filter?.offset) queryParams.append('offset', filter.offset.toString())
  
  const url = `${API_BASE_URL}/ai/tasks${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

export async function getAITaskStats(startTime?: string, endTime?: string): Promise<AITaskStats> {
  const queryParams = new URLSearchParams()
  if (startTime) queryParams.append('start_time', startTime)
  if (endTime) queryParams.append('end_time', endTime)
  
  const url = `${API_BASE_URL}/ai/tasks/stats${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

// ==================== AI 市场解读 ====================

export interface MarketInterpretRequest {
  page_type: 'basis' | 'funding'
  symbol: string
  page_data: Record<string, unknown>
}

export interface MarketInterpretTaskResponse {
  task_id: string
  status: string
}

export interface MarketInterpretStatusResponse {
  task_id: string
  page_type?: string
  symbol?: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  progress: number
  created_at: string
  updated_at: string
  result?: string
  error?: string
}

export interface MarketInterpretHistoryItem {
  task_id: string
  page_type: string
  symbol: string
  status: string
  progress: number
  result?: string
  error?: string
  created_at: string
  updated_at: string
}

// 创建市场 AI 解读任务
export async function createMarketInterpretTask(request: MarketInterpretRequest): Promise<MarketInterpretTaskResponse> {
  return fetchWithAuth(`${API_BASE_URL}/ai/market-interpret`, {
    method: 'POST',
    body: JSON.stringify(request),
  })
}

// 查询市场解读任务状态
export async function getMarketInterpretStatus(taskId: string): Promise<MarketInterpretStatusResponse> {
  return fetchWithAuth(`${API_BASE_URL}/ai/market-interpret/${taskId}`)
}

// 获取当前页面类型下最新一条解读（用于返回页面时恢复显示）
export async function getLatestMarketInterpret(pageType: 'basis' | 'funding'): Promise<MarketInterpretStatusResponse | null> {
  const res = await fetchWithAuth(`${API_BASE_URL}/ai/market-interpret/latest?page_type=${pageType}`)
  if (res && (res as MarketInterpretStatusResponse).task_id) {
    return res as MarketInterpretStatusResponse
  }
  return null
}

// 列出指定页面类型的历史解读
export async function listMarketInterpretHistory(
  pageType: 'basis' | 'funding',
  limit: number = 20
): Promise<{ items: MarketInterpretHistoryItem[] }> {
  const res = await fetchWithAuth(`${API_BASE_URL}/ai/market-interpret/history?page_type=${pageType}&limit=${limit}`)
  return res as { items: MarketInterpretHistoryItem[] }
}

// 轮询市场解读任务直到完成
export async function pollMarketInterpretUntilComplete(
  taskId: string,
  onProgress?: (progress: number, status: string) => void,
  maxAttempts: number = 300,
  interval: number = 2000
): Promise<string> {
  let attempts = 0

  while (attempts < maxAttempts) {
    try {
      const status = await getMarketInterpretStatus(taskId)

      if (onProgress) {
        onProgress(status.progress, status.status)
      }

      if (status.status === 'completed' && status.result) {
        return status.result
      }

      if (status.status === 'failed') {
        throw new Error(status.error || 'AI 解读任务失败')
      }
    } catch (err) {
      if (attempts % 10 === 0) {
        console.warn(`[市场解读] ${taskId} 轮询出错 (${attempts}/${maxAttempts}):`, err)
      }
      // 如果是任务失败的明确错误，直接抛出
      if (err instanceof Error && err.message.includes('解读任务失败')) {
        throw err
      }
    }

    await new Promise(resolve => setTimeout(resolve, interval))
    attempts++
  }

  throw new Error(`AI 解读超时（已等待 ${Math.floor(maxAttempts * interval / 1000)} 秒）`)
}
