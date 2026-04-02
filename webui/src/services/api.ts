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
      // 已在登录页时不重定向，避免 401 -> replace('/login') -> 整页重载 -> 再次 401 的循环
      if (window.location.pathname !== '/login') {
        window.location.replace('/login')
      }
    }
    const errorText = await response.text()
    let parsed: { error_key?: string; group_name?: string; bot_id?: string } | null = null
    try {
      parsed = JSON.parse(errorText) as { error_key?: string; group_name?: string; bot_id?: string }
    } catch {
      /* ignore */
    }
    const err = new Error(`HTTP ${response.status}: ${errorText}`) as Error & {
      status?: number
      errorKey?: string
      groupName?: string
      botId?: string
    }
    err.status = response.status
    if (parsed?.error_key) err.errorKey = parsed.error_key
    if (parsed?.group_name) err.groupName = parsed.group_name
    if (parsed?.bot_id) err.botId = parsed.bot_id
    throw err
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
  price_interval?: number
  profit_spread?: number
  order_quantity?: number
  total_allocated_capital?: number
  strategies?: BotStrategyInfo[] // 该 Bot 配置的策略列表
  leverage?: number // 杠杆倍数
  max_capital_ratio?: number // 最大资金占用比例 (0.1-1.0)
  buy_window_size?: number // 买窗大小（用于计算平仓价）
  created_at?: string // 创建时间 ISO 8601
  stopped_at?: string // 停止时间 ISO 8601（仅当已停止时有值）
  hedge_group_name?: string // 所属对冲组名称，空则非对冲
  direction?: string // 网格/策略方向：LONG/SHORT/BOTH
  /** 最近一次异步启动失败原因（服务端内存记录，成功启动后会清空） */
  last_start_error?: string
  last_start_error_at?: string
}

export interface BotStrategyInfo {
  type: string // 策略类型，如 grid, dca, martingale
  weight: number // 策略权重（资金分配比例）
  name: string // 策略显示名称
}

export interface BotsResponse {
  bots: BotInfo[]
}

export async function getBots(): Promise<BotsResponse> {
  return fetchWithAuth(`${API_BASE_URL}/bots`)
}

// 創建 Bot 請求（含策略配置）
export interface CreateBotRequest {
  name?: string
  exchange: string
  symbol: string
  market_type?: 'spot' | 'futures'
  testnet?: boolean
  strategies?: Array<{ type: string; weight: number; config?: Record<string, unknown> }>
  total_allocated_capital?: number
  price_interval?: number
  profit_spread?: number
  order_quantity?: number
  min_order_value?: number
  buy_window_size?: number
  sell_window_size?: number
  reconcile_interval?: number
  order_cleanup_threshold?: number
  cleanup_batch_size?: number
  margin_lock_duration_seconds?: number
  position_safety_check?: number
  direction?: string
  price_low?: number
  price_high?: number
  trigger_price?: number
  grid_mode?: string
  grid_shift_enabled?: boolean
  grid_shift_step?: number
  close_on_stop?: boolean
  // 网格风控配置
  grid_risk_control_enabled?: boolean
  grid_risk_control_stop_loss_ratio?: number
  grid_risk_control_take_profit_trigger_ratio?: number
  grid_risk_control_trend_filter_enabled?: boolean

  // 三级火箭网格
  rocket_tiered_grid?: {
    enabled: boolean
    tiers: Array<{
      filled_threshold: number
      interval: number
      profit_spread: number
    }>
  }
}

export async function createBot(req: CreateBotRequest): Promise<{ ok: boolean; bot_id: string }> {
  return fetchWithAuth(`${API_BASE_URL}/bots/create`, {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export async function deleteBot(botId: string): Promise<{ ok: boolean; bot_id: string }> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}`, {
    method: 'DELETE',
  })
}

// Bot 組
export interface BotGroupResponse {
  id: string
  name: string
  type: string
  bot_ids: string[]
  hedge_config: {
    hedge_ratio: number
    max_drawdown: number
    auto_rebalance: boolean
    rebalance_interval: number
  }
}

export async function getBotGroups(): Promise<{ bot_groups: BotGroupResponse[] }> {
  return fetchWithAuth(`${API_BASE_URL}/bot-groups`)
}

export async function createBotGroup(req: {
  name?: string
  type: 'futures_spot_hedge' | 'long_short_hedge' | 'spot_grid_futures_hedge' | 'spot_grid_short_futures_long_hedge'
  hedge_config?: {
    hedge_ratio?: number
    short_notional_ratio?: number
    hedge_trigger_layers?: number
    rebalance_interval?: number
    max_drawdown?: number
    auto_rebalance?: boolean
  }
  futures_bot: CreateBotRequest
  spot_bot: CreateBotRequest
}): Promise<{ ok: boolean; group_id: string; bot_ids: string[] }> {
  return fetchWithAuth(`${API_BASE_URL}/bot-groups`, {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export async function deleteBotGroup(groupId: string): Promise<{ ok: boolean; group_id: string }> {
  return fetchWithAuth(`${API_BASE_URL}/bot-groups/${encodeURIComponent(groupId)}`, {
    method: 'DELETE',
  })
}

export interface BotDetailInfo extends BotInfo {
  config?: Record<string, unknown>
}

export async function getBotById(botId: string): Promise<BotDetailInfo> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}`)
}

