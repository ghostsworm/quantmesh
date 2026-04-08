import { fetchWithAuth } from './api'

// 交易所配置
export interface ExchangeConfig {
  api_key: string
  secret_key: string
  passphrase?: string
  fee_rate: number
  testnet: boolean
}

// 阶梯划轉规则
export interface TieredWithdrawRule {
  profit_threshold: number  // 利润阈值 (如 0.1 表示 10%)
  withdraw_ratio: number    // 达到該阈值時划轉的比例
}

// 本金保护設置
export interface PrincipalProtection {
  enabled: boolean
  breakeven_protection: boolean   // 回本即保护（設置保本止损）
  withdraw_principal: boolean     // 盈利足够時划轉本金
  principal_withdraw_at: number   // 利润达到多少時划轉本金 (如 1.0 表示利润=本金時)
  max_loss_ratio: number          // 最大亏损比例 (如 0.2 表示最多亏损本金的 20%)
}

// 定時划轉設置
export interface WithdrawSchedule {
  enabled: boolean
  frequency: 'daily' | 'weekly' | 'monthly'
  day_of_week?: number   // 周几 (1-7, 僅 weekly 模式)
  day_of_month?: number  // 每月几号 (1-31, 僅 monthly 模式)
  time_of_day?: string   // 時间 (如 "23:00")
}

// 提現策略（利润保护）
export interface WithdrawalPolicy {
  enabled: boolean
  threshold: number  // 触发提現的利润比例 (如 0.1 表示 10%)

  // 划轉模式: threshold(阈值触发), fixed(固定金額), tiered(阶梯), scheduled(定時)
  mode?: 'threshold' | 'fixed' | 'tiered' | 'scheduled'

  // 固定金額模式
  fixed_amount?: number  // 每次划轉的固定金額 (USDT)

  // 阶梯划轉模式
  tiered_rules?: TieredWithdrawRule[]

  // 划轉比例 (0-1)，如 0.5 表示划轉利润的 50%
  withdraw_ratio?: number

  // 本金保护
  principal_protection?: PrincipalProtection

  // 定時划轉
  schedule?: WithdrawSchedule

  // 複利比例 (0-1)，剩餘部分划轉
  compound_ratio?: number

  // 目標账戶: spot(現貨), funding(资金账戶), external(外部地址)
  target_wallet?: 'spot' | 'funding' | 'external'
}

// 策略實例
export interface StrategyInstance {
  type: string
  weight: number
  config: Record<string, any>
}

// 配置档案（用于多套配置自动切换）
export interface ProfileConfig {
  price_interval: number
  profit_spread?: number  // 利潤間距（平倉價差），不填則使用 price_interval
  order_quantity: number
  buy_window_size: number
  sell_window_size: number
  min_order_value?: number  // 可選，繼承主配置
}

// 切换规则（根据资金费率和手续费率自动切换配置档案）
export interface SwitchRules {
  funding_rate: {
    threshold: number  // 资金费率阈值，超过此值切换到对应 profile
  }
  fee_rate: {
    threshold: number  // 手续费率阈值，超过此值切换到对应 profile
  }
  cooldown_seconds?: number  // 切换冷却时间（秒），避免频繁切换，預設 300
}

// 网格策略风控配置
export interface GridRiskControl {
  enabled: boolean
  max_grid_layers: number
  max_open_orders_at_cap?: number
  stop_loss_ratio: number
  take_profit_trigger_ratio: number
  trailing_take_profit_ratio: number
  trend_filter_enabled: boolean
}

