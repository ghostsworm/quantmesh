import React, { useEffect, useState, useCallback } from 'react'
import {
  Box,
  VStack,
  HStack,
  Heading,
  Text,
  Badge,
  SimpleGrid,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  Spinner,
  Center,
  useColorModeValue,
  Icon,
  Tooltip,
  Collapse,
  Button,
  Divider,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  TableContainer,
  Alert,
  AlertIcon,
} from '@chakra-ui/react'
import {
  ChevronDownIcon,
  ChevronUpIcon,
  RepeatIcon,
  CheckCircleIcon,
  WarningIcon,
  InfoIcon,
} from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { getSymbols } from '../services/api'
import {
  getStrategyRuntimeStatus,
  type StrategyRuntimeStatus,
} from '../services/strategy'

interface StrategyRuntimeStatusProps {
  exchange?: string
  symbol?: string
  refreshInterval?: number // 刷新間隔（毫秒），默認 10000
}

// 單個策略運行狀態卡片
const StrategyStatusCard: React.FC<{ status: StrategyRuntimeStatus }> = ({ status }) => {
  const { t } = useTranslation()
  const [isExpanded, setIsExpanded] = useState(false)
  
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')
  const expandedBg = useColorModeValue('gray.50', 'gray.700')

  const getStatusBadge = () => {
    if (status.isRunning) {
      return (
        <Badge colorScheme="green" display="flex" alignItems="center" gap={1}>
          <CheckCircleIcon boxSize={3} />
          {t('strategyRuntime.statusRunning')}
        </Badge>
      )
    }
    if (status.isEnabled) {
      return (
        <Badge colorScheme="yellow" display="flex" alignItems="center" gap={1}>
          <WarningIcon boxSize={3} />
          {t('strategyRuntime.statusEnabled')}
        </Badge>
      )
    }
    return (
      <Badge colorScheme="gray" display="flex" alignItems="center" gap={1}>
        <InfoIcon boxSize={3} />
        {t('strategyRuntime.statusDisabled')}
      </Badge>
    )
  }

  const getStrategyDisplayName = (name: string) => {
    const nameMap: Record<string, string> = {
      grid: t('strategyRuntime.strategyNames.grid'),
      dca: t('strategyRuntime.strategyNames.dca'),
      dca_enhanced: t('strategyRuntime.strategyNames.dca_enhanced'),
      martingale: t('strategyRuntime.strategyNames.martingale'),
      trend: t('strategyRuntime.strategyNames.trend'),
      mean_reversion: t('strategyRuntime.strategyNames.mean_reversion'),
      momentum: t('strategyRuntime.strategyNames.momentum'),
      combo: t('strategyRuntime.strategyNames.combo'),
    }
    return nameMap[name] || name
  }

  const formatPnL = (pnl: number) => {
    const formatted = pnl.toFixed(2)
    if (pnl > 0) return <Text color="green.500">+{formatted}</Text>
    if (pnl < 0) return <Text color="red.500">{formatted}</Text>
    return <Text>{formatted}</Text>
  }

  const getOrderSideText = (side: string) => {
    switch (side) {
      case 'BUY':
        return t('orderSideBuy')
      case 'SELL':
        return t('orderSideSell')
      default:
        return side
    }
  }

  const getOrderStatusText = (status: string) => {
    switch (status) {
      case 'NEW':
        return t('orderStatusNew')
      case 'PARTIALLY_FILLED':
        return t('orderStatusPartiallyFilled')
      case 'FILLED':
        return t('orderStatusFilled')
      case 'CANCELED':
        return t('orderStatusCanceled')
      case 'PENDING_CANCEL':
        return t('orderStatusPendingCancel')
      case 'REJECTED':
        return t('orderStatusRejected')
      default:
        return status
    }
  }

  return (
    <Box
      p={4}
      bg={bgColor}
      borderWidth="1px"
      borderColor={borderColor}
      borderRadius="lg"
      transition="all 0.2s"
      _hover={{ borderColor: 'blue.400' }}
    >
      <VStack align="stretch" spacing={3}>
        {/* 頭部信息 */}
        <HStack justify="space-between">
          <VStack align="start" spacing={0}>
            <HStack spacing={2}>
              <Text fontWeight="bold" fontSize="md">
                {getStrategyDisplayName(status.name)}
              </Text>
              {getStatusBadge()}
            </HStack>
            <Text fontSize="xs" color="gray.500">
              {t('strategyRuntime.type')}: {status.type} | {t('strategyRuntime.weight')}: {(status.weight * 100).toFixed(1)}%
            </Text>
          </VStack>
          <VStack align="end" spacing={0}>
            <HStack spacing={2}>
              <Tooltip label={t('strategyRuntime.positionCountLabel')}>
                <Badge colorScheme="blue" variant="subtle">
                  {t('strategyRuntime.positionPrefix')}: {status.positionCount}
                </Badge>
              </Tooltip>
              <Tooltip label={t('strategyRuntime.orderCountLabel')}>
                <Badge colorScheme="purple" variant="subtle">
                  {t('strategyRuntime.orderPrefix')}: {status.orderCount}
                </Badge>
              </Tooltip>
            </HStack>
          </VStack>
        </HStack>

        {/* 資金信息 */}
        <SimpleGrid columns={3} spacing={2}>
          <Box textAlign="center" p={2} bg={expandedBg} borderRadius="md">
            <Text fontSize="xs" color="gray.500">{t('strategyRuntime.allocated')}</Text>
            <Text fontWeight="bold" fontSize="sm">
              {status.allocatedFunds.toFixed(2)}
            </Text>
          </Box>
          <Box textAlign="center" p={2} bg={expandedBg} borderRadius="md">
            <Text fontSize="xs" color="gray.500">{t('strategyRuntime.used')}</Text>
            <Text fontWeight="bold" fontSize="sm" color="orange.500">
              {status.usedFunds.toFixed(2)}
            </Text>
          </Box>
          <Box textAlign="center" p={2} bg={expandedBg} borderRadius="md">
            <Text fontSize="xs" color="gray.500">{t('strategyRuntime.available')}</Text>
            <Text fontWeight="bold" fontSize="sm" color="green.500">
              {status.availableFunds.toFixed(2)}
            </Text>
          </Box>
        </SimpleGrid>

        {/* 統計信息 */}
        {status.statistics && (
          <SimpleGrid columns={4} spacing={2}>
            <Stat size="sm">
              <StatLabel fontSize="xs">{t('strategyRuntime.totalTrades')}</StatLabel>
              <StatNumber fontSize="md">{status.statistics.totalTrades}</StatNumber>
            </Stat>
            <Stat size="sm">
              <StatLabel fontSize="xs">{t('strategyRuntime.winRate')}</StatLabel>
              <StatNumber fontSize="md">
                {(status.statistics.winRate * 100).toFixed(1)}%
              </StatNumber>
            </Stat>
            <Stat size="sm">
              <StatLabel fontSize="xs">{t('strategyRuntime.totalPnL')}</StatLabel>
              <StatNumber fontSize="md">
                {formatPnL(status.statistics.totalPnL)}
              </StatNumber>
            </Stat>
            <Stat size="sm">
              <StatLabel fontSize="xs">{t('strategyRuntime.tradingVolume')}</StatLabel>
              <StatNumber fontSize="md">
                {status.statistics.totalVolume.toFixed(0)}
              </StatNumber>
            </Stat>
          </SimpleGrid>
        )}

        {/* 展開/收起按鈕 */}
        {(status.positions?.length || status.orders?.length) && (
          <>
            <Button
              size="sm"
              variant="ghost"
              rightIcon={isExpanded ? <ChevronUpIcon /> : <ChevronDownIcon />}
              onClick={() => setIsExpanded(!isExpanded)}
            >
              {isExpanded ? t('strategyRuntime.collapseDetails') : t('strategyRuntime.expandDetails')}
            </Button>

            <Collapse in={isExpanded} animateOpacity>
              <VStack align="stretch" spacing={3} pt={2}>
                {/* 持倉列表 */}
                {status.positions && status.positions.length > 0 && (
                  <Box>
                    <Text fontWeight="bold" fontSize="sm" mb={2}>
                      {t('strategyRuntime.positionList')}
                    </Text>
                    <TableContainer>
                      <Table size="sm" variant="simple">
                        <Thead>
                          <Tr>
                            <Th>{t('strategyRuntime.tradingPair')}</Th>
                            <Th isNumeric>{t('strategyRuntime.quantity')}</Th>
                            <Th isNumeric>{t('strategyRuntime.entryPrice')}</Th>
                            <Th isNumeric>{t('strategyRuntime.currentPrice')}</Th>
                            <Th isNumeric>{t('strategyRuntime.pnl')}</Th>
                          </Tr>
                        </Thead>
                        <Tbody>
                          {status.positions.map((pos, idx) => (
                            <Tr key={idx}>
                              <Td>{pos.symbol}</Td>
                              <Td isNumeric>{pos.size.toFixed(6)}</Td>
                              <Td isNumeric>{pos.entryPrice.toFixed(2)}</Td>
                              <Td isNumeric>{pos.currentPrice.toFixed(2)}</Td>
                              <Td isNumeric>{formatPnL(pos.pnl)}</Td>
                            </Tr>
                          ))}
                        </Tbody>
                      </Table>
                    </TableContainer>
                  </Box>
                )}

                {/* 訂單列表 */}
                {status.orders && status.orders.length > 0 && (
                  <Box>
                    <Text fontWeight="bold" fontSize="sm" mb={2}>
                      {t('strategyRuntime.orderList')}
                    </Text>
                    <TableContainer>
                      <Table size="sm" variant="simple">
                        <Thead>
                          <Tr>
                            <Th>{t('strategyRuntime.orderId')}</Th>
                            <Th>{t('strategyRuntime.tradingPair')}</Th>
                            <Th>{t('strategyRuntime.direction')}</Th>
                            <Th isNumeric>{t('strategyRuntime.price')}</Th>
                            <Th isNumeric>{t('strategyRuntime.quantity')}</Th>
                            <Th>{t('strategyRuntime.status')}</Th>
                          </Tr>
                        </Thead>
                        <Tbody>
                          {status.orders.slice(0, 10).map((order, idx) => (
                            <Tr key={idx}>
                              <Td fontSize="xs">{order.orderId}</Td>
                              <Td>{order.symbol}</Td>
                              <Td>
                                <Badge colorScheme={order.side === 'BUY' ? 'green' : 'red'}>
                                  {getOrderSideText(order.side)}
                                </Badge>
                              </Td>
                              <Td isNumeric>{order.price.toFixed(2)}</Td>
                              <Td isNumeric>{order.quantity.toFixed(6)}</Td>
                              <Td>
                                <Badge size="sm">{getOrderStatusText(order.status)}</Badge>
                              </Td>
                            </Tr>
                          ))}
                        </Tbody>
                      </Table>
                    </TableContainer>
                    {status.orders.length > 10 && (
                      <Text fontSize="xs" color="gray.500" mt={1}>
                        {t('strategyRuntime.moreOrders', { count: status.orders.length - 10 })}
                      </Text>
                    )}
                  </Box>
                )}
              </VStack>
            </Collapse>
          </>
        )}
      </VStack>
    </Box>
  )
}

