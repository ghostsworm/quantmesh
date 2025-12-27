import { fetchWithAuth } from './api'

// 配置类型定义（简化版，根据实际需要扩展）
export interface Config {
  app: {
    current_exchange: string
  }
  trading: {
    symbol: string
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

