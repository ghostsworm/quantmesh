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

  // 根据选中的交易所过滤交易对
  const filteredSymbols = selectedExchange
    ? symbols.filter((s) => s.exchange === selectedExchange)
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
    <Flex align="center" gap={4}>
      <Button
        size="sm"
        variant={isGlobalView ? 'solid' : 'outline'}
        colorScheme="blue"
        onClick={clearSelection}
      >
        全局概览
      </Button>

      <Flex align="center" gap={2}>
        <Text fontSize="sm" fontWeight="medium" whiteSpace="nowrap" color="gray.700">
          交易所:
        </Text>
        <Select
          size="sm"
          w="150px"
          value={selectedExchange || ''}
          onChange={handleExchangeChange}
          placeholder="选择交易所"
          bg="white"
          color="gray.800"
          borderColor="gray.300"
        >
          {exchanges.map((ex) => (
            <option key={ex} value={ex}>
              {ex.toUpperCase()}
            </option>
          ))}
        </Select>
      </Flex>

      <Flex align="center" gap={2}>
        <Text fontSize="sm" fontWeight="medium" whiteSpace="nowrap" color="gray.700">
          交易对:
        </Text>
        <Select
          size="sm"
          w="180px"
          value={selectedSymbol || ''}
          onChange={handleSymbolChange}
          placeholder="选择交易对"
          isDisabled={!selectedExchange}
          bg="white"
          color="gray.800"
          borderColor="gray.300"
        >
          {activeSymbols.length > 0 && (
            <optgroup label="━━ 运行中 ━━">
              {activeSymbols.map((sym) => (
                <option key={sym.symbol} value={sym.symbol}>
                  ● {sym.symbol}
                </option>
              ))}
            </optgroup>
          )}
          {inactiveSymbols.length > 0 && (
            <optgroup label="━━ 未运行 ━━">
              {inactiveSymbols.map((sym) => (
                <option key={sym.symbol} value={sym.symbol}>
                  ○ {sym.symbol}
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