export interface StartBotResponse {
  ok: boolean
  bot_id: string
  status?: 'starting' | 'running'
  message?: string
}

export async function startBot(botId: string): Promise<StartBotResponse> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/start`, {
    method: 'POST',
  })
}

/** 輪詢 Bot 狀態直到運行、出現啟動失敗記錄或超時（異步啟動後確認） */
export async function pollBotUntilRunning(
  botId: string,
  options?: { intervalMs?: number; timeoutMs?: number }
): Promise<{
  running: boolean
  lastStartError?: string
  lastStartErrorAt?: string
}> {
  const intervalMs = options?.intervalMs ?? 2000
  const timeoutMs = options?.timeoutMs ?? 60000
  const start = Date.now()
  while (Date.now() - start < timeoutMs) {
    const bot = await getBotById(botId)
    if (bot.running) return { running: true }
    if (bot.last_start_error) {
      return {
        running: false,
        lastStartError: bot.last_start_error,
        lastStartErrorAt: bot.last_start_error_at,
      }
    }
    await new Promise((r) => setTimeout(r, intervalMs))
  }
  const bot = await getBotById(botId)
  return {
    running: !!bot.running,
    lastStartError: bot.last_start_error,
    lastStartErrorAt: bot.last_start_error_at,
  }
}

export async function stopBot(botId: string): Promise<{ ok: boolean; bot_id: string }> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/stop`, {
    method: 'POST',
  })
}

// 更新 Bot 策略配置
export interface UpdateBotStrategyRequest {
  strategies?: Array<{
    type: string
    weight: number
    config?: Record<string, unknown>
  }>
  price_interval?: number
  profit_spread?: number
  order_quantity?: number
  price_low?: number
  price_high?: number
  direction?: string
  // 智能挂单配置
  smart_order_enabled?: boolean
  smart_order_max_open_orders?: number
  smart_order_open_order_distance?: number

  // 三级火箭网格
  rocket_tiered_grid?: {
    enabled: boolean
    tiers: Array<{
      filled_threshold: number
      interval: number
      profit_spread: number
    }>
  }
}

export interface UpdateBotStrategyResponse {
  ok: boolean
  bot_id: string
  message: string
}

