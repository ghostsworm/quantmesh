const API_BASE = '/api'

export interface AuthStatus {
  has_password: boolean
  has_webauthn: boolean
  is_authenticated: boolean
}

export interface WebAuthnCredential {
  id: string
  credential_id: string
  device_name: string
  created_at: string
  last_used_at?: string
  is_active: boolean
}

// 检查认证状态
export async function checkAuthStatus(): Promise<AuthStatus> {
  const response = await fetch(`${API_BASE}/auth/status`)
  if (!response.ok) {
    throw new Error('Failed to check auth status')
  }
  return response.json()
}

// 设置密码
export async function setPassword(password: string): Promise<void> {
  const response = await fetch(`${API_BASE}/auth/password/set`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    credentials: 'include',
    body: JSON.stringify({ password }),
  })
  if (!response.ok) {
    const error = await response.json()
    throw new Error(error.error || 'Failed to set password')
  }
}

// 验证密码
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
    throw new Error(error.error || 'Failed to verify password')
  }
}

// 退出登录
export async function logout(): Promise<void> {
  const response = await fetch(`${API_BASE}/auth/logout`, {
    method: 'POST',
    credentials: 'include',
  })
  if (!response.ok) {
    throw new Error('Failed to logout')
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
    throw new Error(error.error || 'Failed to begin WebAuthn registration')
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
    throw new Error(error.error || 'Failed to finish WebAuthn registration')
  }
}

// WebAuthn 登录开始
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
    throw new Error(error.error || 'Failed to begin WebAuthn login')
  }
  return response.json()
}

// WebAuthn 登录完成（需要密码验证）
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
    throw new Error(error.error || 'Failed to finish WebAuthn login')
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
    throw new Error('Failed to list credentials')
  }
  return response.json()
}

// 删除凭证
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
    throw new Error(error.error || 'Failed to delete credential')
  }
}

