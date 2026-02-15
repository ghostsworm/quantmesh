import React, { useEffect, useState } from 'react'
import {
  Box,
  Flex,
  Select,
  Text,
  Badge,
  Spinner,
  Button,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import { useSymbol } from '../contexts/SymbolContext'
import { getSymbols, getExchanges, SymbolInfo } from '../services/api'

const SYMBOL_MARKET_SEP = '::'

function symbolOptionValue(symbol: string, marketType: 'spot' | 'futures') {
  return `${symbol}${SYMBOL_MARKET_SEP}${marketType}`
}

function parseSymbolOptionValue(value: string): { symbol: string; marketType: 'spot' | 'futures' } | null {
  const idx = value.indexOf(SYMBOL_MARKET_SEP)
  if (idx === -1) return null
  const symbol = value.slice(0, idx)
  const marketType = value.slice(idx + SYMBOL_MARKET_SEP.length) as 'spot' | 'futures'
  if (marketType !== 'spot' && marketType !== 'futures') return null
  return { symbol, marketType }
}

const SymbolSelector: React.FC = () => {
  const { t } = useTranslation()
  const {
    selectedExchange,
    selectedSymbol,
    selectedMarketType,
    setSymbolPair,
    clearSelection,
    isGlobalView,
  } = useSymbol()
  const [pendingExchange, setPendingExchange] = useState<string | null>(selectedExchange)

  const [symbols, setSymbols] = useState<SymbolInfo[]>([])
  const [exchanges, setExchanges] = useState<string[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [symbolsData, exchangesData] = await Promise.all([
          getSymbols(),
          getExchanges(),
        ])
        setSymbols(symbolsData.symbols)
        setExchanges(exchangesData.exchanges)
        setLoading(false)
      } catch (error) {
        console.error('獲取交易對列表失败:', error)
        setLoading(false)
      }
    }

    fetchData()
    const interval = setInterval(fetchData, 30000) // 每30秒更新一次
    return () => clearInterval(interval)
  }, [])

  // 根據选中的交易所過滤交易對（忽略大小写）
  const effectiveExchange = pendingExchange || selectedExchange
  const filteredSymbols = effectiveExchange
    ? symbols.filter((s) => s.exchange.toLowerCase() === effectiveExchange.toLowerCase())
    : symbols

  // 分组：活跃和非活跃
  const activeSymbols = filteredSymbols.filter((s) => s.is_active)
  const inactiveSymbols = filteredSymbols.filter((s) => !s.is_active)

  useEffect(() => {
    setPendingExchange(selectedExchange)
  }, [selectedExchange])

  const handleExchangeChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const value = e.target.value
    if (value === '') {
      clearSelection()
    } else {
      setPendingExchange(value)
    }
  }

  const handleSymbolChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const value = e.target.value
    if (value === '') return
    const parsed = parseSymbolOptionValue(value)
    if (parsed) {
      const ex = pendingExchange || selectedExchange
      if (ex) setSymbolPair(ex, parsed.symbol, parsed.marketType)
    }
  }

  const effectiveMarketType = selectedMarketType ?? 'futures'
  const symbolSelectValue = selectedSymbol && selectedExchange
    ? (() => {
        const composite = symbolOptionValue(selectedSymbol, effectiveMarketType)
        if (filteredSymbols.some(s => s.symbol === selectedSymbol && (s.market_type ?? 'futures') === effectiveMarketType))
          return composite
        const first = filteredSymbols.find(s => s.symbol === selectedSymbol)
        return first ? symbolOptionValue(first.symbol, first.market_type ?? 'futures') : composite
      })()
    : ''

  if (loading) {
    return (
      <Flex align="center" gap={2}>
        <Spinner size="sm" />
        <Text fontSize="sm">{t('symbolSelector.loading')}</Text>
      </Flex>
    )
  }

  return (
    <Flex align="center" gap={3}>
      <Button
        size="xs"
        variant={isGlobalView ? 'solid' : 'ghost'}
        colorScheme="blue"
        onClick={clearSelection}
        leftIcon={<span>🌐</span>}
      >
        {t('symbolSelector.globalOverview')}
      </Button>

      <Flex align="center" gap={1}>
        <Select
          size="xs"
          w="110px"
          value={effectiveExchange || ''}
          onChange={handleExchangeChange}
          placeholder={t('symbolSelector.selectExchange')}
          variant="filled"
          borderRadius="md"
        >
          {exchanges.map((ex) => (
            <option key={ex} value={ex}>
              {ex.toUpperCase()}
            </option>
          ))}
        </Select>
      </Flex>

      <Flex align="center" gap={1}>
        <Select
          size="xs"
          w="180px"
          value={symbolSelectValue}
          onChange={handleSymbolChange}
          placeholder={t('symbolSelector.selectSymbol')}
          isDisabled={!effectiveExchange}
          variant="filled"
          borderRadius="md"
        >
          {activeSymbols.length > 0 && (
            <optgroup label={t('symbolSelector.running')}>
              {activeSymbols.map((sym) => {
                const mt = sym.market_type ?? 'futures'
                const marketLabel = mt === 'spot' ? t('symbolManager.spot') : t('symbolManager.futures')
                return (
                  <option key={`${sym.symbol}::${mt}`} value={symbolOptionValue(sym.symbol, mt)}>
                    🟢 {sym.symbol} ({sym.direction === 'SHORT' ? t('configuration.directionShort') : t('configuration.directionLong')}) {marketLabel}
                  </option>
                )
              })}
            </optgroup>
          )}
          {inactiveSymbols.length > 0 && (
            <optgroup label={t('symbolSelector.notRunning')}>
              {inactiveSymbols.map((sym) => {
                const mt = sym.market_type ?? 'futures'
                const marketLabel = mt === 'spot' ? t('symbolManager.spot') : t('symbolManager.futures')
                return (
                  <option key={`${sym.symbol}::${mt}`} value={symbolOptionValue(sym.symbol, mt)}>
                    ⚪ {sym.symbol} ({sym.direction === 'SHORT' ? t('configuration.directionShort') : t('configuration.directionLong')}) {marketLabel}
                  </option>
                )
              })}
            </optgroup>
          )}
        </Select>
      </Flex>
    </Flex>
  )
}

export default SymbolSelector

