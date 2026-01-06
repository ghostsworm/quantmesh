import React, { useEffect, useState, useMemo } from 'react'
import {
  Box,
  Container,
  Heading,
  SimpleGrid,
  Stat,
  StatLabel,
  StatNumber,
  Badge,
  Text,
  Spinner,
  Center,
  useToast,
  Flex,
  Icon,
  VStack,
  HStack,
  Tooltip,
  Accordion,
  AccordionItem,
  AccordionButton,
  AccordionPanel,
  AccordionIcon,
  Switch,
  Button,
  useColorModeValue,
} from '@chakra-ui/react'
import { 
  CheckCircleIcon, 
  WarningIcon, 
  TimeIcon, 
  RepeatIcon,
  InfoIcon,
  ChevronDownIcon,
} from '@chakra-ui/icons'
import { motion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { 
  getSymbols, 
  getSystemStatus, 
  SymbolInfo, 
  getPnLByExchange,
  ExchangePnLResponse,
  startTrading,
  stopTrading,
  closeAllPositions,
} from '../services/api'
import { useSymbol } from '../contexts/SymbolContext'
import ConfirmDialog from './ConfirmDialog'

const MotionBox = motion(Box)

interface SymbolStatus {
  running: boolean
  exchange: string
  symbol: string
  current_price: number
  total_pnl: number
  total_trades: number
}

const GlobalDashboard: React.FC = () => {
  const { t } = useTranslation()
  const [symbols, setSymbols] = useState<SymbolInfo[]>([])
  const [exchangePnL, setExchangePnL] = useState<ExchangePnLResponse[]>([])
  const [loading, setLoading] = useState(true)
  const [symbolStatuses, setSymbolStatuses] = useState<Map<string, SymbolStatus>>(new Map())
  const [closingPositions, setClosingPositions] = useState<Set<string>>(new Set())
  const [confirmDialog, setConfirmDialog] = useState<{
    isOpen: boolean
    exchange: string
    symbol: string
  } | null>(null)
  const toast = useToast()
  const { setSymbolPair } = useSymbol()

  const cardBg = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.100', 'gray.700')

  const fetchData = async () => {
    try {
      const [symbolsData, pnlData] = await Promise.all([
        getSymbols(),
        getPnLByExchange()
      ])
      
      setSymbols(symbolsData.symbols)
      setExchangePnL(pnlData.exchanges || [])
      
      const statusMap = new Map<string, SymbolStatus>()
      for (const sym of symbolsData.symbols) {
        try {
          const status = await getSystemStatus(sym.exchange, sym.symbol)
          statusMap.set(`${sym.exchange}:${sym.symbol}`, {
            running: status.running,
            exchange: sym.exchange,
            symbol: sym.symbol,
            current_price: status.current_price,
            total_pnl: status.total_pnl,
            total_trades: status.total_trades,
          })
        } catch (err) {
          console.error(`Failed to fetch status for ${sym.exchange}:${sym.symbol}`, err)
        }
      }
      setSymbolStatuses(statusMap)
      setLoading(false)
    } catch (error) {
      console.error('Failed to fetch global data', error)
      toast({
        title: '加载失败',
        description: error instanceof Error ? error.message : '未知错误',
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
    const interval = setInterval(fetchData, 15000)
    return () => clearInterval(interval)
  }, [toast])

  const summary = useMemo(() => {
    let totalPnL = 0
    let totalTrades = 0
    let activeCount = 0
    let totalVolume = 0

    exchangePnL.forEach(ex => {
      totalPnL += ex.total_pnl
      totalTrades += ex.total_trades
      totalVolume += ex.total_volume
    })

    symbolStatuses.forEach((status) => {
      if (status.running) activeCount++
    })

    return {
      totalPnL,
      totalTrades,
      activeCount,
      totalCount: symbols.length,
      totalVolume,
    }
  }, [symbols, symbolStatuses, exchangePnL])

  const handleToggleTrading = async (exchange: string, symbol: string, isRunning: boolean) => {
    try {
      if (isRunning) {
        await stopTrading(exchange, symbol)
        toast({
          title: '交易已停止',
          description: `${exchange}:${symbol}`,
          status: 'info',
          duration: 3000,
        })
      } else {
        await startTrading(exchange, symbol)
        toast({
          title: '交易已启动',
          description: `${exchange}:${symbol}`,
          status: 'success',
          duration: 3000,
        })
      }
      // 刷新数据
      setTimeout(fetchData, 1000)
    } catch (error) {
      toast({
        title: '操作失败',
        description: error instanceof Error ? error.message : '未知错误',
        status: 'error',
        duration: 5000,
      })
    }
  }

  const handleClosePositions = async (exchange: string, symbol: string) => {
    const key = `${exchange}:${symbol}`
    setClosingPositions(prev => new Set(prev).add(key))
    try {
      const result = await closeAllPositions(exchange, symbol)
      toast({
        title: '平仓完成',
        description: result.message,
        status: result.success_count > 0 ? 'success' : 'warning',
        duration: 5000,
      })
    } catch (error) {
      toast({
        title: '平仓失败',
        description: error instanceof Error ? error.message : '未知错误',
        status: 'error',
        duration: 5000,
      })
    } finally {
      setClosingPositions(prev => {
        const next = new Set(prev)
        next.delete(key)
        return next
      })
    }
  }

  const openClosePositionsDialog = (exchange: string, symbol: string) => {
    setConfirmDialog({ isOpen: true, exchange, symbol })
  }

  const closeConfirmDialog = () => {
    setConfirmDialog(null)
  }

  const confirmClosePositions = async () => {
    if (confirmDialog) {
      await handleClosePositions(confirmDialog.exchange, confirmDialog.symbol)
      setConfirmDialog(null)
    }
  }

  // 按交易所分组币种
  const symbolsByExchange = useMemo(() => {
    const map = new Map<string, SymbolInfo[]>()
    symbols.forEach(sym => {
      const exchange = sym.exchange.toLowerCase()
      if (!map.has(exchange)) {
        map.set(exchange, [])
      }
      map.get(exchange)!.push(sym)
    })
    return map
  }, [symbols])

  // 合并交易所盈亏数据和币种列表
  const exchangeData = useMemo(() => {
    const exchangeMap = new Map<string, ExchangePnLResponse & { symbolList: SymbolInfo[] }>()
    
    // 先添加盈亏数据
    exchangePnL.forEach(ex => {
      exchangeMap.set(ex.exchange.toLowerCase(), {
        ...ex,
        symbolList: [],
      })
    })

    // 添加币种列表
    symbolsByExchange.forEach((syms, exchange) => {
      if (exchangeMap.has(exchange)) {
        exchangeMap.get(exchange)!.symbolList = syms
      } else {
        exchangeMap.set(exchange, {
          exchange,
          total_pnl: 0,
          total_trades: 0,
          total_volume: 0,
          win_rate: 0,
          symbols: [],
          symbolList: syms,
        })
      }
    })

    return Array.from(exchangeMap.values())
  }, [exchangePnL, symbolsByExchange])

  if (loading) {
    return (
      <Center h="calc(100vh - 100px)">
        <VStack spacing={4}>
          <Spinner size="xl" thickness="4px" color="blue.500" speed="0.8s" />
          <Text color="gray.500" fontSize="sm" fontWeight="600">加载中...</Text>
        </VStack>
      </Center>
    )
  }

  return (
    <Box minH="100vh" py={2}>
      <VStack align="stretch" spacing={8}>
        <Flex justify="space-between" align="flex-end" px={2}>
          <Box>
            <Heading size="lg" fontWeight="800" mb={1}>概览</Heading>
            <Text color="gray.500" fontSize="sm">所有交易所和币种的盈亏情况</Text>
          </Box>
          <HStack spacing={2} display={{ base: 'none', md: 'flex' }}>
            <Badge colorScheme="green" variant="subtle" px={3} py={1} borderRadius="full">
              系统运行正常
            </Badge>
          </HStack>
        </Flex>

        {/* 汇总统计 */}
        <SimpleGrid columns={{ base: 1, md: 2, lg: 4 }} spacing={6}>
          <Box bg={cardBg} p={5} borderRadius="2xl" border="1px solid" borderColor={borderColor} boxShadow="sm">
            <Stat>
              <StatLabel color="gray.500" fontSize="xs" fontWeight="bold" textTransform="uppercase">总盈亏</StatLabel>
              <StatNumber fontSize="2xl" color={summary.totalPnL >= 0 ? 'green.500' : 'red.500'} fontWeight="800">
                {summary.totalPnL >= 0 ? '+' : ''}{summary.totalPnL.toFixed(2)}
              </StatNumber>
            </Stat>
          </Box>
          <Box bg={cardBg} p={5} borderRadius="2xl" border="1px solid" borderColor={borderColor} boxShadow="sm">
            <Stat>
              <StatLabel color="gray.500" fontSize="xs" fontWeight="bold" textTransform="uppercase">活跃币种</StatLabel>
              <StatNumber fontSize="2xl" fontWeight="800">{summary.activeCount} / {summary.totalCount}</StatNumber>
            </Stat>
          </Box>
          <Box bg={cardBg} p={5} borderRadius="2xl" border="1px solid" borderColor={borderColor} boxShadow="sm">
            <Stat>
              <StatLabel color="gray.500" fontSize="xs" fontWeight="bold" textTransform="uppercase">总交易数</StatLabel>
              <StatNumber fontSize="2xl" fontWeight="800">{summary.totalTrades}</StatNumber>
            </Stat>
          </Box>
          <Box bg={cardBg} p={5} borderRadius="2xl" border="1px solid" borderColor={borderColor} boxShadow="sm">
            <Stat>
              <StatLabel color="gray.500" fontSize="xs" fontWeight="bold" textTransform="uppercase">总交易量</StatLabel>
              <StatNumber fontSize="2xl" fontWeight="800">${summary.totalVolume.toLocaleString()}</StatNumber>
            </Stat>
          </Box>
        </SimpleGrid>

        {/* 交易所列表 */}
        <Box>
          <Heading size="md" mb={6} px={2}>交易所概览</Heading>
          <Accordion allowMultiple defaultIndex={[]}>
            {exchangeData.map((exchange) => {
              const exchangeKey = exchange.exchange.toLowerCase()
              return (
                <AccordionItem key={exchangeKey} border="none" mb={4}>
                  <AccordionButton
                    bg={cardBg}
                    borderRadius="xl"
                    border="1px solid"
                    borderColor={borderColor}
                    _hover={{ bg: useColorModeValue('gray.50', 'gray.700') }}
                    px={6}
                    py={4}
                  >
                    <Flex flex="1" justify="space-between" align="center">
                      <HStack spacing={4}>
                        <Heading size="md" fontWeight="700">{exchangeKey.toUpperCase()}</Heading>
                        <Badge colorScheme={exchange.total_pnl >= 0 ? 'green' : 'red'} variant="subtle">
                          盈亏: {exchange.total_pnl >= 0 ? '+' : ''}{exchange.total_pnl.toFixed(2)}
                        </Badge>
                        <Badge colorScheme="blue" variant="subtle">
                          交易数: {exchange.total_trades}
                        </Badge>
                        <Badge colorScheme="purple" variant="subtle">
                          交易量: ${exchange.total_volume.toLocaleString()}
                        </Badge>
                      </HStack>
                      <AccordionIcon />
                    </Flex>
                  </AccordionButton>
                  <AccordionPanel pb={4} pt={4} px={0}>
                    <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={4}>
                      {exchange.symbolList.map((sym) => {
                        const key = `${sym.exchange}:${sym.symbol}`
                        const status = symbolStatuses.get(key)
                        const isRunning = status?.running || false
                        const pnlInfo = exchange.symbols.find(s => s.symbol === sym.symbol)
                        
                        return (
                          <MotionBox
                            key={key}
                            initial={{ opacity: 0, y: 20 }}
                            animate={{ opacity: 1, y: 0 }}
                          >
                            <Box
                              bg={cardBg}
                              p={5}
                              borderRadius="xl"
                              border="1px solid"
                              borderColor={isRunning ? 'blue.400' : borderColor}
                              boxShadow="sm"
                              _hover={{ boxShadow: 'md' }}
                            >
                              <Flex justify="space-between" align="start" mb={4}>
                                <VStack align="start" spacing={1}>
                                  <HStack>
                                    <Text fontWeight="800" fontSize="lg">{sym.symbol}</Text>
                                    <Box
                                      w={2}
                                      h={2}
                                      borderRadius="full"
                                      bg={isRunning ? 'green.500' : 'gray.300'}
                                      boxShadow={isRunning ? '0 0 8px rgba(72, 187, 120, 0.6)' : 'none'}
                                    />
                                  </HStack>
                                  <Text color="gray.500" fontSize="xs">
                                    价格: ${status?.current_price?.toFixed(2) || sym.current_price.toFixed(2)}
                                  </Text>
                                </VStack>
                                <Switch
                                  isChecked={isRunning}
                                  onChange={() => handleToggleTrading(sym.exchange, sym.symbol, isRunning)}
                                  colorScheme="blue"
                                  size="md"
                                />
                              </Flex>

                              {pnlInfo && (
                                <VStack align="stretch" spacing={2} mb={4}>
                                  <HStack justify="space-between">
                                    <Text color="gray.400" fontSize="xs" fontWeight="bold">盈亏</Text>
                                    <Text 
                                      color={pnlInfo.total_pnl >= 0 ? 'green.500' : 'red.500'} 
                                      fontWeight="800" 
                                      fontSize="sm"
                                    >
                                      {pnlInfo.total_pnl >= 0 ? '+' : ''}{pnlInfo.total_pnl.toFixed(2)}
                                    </Text>
                                  </HStack>
                                  <HStack justify="space-between">
                                    <Text color="gray.400" fontSize="xs" fontWeight="bold">交易数</Text>
                                    <Text fontWeight="700" fontSize="sm">{pnlInfo.total_trades}</Text>
                                  </HStack>
                                  <HStack justify="space-between">
                                    <Text color="gray.400" fontSize="xs" fontWeight="bold">交易量</Text>
                                    <Text fontWeight="700" fontSize="sm">${pnlInfo.total_volume.toLocaleString()}</Text>
                                  </HStack>
                                  <HStack justify="space-between">
                                    <Text color="gray.400" fontSize="xs" fontWeight="bold">胜率</Text>
                                    <Text fontWeight="700" fontSize="sm">{(pnlInfo.win_rate * 100).toFixed(1)}%</Text>
                                  </HStack>
                                </VStack>
                              )}

                              {!isRunning && (
                                <Button
                                  size="sm"
                                  colorScheme="red"
                                  variant="outline"
                                  width="full"
                                  onClick={() => openClosePositionsDialog(sym.exchange, sym.symbol)}
                                  isLoading={closingPositions.has(key)}
                                  borderRadius="lg"
                                >
                                  一键平仓
                                </Button>
                              )}
                            </Box>
                          </MotionBox>
                        )
                      })}
                    </SimpleGrid>
                  </AccordionPanel>
                </AccordionItem>
              )
            })}
          </Accordion>
        </Box>
      </VStack>

      {/* 确认对话框 */}
      {confirmDialog && (
        <ConfirmDialog
          isOpen={confirmDialog.isOpen}
          onClose={closeConfirmDialog}
          onConfirm={confirmClosePositions}
          title="确认平仓"
          message={`确定要平掉 ${confirmDialog.exchange.toUpperCase()}:${confirmDialog.symbol} 的所有持仓吗？此操作不可撤销。`}
          confirmText="确认平仓"
          cancelText="取消"
          confirmColorScheme="red"
          isLoading={closingPositions.has(`${confirmDialog.exchange}:${confirmDialog.symbol}`)}
        />
      )}
    </Box>
  )
}

export default GlobalDashboard
