import React, { createContext, useContext, useCallback, ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { useBot } from './BotContext'
import { getBots } from '../services/api'

export type MarketType = 'spot' | 'futures'

interface SymbolContextType {
  selectedExchange: string | null
  selectedSymbol: string | null
  selectedMarketType: MarketType | null
  setSelectedExchange: (exchange: string | null) => void
  setSelectedSymbol: (symbol: string | null) => void
  setSelectedMarketType: (marketType: MarketType | null) => void
  setSymbolPair: (exchange: string | null, symbol: string | null, marketType?: MarketType | null, subPath?: string) => void
  clearSelection: () => void
  isGlobalView: boolean
  /** 通过 botId 直接导航到 bot 工作区 */
  navigateToBot: (botId: string, subPath?: string) => void
}

const SymbolContext = createContext<SymbolContextType | undefined>(undefined)

export const SymbolProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const navigate = useNavigate()
  const { botId, exchange, symbol, marketType } = useBot()

  // 完全由 URL 决定，不再使用 localStorage
  const selectedExchange = exchange
  const selectedSymbol = symbol
  const selectedMarketType = marketType
  const isGlobalView = !botId

  const navigateToBot = useCallback(
    (id: string, subPath = 'dashboard') => {
      navigate(`/bots/${id}/${subPath}`)
    },
    [navigate]
  )

  const clearSelection = useCallback(() => {
    navigate('/bots')
  }, [navigate])

  const setSymbolPair = useCallback(
    async (ex: string | null, sym: string | null, _mt?: MarketType | null, subPath = 'dashboard') => {
      if (!ex || !sym) {
        navigate('/bots')
        return
      }
      try {
        const { bots } = await getBots()
        const match = bots?.find(
          (b) =>
            b.exchange?.toLowerCase() === ex.toLowerCase() &&
            b.symbol?.toUpperCase() === sym.toUpperCase()
        )
        if (match) {
          navigate(`/bots/${match.bot_id}/${subPath}`)
        } else {
          navigate('/bots')
        }
      } catch {
        navigate('/bots')
      }
    },
    [navigate]
  )

  // 以下为兼容旧 API，实际通过 setSymbolPair 或 navigateToBot 操作
  const setSelectedExchange = useCallback(() => {}, [])
  const setSelectedSymbol = useCallback(() => {}, [])
  const setSelectedMarketType = useCallback(() => {}, [])

  return (
    <SymbolContext.Provider
      value={{
        selectedExchange,
        selectedSymbol,
        selectedMarketType,
        setSelectedExchange,
        setSelectedSymbol,
        setSelectedMarketType,
        setSymbolPair,
        clearSelection,
        isGlobalView,
        navigateToBot,
      }}
    >
      {children}
    </SymbolContext.Provider>
  )
}

export const useSymbol = (): SymbolContextType => {
  const context = useContext(SymbolContext)
  if (context === undefined) {
    throw new Error('useSymbol must be used within a SymbolProvider')
  }
  return context
}
