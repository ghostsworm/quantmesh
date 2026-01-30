import React, { useEffect, useState } from 'react'
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
import { getStatus, startTrading, stopTrading, getSlots, SlotsResponse, getStrategyAllocation, StrategyAllocationResponse, getPendingOrders, PendingOrdersResponse, getPositionsSummary, getStatistics } from '../services/api'
import { checkSetupStatus } from '../services/setup'
import { Alert, AlertIcon, AlertTitle, AlertDescription, useDisclosure } from '@chakra-ui/react'
import { NewbieCheckModal } from './NewbieCheckModal'

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
}

const GlassCard: React.FC<{ title?: string; children: React.ReactNode; p?: number | string }> = ({ title, children, p = 6 }) => {
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
        <Heading size="xs" color="gray.500" textTransform="uppercase" letterSpacing="widest" mb={5}>
          {title}
        </Heading>
      )}
      {children}
    </Box>
  )
}

const Dashboard: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { selectedExchange, selectedSymbol } = useSymbol()
  const [status, setStatus] = useState<SystemStatus | null>(null)
  const [statistics, setStatistics] = useState<any>(null)
  const [slotsInfo, setSlotsInfo] = useState<SlotsResponse | null>(null)
  const [strategyAllocation, setStrategyAllocation] = useState<StrategyAllocationResponse | null>(null)
  const [pendingOrders, setPendingOrders] = useState<PendingOrdersResponse | null>(null)
  const [positionsSummary, setPositionsSummary] = useState<any>(null)
  const [isTrading, setIsTrading] = useState(false)
  const [loading, setLoading] = useState(true)
  const [needsSetup, setNeedsSetup] = useState<boolean | null>(null)
  const { isOpen: isNewbieCheckOpen, onOpen: onNewbieCheckOpen, onClose: onNewbieCheckClose } = useDisclosure()
  const toast = useToast()

  const cardBg = 'white'

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

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [statusData, statsData, slotsData, allocationData, ordersData, positionsData] = await Promise.all([
          getStatus(selectedExchange || undefined, selectedSymbol || undefined),
          getStatistics(selectedExchange || undefined, selectedSymbol || undefined).catch(() => null),
          getSlots(selectedExchange || undefined, selectedSymbol || undefined).catch(() => null),
          getStrategyAllocation().catch(() => null),
          getPendingOrders().catch(() => null),
          getPositionsSummary(selectedExchange || undefined, selectedSymbol || undefined).catch(() => null),
        ])
        setStatus(statusData)
        setStatistics(statsData)
        setSlotsInfo(slotsData)
        setStrategyAllocation(allocationData)
        setPendingOrders(ordersData)
        setPositionsSummary(positionsData)
        // 只有当状态匹配当前选择的币种时才更新 isTrading
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

  const handleToggleTrading = async () => {
    try {
      if (isTrading) {
        await stopTrading(selectedExchange || undefined, selectedSymbol || undefined)
        setIsTrading(false)
        toast({ title: t('dashboard.tradingStopped'), status: 'info', borderRadius: 'full' })
      } else {
        await startTrading(selectedExchange || undefined, selectedSymbol || undefined)
        setIsTrading(true)
        toast({ title: t('dashboard.tradingStarted'), status: 'success', borderRadius: 'full' })
      }
    } catch (error) {
      toast({ title: t('dashboard.operationFailed'), description: error instanceof Error ? error.message : t('dashboard.unknownError'), status: 'error' })
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

  // 检查当前币种是否在运行
  const isCurrentSymbolRunning = status.running && 
    status.exchange?.toLowerCase() === selectedExchange?.toLowerCase() &&
    status.symbol?.toUpperCase() === selectedSymbol?.toUpperCase()
  
  // 检查当前币种是否匹配（即使未运行）
  const isCurrentSymbolMatched = status.exchange?.toLowerCase() === selectedExchange?.toLowerCase() &&
    status.symbol?.toUpperCase() === selectedSymbol?.toUpperCase()

  const currentPrice = (status.current_price && status.current_price > 0)
    ? status.current_price
    : (positionsSummary?.current_price || 0)

  const totalPnL = typeof statistics?.total_pnl === 'number' ? statistics.total_pnl : (status.total_pnl || 0)
  const totalTrades = typeof statistics?.total_trades === 'number' ? statistics.total_trades : (status.total_trades || 0)
  const totalVolume = typeof statistics?.total_volume === 'number' ? statistics.total_volume : 0
  // 移除不准确的 tradesPerHour 计算
  // 原计算使用当前进程 uptime 除以历史总交易数，重启后会导致异常高的值

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

        {/* Header Area */}
        <Flex justify="space-between" align="center" direction={{ base: 'column', md: 'row' }} gap={4}>
          <HStack spacing={4} align="center">
            <Box p={3} bg="blue.500" borderRadius="2xl" boxShadow="0 10px 15px -3px rgba(49, 130, 206, 0.3)">
              <Icon as={RepeatIcon} color="white" w={6} h={6} />
            </Box>
            <VStack align="start" spacing={0}>
              <HStack>
                <Heading size="lg" fontWeight="800">{selectedSymbol}</Heading>
                <Badge colorScheme="blue" variant="subtle" borderRadius="full" px={3}>{selectedExchange?.toUpperCase()}</Badge>
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
                新手体检
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
        <SimpleGrid columns={{ base: 1, md: 3 }} spacing={6}>
          <GlassCard>
            <Stat>
              <StatLabel fontSize="xs" fontWeight="bold" color="gray.500" mb={2}>{t('dashboard.totalPnL')}</StatLabel>
              <StatNumber fontSize="3xl" fontWeight="800" color={totalPnL >= 0 ? 'green.500' : 'red.500'}>
                {totalPnL >= 0 ? '+' : ''}{totalPnL.toFixed(2)}
                <Text as="span" fontSize="sm" ml={1} color="gray.400">USDT</Text>
              </StatNumber>
              <StatHelpText>
                <HStack spacing={1}>
                  <Icon as={totalPnL >= 0 ? TriangleUpIcon : TriangleDownIcon} />
                  <Text fontWeight="600">{((totalPnL / 1000) * 100).toFixed(2)}%</Text>
                  <Text color="gray.400">{t('dashboard.roi')}</Text>
                </HStack>
              </StatHelpText>
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
        </SimpleGrid>

        {/* Details Grid */}
        <SimpleGrid columns={{ base: 1, lg: 2 }} spacing={8}>
          {/* Positions & Allocation */}
          <VStack align="stretch" spacing={6}>
            <GlassCard title={t('dashboard.activePositions')}>
              {positionsSummary && positionsSummary.position_count > 0 ? (
                <VStack align="stretch" spacing={4}>
                  {/* 主要数据展示 */}
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

                  {/* 盈亏对比分析 */}
                  {positionsSummary.exchange_data?.has_data && positionsSummary.slot_data && (
                    <>
                      <Divider />
                      <Box bg="gray.50" p={3} borderRadius="lg">
                        <Text fontSize="xs" fontWeight="bold" color="gray.600" mb={2}>盈亏对比分析</Text>
                        <SimpleGrid columns={2} spacing={3} fontSize="xs">
                          <VStack align="start" spacing={1}>
                            <Text color="gray.500">交易所数据</Text>
                            <Text fontWeight="600" color={(positionsSummary.exchange_data.unrealized_pnl || 0) >= 0 ? 'green.600' : 'red.600'}>
                              {(positionsSummary.exchange_data.unrealized_pnl || 0) >= 0 ? '+' : ''}{positionsSummary.exchange_data.unrealized_pnl?.toFixed(2)} USDT
                            </Text>
                            <Text color="gray.400">入场: ${positionsSummary.exchange_data.entry_price?.toFixed(2)}</Text>
                            <Text color="gray.400">标记价: ${positionsSummary.exchange_data.mark_price?.toFixed(2)}</Text>
                          </VStack>
                          <VStack align="start" spacing={1}>
                            <Text color="gray.500">槽位计算</Text>
                            <Text fontWeight="600" color={(positionsSummary.slot_data.unrealized_pnl || 0) >= 0 ? 'green.600' : 'red.600'}>
                              {(positionsSummary.slot_data.unrealized_pnl || 0) >= 0 ? '+' : ''}{positionsSummary.slot_data.unrealized_pnl?.toFixed(2)} USDT
                            </Text>
                            <Text color="gray.400">均价: ${positionsSummary.slot_data.average_price?.toFixed(2)}</Text>
                            <Text color="gray.400">WS价: ${positionsSummary.slot_data.ws_price?.toFixed(2)}</Text>
                          </VStack>
                        </SimpleGrid>
                        
                        {/* 差异说明 */}
                        {positionsSummary.discrepancy && Math.abs(positionsSummary.discrepancy.pnl_diff) > 0.01 && (
                          <Box mt={2} pt={2} borderTop="1px dashed" borderColor="gray.200">
                            <HStack spacing={1} mb={1}>
                              <Icon as={WarningIcon} color="orange.400" w={3} h={3} />
                              <Text fontSize="xs" fontWeight="600" color="orange.600">
                                差异: {positionsSummary.discrepancy.pnl_diff >= 0 ? '+' : ''}{positionsSummary.discrepancy.pnl_diff.toFixed(2)} USDT
                              </Text>
                            </HStack>
                            {positionsSummary.discrepancy.reasons && positionsSummary.discrepancy.reasons.length > 0 && (
                              <VStack align="start" spacing={0.5}>
                                {positionsSummary.discrepancy.reasons.map((reason, idx) => (
                                  <Text key={idx} fontSize="10px" color="gray.500">• {reason}</Text>
                                ))}
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

            {strategyAllocation && (
              <GlassCard title={t('dashboard.capitalAllocation')}>
                <VStack spacing={4} align="stretch">
                  {Object.entries(strategyAllocation.allocation).map(([name, cap]) => (
                    <Box key={name}>
                      <Flex justify="space-between" mb={2}>
                        <Text fontSize="sm" fontWeight="bold">{name}</Text>
                        <Text fontSize="sm" fontWeight="bold">${cap.allocated.toFixed(1)}</Text>
                      </Flex>
                      <Box w="100%" h="6px" bg="gray.100" borderRadius="full" overflow="hidden">
                        <Box w={`${(cap.allocated / 500) * 100}%`} h="100%" bg="blue.500" borderRadius="full" />
                      </Box>
                    </Box>
                  ))}
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
                        <Badge colorScheme={order.side === 'BUY' ? 'green' : 'red'}>{order.side}</Badge>
                        <Text fontSize="sm" fontWeight="bold">{order.price.toFixed(2)}</Text>
                      </HStack>
                      <Text fontSize="xs" color="gray.400">{new Date(order.created_at).toLocaleTimeString()}</Text>
                    </Flex>
                  ))}
                  {pendingOrders.count > 3 && (
                    <Text fontSize="xs" color="blue.500" textAlign="center" cursor="pointer">{t('dashboard.viewAllOrders', { count: pendingOrders.count })}</Text>
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
      </VStack>
      <NewbieCheckModal isOpen={isNewbieCheckOpen} onClose={onNewbieCheckClose} />
    </Container>
  )
}

export default Dashboard
