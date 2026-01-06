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
import { useSymbol } from '../contexts/SymbolContext'
import { getSymbols, getExchanges, SymbolInfo } from '../services/api'

const SymbolSelector: React.FC = () => {
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
        console.error('获取交易对列表失败:', error)
        setLoading(false)
      }
    }

    fetchData()
    const interval = setInterval(fetchData, 30000) // 每30秒更新一次
    return () => clearInterval(interval)
  }, [])

  // 根据选中的交易所过滤交易对（忽略大小写）
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
      setSelectedSymbol(null) // 清空交易对选择
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
        <Text fontSize="sm">加载中...</Text>
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
        全局概览
      </Button>

      <Flex align="center" gap={1}>
        <Select
          size="xs"
          w="110px"
          value={selectedExchange || ''}
          onChange={handleExchangeChange}
          placeholder="选择交易所"
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
          placeholder="选择交易对"
          isDisabled={!selectedExchange}
          variant="filled"
          borderRadius="md"
        >
          {activeSymbols.length > 0 && (
            <optgroup label="运行中">
              {activeSymbols.map((sym) => (
                <option key={sym.symbol} value={sym.symbol}>
                  🟢 {sym.symbol}
                </option>
              ))}
            </optgroup>
          )}
          {inactiveSymbols.length > 0 && (
            <optgroup label="未运行">
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

