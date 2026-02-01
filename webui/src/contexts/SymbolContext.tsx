import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react'

interface SymbolContextType {
  selectedExchange: string | null
  selectedSymbol: string | null
  setSelectedExchange: (exchange: string | null) => void
  setSelectedSymbol: (symbol: string | null) => void
  setSymbolPair: (exchange: string | null, symbol: string | null) => void
  clearSelection: () => void
  isGlobalView: boolean
}

const SymbolContext = createContext<SymbolContextType | undefined>(undefined)

const STORAGE_KEY_EXCHANGE = 'quantmesh_selected_exchange'
const STORAGE_KEY_SYMBOL = 'quantmesh_selected_symbol'

export const SymbolProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const [selectedExchange, setSelectedExchangeState] = useState<string | null>(() => {
    return localStorage.getItem(STORAGE_KEY_EXCHANGE)
  })
  
  const [selectedSymbol, setSelectedSymbolState] = useState<string | null>(() => {
    return localStorage.getItem(STORAGE_KEY_SYMBOL)
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

  const setSymbolPair = (exchange: string | null, symbol: string | null) => {
    setSelectedExchange(exchange)
    setSelectedSymbol(symbol)
  }

  const clearSelection = () => {
    setSelectedExchange(null)
    setSelectedSymbol(null)
  }

  const isGlobalView = !selectedExchange || !selectedSymbol

  return (
    <SymbolContext.Provider
      value={{
        selectedExchange,
        selectedSymbol,
        setSelectedExchange,
        setSelectedSymbol,
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