// 交易對配置
export interface SymbolConfig {
  id?: string        // 可選：Bot 唯一標識，同交易所同幣多實例時需唯一
  name?: string      // 可選：顯示名稱，用於區分多個同幣 Bot
  enabled?: boolean   // 是否啟用自动交易（后端預設 true）
  exchange?: string  // 交易所，留空時使用 app.current_exchange
  symbol: string     // 交易對名称
  market_type?: 'spot' | 'futures' // 市场類型：現貨 / 合約，預設 futures
  total_allocated_capital?: number // 分配的總资金
  strategies?: StrategyInstance[]  // 並行策略列表
  withdrawal_policy?: WithdrawalPolicy // 提現策略
  price_interval: number  // 主配置，未配置 profiles 时使用
  profit_spread?: number  // 利潤間距（平倉價差），為 0 時等於 price_interval
  order_quantity: number  // 主配置，未配置 profiles 时使用
  min_order_value?: number
  buy_window_size: number  // 主配置，未配置 profiles 时使用
  sell_window_size: number  // 主配置，未配置 profiles 时使用
  direction?: 'LONG' | 'SHORT'  // 交易方向，預設 LONG
  reconcile_interval?: number
  order_cleanup_threshold?: number
  cleanup_batch_size?: number
  margin_lock_duration_seconds?: number
  position_safety_check?: number
  grid_risk_control?: GridRiskControl  // 網格策略风控
  price_low?: number   // 网格价格下限，0 表示不限制
  price_high?: number  // 网格价格上限，0 表示不限制
  trigger_price?: number  // 触发价格，达到后才启动策略
  grid_mode?: 'arithmetic' | 'geometric'  // 等差 / 等比
  grid_shift_enabled?: boolean
  grid_shift_step?: number  // 每次移动步长
  close_on_stop?: boolean   // 终止时全部平仓
  // 多套配置自动切换
  profiles?: Record<string, ProfileConfig>  // 配置档案（如 positive, negative）
  switch_rules?: SwitchRules  // 切换规则
}

// AI模塊配置
export interface AIModuleConfig {
  enabled: boolean
  /** 可選，指向 ai.upstreams 的鍵名 */
  upstream_ref?: string
  update_interval?: number
  optimization_interval?: number
  auto_apply?: boolean
  analysis_interval?: number
  data_sources?: {
    news?: {
      enabled: boolean
      rss_feeds: string[]
      fetch_interval: number
    }
    fear_greed_index?: {
      enabled: boolean
      api_url: string
      fetch_interval: number
    }
    social_media?: {
      enabled: boolean
      subreddits?: string[]
      post_limit?: number
    }
  }
  api_url?: string
  analysis_interval?: number
  markets?: {
    keywords: string[]
    min_liquidity: number
    min_volume_24h: number
    min_days_to_expiry: number
    max_days_to_expiry: number
  }
  signal_generation?: {
    buy_threshold: number
    sell_threshold: number
    min_signal_strength: number
    min_confidence: number
  }
}