// 主組件
const StrategyRuntimeStatusPanel: React.FC<StrategyRuntimeStatusProps> = ({
  exchange,
  symbol,
  refreshInterval = 10000,
}) => {
  const { t } = useTranslation()
  const [statuses, setStatuses] = useState<StrategyRuntimeStatus[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null)
  const [symbolDirection, setSymbolDirection] = useState<'LONG' | 'SHORT' | null>(null)

  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  const fetchStatus = useCallback(async () => {
    try {
      const response = await getStrategyRuntimeStatus(exchange, symbol)
      if (response.success) {
        setStatuses(response.strategies || [])
        setError(null)
      } else {
        setError(response.message || t('strategyRuntime.fetchStatusFailed'))
      }
      setLastRefresh(new Date())
    } catch (err) {
      setError(t('strategyRuntime.fetchStatusFailed'))
      console.error('Failed to fetch strategy runtime status:', err)
    } finally {
      setLoading(false)
    }
  }, [exchange, symbol])

  useEffect(() => {
    if (!exchange || !symbol) {
      setSymbolDirection(null)
      return
    }
    const loadDirection = async () => {
      try {
        const res = await getSymbols()
        const sym = res.symbols?.find(
          s => s.exchange?.toLowerCase() === exchange?.toLowerCase() && s.symbol === symbol
        )
        setSymbolDirection(sym?.direction === 'SHORT' ? 'SHORT' : 'LONG')
      } catch {
        setSymbolDirection('LONG')
      }
    }
    loadDirection()
  }, [exchange, symbol])

  useEffect(() => {
    fetchStatus()
    const interval = setInterval(fetchStatus, refreshInterval)
    return () => clearInterval(interval)
  }, [fetchStatus, refreshInterval])

  const handleRefresh = () => {
    setLoading(true)
    fetchStatus()
  }

  if (loading && statuses.length === 0) {
    return (
      <Center py={8}>
        <VStack spacing={4}>
          <Spinner size="lg" color="blue.500" />
          <Text color="gray.500">{t('strategyRuntime.loadingStatus')}</Text>
        </VStack>
      </Center>
    )
  }

  const runningStrategies = statuses.filter(s => s.isRunning)
  const enabledStrategies = statuses.filter(s => s.isEnabled)

  return (
    <Box
      p={6}
      bg={bgColor}
      borderRadius="xl"
      borderWidth="1px"
      borderColor={borderColor}
    >
      <VStack align="stretch" spacing={4}>
        {/* 標題欄 */}
        <HStack justify="space-between" flexWrap="wrap">
          <HStack spacing={2} align="center">
            <VStack align="start" spacing={0}>
              <Heading size="md">{t('strategyRuntime.title')}</Heading>
              <Text fontSize="sm" color="gray.500">
                {t('strategyRuntime.subtitle')}
              </Text>
            </VStack>
            {symbolDirection != null && (
              <Badge colorScheme={symbolDirection === 'SHORT' ? 'orange' : 'green'} fontSize="sm">
                {symbolDirection === 'SHORT' ? t('configuration.directionShort') : t('configuration.directionLong')}
              </Badge>
            )}
          </HStack>
          <HStack spacing={2}>
            {lastRefresh && (
              <Text fontSize="xs" color="gray.400">
                {t('strategyRuntime.lastUpdate')}: {lastRefresh.toLocaleTimeString()}
              </Text>
            )}
            <Button
              size="sm"
              leftIcon={<RepeatIcon />}
              onClick={handleRefresh}
              isLoading={loading}
              variant="outline"
            >
              {t('strategyRuntime.refresh')}
            </Button>
          </HStack>
        </HStack>

        {/* 概覽統計 */}
        <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
          <Box textAlign="center" p={3} bg={useColorModeValue('gray.50', 'gray.700')} borderRadius="md">
            <Text fontSize="2xl" fontWeight="bold" color="blue.500">
              {statuses.length}
            </Text>
            <Text fontSize="sm" color="gray.500">{t('strategyRuntime.totalStrategies')}</Text>
          </Box>
          <Box textAlign="center" p={3} bg={useColorModeValue('gray.50', 'gray.700')} borderRadius="md">
            <Text fontSize="2xl" fontWeight="bold" color="green.500">
              {runningStrategies.length}
            </Text>
            <Text fontSize="sm" color="gray.500">{t('strategyRuntime.running')}</Text>
          </Box>
          <Box textAlign="center" p={3} bg={useColorModeValue('gray.50', 'gray.700')} borderRadius="md">
            <Text fontSize="2xl" fontWeight="bold" color="purple.500">
              {statuses.reduce((sum, s) => sum + s.positionCount, 0)}
            </Text>
            <Text fontSize="sm" color="gray.500">{t('strategyRuntime.totalPositions')}</Text>
          </Box>
          <Box textAlign="center" p={3} bg={useColorModeValue('gray.50', 'gray.700')} borderRadius="md">
            <Text fontSize="2xl" fontWeight="bold" color="orange.500">
              {statuses.reduce((sum, s) => sum + s.orderCount, 0)}
            </Text>
            <Text fontSize="sm" color="gray.500">{t('strategyRuntime.totalOrders')}</Text>
          </Box>
        </SimpleGrid>

        <Divider />

        {/* 錯誤提示 */}
        {error && (
          <Alert status="warning" borderRadius="md">
            <AlertIcon />
            <Text>{error}</Text>
          </Alert>
        )}

        {/* 策略列表 */}
        {statuses.length === 0 ? (
          <Center py={8}>
            <VStack spacing={2}>
              <Icon as={InfoIcon} boxSize={8} color="gray.300" />
              <Text color="gray.500">{t('strategyRuntime.noStrategies')}</Text>
              <Text fontSize="sm" color="gray.400">
                {t('strategyRuntime.enableStrategiesHint')}
              </Text>
            </VStack>
          </Center>
        ) : (
          <VStack align="stretch" spacing={3}>
            {statuses.map((status, index) => (
              <StrategyStatusCard key={`${status.name}-${index}`} status={status} />
            ))}
          </VStack>
        )}
      </VStack>
    </Box>
  )
}

export default StrategyRuntimeStatusPanel
