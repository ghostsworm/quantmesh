import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react'
import { checkAuthStatus, AuthStatus } from '../services/auth'

interface AuthContextType {
  isAuthenticated: boolean
  hasPassword: boolean
  hasWebAuthn: boolean
  isLoading: boolean
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

  const refreshAuth = async () => {
    try {
      setIsLoading(true)
      const status = await checkAuthStatus()
      setIsAuthenticated(status.is_authenticated)
      setHasPassword(status.has_password)
      setHasWebAuthn(status.has_webauthn)
    } catch (error) {
      console.error('Failed to check auth status:', error)
      setIsAuthenticated(false)
      setHasPassword(false)
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
        refreshAuth,
      }}
    >
      {children}
    </AuthContext.Provider>
  )
}

