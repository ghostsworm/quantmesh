import React, { useEffect, useState, useMemo, useRef } from 'react'
import { flushSync } from 'react-dom'
import {
  Box,
  Container,
  Heading,
  SimpleGrid,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
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
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  TableContainer,
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
import { trackTradingStarted } from '../services/telemetry'
import { 
  getSymbols, 
  getSystemStatus, 
  SymbolInfo, 
  getPnLByExchange,
  ExchangePnLResponse,
  getPositionsSummaryAll,
  type PositionSummaryItem,
  startTrading,
  stopTrading,
  closeAllPositions,
  getSystemStatuses,
} from '../services/api'
import { getCapitalOverview, getCapitalHistory } from '../services/capital'
import { useSymbol } from '../contexts/SymbolContext'
import { checkSetupStatus } from '../services/setup'
import ConfirmDialog from './ConfirmDialog'

// 超时包装函数，防止API调用卡住
async function withTimeout<T>(promise: Promise<T>, timeoutMs: number, defaultValue: T): Promise<T> {
  const timeout = new Promise<never>((_, reject) =>
    setTimeout(() => reject(new Error('Timeout')), timeoutMs)
  )
  try {
    return await Promise.race([promise, timeout])
  } catch {
    return defaultValue
  }
}
import { Alert, AlertIcon, AlertTitle, AlertDescription } from '@chakra-ui/react'
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip as RechartsTooltip,
  ResponsiveContainer,
  PieChart,
  Pie,
  Cell,
  Legend,
} from 'recharts'

const MotionBox = motion(Box)

interface SymbolStatus {
  running: boolean
  exchange: string
  symbol: string
  current_price: number
  total_pnl: number
  total_trades: number
  risk_triggered?: boolean
  opening_paused?: boolean
  pause_reason?: string
}