export async function updateBotStrategy(
  botId: string,
  config: UpdateBotStrategyRequest
): Promise<UpdateBotStrategyResponse> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/strategy`, {
    method: 'PUT',
    body: JSON.stringify(config),
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

/** 交易所持倉彙總（不依賴運行中的 Bot，用於已停止 Bot 的概覽） */
export interface ExchangePositionsSummary {
  has_data: boolean
  quantity: number
  entry_price: number
  mark_price: number
  unrealized_pnl: number
  leverage: number
  current_price: number
  total_value?: number
}

export async function getExchangePositionsSummary(
  exchange: string,
  symbol: string,
  marketType: string = 'futures'
): Promise<ExchangePositionsSummary> {
  const params = new URLSearchParams({ exchange, symbol, market_type: marketType })
  return fetchWithAuth(`${API_BASE_URL}/positions/exchange-summary?${params.toString()}`)
}

// 按交易所、币种、策略列出的所有持倉彙總
export interface PositionSummaryItem {
  bot_id?: string
  exchange: string
  symbol: string
  market_type?: string
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
  leverage?: number     // 杠杆倍数，用于计算资金占用
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
  leverage?: number  // 杠杆倍数，用于计算资金占用
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

// Exchange Open Orders - 直接从交易所查询开放委托
export interface ExchangeOpenOrderInfo {
  order_id: number
  client_order_id: string
  exchange: string
  symbol: string
  price: number
  quantity: number
  executed_qty: number
  side: string
  type: string
  status: string
  created_at: string
  is_mine: boolean       // 是否为本机器人管理的委托
  strategy_name: string  // 关联的策略名（如果 is_mine）
  slot_price: number     // 关联槽位价格（如果 is_mine）
}

export interface ExchangeOpenOrdersResponse {
  success: boolean
  orders: ExchangeOpenOrderInfo[]
  count: number
}

export async function getExchangeOpenOrders(
  exchange: string,
  symbol: string,
  marketType: string = 'futures'
): Promise<ExchangeOpenOrdersResponse> {
  const params = new URLSearchParams({ exchange, symbol, market_type: marketType })
  return fetchWithAuth(`${API_BASE_URL}/orders/exchange-open?${params}`)
}

export interface CancelAllExchangeOrdersResponse {
  success: boolean
  message: string
  count: number
}

export async function cancelAllExchangeOrders(
  exchange: string,
  symbol: string,
  marketType: string = 'futures'
): Promise<CancelAllExchangeOrdersResponse> {
  return fetchWithAuth(`${API_BASE_URL}/orders/cancel-all-exchange`, {
    method: 'POST',
    body: JSON.stringify({ exchange, symbol, market_type: marketType }),
  })
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

export async function cancelOrder(
  orderId: number,
  exchange: string,
  symbol: string,
  marketType: string = 'futures'
): Promise<CancelOrderResponse> {
  const params = new URLSearchParams({ exchange, symbol })
  if (marketType) params.set('market_type', marketType)
  return fetchWithAuth(`${API_BASE_URL}/orders/${orderId}/cancel?${params}`, {
    method: 'POST',
  })
}

export interface BatchCancelOrdersResponse {
  success: boolean
  message: string
  count?: number
}

export async function batchCancelOrders(
  orderIds: number[],
  exchange: string,
  symbol: string,
  marketType: string = 'futures'
): Promise<BatchCancelOrdersResponse> {
  return fetchWithAuth(`${API_BASE_URL}/orders/cancel`, {
    method: 'POST',
    body: JSON.stringify({ order_ids: orderIds, exchange, symbol, market_type: marketType }),
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
  // 🔥 當日統計
  today_trades?: number // 當日成交筆數
  today_pnl?: number // 當日網格盈虧
  today_exchange_pnl?: number // 當日交易所盈虧
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
  /** 请求区间超过服务端上限时已截断为最近 N 天（见 effective_*） */
  range_clamped?: boolean
  effective_start_time?: string
  effective_end_time?: string
}

export async function getPnLByExchange(startTime?: string, endTime?: string): Promise<ExchangePnLResponseData> {
  const queryParams = new URLSearchParams()
  if (startTime) queryParams.append('start_time', startTime)
  if (endTime) queryParams.append('end_time', endTime)
  const url = `${API_BASE_URL}/statistics/pnl/exchange${queryParams.toString() ? '?' + queryParams.toString() : ''}`
  return fetchWithAuth(url)
}

/** 网格盈亏 vs 交易所盈亏 诊断 */
export interface DiagnosisPnLComparison {
  grid_pnl: number
  exchange_pnl: number
  discrepancy: number
  discrepancy_explanation: string
  orders_with_realized_pnl: number
  sell_orders_missing_pnl: number
}

export interface ExchangePnLDiagnosisResponse {
  exchange?: string
  symbol?: string
  error?: string
  time_range?: { start: string; end: string }
  pnl_comparison?: DiagnosisPnLComparison
  summary?: Record<string, unknown>
  by_symbol?: unknown[]
  by_date?: unknown[]
  note?: string
}

export async function getExchangePnLDiagnosis(
  exchange?: string,
  symbol?: string,
  startTime?: string,
  endTime?: string
): Promise<ExchangePnLDiagnosisResponse> {
  const queryParams = new URLSearchParams()
  if (exchange) queryParams.append('exchange', exchange)
  if (symbol) queryParams.append('symbol', symbol)
  if (startTime) queryParams.append('start_time', startTime)
  if (endTime) queryParams.append('end_time', endTime)
  const url = `${API_BASE_URL}/statistics/pnl/diagnosis${queryParams.toString() ? '?' + queryParams.toString() : ''}`
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
  bot_id?: string
}

export async function getRiskCheckHistory(params?: RiskCheckHistoryParams): Promise<RiskCheckHistoryResponse> {
  const queryParams = new URLSearchParams()
  if (params?.start_time) queryParams.append('start_time', params.start_time)
  if (params?.end_time) queryParams.append('end_time', params.end_time)
  if (params?.limit) queryParams.append('limit', String(params.limit))
  if (params?.bot_id) queryParams.append('bot_id', params.bot_id)
  
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

// Market Ticker (当前价、标记价、24h 高低)
export interface MarketTickerResponse {
  mark_price: number
  last_price: number
  high_24h: number
  low_24h: number
  exchange: string
  symbol: string
  market_type: string
}

export async function getMarketTicker(
  exchange: string,
  symbol: string,
  marketType?: string
): Promise<MarketTickerResponse> {
  const params = new URLSearchParams({ exchange, symbol })
  if (marketType) params.append('market_type', marketType)
  return fetchWithAuth(`${API_BASE_URL}/market/ticker?${params.toString()}`)
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

// ========== V2 API: 平倉和槽位管理 ==========

export interface ClosePositionsV2Request {
  quantity_ratio?: number  // 平倉比例 0~1，0 或 1 表示全倉
  method: 'market' | 'limit'
  price_offset?: number
  timeout_sec?: number
  auto_retry?: boolean
}

export interface ClosePositionsV2Response {
  success: boolean
  record_id?: string
  order_id?: number
  status?: string
  error_message?: string
}

export interface ClosePositionRecord {
  record_id: string
  bot_id: string
  symbol: string
  side: string
  target_qty: number
  filled_qty: number
  method: string
  price: number
  order_id: number
  status: string
  created_at: string
  updated_at: string
  timeout_at: string
  retry_count: number
  error_message?: string
}

export interface CloseRecordsResponse {
  records: ClosePositionRecord[]
}

// 平倉 V2 API
export async function closePositionsV2(botID: string, req: ClosePositionsV2Request): Promise<ClosePositionsV2Response> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${botID}/close-positions`, {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export async function getClosePositionRecords(botID: string): Promise<CloseRecordsResponse> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${botID}/close-records`)
}

export async function retryClosePosition(recordID: string, method: 'market' | 'limit'): Promise<ClosePositionsV2Response> {
  return fetchWithAuth(`${API_BASE_URL}/v2/close-records/${recordID}/retry`, {
    method: 'POST',
    body: JSON.stringify({ method }),
  })
}

// 槽位過濾 API
export interface SlotFilterRule {
  type: 'exclude' | 'include'
  prices?: number[]
  min_price?: number
  max_price?: number
  reason?: string
}

export interface SlotFilterConfig {
  rules: SlotFilterRule[]
}

export interface SlotInfo {
  price: number
  position_status: string
  position_qty: number
  order_id: number
  order_side: string
  order_status: string
  order_price: number
  slot_status: string
}

export interface SlotsResponse {
  slots: SlotInfo[]
}

export async function getSlotFilter(botID: string): Promise<SlotFilterConfig> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${botID}/slot-filter`)
}

