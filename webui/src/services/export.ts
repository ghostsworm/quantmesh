// 導出服務 API
// 處理數據導出相關的 API 調用

const API_BASE_URL = `${window.location.origin}/api`

// 導出參數接口
export interface ExportParams {
  format?: 'json' | 'csv'
  start_time?: string
  end_time?: string
  exchange?: string
  symbol?: string
  account?: string
  limit?: number
  offset?: number
}

// 構建查詢字符串
function buildQueryString(params: ExportParams): string {
  const queryParams = new URLSearchParams()
  if (params.format) queryParams.append('format', params.format)
  if (params.start_time) queryParams.append('start_time', params.start_time)
  if (params.end_time) queryParams.append('end_time', params.end_time)
  if (params.exchange) queryParams.append('exchange', params.exchange)
  if (params.symbol) queryParams.append('symbol', params.symbol)
  if (params.account) queryParams.append('account', params.account)
  if (params.limit) queryParams.append('limit', params.limit.toString())
  if (params.offset) queryParams.append('offset', params.offset.toString())
  return queryParams.toString()
}

// 觸發文件下載
async function downloadFile(url: string, defaultFilename: string): Promise<void> {
  const response = await fetch(url, {
    credentials: 'include',
  })

  if (!response.ok) {
    const errorText = await response.text()
    throw new Error(`HTTP ${response.status}: ${errorText}`)
  }

  // 從 Content-Disposition 頭獲取文件名
  const disposition = response.headers.get('Content-Disposition')
  let filename = defaultFilename
  if (disposition) {
    const match = disposition.match(/filename="(.+)"/)
    if (match) {
      filename = match[1]
    }
  }

  const blob = await response.blob()
  const blobUrl = window.URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = blobUrl
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  window.URL.revokeObjectURL(blobUrl)
}

// 導出當前配置（脫敏）
export async function exportConfig(): Promise<void> {
  const url = `${API_BASE_URL}/export/config`
  await downloadFile(url, 'config.yaml')
}

// 導出歷史配置
export async function exportConfigHistory(version: number): Promise<void> {
  const url = `${API_BASE_URL}/export/config/history/${version}`
  await downloadFile(url, `config_v${version}.yaml`)
}

// 導出交易歷史
export async function exportTrades(params: ExportParams = {}): Promise<void> {
  const query = buildQueryString(params)
  const ext = params.format === 'csv' ? 'csv' : 'json'
  const url = `${API_BASE_URL}/export/trades${query ? '?' + query : ''}`
  await downloadFile(url, `trades.${ext}`)
}

// 導出訂單歷史
export async function exportOrders(params: ExportParams = {}): Promise<void> {
  const query = buildQueryString(params)
  const ext = params.format === 'csv' ? 'csv' : 'json'
  const url = `${API_BASE_URL}/export/orders${query ? '?' + query : ''}`
  await downloadFile(url, `orders.${ext}`)
}

// 導出持倉歷史
export async function exportPositions(params: ExportParams = {}): Promise<void> {
  const query = buildQueryString(params)
  const ext = params.format === 'csv' ? 'csv' : 'json'
  const url = `${API_BASE_URL}/export/positions${query ? '?' + query : ''}`
  await downloadFile(url, `positions.${ext}`)
}

// 導出統計數據
export async function exportStatistics(params: ExportParams = {}): Promise<void> {
  const query = buildQueryString(params)
  const ext = params.format === 'csv' ? 'csv' : 'json'
  const url = `${API_BASE_URL}/export/statistics${query ? '?' + query : ''}`
  await downloadFile(url, `statistics.${ext}`)
}

// 導出對賬歷史
export async function exportReconciliation(params: ExportParams = {}): Promise<void> {
  const query = buildQueryString(params)
  const ext = params.format === 'csv' ? 'csv' : 'json'
  const url = `${API_BASE_URL}/export/reconciliation${query ? '?' + query : ''}`
  await downloadFile(url, `reconciliation.${ext}`)
}

// 導出風控檢查歷史
export async function exportRiskChecks(params: ExportParams = {}): Promise<void> {
  const query = buildQueryString(params)
  const ext = params.format === 'csv' ? 'csv' : 'json'
  const url = `${API_BASE_URL}/export/risk-checks${query ? '?' + query : ''}`
  await downloadFile(url, `risk_checks.${ext}`)
}

// 導出系統監控數據
export async function exportSystemMetrics(params: ExportParams = {}): Promise<void> {
  const query = buildQueryString(params)
  const ext = params.format === 'csv' ? 'csv' : 'json'
  const url = `${API_BASE_URL}/export/system-metrics${query ? '?' + query : ''}`
  await downloadFile(url, `system_metrics.${ext}`)
}

// 導出應用日誌
export async function exportLogs(params: ExportParams = {}): Promise<void> {
  const query = buildQueryString(params)
  const ext = params.format === 'csv' ? 'csv' : 'json'
  const url = `${API_BASE_URL}/export/logs${query ? '?' + query : ''}`
  await downloadFile(url, `logs.${ext}`)
}

// 導出審計日誌（ZIP）
export async function exportAuditLogs(params: ExportParams = {}): Promise<void> {
  const query = buildQueryString(params)
  const url = `${API_BASE_URL}/export/audit-logs${query ? '?' + query : ''}`
  await downloadFile(url, 'audit_logs.zip')
}

// 導出全部數據（ZIP）
export async function exportAll(params: ExportParams = {}): Promise<void> {
  const query = buildQueryString(params)
  const url = `${API_BASE_URL}/export/all${query ? '?' + query : ''}`
  await downloadFile(url, 'quantmesh_export.zip')
}

// 導出類型定義
export type ExportType = 
  | 'config'
  | 'trades'
  | 'orders'
  | 'positions'
  | 'statistics'
  | 'reconciliation'
  | 'risk-checks'
  | 'system-metrics'
  | 'logs'
  | 'audit-logs'
  | 'all'
