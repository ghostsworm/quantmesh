import { fetchWithAuth } from './api'

// 交易所配置
export interface ExchangeConfig {
  api_key: string
  secret_key: string
  passphrase?: string
  fee_rate: number
  testnet: boolean
}

// 交易对配置
export interface SymbolConfig {
  exchange?: string  // 交易所，留空时使用 app.current_exchange
  symbol: string     // 交易对名称
  price_interval: number
  order_quantity: number
  min_order_value?: number
  buy_window_size: number
  sell_window_size: number
  reconcile_interval?: number
  order_cleanup_threshold?: number
  cleanup_batch_size?: number
  margin_lock_duration_seconds?: number
  position_safety_check?: number
}

// AI模块配置
export interface AIModuleConfig {
  enabled: boolean
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

// 配置类型定义（完整版）
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
    symbols?: SymbolConfig[]  // 多交易对配置
    price_interval: number
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
  }
  risk_control: {
    enabled: boolean
    monitor_symbols: string[]
    interval: string
    volume_multiplier: number
    average_window: number
    recovery_threshold: number
    max_leverage: number
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
    provider: string
    api_key: string
    base_url: string
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

// 配置变更类型
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
export interface BackupInfo {
  id: string
  timestamp: string
  file_path: string
  size: number
  description?: string
}

// 获取当前配置（YAML格式）
export async function getConfigYAML(): Promise<string> {
  const response = await fetch(`${window.location.origin}/api/config`, {
    credentials: 'include',
  })
  if (!response.ok) {
    throw new Error('获取配置失败')
  }
  return await response.text()
}

// 获取当前配置（JSON格式）
export async function getConfig(): Promise<Config> {
  return fetchWithAuth(`${window.location.origin}/api/config/json`)
}

// 验证配置
export async function validateConfig(config: Config): Promise<{ valid: boolean; error?: string }> {
  return fetchWithAuth(`${window.location.origin}/api/config/validate`, {
    method: 'POST',
    body: JSON.stringify(config),
  })
}

// 预览配置变更
export async function previewConfig(config: Config): Promise<ConfigDiff> {
  return fetchWithAuth(`${window.location.origin}/api/config/preview`, {
    method: 'POST',
    body: JSON.stringify(config),
  })
}

// 更新配置
export async function updateConfig(config: Config): Promise<{
  message: string
  backup_id?: string
  diff?: ConfigDiff
  requires_restart: boolean
}> {
  return fetchWithAuth(`${window.location.origin}/api/config/update`, {
    method: 'POST',
    body: JSON.stringify(config),
  })
}

// 获取备份列表
export async function getBackups(): Promise<BackupInfo[]> {
  const data = await fetchWithAuth(`${window.location.origin}/api/config/backups`)
  return data.backups || []
}

// 恢复备份
export async function restoreBackup(backupId: string): Promise<{ message: string; backup_id: string }> {
  return fetchWithAuth(`${window.location.origin}/api/config/restore/${backupId}`, {
    method: 'POST',
  })
}

// 删除备份
export async function deleteBackup(backupId: string): Promise<{ message: string; backup_id: string }> {
  return fetchWithAuth(`${window.location.origin}/api/config/backup/${backupId}`, {
    method: 'DELETE',
  })
}