export async function setSlotFilter(botID: string, filter: SlotFilterConfig): Promise<{ ok: boolean }> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${botID}/slot-filter`, {
    method: 'POST',
    body: JSON.stringify(filter),
  })
}

export async function addSlotFilterRule(botID: string, rule: SlotFilterRule): Promise<{ ok: boolean }> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${botID}/slot-filter/rules`, {
    method: 'POST',
    body: JSON.stringify(rule),
  })
}

export async function removeSlotFilterRule(botID: string, index: number): Promise<{ ok: boolean }> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${botID}/slot-filter/rules/${index}`, {
    method: 'DELETE',
  })
}

export async function getBotSlots(botID: string): Promise<SlotsResponse> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${botID}/slots`)
}

export async function toggleSlotEnabled(botID: string, price: number, enabled: boolean): Promise<{ ok: boolean }> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${botID}/slots/toggle`, {
    method: 'POST',
    body: JSON.stringify({ price, enabled }),
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

export async function getFundingRateHistory(
  symbol?: string,
  limit: number = 100,
  exchange?: string
): Promise<FundingRateHistoryResponse> {
  const queryParams = new URLSearchParams()
  if (symbol) queryParams.append('symbol', symbol)
  if (exchange) queryParams.append('exchange', exchange)
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

// Macro Events (Polymarket 宏觀事件預測)
export interface MacroEventItem {
  id: string
  title: string
  description: string
  category: string
  category_label: string
  probability: number
  probability_delta: number
  volume: number
  volume_24hr: number
  liquidity: number
  source_url: string
  end_date: string
  last_updated: string
  market_count: number
}

export interface MacroEventsResponse {
  events: MacroEventItem[]
  last_fetched: string | null
  enabled: boolean
}

export interface MacroImpactAssessment {
  event_id: string
  event_title: string
  category: string
  probability: number
  probability_delta: number
  risk_score: number
  impact_direction: string
  crypto_impact: string
  reason: string
  weight: number
}

export interface MacroImpactResponse {
  composite_risk_score: number
  event_count: number
  high_impact_count: number
  assessments: MacroImpactAssessment[]
  last_fetched: string | null
  enabled: boolean
}

export async function getMacroEvents(): Promise<MacroEventsResponse> {
  return fetchWithAuth(`${API_BASE_URL}/macro/events`)
}

export async function getMacroImpact(): Promise<MacroImpactResponse> {
  return fetchWithAuth(`${API_BASE_URL}/macro/impact`)
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

export interface BasisMonitorConfig {
  enabled: boolean
  interval_minutes: number
  symbols: string[]
}

export async function getBasisConfig(): Promise<{ config: BasisMonitorConfig }> {
  const url = `${API_BASE_URL}/basis/config`
  return fetchWithAuth(url)
}

export async function putBasisConfig(config: Partial<BasisMonitorConfig>): Promise<{ ok: boolean; config: BasisMonitorConfig }> {
  const url = `${API_BASE_URL}/basis/config`
  const body: Record<string, unknown> = {}
  if (config.enabled !== undefined) body.enabled = config.enabled
  if (config.interval_minutes !== undefined) body.interval_minutes = config.interval_minutes
  if (config.symbols !== undefined) body.symbols = config.symbols
  return fetchWithAuth(url, { method: 'PUT', body: JSON.stringify(body) })
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

// ============================================================
// Bot Risk Control API (V2)
// ============================================================

// 網格風控配置（止損、止盈、回撤等，觸發時會全平倉）
export interface GridRiskControl {
  enabled?: boolean
  stop_loss_ratio?: number
  take_profit_trigger_ratio?: number
  trailing_take_profit_ratio?: number
  max_grid_layers?: number
  max_open_orders_at_cap?: number
  trend_filter_enabled?: boolean
  /** 關閉條件：滿足時平倉並停止 Bot */
  close_condition_enabled?: boolean
  close_condition_profit_target?: number
  close_condition_loss_limit?: number
}

// Bot 风控配置
export interface BotRiskControl {
  enabled?: boolean
  max_position_quantity?: number
  max_position_qty?: number
  max_position_value?: number
  max_position_layers?: number
  max_open_orders?: number       // 最多開倉掛單數，0=不限制
  open_order_distance?: number   // 開倉單距離當前價的最大間隔數
  stop_loss_ratio?: number
  take_profit_ratio?: number
  trailing_stop_ratio?: number
  pause_opening?: boolean
  pause_opening_reason?: string
  auto_resume_after?: number
  trend_filter_enabled?: boolean
  /** 網格風控（浮虧達止損比例時全平倉，觸發飛書/郵件通知） */
  grid_risk_control?: GridRiskControl
}

// 仓位状态
export interface PositionStatus {
  total_position_qty: number
  total_position_value: number
  total_actual_margin: number  // 当前实际占用资金（保证金）
  leverage: number              // 杠杆倍数
  position_layers: number
  current_price: number
  paused: boolean
  max_position_qty?: number
  reached_limit_qty: boolean
  max_position_value?: number
  reached_limit_value: boolean
  max_position_layers?: number
  reached_limit_layers: boolean
  should_stop_opening: boolean
  error?: string
  /** 已停止的 Bot 无实时仓位，后端返回此标记 */
  stopped?: boolean
}

// 获取 Bot 风控配置
export async function getBotRiskControl(botID: string): Promise<BotRiskControl> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${encodeURIComponent(botID)}/risk-control`)
}

