import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react'
import { checkAuthStatus, AuthStatus } from '../services/auth'

interface AuthContextType {
  isAuthenticated: boolean
  hasPassword: boolean
  hasWebAuthn: boolean
  isLoading: boolean
  connectionError: boolean  // 新增：标识是否为网络连接错误
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

  // 从 localStorage 读取上次已知的 hasPassword 状态，防止网络错误时误判
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
      // 缓存 hasPassword 状态，用于网络错误时的回退
      localStorage.setItem('auth_hasPassword', String(status.has_password))
    } catch (error) {
      console.error('Failed to check auth status:', error)
      // 网络错误时，不要将 hasPassword 设为 false，而是使用上次已知状态
      // 这样可以避免 WiFi 断开时错误显示"设置密码"界面
      setConnectionError(true)
      setIsAuthenticated(false)
      // 保持上次已知的 hasPassword 状态，而不是直接设为 false
      const lastKnownState = getLastKnownPasswordState()
      setHasPassword(lastKnownState)
      setHasWebAuthn(false)
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
        refreshAuth,
      }}
    >
      {children}
    </AuthContext.Provider>
  )
}