const GlobalDashboard: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [symbols, setSymbols] = useState<SymbolInfo[]>([])
  const [exchangePnL, setExchangePnL] = useState<ExchangePnLResponse[]>([])
  const [positionsAll, setPositionsAll] = useState<PositionSummaryItem[]>([])
  const [loading, setLoading] = useState(true)
  const [symbolStatuses, setSymbolStatuses] = useState<Map<string, SymbolStatus>>(new Map())
  const [closingPositions, setClosingPositions] = useState<Set<string>>(new Set())
  const [togglePendingKeys, setTogglePendingKeys] = useState<Set<string>>(new Set())
  const [needsSetup, setNeedsSetup] = useState<boolean | null>(null)
  const isFetchingRef = useRef(false)
  const [confirmDialog, setConfirmDialog] = useState<{
    isOpen: boolean
    exchange: string
    symbol: string
  } | null>(null)
  const [expandedIndices, setExpandedIndices] = useState<number[]>([])
  const hasInitialExpandedRef = useRef(false)
  const [capitalOverview, setCapitalOverview] = useState<{ totalBalance: number; unrealizedPnL: number } | null>(null)
  const [capitalHistory, setCapitalHistory] = useState<Array<{ date: string; balance: number }>>([])
  const toast = useToast()
  const { setSymbolPair } = useSymbol()

  const cardBg = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.100', 'gray.700')
  const hoverBg = useColorModeValue('gray.50', 'gray.700')

  // 辅助函数：去掉交易所名称中的 [DryRun] 后缀
  const normalizeExchangeName = (exchangeName: string): string => {
    return exchangeName.toLowerCase().replace(/\s*\[dryrun\]\s*/gi, '').trim()
  }

  // 检查配置状態
  useEffect(() => {
    const checkConfig = async () => {
      try {
        const setupStatus = await checkSetupStatus()
        setNeedsSetup(setupStatus.needs_setup)
      } catch (error) {
        console.error('检查配置状態失败:', error)
        // 如果检查失败，不显示提示
        setNeedsSetup(false)
      }
    }
    checkConfig()
  }, [])

  const fetchData = async () => {
    // 防止輪詢重入：上一次请求还没完成就不要再发
    if (isFetchingRef.current) return
    isFetchingRef.current = true
    try {
      // 查詢所有历史數據（從2020年开始，确保包含所有交易記錄）
      const startTime = new Date('2020-01-01T00:00:00Z').toISOString()
      const endTime = new Date().toISOString()
      
      const [symbolsData, pnlData, statusesData, positionsAllData, capitalRes, historyRes] = await Promise.all([
        getSymbols(),
        getPnLByExchange(startTime, endTime),
        getSystemStatuses(),
        getPositionsSummaryAll().catch(() => ({ positions: [] })),
        // 使用超时包装，防止 capital API 卡住页面加载（5秒超时）
        withTimeout(getCapitalOverview(), 5000, null as any),
        // 使用超时包装，防止 history API 卡住页面加载（5秒超时）
        withTimeout(getCapitalHistory(30), 5000, { history: [] } as any),
      ])
      
      setSymbols(symbolsData.symbols)
      setExchangePnL(pnlData.exchanges || [])
      setPositionsAll(positionsAllData?.positions || [])
      if (capitalRes?.success && capitalRes.overview) {
        setCapitalOverview({
          totalBalance: capitalRes.overview.totalBalance,
          unrealizedPnL: capitalRes.overview.unrealizedPnL,
        })
      } else {
        setCapitalOverview(null)
      }
      const hist = historyRes?.history || []
      setCapitalHistory(
        hist
          .map((h) => ({
            date: h.timestamp?.slice(0, 10) || '',
            balance: h.totalBalance ?? 0,
          }))
          .sort((a, b) => a.date.localeCompare(b.date))
      )
      
      const statusMap = new Map<string, SymbolStatus>()
      
      // 优先使用批量状態介面（一次返回全部交易對）
      if (statusesData?.statuses?.length) {
        for (const st of statusesData.statuses) {
          const normalizedExchange = normalizeExchangeName(st.exchange || '')
          const key = `${normalizedExchange}:${st.symbol}:${st.market_type || 'futures'}`
          statusMap.set(key, {
            running: st.running,
            exchange: normalizedExchange,
            symbol: st.symbol,
            current_price: st.current_price,
            total_pnl: st.total_pnl,
            total_trades: st.total_trades,
            risk_triggered: st.risk_triggered,
            opening_paused: st.opening_paused,
            pause_reason: st.pause_reason,
          })
        }
      } else {
        // 兜底：批量接口异常時，改為並发拉單個状態，避免串行卡住
        const results = await Promise.allSettled(
          symbolsData.symbols.map(sym => getSystemStatus(sym.exchange, sym.symbol, sym.market_type))
        )
        results.forEach((res, idx) => {
          const sym = symbolsData.symbols[idx]
          if (res.status === 'fulfilled') {
            const st = res.value
            const normalizedExchange = normalizeExchangeName(sym.exchange)
            statusMap.set(`${normalizedExchange}:${sym.symbol}:${sym.market_type || 'futures'}`, {
              running: st.running,
              exchange: normalizedExchange,
              symbol: sym.symbol,
              current_price: st.current_price,
              total_pnl: st.total_pnl,
              risk_triggered: st.risk_triggered,
              total_trades: st.total_trades,
              opening_paused: st.opening_paused,
              pause_reason: st.pause_reason,
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
    // 概览價格更敏感，缩短刷新间隔
    const interval = setInterval(fetchData, 5000)
    return () => clearInterval(interval)
  }, [toast])

  const summary = useMemo(() => {
    let totalPnL = 0
    let totalTrades = 0
    let activeCount = 0
    let totalVolume = 0
    let riskTriggered = false
    let openingPausedCount = 0

    exchangePnL.forEach(ex => {
      totalPnL += ex.total_pnl
      totalTrades += ex.total_trades
      totalVolume += ex.total_volume
    })

    let normalCount = 0
    let riskTriggeredCount = 0
    let openingPausedCountDist = 0

    symbolStatuses.forEach((status) => {
      if (status.running) {
        activeCount++
        if (status.risk_triggered) riskTriggered = true
      }
      if (status.opening_paused) openingPausedCount++

      // 风险分布：风控暂停优先，其次开仓暂停，否则正常
      if (status.risk_triggered) {
        riskTriggeredCount++
      } else if (status.opening_paused) {
        openingPausedCountDist++
      } else {
        normalCount++
      }
    })

    const riskDistribution = [
      { name: 'normal', value: normalCount, color: '#38A169' },
      { name: 'riskTriggered', value: riskTriggeredCount, color: '#E53E3E' },
      { name: 'openingPaused', value: openingPausedCountDist, color: '#DD6B20' },
    ].filter((d) => d.value > 0)

    // 按市场类型分组交易对
    const spotSymbols = symbols.filter(s => s.market_type === 'spot').slice(0, 3).map(s => s.symbol)
    const futuresSymbols = symbols.filter(s => (s.market_type || 'futures') === 'futures').slice(0, 3).map(s => s.symbol)
    const spotMoreCount = symbols.filter(s => s.market_type === 'spot').length - spotSymbols.length
    const futuresMoreCount = symbols.filter(s => (s.market_type || 'futures') === 'futures').length - futuresSymbols.length

    return {
      totalPnL,
      totalTrades,
      activeCount,
      totalCount: symbols.length,
      totalVolume,
      riskTriggered,
      spotSymbols,
      futuresSymbols,
      spotMoreCount,
      futuresMoreCount,
      openingPausedCount,
      riskDistribution,
    }
  }, [symbols, symbolStatuses, exchangePnL])

  const TOGGLE_TIMEOUT_MS = 10_000

  const handleToggleTrading = async (exchange: string, symbol: string, isRunning: boolean, marketType?: string) => {
    const mt = marketType || 'futures'
    const key = `${exchange}:${symbol}:${mt}`
    const oldStatus = symbolStatuses.get(key)

    if (togglePendingKeys.has(key)) return
    flushSync(() => setTogglePendingKeys(prev => new Set(prev).add(key)))

    // 乐观更新：立即更新本地状態
    if (oldStatus) {
      setSymbolStatuses(prev => {
        const next = new Map(prev)
        const updated = { ...oldStatus, running: !isRunning }
        next.set(key, updated)
        return next
      })
    }

    const timeoutPromise = new Promise<never>((_, reject) => {
      setTimeout(() => reject(new Error('TIMEOUT')), TOGGLE_TIMEOUT_MS)
    })
    const actionPromise = isRunning
      ? stopTrading(exchange, symbol, mt)
      : startTrading(exchange, symbol, mt)

    try {
      await Promise.race([actionPromise, timeoutPromise])
      if (!isRunning) {
        trackTradingStarted(exchange, symbol)
      }
      toast({
        title: isRunning ? t('globalDashboard.tradingStopped') : t('globalDashboard.tradingStarted'),
        description: `${exchange}:${symbol}`,
        status: isRunning ? 'info' : 'success',
        duration: 3000,
      })
      setTimeout(fetchData, 1000)
    } catch (error) {
      // 如果 API 調用失败，回滚状態
      if (oldStatus) {
        setSymbolStatuses(prev => {
          const next = new Map(prev)
          next.set(key, oldStatus)
          return next
        })
      }

      if (error instanceof Error && error.message === 'TIMEOUT') {
        toast({
          title: t('dashboard.startStopTimeout'),
          status: 'warning',
          duration: 5000,
        })
      } else {
        const errorMessage = error instanceof Error ? error.message : t('globalDashboard.unknownError')
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
      }
      setTimeout(fetchData, 1000)
    } finally {
      setTogglePendingKeys(prev => {
        const next = new Set(prev)
        next.delete(key)
        return next
      })
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

  // 按交易所分组币种（去掉 [DryRun] 后缀）
  const symbolsByExchange = useMemo(() => {
    const map = new Map<string, SymbolInfo[]>()
    symbols.forEach(sym => {
      const exchange = normalizeExchangeName(sym.exchange)
      if (!map.has(exchange)) {
        map.set(exchange, [])
      }
      map.get(exchange)!.push(sym)
    })
    return map
  }, [symbols])

  // 合並交易所盈亏數據和币种列表（去掉 [DryRun] 后缀，合并显示）
  const exchangeData = useMemo(() => {
    const exchangeMap = new Map<string, ExchangePnLResponse & { symbolList: SymbolInfo[] }>()
    
    // 先添加盈亏數據（合并 [DryRun] 和正常交易所的数据）
    exchangePnL.forEach(ex => {
      const normalizedExchange = normalizeExchangeName(ex.exchange)
      if (exchangeMap.has(normalizedExchange)) {
        // 如果已存在，合并数据
        const existing = exchangeMap.get(normalizedExchange)!
        existing.total_pnl += ex.total_pnl
        existing.total_trades += ex.total_trades
        existing.total_volume += ex.total_volume
        // 合并 symbols 列表（按 symbol + market_type 去重，避免同名交易對的現貨/合約被錯誤合併）
        const symbolMap = new Map<string, any>()
        const symKey = (s: any) => `${s.symbol}:${s.market_type || 'futures'}`
        existing.symbols.forEach(s => symbolMap.set(symKey(s), s))
        ex.symbols.forEach(s => {
          const key = symKey(s)
          if (symbolMap.has(key)) {
            // 如果已存在，合并数据
            const existingSymbol = symbolMap.get(key)
            existingSymbol.total_pnl += s.total_pnl
            existingSymbol.total_trades += s.total_trades
            existingSymbol.total_volume += s.total_volume
            // 重新计算胜率
            if (existingSymbol.total_trades > 0) {
              const winningTrades = existingSymbol.total_trades * existingSymbol.win_rate + s.total_trades * s.win_rate
              existingSymbol.win_rate = winningTrades / existingSymbol.total_trades
            }
          } else {
            symbolMap.set(key, { ...s })
          }
        })
        existing.symbols = Array.from(symbolMap.values())
        // 重新计算交易所胜率（总盈利交易数 / 总交易数）
        if (existing.total_trades > 0) {
          const totalWinningTrades = existing.symbols.reduce((sum, s) => {
            return sum + Math.round(s.total_trades * s.win_rate)
          }, 0)
          existing.win_rate = totalWinningTrades / existing.total_trades
        }
      } else {
        // 新建条目，使用规范化后的交易所名称
        exchangeMap.set(normalizedExchange, {
          ...ex,
          exchange: normalizedExchange,
          symbolList: [],
        })
      }
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

  // 交易對方向映射 (exchange:symbol -> LONG|SHORT)
  const symbolDirectionMap = useMemo(() => {
    const m = new Map<string, 'LONG' | 'SHORT'>()
    symbols.forEach(s => m.set(`${s.exchange?.toLowerCase()}:${s.symbol}`, s.direction === 'SHORT' ? 'SHORT' : 'LONG'))
    return m
  }, [symbols])

  // 交易對市場類型映射 (exchange:symbol -> spot|futures)
  const symbolMarketTypeMap = useMemo(() => {
    const m = new Map<string, 'spot' | 'futures'>()
    symbols.forEach(s => {
      if (s.market_type) {
        m.set(`${s.exchange?.toLowerCase()}:${s.symbol}`, s.market_type)
      }
    })
    return m
  }, [symbols])

  // 仅在首次加载数据时默认展开所有交易所，用户手动收起后不再自动展开
  useEffect(() => {
    if (exchangeData.length > 0 && !hasInitialExpandedRef.current) {
      hasInitialExpandedRef.current = true
      setExpandedIndices(exchangeData.map((_, index) => index))
    }
  }, [exchangeData])

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
            <Badge
              colorScheme={summary.riskTriggered ? 'red' : 'green'}
              variant="subtle"
              px={3}
              py={1}
              borderRadius="full"
            >
              {summary.riskTriggered ? t('globalDashboard.riskControlPaused') : t('globalDashboard.systemRunningNormal')}
            </Badge>
          </HStack>
        </Flex>

        {/* 彙總统计 */}
        <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={6}>
          {capitalOverview != null && (
            <Box bg={cardBg} p={5} borderRadius="2xl" border="1px solid" borderColor={borderColor} boxShadow="sm">
              <Stat>
                <StatLabel color="gray.500" fontSize="xs" fontWeight="bold" textTransform="uppercase">{t('globalDashboard.totalAssets')}</StatLabel>
                <StatNumber fontSize="2xl" fontWeight="800">{capitalOverview.totalBalance.toLocaleString(undefined, { minimumFractionDigits: 2 })}</StatNumber>
                <StatHelpText color="gray.500" fontSize="xs">
                  {t('globalDashboard.unrealizedPnL')}: {capitalOverview.unrealizedPnL >= 0 ? '+' : ''}{capitalOverview.unrealizedPnL.toFixed(2)} USDT
                </StatHelpText>
              </Stat>
            </Box>
          )}
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
              <StatLabel color="gray.500" fontSize="xs" fontWeight="bold" textTransform="uppercase">{t('globalDashboard.totalVolume')}</StatLabel>
              <StatNumber fontSize="2xl" fontWeight="800">{summary.totalVolume.toLocaleString()}</StatNumber>
            </Stat>
          </Box>
          <Box bg={cardBg} p={5} borderRadius="2xl" border="1px solid" borderColor={borderColor} boxShadow="sm">
            <Stat>
              <StatLabel color="gray.500" fontSize="xs" fontWeight="bold" textTransform="uppercase">{t('globalDashboard.mainTradingSymbols')}</StatLabel>
              <VStack align="stretch" spacing={2} mt={2}>
                {summary.spotSymbols.length > 0 && (
                  <Box>
                    <Text fontSize="xs" color="gray.400" mb={1}>{t('symbolManager.spotTrading')}</Text>
                    <Text fontSize="sm" fontWeight="600" noOfLines={1}>
                      {summary.spotSymbols.join(', ')}{summary.spotMoreCount > 0 ? ` +${summary.spotMoreCount}` : ''}
                    </Text>
                  </Box>
                )}
                {summary.futuresSymbols.length > 0 && (
                  <Box>
                    <Text fontSize="xs" color="gray.400" mb={1}>{t('symbolManager.futuresTrading')}</Text>
                    <Text fontSize="sm" fontWeight="600" noOfLines={1}>
                      {summary.futuresSymbols.join(', ')}{summary.futuresMoreCount > 0 ? ` +${summary.futuresMoreCount}` : ''}
                    </Text>
                  </Box>
                )}
                {summary.spotSymbols.length === 0 && summary.futuresSymbols.length === 0 && (
                  <Text fontSize="sm" fontWeight="600">—</Text>
                )}
              </VStack>
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
              <StatLabel color="gray.500" fontSize="xs" fontWeight="bold" textTransform="uppercase">{t('globalDashboard.riskControlStatus')}</StatLabel>
              <StatHelpText mt={2}>
                <HStack spacing={2} align="center">
                  <Box
                    w={3}
                    h={3}
                    borderRadius="full"
                    bg={summary.riskTriggered ? 'red.500' : 'green.500'}
                    boxShadow={summary.riskTriggered ? 'none' : '0 0 8px rgba(72, 187, 120, 0.6)'}
                  />
                  <Text fontWeight="700" color={summary.riskTriggered ? 'red.500' : 'green.500'}>
                    {summary.riskTriggered ? t('globalDashboard.riskControlPaused') : t('globalDashboard.riskControlNormal')}
                  </Text>
                </HStack>
              </StatHelpText>
            </Stat>
          </Box>
        </SimpleGrid>

        {/* 权益曲线 */}
        {capitalHistory.length > 0 && (
          <Box bg={cardBg} p={5} borderRadius="2xl" border="1px solid" borderColor={borderColor} boxShadow="sm">
            <Text color="gray.500" fontSize="xs" fontWeight="bold" textTransform="uppercase" mb={4}>{t('globalDashboard.equityCurve')}</Text>
            <Box h="200px">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={capitalHistory} margin={{ top: 5, right: 5, left: -20, bottom: 0 }}>
                  <defs>
                    <linearGradient id="equityGradient" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#3182CE" stopOpacity={0.3} />
                      <stop offset="95%" stopColor="#3182CE" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <XAxis dataKey="date" tick={{ fontSize: 10 }} />
                  <YAxis tick={{ fontSize: 10 }} tickFormatter={(v) => v.toLocaleString()} />
                  <RechartsTooltip formatter={(v: number) => [v.toLocaleString(undefined, { minimumFractionDigits: 2 }), t('globalDashboard.totalAssets')]} />
                  <Area type="monotone" dataKey="balance" stroke="#3182CE" fill="url(#equityGradient)" strokeWidth={2} />
                </AreaChart>
              </ResponsiveContainer>
            </Box>
          </Box>
        )}

        {/* 风险分布 */}
        {summary.riskDistribution.length > 0 && (
          <Box bg={cardBg} p={5} borderRadius="2xl" border="1px solid" borderColor={borderColor} boxShadow="sm">
            <Text color="gray.500" fontSize="xs" fontWeight="bold" textTransform="uppercase" mb={4}>{t('globalDashboard.riskDistribution')}</Text>
            <Box h="200px">
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie
                    data={summary.riskDistribution}
                    dataKey="value"
                    nameKey="name"
                    cx="50%"
                    cy="50%"
                    outerRadius={70}
                    label={({ name, value }) => {
                      const labelKey = name === 'normal' ? 'riskNormal' : name === 'riskTriggered' ? 'riskTriggered' : 'openingPaused'
                      return `${t(`globalDashboard.${labelKey}`)}: ${value}`
                    }}
                  >
                    {summary.riskDistribution.map((entry, index) => (
                      <Cell key={entry.name} fill={entry.color} />
                    ))}
                  </Pie>
                  <RechartsTooltip
                    formatter={(value: number, name: string) => {
                      const labelKey = name === 'normal' ? 'riskNormal' : name === 'riskTriggered' ? 'riskTriggered' : 'openingPaused'
                      return [value, t(`globalDashboard.${labelKey}`)]
                    }}
                  />
                  <Legend
                    formatter={(value) => {
                      const labelKey = value === 'normal' ? 'riskNormal' : value === 'riskTriggered' ? 'riskTriggered' : 'openingPaused'
                      return t(`globalDashboard.${labelKey}`)
                    }}
                  />
                </PieChart>
              </ResponsiveContainer>
            </Box>
          </Box>
        )}

        {/* 暂停开仓全局提示 */}
        {summary.openingPausedCount > 0 && (
          <Alert
            status="warning"
            borderRadius="xl"
            variant="left-accent"
            bg="orange.50"
            borderLeftColor="orange.400"
            mx={2}
            cursor="pointer"
            _hover={{ bg: 'orange.100' }}
            transition="background 0.2s"
            onClick={() => navigate('/bots')}
          >
            <AlertIcon color="orange.500" />
            <Box flex="1">
              <AlertTitle color="orange.700" fontSize="md" fontWeight="bold">
                {t('globalDashboard.openingPausedAlert', { count: summary.openingPausedCount })}
              </AlertTitle>
              <AlertDescription color="orange.600">
                {t('globalDashboard.openingPausedAlertDesc')}
              </AlertDescription>
            </Box>
            <Button
              colorScheme="orange"
              size="sm"
              variant="outline"
              onClick={(e) => { e.stopPropagation(); navigate('/bots') }}
            >
              {t('dashboard.goToOpeningControl')}
            </Button>
          </Alert>
        )}

        {/* 當前持倉（按交易所、币种、策略） */}
        {positionsAll.length > 0 && (
          <Box px={2}>
            <Heading size="md" mb={4}>{t('dashboard.activePositions')}</Heading>
            <Box bg={cardBg} borderRadius="2xl" border="1px solid" borderColor={borderColor} overflow="hidden" boxShadow="sm">
              <TableContainer>
                <Table size="sm" variant="simple">
                  <Thead bg="gray.50">
                    <Tr>
                      <Th>{t('globalDashboard.positionExchange')}</Th>
                      <Th>{t('globalDashboard.positionSymbol')}</Th>
                      <Th>{t('symbolManager.marketTypeLabel')}</Th>
                      <Th>{t('configuration.direction')}</Th>
                      <Th isNumeric>{t('dashboard.leverage')}</Th>
                      <Th>{t('globalDashboard.positionStrategy')}</Th>
                      <Th isNumeric>{t('dashboard.size')}</Th>
                      <Th isNumeric>{t('dashboard.unrealizedPnL')}</Th>
                      <Th isNumeric>{t('dashboard.value')}</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {positionsAll.map((pos, i) => {
                      const marketType = symbolMarketTypeMap.get(`${pos.exchange?.toLowerCase()}:${pos.symbol}`)
                      return (
                        <Tr
                          key={`${pos.exchange}:${pos.symbol}:${i}`}
                          _hover={{ bg: hoverBg }}
                          cursor="pointer"
                          onClick={() => {
                            setSymbolPair(pos.exchange, pos.symbol)
                          }}
                        >
                          <Td fontWeight="600">{pos.exchange.toUpperCase()}</Td>
                          <Td>{pos.symbol}</Td>
                          <Td>
                            {marketType ? (
                              <Badge 
                                colorScheme={marketType === 'spot' ? 'green' : 'blue'} 
                                fontSize="xs"
                                variant="subtle"
                              >
                                {marketType === 'spot' ? t('symbolManager.spotTrading') : t('symbolManager.futuresTrading')}
                              </Badge>
                            ) : (
                              <Text fontSize="xs" color="gray.400">—</Text>
                            )}
                          </Td>
                          <Td>
                            <Badge colorScheme={(symbolDirectionMap.get(`${pos.exchange?.toLowerCase()}:${pos.symbol}`) === 'SHORT' ? 'orange' : 'green') as any} fontSize="xs">
                              {(symbolDirectionMap.get(`${pos.exchange?.toLowerCase()}:${pos.symbol}`) || 'LONG') === 'SHORT' ? t('configuration.directionShort') : t('configuration.directionLong')}
                            </Badge>
                          </Td>
                          <Td isNumeric>
                            {pos.leverage != null && pos.leverage > 0 ? t('dashboard.leverageTimes', { count: pos.leverage }) : '—'}
                          </Td>
                          <Td>
                            <Badge size="sm" colorScheme="blue" variant="subtle">
                              {t('strategyNames.' + pos.strategy, { defaultValue: pos.strategy })}
                            </Badge>
                          </Td>
                          <Td isNumeric>{pos.total_quantity?.toFixed(4)}</Td>
                          <Td isNumeric color={(pos.unrealized_pnl || 0) >= 0 ? 'green.500' : 'red.500'} fontWeight="600">
                            {(pos.unrealized_pnl || 0) >= 0 ? '+' : ''}{(pos.unrealized_pnl || 0).toFixed(2)}
                          </Td>
                          <Td isNumeric>${pos.total_value?.toFixed(2)}</Td>
                        </Tr>
                      )
                    })}
                  </Tbody>
                </Table>
              </TableContainer>
            </Box>
          </Box>
        )}

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
          <Accordion allowMultiple index={expandedIndices} onChange={(indices) => setExpandedIndices(indices as number[])}>
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
                        const normalizedExchange = normalizeExchangeName(sym.exchange)
                        const key = `${normalizedExchange}:${sym.symbol}:${sym.market_type || 'futures'}`
                        const status = symbolStatuses.get(key)
                        const isRunning = status?.running || false
                        const pnlInfo = exchange.symbols.find(s => s.symbol === sym.symbol && (s.market_type || 'futures') === (sym.market_type || 'futures'))
                        
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
                                  <HStack spacing={2} flexWrap="wrap">
                                    <Text fontWeight="800" fontSize="lg">{sym.symbol}</Text>
                                    {sym.market_type && (
                                      <Badge 
                                        colorScheme={sym.market_type === 'spot' ? 'green' : 'blue'} 
                                        fontSize="xs"
                                        variant="subtle"
                                      >
                                        {sym.market_type === 'spot' ? t('symbolManager.spotTrading') : t('symbolManager.futuresTrading')}
                                      </Badge>
                                    )}
                                    <Badge colorScheme={(sym.direction === 'SHORT' ? 'orange' : 'green') as any} fontSize="xs">
                                      {sym.direction === 'SHORT' ? t('configuration.directionShort') : t('configuration.directionLong')}
                                    </Badge>
                                    <Box
                                      w={2}
                                      h={2}
                                      borderRadius="full"
                                      bg={isRunning ? 'green.500' : 'gray.300'}
                                      boxShadow={isRunning ? '0 0 8px rgba(72, 187, 120, 0.6)' : 'none'}
                                    />
                                    {isRunning && status?.risk_triggered && (
                                      <Badge colorScheme="red" variant="solid" fontSize="10px">
                                        {t('globalDashboard.riskControlPaused')}
                                      </Badge>
                                    )}
                                    {status?.opening_paused && (
                                      <Tooltip
                                        label={t('dashboard.openingPausedReason.' + (status.pause_reason || 'manual'), { defaultValue: t('dashboard.openingPausedReason.manual') }) + ' — ' + t('dashboard.goToOpeningControl')}
                                        hasArrow
                                        placement="top"
                                      >
                                        <Badge
                                          colorScheme="orange"
                                          variant="solid"
                                          fontSize="10px"
                                          cursor="pointer"
                                          _hover={{ opacity: 0.8 }}
                                          onClick={(e) => {
                                            e.stopPropagation()
                                            setSymbolPair(normalizedExchange, sym.symbol, undefined, 'opening-control')
                                          }}
                                        >
                                          {t('openingControl.paused')}
                                        </Badge>
                                      </Tooltip>
                                    )}
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

                              {/* 主操作按钮：啟动/停止交易 */}
                              <Button
                                size="sm"
                                colorScheme={isRunning ? 'red' : 'green'}
                                width="full"
                                onClick={() => handleToggleTrading(normalizedExchange, sym.symbol, isRunning, sym.market_type)}
                                isDisabled={togglePendingKeys.has(key)}
                                isLoading={togglePendingKeys.has(key)}
                                loadingText={isRunning ? t('dashboard.stopping') : t('dashboard.starting')}
                                borderRadius="lg"
                                mb={2}
                              >
                                {isRunning ? t('globalDashboard.stopTrading') : t('globalDashboard.startTrading')}
                              </Button>

                              {/* 副操作：一键平倉（僅在交易停止時显示） */}
                              {!isRunning && (
                                <Button
                                  size="sm"
                                  colorScheme="red"
                                  variant="outline"
                                  width="full"
                                  onClick={() => openClosePositionsDialog(normalizedExchange, sym.symbol)}
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

      {/* 确认對话框 */}
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