// 更新 Bot 风控配置
export async function updateBotRiskControl(botID: string, config: BotRiskControl): Promise<BotRiskControl> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${encodeURIComponent(botID)}/risk-control`, {
    method: 'PUT',
    body: JSON.stringify(config),
  })
}

// 暂停 Bot 开仓
export async function pauseBotOpening(
  botID: string,
  reason: string,
  autoResumeSec?: number
): Promise<{ status: string; reason: string; auto_resume_at?: number }> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${encodeURIComponent(botID)}/pause-opening`, {
    method: 'POST',
    body: JSON.stringify({ reason, auto_resume_sec: autoResumeSec }),
  })
}

// 恢复 Bot 开仓
export async function resumeBotOpening(botID: string): Promise<{ status: string }> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${encodeURIComponent(botID)}/resume-opening`, {
    method: 'POST',
  })
}

// 获取 Bot 仓位状态
export async function getBotPositionStatus(botID: string): Promise<PositionStatus> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${encodeURIComponent(botID)}/position-status`)
}

// ==================== Option Hedge API ====================

export interface OptionHedgePosition {
  exchange: string
  symbol: string
  instrument: string
  right: string
  strike: number
  expiry: string
  qty: number
  mark_price: number
  delta: number
  vega: number
  theta: number
  premium: number
  source: string
  updated_at: string
}