// 配置類型定义（完整版）
export interface Config {
  app: {
    current_exchange: string
  }
  exchanges: {
    binance?: ExchangeConfig
    bitget?: ExchangeConfig
    bybit?: ExchangeConfig
    gate?: ExchangeConfig
    edgex?: ExchangeConfig
    bit?: ExchangeConfig
  }
  trading: {
    symbol: string
    symbols?: SymbolConfig[]  // 多交易對配置
    price_interval: number
    profit_spread?: number  // 利潤間距（平倉價差），不填則使用 price_interval
    order_quantity: number
    min_order_value: number
    buy_window_size: number
    sell_window_size: number
    reconcile_interval: number
    order_cleanup_threshold: number
    cleanup_batch_size: number
    margin_lock_duration_seconds: number
    position_safety_check: number
    dynamic_adjustment?: {
      enabled: boolean
      price_interval?: {
        enabled: boolean
        min: number
        max: number
        volatility_window: number
        volatility_threshold: number
        adjustment_step: number
        check_interval: number
      }
      window_size?: {
        enabled: boolean
        buy_window: {
          min: number
          max: number
        }
        sell_window: {
          min: number
          max: number
        }
        utilization_threshold: number
        adjustment_step: number
        check_interval: number
      }
      order_quantity?: {
        enabled: boolean
        min: number
        max: number
        frequency_threshold: number
        adjustment_step: number
        check_interval?: number
      }
    }
    smart_position?: {
      enabled: boolean
      trend_detection?: {
        enabled: boolean
        window: number
        method: string
        short_period: number
        long_period: number
        check_interval: number
      }
      window_adjustment?: {
        enabled: boolean
        max_adjustment: number
        adjustment_step: number
        min_buy_window: number
        min_sell_window: number
      }
    }
  }
  system: {
    log_level: string
    timezone: string
    cancel_on_exit: boolean
    close_positions_on_exit: boolean
    log_cleanup?: {
      enabled: boolean
      schedule: string
      retention_days: number
      levels_to_clean: string[]
    }
  }
  risk_control: {
    enabled: boolean
    monitor_symbols: string[]
    interval: string
    volume_multiplier: number
    average_window: number
    recovery_threshold: number
    max_leverage: number
    depth_monitor?: {
      enabled: boolean
      check_interval: number
      depth_levels: number
      drop_threshold: number
      recovery_threshold: number
      min_depth_usdt: number
    }
  }
  timing: {
    websocket_reconnect_delay: number
    websocket_write_wait: number
    websocket_pong_wait: number
    websocket_ping_interval: number
    listen_key_keepalive_interval: number
    price_send_interval: number
    rate_limit_retry_delay: number
    order_retry_delay: number
    price_poll_interval: number
    status_print_interval: number
    order_cleanup_interval: number
  }
  notifications: {
    enabled: boolean
    telegram: {
      enabled: boolean
      bot_token: string
      chat_id: string
    }
    webhook: {
      enabled: boolean
      url: string
      timeout: number
    }
    email: {
      enabled: boolean
      provider: string
      smtp: {
        host: string
        port: number
        username: string
        password: string
      }
      resend: {
        api_key: string
      }
      mailgun: {
        api_key: string
        domain: string
      }
      from: string
      to: string
      subject: string
    }
    feishu: {
      enabled: boolean
      webhook: string
    }
    dingtalk: {
      enabled: boolean
      webhook: string
      secret: string
    }
    wechat_work: {
      enabled: boolean
      webhook: string
    }
    slack: {
      enabled: boolean
      webhook: string
    }
    rules: {
      order_placed: boolean
      order_filled: boolean
      risk_triggered: boolean
      stop_loss: boolean
      error: boolean
    }
  }
  storage: {
    enabled: boolean
    type: string
    path: string
    buffer_size: number
    batch_size: number
    flush_interval: number
  }
  web: {
    enabled: boolean
    host: string
    port: number
    api_key: string
  }
  ai: {
    enabled: boolean
    /** 默認使用的命名上游鍵（對應 ai.upstreams） */
    default_upstream?: string
    /** 命名上游表，鍵為自定義名稱 */
    upstreams?: Record<string, {
      provider?: string
      model?: string
      api_key?: string
      base_url?: string
    }>
    provider: string
    api_key: string
    gemini_api_key?: string  // Gemini API Key，用於 AI 配置助手
    base_url: string
    access_mode?: 'native' | 'proxy'  // 访问模式：native=直接访问，proxy=通過中轉服務
    proxy?: {
      base_url?: string  // 代理服務地址
      username?: string   // Basic Auth 用戶名
      password?: string   // Basic Auth 密碼
    }
    modules: {
      market_analysis?: AIModuleConfig
      parameter_optimization?: AIModuleConfig
      risk_analysis?: AIModuleConfig
      sentiment_analysis?: AIModuleConfig
      polymarket_signal?: AIModuleConfig
      strategy_generation?: AIModuleConfig
    }
    decision_mode: string
    execution_rules: {
      high_risk_threshold: number
      low_risk_threshold: number
      require_confirmation: boolean
    }
  }
  news_monitor?: {
    enabled: boolean
    enable_analysis?: boolean  // 是否启用新闻分析功能（AI分析），默认true
    check_interval?: string
    analysis_interval?: string  // AI分析间隔，支持 "5m", "15m", "30m", "1h", "2h", "4h", "8h", "24h"
    news_collect_interval?: string
    use_gemini_search?: boolean  // 是否使用Gemini实时搜索（兼容旧配置）
    sources?: string[]
    news_api_key?: string
    rss_feeds?: string[]
    keywords?: string[]
    risk_threshold?: number
    prediction_timeframes?: string[]
    risk_thresholds?: {
      stop_trading_probability?: number
      reduce_position_probability?: number
    }
    history_retention_days?: number
    assets?: Array<{
      asset_type: string
      symbol: string
      keywords?: string[]
      enabled?: boolean
    }>
    ai_provider?: {
      upstream_ref?: string  // 可選，指向 ai.upstreams（優先於內聯字段）
      provider?: string  // gemini, openai, claude, poe
      model?: string     // 模型名称，如 "gpt-4", "claude-3-opus"
      api_key?: string   // Provider的API Key
      base_url?: string  // 可选，自定义API端点（用于Poe等代理）
    }
  }
  /** 宏觀事件（Polymarket Gamma）拉取與風控；保存後需重啟進程使拉取任務生效 */
  macro_event?: {
    enabled?: boolean
    fetch_interval?: number
    gamma_api_url?: string
  }
  strategies?: {
    enabled: boolean
    capital_allocation?: {
      mode: string
      total_capital: number
      fixed?: {
        enabled: boolean
        rebalance_on_start: boolean
      }
      dynamic?: {
        enabled: boolean
        rebalance_interval: number
        max_change_per_rebalance: number
        min_weight: number
        max_weight: number
        performance_weights: {
          max_drawdown: number
          sharpe_ratio: number
          total_pnl: number
          win_rate: number
        }
      }
    }
    configs?: Record<string, any>
  }
  backtest?: {
    enabled: boolean
    start_time: string
    end_time: string
    initial_capital: number
  }
  metrics?: {
    enabled: boolean
    collect_interval: number
  }
  watchdog?: {
    enabled: boolean
    sampling: {
      interval: number
    }
    retention: {
      detail_days: number
      daily_days: number
    }
    notifications: {
      enabled: boolean
      fixed_threshold: {
        enabled: boolean
        cpu_percent: number
        memory_mb: number
      }
      rate_threshold: {
        enabled: boolean
        window_minutes: number
        cpu_increase: number
        memory_increase_mb: number
      }
      cooldown_minutes: number
    }
    aggregation: {
      enabled: boolean
      schedule: string
    }
  }
  [key: string]: any
}

