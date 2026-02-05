import posthog from 'posthog-js'

// PostHog 配置（与后端保持一致）
const POSTHOG_PROJECT_ID = 'phc_kz2U334i5MD8ozz78zvCdN6aRkkx3kYyoU1RSigJOiA'
const POSTHOG_HOST = 'https://us.i.posthog.com'

// 检查是否禁用统计
const isTelemetryDisabled = () => {
  // 检查环境变量（Vite 会将环境变量注入到 import.meta.env）
  if (import.meta.env.VITE_DISABLE_TELEMETRY === '1' || import.meta.env.VITE_DISABLE_TELEMETRY === 'true') {
    return true
  }
  // 检查 localStorage（用户可能通过设置禁用）
  if (typeof window !== 'undefined' && localStorage.getItem('QUANTMESH_DISABLE_TELEMETRY') === '1') {
    return true
  }
  return false
}

// 初始化 PostHog
let posthogInitialized = false

const initPostHog = () => {
  if (isTelemetryDisabled() || posthogInitialized) {
    return
  }

  try {
    posthog.init(POSTHOG_PROJECT_ID, {
      api_host: POSTHOG_HOST,
      // 禁用自动捕获（只手动追踪）
      autocapture: false,
      // 禁用会话录制（隐私保护）
      disable_session_recording: true,
      // 禁用热力图
      disable_persistence: false,
      // 禁用 cookie
      disable_cookie: false,
      // 加载时立即初始化
      loaded: (posthog) => {
        if (import.meta.env.MODE === 'development') {
          console.log('[Telemetry] PostHog initialized')
        }
      },
    })
    posthogInitialized = true
  } catch (error) {
    // 静默处理错误，不影响应用功能
    if (import.meta.env.MODE === 'development') {
      console.error('[Telemetry] Failed to initialize PostHog:', error)
    }
  }
}

// 获取版本号
const getVersion = async (): Promise<string> => {
  try {
    const response = await fetch('/api/version')
    if (response.ok) {
      const data = await response.json()
      return data.version || 'unknown'
    }
  } catch (error) {
    // 静默处理
  }
  return 'unknown'
}

// 追踪应用初始化
export const trackAppInit = async () => {
  if (isTelemetryDisabled()) {
    return
  }

  try {
    initPostHog()
    if (!posthogInitialized) {
      return
    }

    const version = await getVersion()
    posthog.capture('app_init', {
      version,
      timestamp: new Date().toISOString(),
    })
  } catch (error) {
    // 静默处理错误
    if (import.meta.env.MODE === 'development') {
      console.error('[Telemetry] Failed to track app_init:', error)
    }
  }
}

// 追踪用户登录
export const trackUserLogin = async (method: 'password' | 'webauthn') => {
  if (isTelemetryDisabled()) {
    return
  }

  try {
    initPostHog()
    if (!posthogInitialized) {
      return
    }

    const version = await getVersion()
    posthog.capture('user_login', {
      method,
      version,
      timestamp: new Date().toISOString(),
    })
  } catch (error) {
    // 静默处理错误
    if (import.meta.env.MODE === 'development') {
      console.error('[Telemetry] Failed to track user_login:', error)
    }
  }
}

// 追踪配置保存
export const trackConfigSaved = async (configType?: string) => {
  if (isTelemetryDisabled()) {
    return
  }

  try {
    initPostHog()
    if (!posthogInitialized) {
      return
    }

    const version = await getVersion()
    posthog.capture('config_saved', {
      config_type: configType || 'general',
      version,
      timestamp: new Date().toISOString(),
    })
  } catch (error) {
    // 静默处理错误
    if (import.meta.env.MODE === 'development') {
      console.error('[Telemetry] Failed to track config_saved:', error)
    }
  }
}

// 追踪交易启动
export const trackTradingStarted = async (exchange?: string, symbol?: string) => {
  if (isTelemetryDisabled()) {
    return
  }

  try {
    initPostHog()
    if (!posthogInitialized) {
      return
    }

    const version = await getVersion()
    posthog.capture('trading_started', {
      exchange,
      symbol,
      version,
      timestamp: new Date().toISOString(),
    })
  } catch (error) {
    // 静默处理错误
    if (import.meta.env.MODE === 'development') {
      console.error('[Telemetry] Failed to track trading_started:', error)
    }
  }
}