export interface OptionHedgeCoverage {
  bot_id: string
  hedge_type?: string // PUT / CALL，用于显示「Put 保护」或「Call 保护」
  grid_notional: number
  grid_position_qty: number
  option_notional: number
  option_delta_hedge: number
  nominal_coverage: number
  delta_coverage: number
  min_dte: number
  total_premium: number
  below_min_coverage: boolean
  dte_warning: boolean
  snapshot_at: string
}

export interface OptionHedgeStatus {
  bot_id: string
  enabled: boolean
  hedge_type?: string // PUT / CALL，用于显示「Put 保护」或「Call 保护」
  positions: OptionHedgePosition[]
  coverage?: OptionHedgeCoverage
  sync_status: string
  last_sync_at?: string
  alerts?: string[]
}

export interface RollSuggestion {
  rank: number
  label: string
  instrument?: string
  strike: number
  expiry?: string
  dte: number
  estimated_premium?: number
  expected_coverage: number
  risk_if_skip: string
}

export async function getOptionHedgeStatus(botID: string): Promise<OptionHedgeStatus> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${encodeURIComponent(botID)}/option-hedge/status`)
}

export async function syncOptionHedge(botID: string): Promise<{
  bot_id: string
  sync_status: string
  error?: string
  positions: OptionHedgePosition[]
  coverage?: OptionHedgeCoverage
}> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${encodeURIComponent(botID)}/option-hedge/sync`, {
    method: 'POST',
  })
}

export async function getOptionHedgeRollSuggestions(botID: string): Promise<{ suggestions: RollSuggestion[] }> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${encodeURIComponent(botID)}/option-hedge/roll-suggestions`)
}

export async function executeOptionHedgeRoll(
  botID: string,
  body: { from_instrument?: string; to_instrument?: string; action?: string; details?: string }
): Promise<{ bot_id: string; action: string; recorded: boolean }> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${encodeURIComponent(botID)}/option-hedge/execute-roll`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

// ==================== Bot Backtest API ====================

// Bot 回测请求
export interface BotBacktestRequest {
  bot_id: string
  start_date?: string // ISO 8601 format
  end_date?: string   // ISO 8601 format
  data_dir?: string
  commission?: number
  leverage?: number
}

// Bot 回测响应
export interface BotBacktestResponse {
  task_id: string
  status: string
  message: string
  bot_config?: any
  backtest_config?: any
}

// Bot 回测任务
export interface BotBacktestTask {
  task_id: string
  bot_id: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  created_at: string
  started_at?: string
  completed_at?: string
  result?: BotBacktestResult
  error?: string
  progress: number
}

// Bot 回测结果
export interface BotBacktestResult {
  symbol: string
  start_time: string
  end_time: string
  duration: string
  initial_capital: number
  final_equity: number
  total_return: number
  total_return_pct: number
  total_trades: number
  total_volume: number
  total_fees: number
  total_slippage: number
  total_funding: number
  equity_curve: Array<{ timestamp: number; equity: number }>
  trades: Array<{
    trade_id: string
    order_id: string
    side: string
    price: number
    size: number
    strategy: string
    timestamp: number
    grid_level?: number
    slippage: number
  }>
  completed_trades: Array<{
    timestamp: number
    side: 'long' | 'short'
    entry_price: number
    exit_price: number
    size: number
    pnl: number
    fee: number
    slippage: number
    strategy: string
    grid_level?: number
  }>
  stats_by_strategy: Record<string, {
    name: string
    type: string
    total_trades: number
    realized_pnl: number
    slippage_cost: number
    funding_cost: number
    win_rate: number
    max_drawdown: number
    completed_trades: any[]
  }>
  risk_metrics: {
    max_drawdown: number
    max_drawdown_pct: number
    sharpe_ratio: number
    win_rate: number
    profit_factor: number
    avg_win: number
    avg_loss: number
    largest_win: number
    largest_loss: number
  }
}

// 创建 Bot 回测任务
export async function createBotBacktest(botId: string, request: Omit<BotBacktestRequest, 'bot_id'>): Promise<BotBacktestResponse> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${encodeURIComponent(botId)}/backtest`, {
    method: 'POST',
    body: JSON.stringify({ ...request, bot_id: botId }),
  })
}

// 获取 Bot 回测任务状态
export async function getBotBacktestTask(taskId: string): Promise<BotBacktestTask> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bot/backtest/${encodeURIComponent(taskId)}`)
}

// 获取 Bot 回测结果
export async function getBotBacktestResult(taskId: string): Promise<BotBacktestResult> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bot/backtest/${encodeURIComponent(taskId)}/result`)
}

// 列出 Bot 回测任务
export async function listBotBacktestTasks(botId: string): Promise<{ tasks: BotBacktestTask[]; count: number }> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bots/${encodeURIComponent(botId)}/backtest/tasks`)
}