// 配置变更類型
export interface ConfigChange {
  path: string
  type: 'added' | 'modified' | 'deleted'
  old_value: any
  new_value: any
  requires_restart: boolean
}

// 配置差异
export interface ConfigDiff {
  changes: ConfigChange[]
  requires_restart: boolean
}

// 备份信息
// 獲取當前配置（YAML格式）
export async function getConfigYAML(): Promise<string> {
  const response = await fetch(`${window.location.origin}/api/config`, {
    credentials: 'include',
  })
  if (!response.ok) {
    throw new Error('獲取配置失败')
  }
  return await response.text()
}

// 獲取當前配置（JSON格式）
export async function getConfig(): Promise<Config> {
  return fetchWithAuth(`${window.location.origin}/api/config/json`)
}

// 驗证配置
export async function validateConfig(config: Config): Promise<{ valid: boolean; error?: string }> {
  return fetchWithAuth(`${window.location.origin}/api/config/validate`, {
    method: 'POST',
    body: JSON.stringify(config),
  })
}

// 預览配置变更
export async function previewConfig(config: Config): Promise<ConfigDiff> {
  return fetchWithAuth(`${window.location.origin}/api/config/preview`, {
    method: 'POST',
    body: JSON.stringify(config),
  })
}

// 更新配置
export async function updateConfig(config: Config): Promise<{
  message: string
  diff?: ConfigDiff
  requires_restart: boolean
}> {
  return fetchWithAuth(`${window.location.origin}/api/config/update`, {
    method: 'POST',
    body: JSON.stringify(config),
  })
}

// 驗證 YAML 配置
export async function validateConfigYAML(yamlContent: string): Promise<{ valid: boolean; error?: string }> {
  const response = await fetch(`${window.location.origin}/api/config/validate-yaml`, {
    method: 'POST',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/x-yaml',
    },
    body: yamlContent,
  })
  if (!response.ok) {
    const data = await response.json()
    return { valid: false, error: data.error || '驗证失败' }
  }
  return await response.json()
}

// 獲取安全状态
export async function getSecurityStatus(): Promise<{
  encryption_enabled: boolean
  master_key_path: string
  master_key_exists: boolean
}> {
  return fetchWithAuth(`${window.location.origin}/api/config/security/status`)
}

// 生成主密钥
export async function generateMasterKey(): Promise<{
  master_key_path: string
}> {
  return fetchWithAuth(`${window.location.origin}/api/config/security/generate-key`, {
    method: 'POST',
  })
}

// 测试通知渠道
export interface TestNotificationResponse {
  success: boolean
  message?: string
  error?: string
}

export async function testNotification(
  config: Partial<Config>,
  channel: string
): Promise<TestNotificationResponse> {
  return fetchWithAuth(
    `${window.location.origin}/api/config/test-notification?channel=${encodeURIComponent(channel)}`,
    {
      method: 'POST',
      body: JSON.stringify(config),
    }
  )
}

// 更新配置（YAML 格式）
export async function updateConfigYAML(yamlContent: string): Promise<{
  message: string
  changes_count: number
  requires_restart: boolean
}> {
  const response = await fetch(`${window.location.origin}/api/config/update-yaml`, {
    method: 'POST',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/x-yaml',
    },
    body: yamlContent,
  })
  if (!response.ok) {
    const data = await response.json()
    throw new Error(data.error || '更新失败')
  }
  return await response.json()
}

