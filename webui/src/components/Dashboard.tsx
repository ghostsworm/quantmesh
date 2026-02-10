import React, { useEffect, useState } from 'react'
import { flushSync } from 'react-dom'
import {
  Box,
  Heading,
  SimpleGrid,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  Button,
  ButtonGroup,
  Badge,
  Text,
  Spinner,
  Center,
  useToast,
  Flex,
  VStack,
  HStack,
  Icon,
  Divider,
  Container,
  Tooltip,
} from '@chakra-ui/react'
import { 
  TriangleUpIcon, 
  TriangleDownIcon, 
  TimeIcon, 
  SettingsIcon,
  CheckCircleIcon,
  WarningIcon,
  RepeatIcon,
  InfoIcon,
} from '@chakra-ui/icons'
import { motion, AnimatePresence } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { useSymbol } from '../contexts/SymbolContext'
import { getStatus, startTrading, stopTrading, getSlots, SlotsResponse, getStrategyAllocation, StrategyAllocationResponse, getPendingOrders, PendingOrdersResponse, getPositionsSummary, getStatistics, releaseStrategyCapital, releaseAllStrategiesCapital, getSymbols } from '../services/api'
import { getStrategyRuntimeStatus } from '../services/strategy'
import StrategyVisualization from './strategy-visualization/StrategyVisualization'
import type { StrategyRuntimeStatus } from '../services/strategy'
import { checkSetupStatus } from '../services/setup'
import { Alert, AlertIcon, AlertTitle, AlertDescription, useDisclosure } from '@chakra-ui/react'
import { NewbieCheckModal } from './NewbieCheckModal'
import { trackTradingStarted } from '../services/telemetry'
import { getQuoteAsset } from '../utils/symbol'

const MotionBox = motion(Box)
const MotionFlex = motion(Flex)

interface SystemStatus {
  running: boolean
  exchange: string
  symbol: string
  current_price: number
  total_pnl: number
  total_trades: number
  risk_triggered: boolean
  uptime: number
  opening_paused?: boolean
  pause_reason?: string
}

const GlassCard: React.FC<{ title?: React.ReactNode; children: React.ReactNode; p?: number | string }> = ({ title, children, p = 6 }) => {
  const bg = 'white'
  const borderColor = 'gray.100'
  
  return (
    <Box
      bg={bg}
      p={p}
      borderRadius="3xl"
      border="1px solid"
      borderColor={borderColor}
      boxShadow="sm"
      backdropFilter="blur(20px)"
      overflow="hidden"
    >
      {title && (
        <Box mb={5}>
          {typeof title === 'string' ? (
            <Heading size="xs" color="gray.500" textTransform="uppercase" letterSpacing="widest">{title}</Heading>
          ) : (
            title
          )}
        </Box>
      )}
      {children}
    </Box>
  )
}