// 删除 Bot 回测任务
export async function deleteBotBacktestTask(taskId: string): Promise<{ ok: boolean; task_id: string }> {
  return fetchWithAuth(`${API_BASE_URL}/v2/bot/backtest/${encodeURIComponent(taskId)}`, {
    method: 'DELETE',
  })
}

// ==================== Bot 配置文件 API ====================

// Bot 配置文件结构
export interface BotConfigFile {
  bot_id: string
  name: string
  created_at: string
  updated_at: string
  exchange: string
  symbol: string
  market_type: string
  testnet: boolean
  strategy_mode: 'single' | 'multi'
  strategies: BotStrategyConfig[]
  capital: {
    total_allocated: number
    per_strategy: boolean
    withdrawal?: any
  }
  grid?: {
    price_interval: number
    profit_spread?: number
    order_quantity: number
    min_order_value: number
    buy_window_size: number
    sell_window_size: number
    direction?: string
    price_low?: number
    price_high?: number
    trigger_price?: number
    grid_mode?: string
    grid_shift_enabled?: boolean
    grid_shift_step?: number
  }
  risk_control: {
    grid_risk_control?: any
    open_position_control?: any
    max_drawdown_ratio?: number
    stop_loss_ratio?: number
    take_profit_ratio?: number
  }
  advanced?: {
    reconcile_interval?: number
    order_cleanup_threshold?: number
    cleanup_batch_size?: number
    margin_lock_duration_sec?: number
    position_safety_check?: number
    close_on_stop?: boolean
    close_on_stop_config?: any
    slot_filter?: any
    smart_order?: any
    profiles?: any
    switch_rules?: any
  }
  hedge?: {
    group_id: string
    group_name: string
    role: string
    hedge_ratio: number
    rebalance: boolean
    sync_position: boolean
  }
}

// Bot 策略配置
export interface BotStrategyConfig {
  type: string
  enabled: boolean
  weight: number
  params?: Record<string, any>
  settings?: Record<string, any>
}

// Bot 配置文件响应
export interface BotConfigFileResponse {
  bot_id: string
  name: string
  exchange: string
  symbol: string
  market_type: string
  config: BotConfigFile
  exists: boolean
}

// 获取 Bot 配置文件
export async function getBotConfigFile(botId: string): Promise<BotConfigFileResponse> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/config-file`)
}

// 更新 Bot 配置文件
export async function updateBotConfigFile(
  botId: string,
  config: BotConfigFile
): Promise<{ ok: boolean; bot_id: string; action: string }> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/config-file`, {
    method: 'PUT',
    body: JSON.stringify(config),
  })
}

// 删除 Bot 配置文件
export async function deleteBotConfigFile(botId: string): Promise<{ ok: boolean; bot_id: string }> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/config-file`, {
    method: 'DELETE',
  })
}

// 更新单个策略配置
export async function updateBotStrategyConfig(
  botId: string,
  strategyIndex: number,
  strategy: BotStrategyConfig
): Promise<{ ok: boolean; bot_id: string; strategy_index: number }> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/strategy-config`, {
    method: 'PUT',
    body: JSON.stringify({
      strategy_index: strategyIndex,
      strategy: strategy,
    }),
  })
}

// 添加策略
export async function addBotStrategy(
  botId: string,
  strategy: BotStrategyConfig
): Promise<{ ok: boolean; bot_id: string; strategy_type: string; strategy_count: number }> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/strategies`, {
    method: 'POST',
    body: JSON.stringify(strategy),
  })
}

// 移除策略
export async function removeBotStrategy(
  botId: string,
  strategyIndex: number
): Promise<{ ok: boolean; bot_id: string; strategy_type: string; strategy_count: number }> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/strategies/${strategyIndex}`, {
    method: 'DELETE',
  })
}

// 导出 Bot 配置为 JSON
export async function exportBotConfig(botId: string): Promise<string> {
  const response = await fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/config-file`)
  const config = response.config
  return JSON.stringify(config, null, 2)
}

// 从 JSON 导入 Bot 配置
export async function importBotConfig(
  botId: string,
  jsonConfig: string
): Promise<{ ok: boolean; bot_id: string }> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/config-file`, {
    method: 'PUT',
    body: jsonConfig,
  })
}

// ==================== 混合策略 API ====================

// 混合策略配置
export interface HybridStrategyConfig {
  name: string
  description?: string
  sub_strategies: SubStrategyConfig[]
  collaboration_rules: CollaborationRule[]
  global_settings?: Record<string, any>
}

// 子策略配置
export interface SubStrategyConfig {
  id: string
  name: string
  type: string
  role: 'primary' | 'signal' | 'hybrid' | 'monitor'
  weight: number
  enabled: boolean
  config: Record<string, any>
  metadata?: Record<string, any>
}

