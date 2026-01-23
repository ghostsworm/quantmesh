import React, { useEffect, useState, useMemo, useRef } from 'react'
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
  AddIcon,
} from '@chakra-ui/icons'
import { motion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { 
  getSymbols, 
  getSystemStatus, 
  SymbolInfo, 
  getPnLByExchange,
  ExchangePnLResponse,
  startTrading,
  stopTrading,
  closeAllPositions,
  getSystemStatuses,
} from '../services/api'
import { useSymbol } from '../contexts/SymbolContext'
import { checkSetupStatus } from '../services/setup'
import ConfirmDialog from './ConfirmDialog'
import { Alert, AlertIcon, AlertTitle, AlertDescription } from '@chakra-ui/react'

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
  const navigate = useNavigate()
  const [symbols, setSymbols] = useState<SymbolInfo[]>([])
  const [exchangePnL, setExchangePnL] = useState<ExchangePnLResponse[]>([])
  const [loading, setLoading] = useState(true)
  const [symbolStatuses, setSymbolStatuses] = useState<Map<string, SymbolStatus>>(new Map())
  const [closingPositions, setClosingPositions] = useState<Set<string>>(new Set())
  const [needsSetup, setNeedsSetup] = useState<boolean | null>(null)
  const isFetchingRef = useRef(false)
  const [confirmDialog, setConfirmDialog] = useState<{
    isOpen: boolean
    exchange: string
    symbol: string
  } | null>(null)
  const toast = useToast()
  const { setSymbolPair } = useSymbol()

  const cardBg = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.100', 'gray.700')
  const hoverBg = useColorModeValue('gray.50', 'gray.700')

  // 检查配置状态
  useEffect(() => {
    const checkConfig = async () => {
      try {
        const setupStatus = await checkSetupStatus()
        setNeedsSetup(setupStatus.needs_setup)
      } catch (error) {
        console.error('检查配置状态失败:', error)
        // 如果检查失败，不显示提示
        setNeedsSetup(false)
      }
    }
    checkConfig()
  }, [])

  const fetchData = async () => {
    // 防止轮询重入：上一次请求还没完成就不要再发
    if (isFetchingRef.current) return
    isFetchingRef.current = true
    try {
      const [symbolsData, pnlData, statusesData] = await Promise.all([
        getSymbols(),
        getPnLByExchange(),
        getSystemStatuses(),
      ])
      
      setSymbols(symbolsData.symbols)
      setExchangePnL(pnlData.exchanges || [])
      
      const statusMap = new Map<string, SymbolStatus>()
      
      // 优先使用批量状态接口（一次返回全部交易对）
      if (statusesData?.statuses?.length) {
        for (const st of statusesData.statuses) {
          const key = `${(st.exchange || '').toLowerCase()}:${st.symbol}`
          statusMap.set(key, {
            running: st.running,
            exchange: (st.exchange || '').toLowerCase(),
            symbol: st.symbol,
            current_price: st.current_price,
            total_pnl: st.total_pnl,
            total_trades: st.total_trades,
          })
        }
      } else {
        // 兜底：批量接口异常时，改为并发拉单个状态，避免串行卡住
        const results = await Promise.allSettled(
          symbolsData.symbols.map(sym => getSystemStatus(sym.exchange, sym.symbol))
        )
        results.forEach((res, idx) => {
          const sym = symbolsData.symbols[idx]
          if (res.status === 'fulfilled') {
            const st = res.value
            statusMap.set(`${sym.exchange}:${sym.symbol}`, {
              running: st.running,
              exchange: sym.exchange,
              symbol: sym.symbol,
              current_price: st.current_price,
              total_pnl: st.total_pnl,
              total_trades: st.total_trades,
            })
          } else {
            console.error(`Failed to fetch status for ${sym.exchange}:${sym.symbol}`, res.reason)
          }
        })
      }

      setSymbolStatuses(statusMap)
      setLoading(false)
    } catch (error) {
      console.error('Failed to fetch global data', error)
      toast({
        title: t('globalDashboard.loadFailed'),
        description: error instanceof Error ? error.message : t('globalDashboard.unknownError'),
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
      setLoading(false)
    } finally {
      isFetchingRef.current = false
    }
  }

  useEffect(() => {
    fetchData()
    // 概览价格更敏感，缩短刷新间隔
    const interval = setInterval(fetchData, 5000)
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
    const key = `${exchange}:${symbol}`
    const oldStatus = symbolStatuses.get(key)
    
    // 乐观更新：立即更新本地状态
    if (oldStatus) {
      setSymbolStatuses(prev => {
        const next = new Map(prev)
        const updated = { ...oldStatus, running: !isRunning }
        next.set(key, updated)
        return next
      })
    }
    
    try {
      if (isRunning) {
        await stopTrading(exchange, symbol)
        toast({
          title: t('globalDashboard.tradingStopped'),
          description: `${exchange}:${symbol}`,
          status: 'info',
          duration: 3000,
        })
      } else {
        await startTrading(exchange, symbol)
        toast({
          title: t('globalDashboard.tradingStarted'),
          description: `${exchange}:${symbol}`,
          status: 'success',
          duration: 3000,
        })
      }
      // 刷新数据以同步后端状态
      setTimeout(fetchData, 1000)
    } catch (error) {
      // 如果 API 调用失败，回滚状态
      if (oldStatus) {
        setSymbolStatuses(prev => {
          const next = new Map(prev)
          next.set(key, oldStatus)
          return next
        })
      }
      
      const errorMessage = error instanceof Error ? error.message : t('globalDashboard.unknownError')
      
      // 如果后端返回"未运行"错误，说明状态确实未运行，更新本地状态
      const lowerErrorMessage = errorMessage.toLowerCase()
      if (lowerErrorMessage.includes('未运行') || lowerErrorMessage.includes('not running') || lowerErrorMessage.includes('is not running')) {
        if (oldStatus) {
          setSymbolStatuses(prev => {
            const next = new Map(prev)
            const updated = { ...oldStatus, running: false }
            next.set(key, updated)
            return next
          })
        }
        toast({
          title: t('globalDashboard.operationFailed'),
          description: `${exchange}:${symbol} ${t('globalDashboard.tradingStopped')}`,
          status: 'warning',
          duration: 3000,
        })
      } else {
        toast({
          title: t('globalDashboard.operationFailed'),
          description: errorMessage,
          status: 'error',
          duration: 5000,
        })
      }
      
      // 即使失败也刷新数据，确保状态同步
      setTimeout(fetchData, 1000)
    }
  }

  const handleClosePositions = async (exchange: string, symbol: string) => {
    const key = `${exchange}:${symbol}`
    setClosingPositions(prev => new Set(prev).add(key))
    try {
      const result = await closeAllPositions(exchange, symbol)
      toast({
        title: t('globalDashboard.closePositionsComplete'),
        description: result.message,
        status: result.success_count > 0 ? 'success' : 'warning',
        duration: 5000,
      })
    } catch (error) {
      toast({
        title: t('globalDashboard.closePositionsFailed'),
        description: error instanceof Error ? error.message : t('globalDashboard.unknownError'),
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

    // 添加币种列表（按字母顺序排序）
    symbolsByExchange.forEach((syms, exchange) => {
      // 按 symbol 字段字母顺序排序
      const sortedSyms = [...syms].sort((a, b) => a.symbol.localeCompare(b.symbol))
      if (exchangeMap.has(exchange)) {
        exchangeMap.get(exchange)!.symbolList = sortedSyms
      } else {
        exchangeMap.set(exchange, {
          exchange,
          total_pnl: 0,
          total_trades: 0,
          total_volume: 0,
          win_rate: 0,
          symbols: [],
          symbolList: sortedSyms,
        })
      }
    })

    return Array.from(exchangeMap.values()).sort((a, b) => 
      a.exchange.localeCompare(b.exchange)
    )
  }, [exchangePnL, symbolsByExchange])

  if (loading) {
    return (
      <Center h="calc(100vh - 100px)">
        <VStack spacing={4}>
          <Spinner size="xl" thickness="4px" color="blue.500" speed="0.8s" />
          <Text color="gray.500" fontSize="sm" fontWeight="600">{t('globalDashboard.loading')}</Text>
        </VStack>
      </Center>
    )
  }

  return (
    <Box minH="100vh" py={2}>
      <VStack align="stretch" spacing={8}>
        {/* 配置未完成提示 */}
        {needsSetup && (
          <Alert status="warning" borderRadius="xl" variant="left-accent" mx={2}>
            <AlertIcon />
            <Box flex="1">
              <AlertTitle>{t('dashboard.setupIncomplete')}</AlertTitle>
              <AlertDescription>
                {t('dashboard.setupIncompleteDescription')}
              </AlertDescription>
            </Box>
            <Button
              colorScheme="blue"
              size="sm"
              onClick={() => {
                sessionStorage.setItem('wizard_step', 'pending')
                navigate('/wizard')
              }}
            >
              {t('dashboard.openSetupWizard')}
            </Button>
          </Alert>
        )}

        <Flex justify="space-between" align="flex-end" px={2}>
          <Box>
            <Heading size="lg" fontWeight="800" mb={1}>{t('globalDashboard.overview')}</Heading>
            <Text color="gray.500" fontSize="sm">{t('globalDashboard.overviewDescription')}</Text>
          </Box>
          <HStack spacing={2} display={{ base: 'none', md: 'flex' }}>
            <Badge colorScheme="green" variant="subtle" px={3} py={1} borderRadius="full">
              {t('globalDashboard.systemRunningNormal')}
            </Badge>
          </HStack>
        </Flex>

        {/* 汇总统计 */}
        <SimpleGrid columns={{ base: 1, md: 2, lg: 4 }} spacing={6}>
          <Box bg={cardBg} p={5} borderRadius="2xl" border="1px solid" borderColor={borderColor} boxShadow="sm">
            <Stat>
              <StatLabel color="gray.500" fontSize="xs" fontWeight="bold" textTransform="uppercase">{t('globalDashboard.totalPnL')}</StatLabel>
              <StatNumber fontSize="2xl" color={summary.totalPnL >= 0 ? 'green.500' : 'red.500'} fontWeight="800">
                {summary.totalPnL >= 0 ? '+' : ''}{summary.totalPnL.toFixed(2)}
              </StatNumber>
            </Stat>
          </Box>
          <Box bg={cardBg} p={5} borderRadius="2xl" border="1px solid" borderColor={borderColor} boxShadow="sm">
            <Stat>
              <StatLabel color="gray.500" fontSize="xs" fontWeight="bold" textTransform="uppercase">{t('globalDashboard.activeSymbols')}</StatLabel>
              <StatNumber fontSize="2xl" fontWeight="800">{summary.activeCount} / {summary.totalCount}</StatNumber>
            </Stat>
          </Box>
          <Box bg={cardBg} p={5} borderRadius="2xl" border="1px solid" borderColor={borderColor} boxShadow="sm">
            <Stat>
              <StatLabel color="gray.500" fontSize="xs" fontWeight="bold" textTransform="uppercase">{t('globalDashboard.totalTrades')}</StatLabel>
              <StatNumber fontSize="2xl" fontWeight="800">{summary.totalTrades}</StatNumber>
            </Stat>
          </Box>
          <Box bg={cardBg} p={5} borderRadius="2xl" border="1px solid" borderColor={borderColor} boxShadow="sm">
            <Stat>
              <StatLabel color="gray.500" fontSize="xs" fontWeight="bold" textTransform="uppercase">{t('globalDashboard.totalVolume')}</StatLabel>
              <StatNumber fontSize="2xl" fontWeight="800">{summary.totalVolume.toLocaleString()}</StatNumber>
            </Stat>
          </Box>
        </SimpleGrid>

        {/* 交易所列表 */}
        <Box>
          <Flex justify="space-between" align="center" mb={6} px={2}>
            <Heading size="md">{t('globalDashboard.exchangeOverview')}</Heading>
            <Button
              leftIcon={<AddIcon />}
              colorScheme="blue"
              size="sm"
              onClick={() => navigate('/config')}
            >
              {t('globalDashboard.addSymbol')}
            </Button>
          </Flex>
          <Accordion allowMultiple defaultIndex={exchangeData.map((_, index) => index)}>
            {exchangeData.map((exchange) => {
              const exchangeKey = exchange.exchange.toLowerCase()
              return (
                <AccordionItem key={exchangeKey} border="none" mb={4}>
                  <AccordionButton
                    bg={cardBg}
                    borderRadius="xl"
                    border="1px solid"
                    borderColor={borderColor}
                    _hover={{ bg: hoverBg }}
                    px={6}
                    py={4}
                  >
                    <Flex flex="1" justify="space-between" align="center">
                      <HStack spacing={4}>
                        <Heading size="md" fontWeight="700">{exchangeKey.toUpperCase()}</Heading>
                        <Badge colorScheme={exchange.total_pnl >= 0 ? 'green' : 'red'} variant="subtle">
                          {t('globalDashboard.pnlLabel')} {exchange.total_pnl >= 0 ? '+' : ''}{exchange.total_pnl.toFixed(2)}
                        </Badge>
                        <Badge colorScheme="blue" variant="subtle">
                          {t('globalDashboard.tradeCountBadge')} {exchange.total_trades}
                        </Badge>
                        <Badge colorScheme="purple" variant="subtle">
                          {t('globalDashboard.volumeBadge')} {exchange.total_volume.toLocaleString()}
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
                                    {t('globalDashboard.price')}: ${status?.current_price?.toFixed(2) || sym.current_price.toFixed(2)}
                                  </Text>
                                </VStack>
                              </Flex>

                              {pnlInfo && (
                                <VStack align="stretch" spacing={2} mb={4}>
                                  <HStack justify="space-between">
                                    <Text color="gray.400" fontSize="xs" fontWeight="bold">{t('globalDashboard.pnl')}</Text>
                                    <Text 
                                      color={pnlInfo.total_pnl >= 0 ? 'green.500' : 'red.500'} 
                                      fontWeight="800" 
                                      fontSize="sm"
                                    >
                                      {pnlInfo.total_pnl >= 0 ? '+' : ''}{pnlInfo.total_pnl.toFixed(2)}
                                    </Text>
                                  </HStack>
                                  <HStack justify="space-between">
                                    <Text color="gray.400" fontSize="xs" fontWeight="bold">{t('globalDashboard.tradeCountLabel')}</Text>
                                    <Text fontWeight="700" fontSize="sm">{pnlInfo.total_trades}</Text>
                                  </HStack>
                                  <HStack justify="space-between">
                                    <Text color="gray.400" fontSize="xs" fontWeight="bold">{t('globalDashboard.volume')}</Text>
                                    <Text fontWeight="700" fontSize="sm">{pnlInfo.total_volume.toLocaleString()}</Text>
                                  </HStack>
                                  <HStack justify="space-between">
                                    <Text color="gray.400" fontSize="xs" fontWeight="bold">{t('globalDashboard.winRateLabel')}</Text>
                                    <Text fontWeight="700" fontSize="sm">{(pnlInfo.win_rate * 100).toFixed(1)}%</Text>
                                  </HStack>
                                </VStack>
                              )}

                              {/* 主操作按钮：启动/停止交易 */}
                              <Button
                                size="sm"
                                colorScheme={isRunning ? 'red' : 'green'}
                                width="full"
                                onClick={() => handleToggleTrading(sym.exchange, sym.symbol, isRunning)}
                                borderRadius="lg"
                                mb={2}
                              >
                                {isRunning ? t('globalDashboard.stopTrading') : t('globalDashboard.startTrading')}
                              </Button>

                              {/* 副操作：一键平仓（仅在交易停止时显示） */}
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
                                  {t('globalDashboard.closeAllPositions')}
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
          title={t('globalDashboard.confirmClosePositions')}
          message={t('globalDashboard.confirmClosePositionsMessage', { exchange: confirmDialog.exchange.toUpperCase(), symbol: confirmDialog.symbol })}
          confirmText={t('globalDashboard.confirmClosePositions')}
          cancelText={t('common.cancel')}
          confirmColorScheme="red"
          isLoading={closingPositions.has(`${confirmDialog.exchange}:${confirmDialog.symbol}`)}
        />
      )}
    </Box>
  )
}

export default GlobalDashboard
