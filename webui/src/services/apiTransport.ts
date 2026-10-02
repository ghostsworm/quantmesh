// 使用页面同源，避免相對路径被代理/扩展劫持
export const API_BASE_URL = `${window.location.origin}/api`

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
    let parsed: { error?: string; message?: string; error_key?: string; code?: string; group_name?: string; bot_id?: string } | null = null
    try {
      parsed = JSON.parse(errorText) as { error?: string; message?: string; error_key?: string; code?: string; group_name?: string; bot_id?: string }
    } catch {
      /* ignore */
    }
    const message = parsed?.error || parsed?.message || errorText
    const err = new Error(`HTTP ${response.status}: ${message}`) as Error & {
      status?: number
      errorKey?: string
      code?: string
      groupName?: string
      botId?: string
      responseBody?: unknown
    }
    err.status = response.status
    err.responseBody = parsed
    if (parsed?.error_key) err.errorKey = parsed.error_key
    if (parsed?.code) err.code = parsed.code
    if (parsed?.group_name) err.groupName = parsed.group_name
    if (parsed?.bot_id) err.botId = parsed.bot_id
    throw err
  }

  return response.json()
}
