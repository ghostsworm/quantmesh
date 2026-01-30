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

// 策略参数定义
export interface ParamField {
  name: string
  label: string
  type: string
  required: boolean
  default?: unknown
  min?: number
  max?: number
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

// 交易对预设
export interface SymbolBacktestPreset {
  symbol: string
  volatility_type: string
  recommended_days: number[]
  recommended_interval: string
  grid_gap_range: string
  interval_options: string[]
}

// 回测任务
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

export async function deleteBacktestTask(id: string): Promise<{ success: boolean; message: string }> {
  return fetchWithAuth(`${API_BASE_URL}/backtest/tasks/${encodeURIComponent(id)}`, { method: 'DELETE' })
}
