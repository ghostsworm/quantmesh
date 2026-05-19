import i18n from '../i18n/config'

// 使用页面同源，避免某些环境下相對路径被代理/扩展劫持
const API_BASE = `${window.location.origin}/api`

export interface AuthStatus {
  has_password: boolean
  has_webauthn: boolean
  is_authenticated: boolean
  security_compromised?: boolean  // 🔒 新增：標識是否存在安全隱患（數據丟失）
  password_manager_error?: boolean  // 🔒 新增：標識密碼管理器初始化失敗
}

export interface WebAuthnCredential {
  id: string
  credential_id: string
  device_name: string
  created_at: string
  last_used_at?: string
  is_active: boolean
}

// 检查认证状態
export async function checkAuthStatus(): Promise<AuthStatus> {
  const response = await fetch(`${API_BASE}/auth/status`)
  if (!response.ok) {
    throw new Error(i18n.t('auth.errors.checkStatusFailed'))
  }
  return response.json()
}

// 設置密碼
export async function setPassword(password: string): Promise<void> {
  const response = await fetch(`${API_BASE}/auth/password/set`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    credentials: 'include',
    cache: 'no-store',
    body: JSON.stringify({ password }),
  })
  if (!response.ok) {
    // 尝試解析 JSON，否则退回纯文本，附带状態碼
    let message = `設置密碼失败 (HTTP ${response.status})`
    try {
      const error = await response.json()
      message = error.error || message
    } catch {
      try {
        message = await response.text()
      } catch {
        // ignore
      }
    }
    throw new Error(message)
  }
}

// 驗证密碼
export async function verifyPassword(password: string): Promise<void> {
  const response = await fetch(`${API_BASE}/auth/password/verify`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    credentials: 'include',
    body: JSON.stringify({ password }),
  })
  if (!response.ok) {
    const error = await response.json()
    throw new Error(error.error || i18n.t('auth.errors.verifyPasswordFailed'))
  }
}

// 修改密碼
export async function changePassword(currentPassword: string, newPassword: string): Promise<void> {
  const response = await fetch(`${API_BASE}/auth/password/change`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    credentials: 'include',
    body: JSON.stringify({ 
      current_password: currentPassword,
      new_password: newPassword 
    }),
  })
  if (!response.ok) {
    const error = await response.json()
    throw new Error(error.error || i18n.t('auth.errors.changePasswordFailed'))
  }
}

// 退出登錄
export async function logout(): Promise<void> {
  const response = await fetch(`${API_BASE}/auth/logout`, {
    method: 'POST',
    credentials: 'include',
  })
  if (!response.ok) {
    throw new Error(i18n.t('auth.errors.logoutFailed'))
  }
}

// WebAuthn 注册开始
export async function beginWebAuthnRegistration(deviceName: string): Promise<{
  success: boolean
  options: any
  session_key: string
}> {
  const response = await fetch(`${API_BASE}/webauthn/register/begin`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    credentials: 'include',
    body: JSON.stringify({ device_name: deviceName }),
  })
  if (!response.ok) {
    const error = await response.json()
    throw new Error(error.error || i18n.t('auth.errors.beginWebAuthnRegistrationFailed'))
  }
  return response.json()
}

// WebAuthn 注册完成
export async function finishWebAuthnRegistration(
  sessionKey: string,
  deviceName: string,
  response: any
): Promise<void> {
  const apiResponse = await fetch(`${API_BASE}/webauthn/register/finish`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    credentials: 'include',
    body: JSON.stringify({
      session_key: sessionKey,
      device_name: deviceName,
      response,
    }),
  })
  if (!apiResponse.ok) {
    const error = await apiResponse.json()
    throw new Error(error.error || i18n.t('auth.errors.finishWebAuthnRegistrationFailed'))
  }
}

// WebAuthn 登錄开始
export async function beginWebAuthnLogin(username: string): Promise<{
  success: boolean
  options: any
  session_key: string
}> {
  const response = await fetch(`${API_BASE}/webauthn/login/begin`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    credentials: 'include',
    body: JSON.stringify({ username }),
  })
  if (!response.ok) {
    const error = await response.json()
    throw new Error(error.error || i18n.t('auth.errors.beginWebAuthnLoginFailed'))
  }
  return response.json()
}

// WebAuthn 登錄完成（需要密碼驗证）
export async function finishWebAuthnLogin(
  username: string,
  sessionKey: string,
  response: any,
  password: string
): Promise<{ success: boolean }> {
  const apiResponse = await fetch(`${API_BASE}/webauthn/login/finish`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    credentials: 'include',
    body: JSON.stringify({
      username,
      session_key: sessionKey,
      response,
      password,
    }),
  })
  if (!apiResponse.ok) {
    const error = await apiResponse.json()
    throw new Error(error.error || i18n.t('auth.errors.finishWebAuthnLoginFailed'))
  }
  return apiResponse.json()
}

// 列出所有凭证
export async function listWebAuthnCredentials(): Promise<{
  success: boolean
  credentials: WebAuthnCredential[]
}> {
  const response = await fetch(`${API_BASE}/webauthn/credentials`, {
    credentials: 'include',
  })
  if (!response.ok) {
    throw new Error(i18n.t('auth.errors.listCredentialsFailed'))
  }
  return response.json()
}

// 刪除凭证
export async function deleteWebAuthnCredential(credentialID: string): Promise<void> {
  const response = await fetch(`${API_BASE}/webauthn/credentials/delete`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    credentials: 'include',
    body: JSON.stringify({ credential_id: credentialID }),
  })
  if (!response.ok) {
    const error = await response.json()
    throw new Error(error.error || i18n.t('auth.errors.deleteCredentialFailed'))
  }
}
