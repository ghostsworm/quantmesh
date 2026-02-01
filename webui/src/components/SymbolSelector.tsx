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

const SymbolSelector: React.FC = () => {
  const { t } = useTranslation()
  const {
    selectedExchange,
    selectedSymbol,
    setSelectedExchange,
    setSelectedSymbol,
    clearSelection,
    isGlobalView,
  } = useSymbol()

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
  const filteredSymbols = selectedExchange
    ? symbols.filter((s) => s.exchange.toLowerCase() === selectedExchange.toLowerCase())
    : symbols

  // 分组：活跃和非活跃
  const activeSymbols = filteredSymbols.filter((s) => s.is_active)
  const inactiveSymbols = filteredSymbols.filter((s) => !s.is_active)

  const handleExchangeChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const value = e.target.value
    if (value === '') {
      clearSelection()
    } else {
      setSelectedExchange(value)
      setSelectedSymbol(null) // 清空交易對选擇
    }
  }

  const handleSymbolChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const value = e.target.value
    if (value === '') {
      setSelectedSymbol(null)
    } else {
      setSelectedSymbol(value)
    }
  }

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
          value={selectedExchange || ''}
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
          w="140px"
          value={selectedSymbol || ''}
          onChange={handleSymbolChange}
          placeholder={t('symbolSelector.selectSymbol')}
          isDisabled={!selectedExchange}
          variant="filled"
          borderRadius="md"
        >
          {activeSymbols.length > 0 && (
            <optgroup label={t('symbolSelector.running')}>
              {activeSymbols.map((sym) => (
                <option key={sym.symbol} value={sym.symbol}>
                  🟢 {sym.symbol}
                </option>
              ))}
            </optgroup>
          )}
          {inactiveSymbols.length > 0 && (
            <optgroup label={t('symbolSelector.notRunning')}>
              {inactiveSymbols.map((sym) => (
                <option key={sym.symbol} value={sym.symbol}>
                  ⚪ {sym.symbol}
                </option>
              ))}
            </optgroup>
          )}
        </Select>
      </Flex>
    </Flex>
  )
}

export default SymbolSelector

