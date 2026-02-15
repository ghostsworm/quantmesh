import React, { createContext, useContext, useEffect, useState, ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { getBotById, BotDetailInfo } from '../services/api'

export type MarketType = 'spot' | 'futures'

interface BotContextValue {
  botId: string | null
  bot: BotDetailInfo | null
  exchange: string | null
  symbol: string | null
  marketType: MarketType | null
  loading: boolean
  error: Error | null
}

const BotContext = createContext<BotContextValue | undefined>(undefined)

/** 非 bot 的路径段（不应解析为 botId） */
const RESERVED_BOT_PATH_SEGMENTS = ['create']

/**
 * 从 URL 路径解析 botId，例如 /bots/xxx 或 /bots/xxx/positions
 * 排除 /bots/create 等保留路径，这些场景下不显示 bot 相关菜单
 */
function parseBotIdFromPath(pathname: string): string | null {
  const match = pathname.match(/^\/bots\/([^/]+)(?:\/|$)/)
  if (!match) return null
  const segment = match[1]
  if (RESERVED_BOT_PATH_SEGMENTS.includes(segment.toLowerCase())) return null
  return segment
}

/**
 * 判断是否为 bot 相关路径（排除 /bots 列表和 /bots/create）
 */
function isBotRoute(pathname: string): boolean {
  if (pathname === '/bots' || pathname === '/bots/create') return false
  return pathname.startsWith('/bots/')
}

export const BotProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const location = useLocation()
  const pathBotId = parseBotIdFromPath(location.pathname)
  const [bot, setBot] = useState<BotDetailInfo | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<Error | null>(null)

  useEffect(() => {
    if (!pathBotId || !isBotRoute(location.pathname)) {
      setBot(null)
      setError(null)
      return
    }
    let cancelled = false
    setLoading(true)
    setError(null)
    getBotById(pathBotId)
      .then((data) => {
        if (!cancelled) {
          setBot(data)
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err)
          setBot(null)
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [pathBotId, location.pathname])

  const value: BotContextValue = {
    botId: pathBotId,
    bot,
    exchange: bot?.exchange ?? null,
    symbol: bot?.symbol ?? null,
    marketType: (bot?.market_type as MarketType) ?? null,
    loading,
    error,
  }

  return <BotContext.Provider value={value}>{children}</BotContext.Provider>
}

export function useBot(): BotContextValue {
  const context = useContext(BotContext)
  if (context === undefined) {
    throw new Error('useBot must be used within a BotProvider')
  }
  return context
}

/**
 * 是否处于 bot 工作区（URL 中有 botId 且非列表/创建页）
 */
export function useIsInBotWorkspace(): boolean {
  const { botId } = useBot()
  return !!botId
}