const Dashboard: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { selectedExchange, selectedSymbol, selectedMarketType } = useSymbol()
  const quoteAsset = getQuoteAsset(selectedSymbol)
  
  const getOrderSideText = (side: string) => {
    switch (side) {
      case 'BUY':
        return t('orders.buy')
      case 'SELL':
        return t('orders.sell')
      default:
        return side
    }
  }
  const [status, setStatus] = useState<SystemStatus | null>(null)
  const [statistics, setStatistics] = useState<any>(null)
  const [slotsInfo, setSlotsInfo] = useState<SlotsResponse | null>(null)
  const [strategyAllocation, setStrategyAllocation] = useState<StrategyAllocationResponse | null>(null)
  const [pendingOrders, setPendingOrders] = useState<PendingOrdersResponse | null>(null)
  const [positionsSummary, setPositionsSummary] = useState<any>(null)
  const [strategyRuntimeStatuses, setStrategyRuntimeStatuses] = useState<StrategyRuntimeStatus[]>([])
  const [isTrading, setIsTrading] = useState(false)
  const [loading, setLoading] = useState(true)
  const [togglePending, setTogglePending] = useState(false) // 启动/停止请求进行中，防重复点击
  const [needsSetup, setNeedsSetup] = useState<boolean | null>(null)
  const [releasingCapital, setReleasingCapital] = useState<string | null>(null)
  const [symbolDirection, setSymbolDirection] = useState<'LONG' | 'SHORT' | null>(null)
  const { isOpen: isNewbieCheckOpen, onOpen: onNewbieCheckOpen, onClose: onNewbieCheckClose } = useDisclosure()
  const toast = useToast()

  const cardBg = 'white'

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

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [statusData, statsData, slotsData, allocationData, ordersData, positionsData, runtimeStatuses] = await Promise.all([
          getStatus(selectedExchange || undefined, selectedSymbol || undefined),
          getStatistics(selectedExchange || undefined, selectedSymbol || undefined).catch(() => null),
          getSlots(selectedExchange || undefined, selectedSymbol || undefined).catch(() => null),
          getStrategyAllocation().catch(() => null),
          getPendingOrders().catch(() => null),
          getPositionsSummary(selectedExchange || undefined, selectedSymbol || undefined).catch(() => null),
          getStrategyRuntimeStatus(selectedExchange || undefined, selectedSymbol || undefined).catch(() => ({ success: true, strategies: [] })),
        ])
        setStatus(statusData)
        setStatistics(statsData)
        setSlotsInfo(slotsData)
        setStrategyAllocation(allocationData)
        setPendingOrders(ordersData)
        setPositionsSummary(positionsData)
        if (runtimeStatuses && runtimeStatuses.success) {
          setStrategyRuntimeStatuses(runtimeStatuses.strategies || [])
        }
        // 只有當状態匹配當前选擇的币种時才更新 isTrading
        const isMatched = statusData?.exchange?.toLowerCase() === selectedExchange?.toLowerCase() &&
          statusData?.symbol?.toUpperCase() === selectedSymbol?.toUpperCase()
        setIsTrading(isMatched && (statusData?.running || false))
        setLoading(false)
      } catch (error) {
        console.error('Failed to fetch data:', error)
      }
    }

    fetchData()
    const interval = setInterval(fetchData, 5000)
    return () => clearInterval(interval)
  }, [selectedExchange, selectedSymbol])

  // 獲取當前交易對的方向（做多/做空）
  useEffect(() => {
    if (!selectedExchange || !selectedSymbol) {
      setSymbolDirection(null)
      return
    }
    const loadDirection = async () => {
      try {
        const res = await getSymbols()
        const sym = res.symbols?.find(
          s => s.exchange?.toLowerCase() === selectedExchange?.toLowerCase() && s.symbol === selectedSymbol && (s.market_type ?? 'futures') === (selectedMarketType ?? 'futures')
        )
        setSymbolDirection(sym?.direction === 'SHORT' ? 'SHORT' : 'LONG')
      } catch {
        setSymbolDirection('LONG')
      }
    }
    loadDirection()
  }, [selectedExchange, selectedSymbol, selectedMarketType])

  const TOGGLE_TIMEOUT_MS = 10_000

  // 刷新策略资金分配
  const refreshStrategyAllocation = async () => {
    try {
      const data = await getStrategyAllocation()
      setStrategyAllocation(data)
    } catch (error) {
      console.error('刷新策略资金分配失败:', error)
    }
  }

  // 释放单个策略的锁定资金
  const handleReleaseCapital = async (strategyName: string) => {
    setReleasingCapital(strategyName)
    try {
      const result = await releaseStrategyCapital(strategyName)
      if (result.success) {
        toast({
          title: t('dashboard.releaseCapitalSuccess'),
          description: t('dashboard.releasedAmount', { amount: result.released.toFixed(2) }),
          status: 'success',
          duration: 3000,
        })
        await refreshStrategyAllocation()
      } else {
        toast({
          title: t('dashboard.releaseCapitalFailed'),
          description: result.message,
          status: 'error',
          duration: 5000,
        })
      }
    } catch (err) {
      toast({
        title: t('dashboard.releaseCapitalFailed'),
        description: err instanceof Error ? err.message : String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setReleasingCapital(null)
    }
  }

  // 释放所有策略的锁定资金
  const handleReleaseAllCapital = async () => {
    setReleasingCapital('all')
    try {
      const result = await releaseAllStrategiesCapital()
      if (result.success) {
        toast({
          title: t('dashboard.releaseAllCapitalSuccess'),
          description: t('dashboard.releasedTotalAmount', { amount: result.total_released.toFixed(2) }),
          status: 'success',
          duration: 3000,
        })
        await refreshStrategyAllocation()
      } else {
        toast({
          title: t('dashboard.releaseCapitalFailed'),
          description: result.message,
          status: 'error',
          duration: 5000,
        })
      }
    } catch (err) {
      toast({
        title: t('dashboard.releaseCapitalFailed'),
        description: err instanceof Error ? err.message : String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setReleasingCapital(null)
    }
  }

  const handleToggleTrading = async () => {
    if (togglePending) return
    flushSync(() => setTogglePending(true))
    const isStop = isTrading
    const timeoutPromise = new Promise<never>((_, reject) => {
      setTimeout(() => reject(new Error('TIMEOUT')), TOGGLE_TIMEOUT_MS)
    })
    const actionPromise = isStop
      ? stopTrading(selectedExchange || undefined, selectedSymbol || undefined, selectedMarketType ?? undefined)
      : startTrading(selectedExchange || undefined, selectedSymbol || undefined, selectedMarketType ?? undefined)
    try {
      await Promise.race([actionPromise, timeoutPromise])
      setIsTrading(!isStop)
      // 追踪交易启动事件
      if (!isStop) {
        trackTradingStarted(selectedExchange || undefined, selectedSymbol || undefined)
      }
      toast({
        title: isStop ? t('dashboard.tradingStopped') : t('dashboard.tradingStarted'),
        status: isStop ? 'info' : 'success',
        borderRadius: 'full',
      })
    } catch (err) {
      if (err instanceof Error && err.message === 'TIMEOUT') {
        toast({
          title: t('dashboard.startStopTimeout'),
          status: 'warning',
          duration: 5000,
          borderRadius: 'full',
        })
      } else {
        toast({
          title: t('dashboard.operationFailed'),
          description: err instanceof Error ? err.message : t('dashboard.unknownError'),
          status: 'error',
        })
      }
    } finally {
      setTogglePending(false)
    }
  }

  const formatUptime = (seconds: number) => {
    const hours = Math.floor(seconds / 3600)
    const minutes = Math.floor((seconds % 3600) / 60)
    return `${hours}h ${minutes}m`
  }

  if (loading || !status) {
    return (
      <Center h="400px">
        <Spinner size="xl" thickness="4px" color="blue.500" speed="0.8s" />
      </Center>
    )
  }

  // 检查當前币种是否在运行
  const isCurrentSymbolRunning = status.running && 
    status.exchange?.toLowerCase() === selectedExchange?.toLowerCase() &&
    status.symbol?.toUpperCase() === selectedSymbol?.toUpperCase()
  
  // 检查當前币种是否匹配（即使未运行）
  const isCurrentSymbolMatched = status.exchange?.toLowerCase() === selectedExchange?.toLowerCase() &&
    status.symbol?.toUpperCase() === selectedSymbol?.toUpperCase()

  const currentPrice = (status.current_price && status.current_price > 0)
    ? status.current_price
    : (positionsSummary?.current_price || 0)

  const totalPnL = typeof statistics?.total_pnl === 'number' ? statistics.total_pnl : (status.total_pnl || 0)
  const exchangePnL = typeof statistics?.exchange_pnl === 'number' ? statistics.exchange_pnl : 0
  const totalTrades = typeof statistics?.total_trades === 'number' ? statistics.total_trades : (status.total_trades || 0)
  const totalVolume = typeof statistics?.total_volume === 'number' ? statistics.total_volume : 0
  // 🔥 价格偏差统计
  const buyPriceDeviation = typeof statistics?.total_buy_deviation === 'number' ? statistics.total_buy_deviation : 0
  const sellPriceDeviation = typeof statistics?.total_sell_deviation === 'number' ? statistics.total_sell_deviation : 0
  // 总偏差净额：买入偏差 + 卖出偏差（正数表示净收益，负数表示净损失）
  const priceDeviationNet = buyPriceDeviation + sellPriceDeviation
  // 收益率：用分配资金作为分母，无分配资金时不显示百分比
  const totalAllocated = strategyAllocation?.allocation
    ? Object.values(strategyAllocation.allocation).reduce((sum, cap) => sum + (cap.allocated || 0), 0)
    : 0
  const roiPct = totalAllocated > 0 && isFinite(totalPnL)
    ? (totalPnL / totalAllocated) * 100
    : null

  // 当前币种杠杆（来自持仓汇总或交易所数据）
  const currentLeverage = selectedExchange && selectedSymbol
    ? (positionsSummary?.leverage ?? positionsSummary?.exchange_data?.leverage)
    : undefined

  return (
    <Container maxW="container.xl" py={4}>
      <VStack spacing={8} align="stretch">
        {/* 配置未完成提示 */}
        {needsSetup && (
          <Alert status="warning" borderRadius="xl" variant="left-accent">
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

        {/* 币种未运行提示 - 用红色警告样式，更醒目 */}
        {!needsSetup && isCurrentSymbolMatched && !status.running && (
          <Alert status="error" borderRadius="xl" variant="solid" bg="red.500">
            <AlertIcon color="white" />
            <Box flex="1">
              <AlertTitle color="white" fontSize="md" fontWeight="bold">{t('dashboard.symbolNotRunning')}</AlertTitle>
              <AlertDescription color="whiteAlpha.900">
                {t('dashboard.symbolNotRunningDescription')}
              </AlertDescription>
            </Box>
            <Button
              colorScheme="whiteAlpha"
              bg="white"
              color="red.500"
              size="sm"
              fontWeight="bold"
              _hover={{ bg: 'red.50' }}
              onClick={() => navigate('/strategy-slots')}
            >
              {t('dashboard.goToSettings')}
            </Button>
          </Alert>
        )}

        {/* 暂停开仓提示 - 橙色醒目警告 */}
        {status.opening_paused && (
          <Alert
            status="warning"
            borderRadius="xl"
            variant="left-accent"
            bg="orange.50"
            borderLeftColor="orange.400"
            cursor="pointer"
            _hover={{ bg: 'orange.100' }}
            transition="background 0.2s"
            onClick={() => navigate('/opening-control')}
          >
            <AlertIcon color="orange.500" />
            <Box flex="1">
              <AlertTitle color="orange.700" fontSize="md" fontWeight="bold">
                {t('dashboard.openingPaused')}
              </AlertTitle>
              <AlertDescription color="orange.600">
                {t('dashboard.openingPausedReason.' + (status.pause_reason || 'manual'), { defaultValue: t('dashboard.openingPausedReason.manual') })}
              </AlertDescription>
            </Box>
            <Button
              colorScheme="orange"
              size="sm"
              variant="outline"
              onClick={(e) => { e.stopPropagation(); navigate('/opening-control') }}
            >
              {t('dashboard.goToOpeningControl')}
            </Button>
          </Alert>
        )}

        {/* Header Area */}
        <Flex justify="space-between" align="center" direction={{ base: 'column', md: 'row' }} gap={4}>
          <HStack spacing={4} align="center">
            <Box p={3} bg="blue.500" borderRadius="2xl" boxShadow="0 10px 15px -3px rgba(49, 130, 206, 0.3)">
              <Icon as={RepeatIcon} color="white" w={6} h={6} />
            </Box>
            <VStack align="start" spacing={0}>
              <HStack spacing={2} flexWrap="wrap">
                <Heading size="lg" fontWeight="800">{selectedSymbol}</Heading>
                <Badge colorScheme="blue" variant="subtle" borderRadius="full" px={3}>{selectedExchange?.toUpperCase()}</Badge>
                {symbolDirection != null && (
                  <Badge colorScheme={symbolDirection === 'SHORT' ? 'orange' : 'green'} fontSize="sm">
                    {symbolDirection === 'SHORT' ? t('configuration.directionShort') : t('configuration.directionLong')}
                  </Badge>
                )}
                {currentLeverage != null && currentLeverage > 0 && (
                  <Badge colorScheme="purple" variant="outline" fontSize="sm">
                    {t('dashboard.leverage')} {t('dashboard.leverageTimes', { count: currentLeverage })}
                  </Badge>
                )}
              </HStack>
              {isCurrentSymbolRunning && currentPrice > 0 ? (
                <Text color="gray.500" fontSize="sm">{t('dashboard.currentPrice')}: <Text as="span" fontWeight="bold" color="blue.500">${currentPrice.toFixed(2)}</Text></Text>
              ) : (
                <Text color="gray.400" fontSize="sm">{t('dashboard.priceNotAvailable')}</Text>
              )}
            </VStack>
          </HStack>

          <GlassCard p={2}>
            <HStack spacing={6} px={4}>
              <Button
                size="md"
                variant="ghost"
                leftIcon={<span>🛡️</span>}
                onClick={onNewbieCheckOpen}
                borderRadius="2xl"
                fontSize="sm"
                fontWeight="800"
                color="blue.500"
                _hover={{ bg: 'blue.50', transform: 'translateY(-2px)' }}
              >
                {t('newbieCheck.button')}
              </Button>
              <Divider orientation="vertical" h="30px" />
              <VStack align="start" spacing={0}>
                <Text fontSize="10px" fontWeight="bold" color="gray.400" textTransform="uppercase">{t('dashboard.status')}</Text>
                <HStack spacing={2}>
                  <Box w={2} h={2} borderRadius="full" bg={isCurrentSymbolRunning ? 'green.500' : 'red.500'} boxShadow={isCurrentSymbolRunning ? '0 0 8px #48BB78' : 'none'} />
                  <Text fontWeight="bold" fontSize="sm">{isCurrentSymbolRunning ? t('dashboard.running') : t('dashboard.stopped')}</Text>
                </HStack>
              </VStack>
              <Divider orientation="vertical" h="30px" />
              <Button
                size="md"
                colorScheme={isCurrentSymbolRunning ? 'red' : 'green'}
                onClick={handleToggleTrading}
                isDisabled={togglePending}
                isLoading={togglePending}
                loadingText={isCurrentSymbolRunning ? t('dashboard.stopping') : t('dashboard.starting')}
                borderRadius="2xl"
                px={8}
                fontSize="sm"
                fontWeight="800"
                boxShadow={isCurrentSymbolRunning ? '0 4px 12px rgba(245, 101, 101, 0.3)' : '0 4px 12px rgba(72, 187, 120, 0.3)'}
                _hover={{ transform: 'translateY(-2px)' }}
                _active={{ transform: 'scale(0.95)' }}
              >
                {isCurrentSymbolRunning ? t('dashboard.stop') : t('dashboard.start')}
              </Button>
            </HStack>
          </GlassCard>
        </Flex>

        {/* Top Metrics Row */}
        <SimpleGrid columns={{ base: 1, md: 2, lg: 4 }} spacing={6}>
          <GlassCard>
            <Stat>
              <StatLabel fontSize="xs" fontWeight="bold" color="gray.500" mb={2}>{t('dashboard.totalPnL')}</StatLabel>
              <StatNumber fontSize="3xl" fontWeight="800" color={totalPnL >= 0 ? 'green.500' : 'red.500'}>
                {totalPnL >= 0 ? '+' : ''}{totalPnL.toFixed(2)}
                <Text as="span" fontSize="sm" ml={1} color="gray.400">{quoteAsset}</Text>
              </StatNumber>
              <StatHelpText>
                <HStack spacing={1}>
                  <Icon as={totalPnL >= 0 ? TriangleUpIcon : TriangleDownIcon} />
                  <Text fontWeight="600">
                    {roiPct !== null ? `${roiPct.toFixed(2)}%` : '—'}
                  </Text>
                  <Text color="gray.400">{t('dashboard.roi')}</Text>
                </HStack>
              </StatHelpText>
              {exchangePnL !== 0 && (
                <Tooltip label={t('dashboard.exchangePnlTooltip')} placement="top" hasArrow>
                  <Text fontSize="xs" color={exchangePnL >= 0 ? 'green.400' : 'red.400'} mt={1}>
                    {t('dashboard.exchangePnl')}: {exchangePnL >= 0 ? '+' : ''}{exchangePnL.toFixed(2)} {quoteAsset}
                  </Text>
                </Tooltip>
              )}
            </Stat>
          </GlassCard>

          <GlassCard>
            <Stat>
              <StatLabel fontSize="xs" fontWeight="bold" color="gray.500" mb={2}>{t('dashboard.tradingVolume')}</StatLabel>
              <StatNumber fontSize="3xl" fontWeight="800">{totalVolume.toFixed(4)}</StatNumber>
              <StatHelpText>
                <HStack spacing={1}>
                  <Text fontWeight="600" color="blue.500">{totalTrades}</Text>
                  <Text color="gray.400">{t('globalDashboard.tradeCountLabel')}</Text>
                </HStack>
              </StatHelpText>
            </Stat>
          </GlassCard>

          <GlassCard>
            <Stat>
              <StatLabel fontSize="xs" fontWeight="bold" color="gray.500" mb={2}>{t('dashboard.systemUptime')}</StatLabel>
              <StatNumber fontSize="3xl" fontWeight="800">{formatUptime(status.uptime)}</StatNumber>
              <StatHelpText>
                <HStack spacing={1}>
                  <Icon as={CheckCircleIcon} color="green.500" />
                  <Text fontWeight="600" color="green.500">{t('dashboard.normal')}</Text>
                  <Text color="gray.400">{t('dashboard.status')}</Text>
                </HStack>
              </StatHelpText>
            </Stat>
          </GlassCard>

          <GlassCard>
            <Stat>
              <StatLabel fontSize="xs" fontWeight="bold" color="gray.500" mb={2}>{t('dashboard.riskControlStatus')}</StatLabel>
              <StatHelpText>
                <HStack spacing={2} align="center">
                  <Box
                    w={2}
                    h={2}
                    borderRadius="full"
                    bg={status.risk_triggered ? 'red.500' : 'green.500'}
                    boxShadow={status.risk_triggered ? 'none' : '0 0 8px rgba(72, 187, 120, 0.6)'}
                  />
                  <Text fontWeight="700" color={status.risk_triggered ? 'red.500' : 'green.500'}>
                    {status.risk_triggered ? t('dashboard.riskControlPaused') : t('dashboard.riskControlNormal')}
                  </Text>
                </HStack>
              </StatHelpText>
            </Stat>
          </GlassCard>
        </SimpleGrid>

        {/* 🔥 价格偏差统计 */}
        {(Math.abs(priceDeviationNet) > 0.01 || Math.abs(buyPriceDeviation) > 0.01 || Math.abs(sellPriceDeviation) > 0.01) && (
          <GlassCard>
            <Heading size="sm" mb={4} color="gray.700">{t('dashboard.priceDeviation')}</Heading>
            <SimpleGrid columns={{ base: 1, md: 3 }} spacing={4}>
              <Box>
                <Text fontSize="xs" color="gray.500" mb={1}>{t('dashboard.buyPriceDeviation')}</Text>
                <Text fontSize="lg" fontWeight="bold" color={buyPriceDeviation >= 0 ? 'green.500' : 'red.500'}>
                  {buyPriceDeviation > 0 ? '+' : ''}{buyPriceDeviation.toFixed(2)} {quoteAsset}
                </Text>
                <Text fontSize="xs" color="gray.400" mt={1}>
                  {buyPriceDeviation < -0.01 
                    ? t('dashboard.buyPriceDeviationDescHigher')
                    : buyPriceDeviation > 0.01 
                    ? t('dashboard.buyPriceDeviationDescLower')
                    : t('dashboard.buyPriceDeviationDescNone')}
                </Text>
              </Box>
              <Box>
                <Text fontSize="xs" color="gray.500" mb={1}>{t('dashboard.sellPriceDeviation')}</Text>
                <Text fontSize="lg" fontWeight="bold" color={sellPriceDeviation >= 0 ? 'green.500' : 'red.500'}>
                  {sellPriceDeviation > 0 ? '+' : ''}{sellPriceDeviation.toFixed(2)} {quoteAsset}
                </Text>
                <Text fontSize="xs" color="gray.400" mt={1}>
                  {sellPriceDeviation < -0.01 
                    ? t('dashboard.sellPriceDeviationDescLower')
                    : sellPriceDeviation > 0.01 
                    ? t('dashboard.sellPriceDeviationDescHigher')
                    : t('dashboard.sellPriceDeviationDescNone')}
                </Text>
              </Box>
              <Box>
                <Text fontSize="xs" color="gray.500" mb={1}>{t('dashboard.totalDeviationLoss')}</Text>
                <Text fontSize="lg" fontWeight="bold" color={priceDeviationNet >= 0 ? 'green.500' : 'red.500'}>
                  {priceDeviationNet > 0 ? '+' : ''}{priceDeviationNet.toFixed(2)} {quoteAsset}
                </Text>
                <Text fontSize="xs" color="gray.400" mt={1}>
                  {priceDeviationNet < -0.01 
                    ? t('dashboard.totalDeviationDescLoss')
                    : priceDeviationNet > 0.01 
                    ? t('dashboard.totalDeviationDescGain')
                    : t('dashboard.totalDeviationDescNone')}
                </Text>
              </Box>
            </SimpleGrid>
          </GlassCard>
        )}

        {/* Details Grid */}
        <SimpleGrid columns={{ base: 1, lg: 2 }} spacing={8}>
          {/* Positions & Allocation */}
          <VStack align="stretch" spacing={6}>
            <GlassCard title={
                <Flex justify="space-between" align="center" flexWrap="wrap" gap={2}>
                  <Heading size="xs" color="gray.500" textTransform="uppercase" letterSpacing="widest">{t('dashboard.activePositions')}</Heading>
                  {positionsSummary?.exchange && positionsSummary?.symbol && (
                    <HStack spacing={2} fontSize="xs" color="gray.500" fontWeight="normal">
                      <Badge variant="subtle" colorScheme="gray">{positionsSummary.exchange.toUpperCase()}</Badge>
                      <Text>{positionsSummary.symbol}</Text>
                      <Badge variant="subtle" colorScheme="blue">{t('strategyNames.' + (positionsSummary.strategy || 'grid'), { defaultValue: positionsSummary.strategy || 'grid' })}</Badge>
                    </HStack>
                  )}
                </Flex>
              }>
              {positionsSummary && positionsSummary.position_count > 0 ? (
                <VStack align="stretch" spacing={4}>
                  {/* 主要數據展示 */}
                  <Flex justify="space-between" align="center">
                    <VStack align="start" spacing={0}>
                      <Text fontSize="xs" color="gray.500">{t('dashboard.size')}</Text>
                      <Text fontWeight="800" fontSize="xl">{positionsSummary.total_quantity?.toFixed(4)}</Text>
                    </VStack>
                    <VStack align="end" spacing={0}>
                      <Text fontSize="xs" color="gray.500">{t('dashboard.unrealizedPnL')}</Text>
                      <Text fontWeight="800" fontSize="xl" color={(positionsSummary.unrealized_pnl || 0) >= 0 ? 'green.500' : 'red.500'}>
                        {(positionsSummary.unrealized_pnl || 0) >= 0 ? '+' : ''}{positionsSummary.unrealized_pnl?.toFixed(2)}
                      </Text>
                    </VStack>
                  </Flex>
                  <Divider />
                  <Flex justify="space-between">
                    <Text fontSize="xs" color="gray.500">{t('dashboard.entryPrice')}: ${positionsSummary.average_price?.toFixed(2) || '0.00'}</Text>
                    <Text fontSize="xs" color="gray.500">{t('dashboard.value')}: ${positionsSummary.total_value?.toFixed(2)}</Text>
                  </Flex>

                  {/* 盈亏對比分析 */}
                  {positionsSummary.exchange_data?.has_data && positionsSummary.slot_data && (
                    <>
                      <Divider />
                      <Box bg="gray.50" p={3} borderRadius="lg">
                        <Text fontSize="xs" fontWeight="bold" color="gray.600" mb={2}>{t('dashboard.pnlComparison')}</Text>
                        <SimpleGrid columns={2} spacing={3} fontSize="xs">
                          <VStack align="start" spacing={1}>
                            <Text color="gray.500">{t('dashboard.exchangeData')}</Text>
                            <Text fontWeight="600" color={(positionsSummary.exchange_data.unrealized_pnl || 0) >= 0 ? 'green.600' : 'red.600'}>
                              {(positionsSummary.exchange_data.unrealized_pnl || 0) >= 0 ? '+' : ''}{positionsSummary.exchange_data.unrealized_pnl?.toFixed(2)} {quoteAsset}
                            </Text>
                            <Text color="gray.400">{t('dashboard.entry')}: ${positionsSummary.exchange_data.entry_price?.toFixed(2)}</Text>
                            <Text color="gray.400">{t('dashboard.markPrice')}: ${positionsSummary.exchange_data.mark_price?.toFixed(2)}</Text>
                          </VStack>
                          <VStack align="start" spacing={1}>
                            <Text color="gray.500">{t('dashboard.slotData')}</Text>
                            <Text fontWeight="600" color={(positionsSummary.slot_data.unrealized_pnl || 0) >= 0 ? 'green.600' : 'red.600'}>
                              {(positionsSummary.slot_data.unrealized_pnl || 0) >= 0 ? '+' : ''}{positionsSummary.slot_data.unrealized_pnl?.toFixed(2)} {quoteAsset}
                            </Text>
                            <Text color="gray.400">{t('dashboard.avgPrice')}: ${positionsSummary.slot_data.average_price?.toFixed(2)}</Text>
                            <Text color="gray.400">{t('dashboard.wsPrice')}: ${positionsSummary.slot_data.ws_price?.toFixed(2)}</Text>
                          </VStack>
                        </SimpleGrid>
                        
                        {/* 差异說明 */}
                        {positionsSummary.discrepancy && Math.abs(positionsSummary.discrepancy.pnl_diff) > 0.01 && (
                          <Box mt={2} pt={2} borderTop="1px dashed" borderColor="gray.200">
                            <HStack spacing={1} mb={1}>
                              <Icon as={WarningIcon} color="orange.400" w={3} h={3} />
                              <Text fontSize="xs" fontWeight="600" color="orange.600">
                                {t('dashboard.difference')}: {positionsSummary.discrepancy.pnl_diff >= 0 ? '+' : ''}{positionsSummary.discrepancy.pnl_diff.toFixed(2)} {quoteAsset}
                              </Text>
                            </HStack>
                            {positionsSummary.discrepancy.reasons && positionsSummary.discrepancy.reasons.length > 0 && (
                              <VStack align="start" spacing={0.5}>
                                {positionsSummary.discrepancy.reasons.map((reason, idx) => {
                                  if (typeof reason === 'string') {
                                    return <Text key={idx} fontSize="10px" color="gray.500">• {reason}</Text>
                                  }
                                  const key = `dashboard.discrepancyReasons.${reason.type}`
                                  const values: Record<string, string | number> = {}
                                  if (reason.exchange != null) values.exchange = reason.type === 'quantity_diff' ? reason.exchange.toFixed(6) : reason.exchange.toFixed(2)
                                  if (reason.slot != null) values.slot = reason.slot.toFixed(6)
                                  if (reason.slot_avg != null) values.slotAvg = reason.slot_avg.toFixed(2)
                                  if (reason.diff != null) values.diff = reason.type === 'quantity_diff' ? reason.diff.toFixed(6) : reason.diff.toFixed(2)
                                  if (reason.diff_pct != null) values.diffPct = reason.type === 'quantity_diff' ? reason.diff_pct.toFixed(2) : reason.diff_pct.toFixed(4)
                                  if (reason.mark_price != null) values.markPrice = reason.mark_price.toFixed(2)
                                  if (reason.ws_price != null) values.wsPrice = reason.ws_price.toFixed(2)
                                  if (reason.pnl_diff != null) values.pnlDiff = reason.pnl_diff.toFixed(2)
                                  return (
                                    <Text key={idx} fontSize="10px" color="gray.500">• {t(key, values)}</Text>
                                  )
                                })}
                              </VStack>
                            )}
                          </Box>
                        )}
                      </Box>
                    </>
                  )}
                </VStack>
              ) : (
                <Center h="100px">
                  <VStack spacing={2}>
                    <Icon as={InfoIcon} color="gray.300" w={6} h={6} />
                    <Text color="gray.400" fontSize="sm">{t('dashboard.noActivePositions')}</Text>
                  </VStack>
                </Center>
              )}
            </GlassCard>

            {strategyAllocation && Object.keys(strategyAllocation.allocation).length > 0 && (
              <GlassCard title={t('dashboard.capitalAllocation')}>
                <VStack spacing={4} align="stretch">
                  {Object.entries(strategyAllocation.allocation).map(([name, cap]) => {
                    const usedPct = cap.allocated > 0 ? (cap.used / cap.allocated) * 100 : 0
                    return (
                      <Box key={name} p={3} bg="gray.50" borderRadius="lg">
                        <Flex justify="space-between" mb={2} align="center">
                          <Text fontSize="sm" fontWeight="bold">{t(`strategyNames.${name}`, { defaultValue: name })}</Text>
                          <HStack spacing={2}>
                            <Badge colorScheme={usedPct > 80 ? 'red' : usedPct > 50 ? 'orange' : 'green'}>
                              {usedPct.toFixed(0)}% {t('dashboard.used')}
                            </Badge>
                            {cap.used > 0 && (
                              <Button
                                size="xs"
                                colorScheme="orange"
                                variant="ghost"
                                isLoading={releasingCapital === name}
                                onClick={() => handleReleaseCapital(name)}
                              >
                                {t('dashboard.releaseCapital')}
                              </Button>
                            )}
                          </HStack>
                        </Flex>
                        {/* 进度条显示已用/可用 */}
                        <Box w="100%" h="8px" bg="gray.200" borderRadius="full" overflow="hidden" mb={2}>
                          <Box w={`${Math.min(usedPct, 100)}%`} h="100%" bg={usedPct > 80 ? 'red.500' : usedPct > 50 ? 'orange.500' : 'green.500'} borderRadius="full" />
                        </Box>
                        {/* 详细数字 */}
                        <SimpleGrid columns={3} spacing={2} fontSize="xs">
                          <Box>
                            <Text color="gray.500">{t('dashboard.allocated')}</Text>
                            <Text fontWeight="bold">${cap.allocated.toFixed(2)}</Text>
                          </Box>
                          <Box>
                            <Text color="gray.500">{t('dashboard.reserved')}</Text>
                            <Text fontWeight="bold" color="orange.600">${cap.used.toFixed(2)}</Text>
                          </Box>
                          <Box>
                            <Text color="gray.500">{t('dashboard.available')}</Text>
                            <Text fontWeight="bold" color="green.600">${cap.available.toFixed(2)}</Text>
                          </Box>
                        </SimpleGrid>
                      </Box>
                    )
                  })}
                  {/* 释放所有策略资金按钮 */}
                  {Object.values(strategyAllocation.allocation).some(cap => cap.used > 0) && (
                    <Button
                      size="sm"
                      colorScheme="orange"
                      variant="outline"
                      isLoading={releasingCapital === 'all'}
                      onClick={handleReleaseAllCapital}
                    >
                      {t('dashboard.releaseAllCapital')}
                    </Button>
                  )}
                </VStack>
              </GlassCard>
            )}
          </VStack>

          {/* Orders */}
          <VStack align="stretch" spacing={6}>
            <GlassCard title={t('dashboard.recentActivity')}>
              {pendingOrders && pendingOrders.orders && pendingOrders.orders.length > 0 ? (
                <VStack align="stretch" spacing={3}>
                  {pendingOrders.orders.slice(0, 3).map((order, i) => (
                    <Flex key={i} justify="space-between" align="center" bg="gray.50" p={3} borderRadius="xl">
                      <HStack>
                        <Badge colorScheme={order.side === 'BUY' ? 'green' : 'red'}>{getOrderSideText(order.side)}</Badge>
                        <Text fontSize="sm" fontWeight="bold">{order.price.toFixed(2)}</Text>
                      </HStack>
                      <Text fontSize="xs" color="gray.400">{new Date(order.created_at).toLocaleTimeString()}</Text>
                    </Flex>
                  ))}
                  {pendingOrders.count > 3 && (
                    <Text 
                      fontSize="xs" 
                      color="blue.500" 
                      textAlign="center" 
                      cursor="pointer"
                      onClick={() => navigate('/orders')}
                      _hover={{ color: 'blue.600', textDecoration: 'underline' }}
                      transition="all 0.2s"
                    >
                      {t('dashboard.viewAllOrders', { count: pendingOrders.count })}
                    </Text>
                  )}
                </VStack>
              ) : (
                <Text color="gray.400" fontSize="sm" textAlign="center">{t('dashboard.noPendingOrders')}</Text>
              )}
            </GlassCard>
          </VStack>
        </SimpleGrid>

        {/* Slots Matrix - 移到底部，更紧凑的展示 */}
        <GlassCard title={t('dashboard.slotsMatrix')}>
          {slotsInfo ? (
            <SimpleGrid columns={{ base: 4, md: 8 }} spacing={3}>
              {slotsInfo.slots.map((slot, i) => (
                <HStack 
                  key={i} 
                  bg={slot.position_status === 'FILLED' ? 'green.50' : 'gray.50'} 
                  px={3}
                  py={2}
                  borderRadius="lg"
                  border="1px solid"
                  borderColor={slot.position_status === 'FILLED' ? 'green.100' : 'gray.100'}
                  spacing={2}
                  justify="center"
                >
                  <Text fontSize="10px" fontWeight="bold" color="gray.400">#{i+1}</Text>
                  <Box w={2} h={2} borderRadius="full" bg={slot.position_status === 'FILLED' ? 'green.500' : 'gray.300'} />
                </HStack>
              ))}
            </SimpleGrid>
          ) : (
            <Text color="gray.400" fontSize="sm">{t('dashboard.noSlotsInfo')}</Text>
          )}
        </GlassCard>

        {/* 策略可视化 */}
        {strategyAllocation && strategyRuntimeStatuses.length > 0 && (
          <>
            {Object.entries(strategyAllocation.allocation)
              .filter(([name, cap]) => cap.allocated > 0)
              .map(([name, cap]) => {
                const strategyStatus = strategyRuntimeStatuses.find(s => s.name === name)
                if (!strategyStatus || !strategyStatus.visualizationData) return null
                
                return (
                  <GlassCard 
                    key={name} 
                    title={`${t(`strategyNames.${name}`, { defaultValue: name })} - ${t('dashboard.strategyVisualization')}`}
                  >
                    <StrategyVisualization
                      strategy={strategyStatus}
                      exchange={selectedExchange}
                      symbol={selectedSymbol}
                    />
                  </GlassCard>
                )
              })}
          </>
        )}
      </VStack>
      <NewbieCheckModal isOpen={isNewbieCheckOpen} onClose={onNewbieCheckClose} />
    </Container>
  )
}

export default Dashboard