// 协作规则
export interface CollaborationRule {
  id: string
  name: string
  description?: string
  priority?: number
  enabled: boolean
  when: SignalCondition
  then: Action[]
}

// 信号条件
export interface SignalCondition {
  source_strategy: string
  signal_type: string
  operator: '==' | '!=' | '>' | '<' | '>=' | '<=' | 'in' | 'not_in'
  value: any
  within?: string // 时间窗口，如 "1m", "5m"
}

// 执行动作
export interface Action {
  target_strategy: string
  operation: 'allow_open' | 'deny_open' | 'allow_close' | 'deny_close' | 'modify_params' | 'enable_strategy' | 'disable_strategy' | 'emit_signal'
  condition?: string
  params?: Record<string, any>
}

// 获取混合策略配置
export async function getHybridStrategyConfig(botId: string): Promise<{
  hybrid_mode: boolean
  config?: HybridStrategyConfig
  message?: string
}> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/hybrid-config`)
}

// 更新混合策略配置
export async function updateHybridStrategy(
  botId: string,
  data: { hybrid_strategy: HybridStrategyConfig }
): Promise<{ ok: boolean; bot_id: string }> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/hybrid-config`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

// 启用混合模式
export async function enableHybridMode(
  botId: string,
  data: {
    name: string
    description?: string
    sub_strategies: SubStrategyConfig[]
    collaboration_rules: CollaborationRule[]
  }
): Promise<{ ok: boolean; bot_id: string; mode: string }> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/enable-hybrid`, {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

// 禁用混合模式
export async function disableHybridMode(botId: string): Promise<{
  ok: boolean
  bot_id: string
  mode: string
}> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/disable-hybrid`, {
    method: 'POST',
  })
}

// 获取混合策略状态
export async function getHybridStrategyStatus(botId: string): Promise<{
  bot_id: string
  hybrid_mode: boolean
  sub_strategies_count?: number
  rules_count?: number
  enabled_sub_strategies?: number
  enabled_rules?: number
}> {
  return fetchWithAuth(`${API_BASE_URL}/bots/${encodeURIComponent(botId)}/hybrid-status`)
}

// 获取内置规则模板
export async function getBuiltInRuleTemplates(): Promise<{
  templates: CollaborationRule[]
}> {
  return fetchWithAuth(`${API_BASE_URL}/hybrid/rules/templates`)
}

// FIX 协议
export interface FixSessionItem {
  session_id: string
  bot_id: string
  role: string
  begin_string: string
  sender_comp_id: string
  target_comp_id: string
  next_sender_seq: number
  next_target_seq: number
  is_logged_on: boolean
  last_logon_at?: string
  last_heartbeat_at?: string
  updated_at: string
}

export interface FixSessionsResponse {
  sessions: FixSessionItem[]
  total_count: number
}

export async function getFixSessions(limit?: number, offset?: number): Promise<FixSessionsResponse> {
  const params = new URLSearchParams()
  if (limit != null) params.append('limit', String(limit))
  if (offset != null) params.append('offset', String(offset))
  const q = params.toString()
  return fetchWithAuth(`${API_BASE_URL}/fix/sessions${q ? '?' + q : ''}`)
}

export interface FixOrderLinkItem {
  id: number
  session_id: string
  cl_ord_id: string
  orig_cl_ord_id: string
  bot_id: string
  exchange: string
  symbol: string
  side: string
  internal_order_id: number
  last_exec_id: string
  ord_status: string
  cum_qty: number
  leaves_qty: number
  avg_px: number
  created_at: string
  updated_at: string
}

export interface FixOrdersResponse {
  orders: FixOrderLinkItem[]
  total_count: number
}

export async function getFixOrders(params?: {
  session_id?: string
  ord_status?: string
  limit?: number
  offset?: number
}): Promise<FixOrdersResponse> {
  const q = new URLSearchParams()
  if (params?.session_id) q.append('session_id', params.session_id)
  if (params?.ord_status) q.append('ord_status', params.ord_status)
  if (params?.limit != null) q.append('limit', String(params.limit))
  if (params?.offset != null) q.append('offset', String(params.offset))
  const qs = q.toString()
  return fetchWithAuth(`${API_BASE_URL}/fix/orders${qs ? '?' + qs : ''}`)
}

export async function fixLogout(sessionId: string): Promise<{ ok: boolean; session_id: string }> {
  return fetchWithAuth(`${API_BASE_URL}/fix/sessions/logout`, {
    method: 'POST',
    body: JSON.stringify({ session_id: sessionId }),
  })
}

