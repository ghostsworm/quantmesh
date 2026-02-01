import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react'
import { checkAuthStatus, AuthStatus } from '../services/auth'

interface AuthContextType {
  isAuthenticated: boolean
  hasPassword: boolean
  hasWebAuthn: boolean
  isLoading: boolean
  connectionError: boolean  // 新增：標识是否為网络连接錯误
  securityCompromised: boolean  // 🔒 新增：標識是否存在安全隱患（數據丟失）
  passwordManagerError: boolean  // 🔒 新增：標識密碼管理器初始化失敗
  refreshAuth: () => Promise<void>
}

const AuthContext = createContext<AuthContextType | undefined>(undefined)

export const useAuth = () => {
  const context = useContext(AuthContext)
  if (!context) {
    throw new Error('useAuth must be used within AuthProvider')
  }
  return context
}

interface AuthProviderProps {
  children: ReactNode
}

export const AuthProvider: React.FC<AuthProviderProps> = ({ children }) => {
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [hasPassword, setHasPassword] = useState(false)
  const [hasWebAuthn, setHasWebAuthn] = useState(false)
  const [isLoading, setIsLoading] = useState(true)
  const [connectionError, setConnectionError] = useState(false)
  const [securityCompromised, setSecurityCompromised] = useState(false)
  const [passwordManagerError, setPasswordManagerError] = useState(false)

  // 從 localStorage 读取上次已知的 hasPassword 状態，防止网络錯误時误判
  const getLastKnownPasswordState = (): boolean => {
    const cached = localStorage.getItem('auth_hasPassword')
    return cached === 'true'
  }

  const refreshAuth = async () => {
    try {
      setIsLoading(true)
      setConnectionError(false)
      const status = await checkAuthStatus()
      setIsAuthenticated(status.is_authenticated)
      setHasPassword(status.has_password)
      setHasWebAuthn(status.has_webauthn)
      // 🔒 檢測安全隱患
      setSecurityCompromised(status.security_compromised || false)
      // 🔒 檢測密碼管理器錯誤
      setPasswordManagerError(status.password_manager_error || false)
      // 缓存 hasPassword 状態，用於网络錯误時的回退
      localStorage.setItem('auth_hasPassword', String(status.has_password))
    } catch (error) {
      console.error('Failed to check auth status:', error)
      // 网络錯误時，不要將 hasPassword 設為 false，而是使用上次已知状態
      // 这样可以避免 WiFi 断开時錯误显示"設置密碼"界面
      setConnectionError(true)
      setIsAuthenticated(false)
      // 保持上次已知的 hasPassword 状態，而不是直接設為 false
      const lastKnownState = getLastKnownPasswordState()
      setHasPassword(lastKnownState)
      setHasWebAuthn(false)
      setSecurityCompromised(false)
      setPasswordManagerError(false)
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    refreshAuth()
  }, [])

  return (
    <AuthContext.Provider
      value={{
        isAuthenticated,
        hasPassword,
        hasWebAuthn,
        isLoading,
        connectionError,
        securityCompromised,
        passwordManagerError,
        refreshAuth,
      }}
    >
      {children}
    </AuthContext.Provider>
  )
}

