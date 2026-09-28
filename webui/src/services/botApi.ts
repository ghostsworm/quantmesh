import { API_BASE_URL, fetchWithAuth } from './apiTransport'
import type { WithdrawalPolicy } from './config'

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
  api_key?: string  // 通用 AI API Key，优先于 gemini_api_key
  provider?: string  // gemini/openai/claude/dashscope/kimi/deepseek/custom
  model?: string
  base_url?: string

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

export interface GeminiUsageEntry {
  id?: number
  at: string
  model: string
  source: string
  input_tokens: number
  output_tokens: number
  duration_ms: number
}

export interface GeminiUsageSummary {
  call_count: number
  total_input_tokens: number
  total_output_tokens: number
}

export interface GeminiUsageResponse {
  entries: GeminiUsageEntry[]
  summary: GeminiUsageSummary
  /** 符合篩選條件的總條數（分頁用） */
  total?: number
  limit?: number
  offset?: number
  /** database=主庫持久化；memory=僅進程內（無主庫時） */
  source?: 'database' | 'memory'
}

export interface GeminiUsageQuery {
  limit?: number
  offset?: number
  /** RFC3339 */
  startTime?: string
  /** RFC3339 */
  endTime?: string
}

/** Gemini 調用記錄：默認從主庫讀取；無主庫時回退進程內緩存 */
export async function getGeminiUsageLog(params?: GeminiUsageQuery): Promise<GeminiUsageResponse> {
  const q = new URLSearchParams()
  if (params?.limit != null) q.set('limit', String(params.limit))
  if (params?.offset != null) q.set('offset', String(params.offset))
  if (params?.startTime) q.set('start_time', params.startTime)
  if (params?.endTime) q.set('end_time', params.endTime)
  const qs = q.toString()
  return fetchWithAuth(`${API_BASE_URL}/ai/gemini/usage${qs ? `?${qs}` : ''}`)
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

/** Bot 開倉風控暫停/恢復事件（持久化記錄） */
export interface BotRiskControlEventItem {
  id: number
  event_type: string
  reason: string
  source: string
  created_at: string
}

export interface BotRiskControlEventsResponse {
  events: BotRiskControlEventItem[]
  total: number
  page: number
  page_size: number
  total_page: number
}

export async function getBotRiskControlEvents(
  botID: string,
  page = 1,
  pageSize = 20
): Promise<BotRiskControlEventsResponse> {
  const q = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize),
  })
  return fetchWithAuth(
    `${API_BASE_URL}/v2/bots/${encodeURIComponent(botID)}/risk-control/events?${q.toString()}`
  )
}

/** 下載風控事件 CSV（需已登入，含 Cookie） */
export async function downloadBotRiskControlEventsCsv(botID: string): Promise<void> {
  const url = `${API_BASE_URL}/v2/bots/${encodeURIComponent(botID)}/risk-control/events/export`
  const currentLang = localStorage.getItem('i18nextLng') || 'zh-CN'
  const res = await fetch(url, {
    credentials: 'include',
    headers: {
      Accept: 'text/csv',
      'Accept-Language': currentLang,
    },
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || `HTTP ${res.status}`)
  }
  const blob = await res.blob()
  const safe = botID.replace(/[/\\:*?"<>|]/g, '_')
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = `bot_risk_events_${safe}.csv`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(a.href)
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
