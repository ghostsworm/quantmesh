import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react'

export type MarketType = 'spot' | 'futures'

interface SymbolContextType {
  selectedExchange: string | null
  selectedSymbol: string | null
  selectedMarketType: MarketType | null
  setSelectedExchange: (exchange: string | null) => void
  setSelectedSymbol: (symbol: string | null) => void
  setSelectedMarketType: (marketType: MarketType | null) => void
  setSymbolPair: (exchange: string | null, symbol: string | null, marketType?: MarketType | null) => void
  clearSelection: () => void
  isGlobalView: boolean
}

const SymbolContext = createContext<SymbolContextType | undefined>(undefined)

const STORAGE_KEY_EXCHANGE = 'quantmesh_selected_exchange'
const STORAGE_KEY_SYMBOL = 'quantmesh_selected_symbol'
const STORAGE_KEY_MARKET_TYPE = 'quantmesh_selected_market_type'

export const SymbolProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const [selectedExchange, setSelectedExchangeState] = useState<string | null>(() => {
    return localStorage.getItem(STORAGE_KEY_EXCHANGE)
  })
  
  const [selectedSymbol, setSelectedSymbolState] = useState<string | null>(() => {
    return localStorage.getItem(STORAGE_KEY_SYMBOL)
  })

  const [selectedMarketType, setSelectedMarketTypeState] = useState<MarketType | null>(() => {
    const stored = localStorage.getItem(STORAGE_KEY_MARKET_TYPE)
    if (stored === 'spot' || stored === 'futures') return stored
    return null
  })

  const setSelectedExchange = (exchange: string | null) => {
    setSelectedExchangeState(exchange)
    if (exchange) {
      localStorage.setItem(STORAGE_KEY_EXCHANGE, exchange)
    } else {
      localStorage.removeItem(STORAGE_KEY_EXCHANGE)
    }
  }

  const setSelectedSymbol = (symbol: string | null) => {
    setSelectedSymbolState(symbol)
    if (symbol) {
      localStorage.setItem(STORAGE_KEY_SYMBOL, symbol)
    } else {
      localStorage.removeItem(STORAGE_KEY_SYMBOL)
    }
  }

  const setSelectedMarketType = (marketType: MarketType | null) => {
    setSelectedMarketTypeState(marketType)
    if (marketType) {
      localStorage.setItem(STORAGE_KEY_MARKET_TYPE, marketType)
    } else {
      localStorage.removeItem(STORAGE_KEY_MARKET_TYPE)
    }
  }

  const setSymbolPair = (exchange: string | null, symbol: string | null, marketType?: MarketType | null) => {
    setSelectedExchange(exchange)
    setSelectedSymbol(symbol)
    if (marketType !== undefined) {
      setSelectedMarketType(marketType)
    }
  }

  const clearSelection = () => {
    setSelectedExchange(null)
    setSelectedSymbol(null)
    setSelectedMarketType(null)
  }

  const isGlobalView = !selectedExchange || !selectedSymbol

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

