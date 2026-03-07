import React, { createContext, useContext, useState, useEffect, useCallback, ReactNode } from 'react'
import { getConfig } from '../services/config'
import { useTranslation } from 'react-i18next'
import { getDefaultTimezoneByLanguage } from '../utils/dateFormat'
import { useAuth } from './AuthContext'

interface ConfigContextType {
  timezone: string
  setTimezone: (timezone: string) => void
  loading: boolean
}

const ConfigContext = createContext<ConfigContextType | undefined>(undefined)

export const ConfigProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const { i18n } = useTranslation()
  const { isAuthenticated } = useAuth()
  const [timezone, setTimezone] = useState<string>(() => {
    // 从 localStorage 读取，如果没有则根据语言设置默认值
    const saved = localStorage.getItem('quantmesh-timezone')
    if (saved) return saved
    return getDefaultTimezoneByLanguage(i18n.language)
  })
  const [loading, setLoading] = useState(true)

  // 仅已认证时从后端加载配置，避免在 /login 页触发 getConfig() -> 401 -> 重定向循环
  useEffect(() => {
    if (!isAuthenticated) {
      setLoading(false)
      return
    }
    const loadConfig = async () => {
      try {
        const config = await getConfig()
        const tz = config.system?.timezone
        if (tz) {
          setTimezone(tz)
          localStorage.setItem('quantmesh-timezone', tz)
        }
      } catch (err) {
        console.error('Failed to load config:', err)
      } finally {
        setLoading(false)
      }
    }
    loadConfig()
  }, [isAuthenticated])

  // 语言变化时更新默认时区
  useEffect(() => {
    const saved = localStorage.getItem('quantmesh-timezone')
    if (!saved) {
      setTimezone(getDefaultTimezoneByLanguage(i18n.language))
    }
  }, [i18n.language])

  const handleSetTimezone = useCallback((tz: string) => {
    setTimezone(tz)
    localStorage.setItem('quantmesh-timezone', tz)
  }, [])

  return (
    <ConfigContext.Provider
      value={{
        timezone,
        setTimezone: handleSetTimezone,
        loading,
      }}
    >
      {children}
    </ConfigContext.Provider>
  )
}

export const useConfig = (): ConfigContextType => {
  const context = useContext(ConfigContext)
  if (context === undefined) {
    throw new Error('useConfig must be used within a ConfigProvider')
  }
  return context
}
