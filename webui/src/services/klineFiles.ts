// K线数据文件服务 API

const API_BASE_URL = `${window.location.origin}/api`

// K线文件信息接口
export interface KlineFileInfo {
  filename: string
  exchange: string
  symbol: string
  interval: string // tick, 1m, 1h
  has_depth: boolean
  file_size: number
  created_at: string
  modified_at: string
  is_protected: boolean
}

// 列出所有K线文件
export async function listKlineFiles(): Promise<KlineFileInfo[]> {
  const response = await fetch(`${API_BASE_URL}/kline-files`, {
    credentials: 'include',
  })

  if (!response.ok) {
    const errorText = await response.text()
    throw new Error(`HTTP ${response.status}: ${errorText}`)
  }

  const data = await response.json()
  if (!data.success) {
    throw new Error(data.error || '获取文件列表失败')
  }

  return data.files || []
}

// 保护文件
export async function protectKlineFile(filename: string): Promise<void> {
  const response = await fetch(`${API_BASE_URL}/kline-files/${encodeURIComponent(filename)}/protect`, {
    method: 'POST',
    credentials: 'include',
  })

  if (!response.ok) {
    const errorText = await response.text()
    throw new Error(`HTTP ${response.status}: ${errorText}`)
  }

  const data = await response.json()
  if (!data.success) {
    throw new Error(data.error || '保护文件失败')
  }
}

// 取消保护文件
export async function unprotectKlineFile(filename: string): Promise<void> {
  const response = await fetch(`${API_BASE_URL}/kline-files/${encodeURIComponent(filename)}/protect`, {
    method: 'DELETE',
    credentials: 'include',
  })

  if (!response.ok) {
    const errorText = await response.text()
    throw new Error(`HTTP ${response.status}: ${errorText}`)
  }

  const data = await response.json()
  if (!data.success) {
    throw new Error(data.error || '取消保护文件失败')
  }
}

// 下载文件
export async function downloadKlineFile(filename: string): Promise<void> {
  const url = `${API_BASE_URL}/kline-files/${encodeURIComponent(filename)}/download`
  
  const response = await fetch(url, {
    credentials: 'include',
  })

  if (!response.ok) {
    const errorText = await response.text()
    throw new Error(`HTTP ${response.status}: ${errorText}`)
  }

  // 从 Content-Disposition 头获取文件名
  const disposition = response.headers.get('Content-Disposition')
  let downloadFilename = filename
  if (disposition) {
    const match = disposition.match(/filename="(.+)"/)
    if (match) {
      downloadFilename = match[1]
    }
  }

  const blob = await response.blob()
  const blobUrl = window.URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = blobUrl
  link.download = downloadFilename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  window.URL.revokeObjectURL(blobUrl)
}
