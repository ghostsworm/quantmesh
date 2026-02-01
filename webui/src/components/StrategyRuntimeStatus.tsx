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
          運行中
        </Badge>
      )
    }
    if (status.isEnabled) {
      return (
        <Badge colorScheme="yellow" display="flex" alignItems="center" gap={1}>
          <WarningIcon boxSize={3} />
          已啟用
        </Badge>
      )
    }
    return (
      <Badge colorScheme="gray" display="flex" alignItems="center" gap={1}>
        <InfoIcon boxSize={3} />
        未啟用
      </Badge>
    )
  }

  const getStrategyDisplayName = (name: string) => {
    const nameMap: Record<string, string> = {
      grid: '網格交易策略',
      dca: 'DCA 定投策略',
      dca_enhanced: '增強型 DCA 策略',
      martingale: '馬丁格爾策略',
      trend: '趨勢跟蹤策略',
      mean_reversion: '均值回歸策略',
      momentum: '動量策略',
      combo: '組合策略',
    }
    return nameMap[name] || name
  }

  const formatPnL = (pnl: number) => {
    const formatted = pnl.toFixed(2)
    if (pnl > 0) return <Text color="green.500">+{formatted}</Text>
    if (pnl < 0) return <Text color="red.500">{formatted}</Text>
    return <Text>{formatted}</Text>
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
              類型: {status.type} | 權重: {(status.weight * 100).toFixed(1)}%
            </Text>
          </VStack>
          <VStack align="end" spacing={0}>
            <HStack spacing={2}>
              <Tooltip label="持倉數">
                <Badge colorScheme="blue" variant="subtle">
                  持倉: {status.positionCount}
                </Badge>
              </Tooltip>
              <Tooltip label="訂單數">
                <Badge colorScheme="purple" variant="subtle">
                  訂單: {status.orderCount}
                </Badge>
              </Tooltip>
            </HStack>
          </VStack>
        </HStack>

        {/* 資金信息 */}
        <SimpleGrid columns={3} spacing={2}>
          <Box textAlign="center" p={2} bg={expandedBg} borderRadius="md">
            <Text fontSize="xs" color="gray.500">已分配</Text>
            <Text fontWeight="bold" fontSize="sm">
              {status.allocatedFunds.toFixed(2)}
            </Text>
          </Box>
          <Box textAlign="center" p={2} bg={expandedBg} borderRadius="md">
            <Text fontSize="xs" color="gray.500">已使用</Text>
            <Text fontWeight="bold" fontSize="sm" color="orange.500">
              {status.usedFunds.toFixed(2)}
            </Text>
          </Box>
          <Box textAlign="center" p={2} bg={expandedBg} borderRadius="md">
            <Text fontSize="xs" color="gray.500">可用</Text>
            <Text fontWeight="bold" fontSize="sm" color="green.500">
              {status.availableFunds.toFixed(2)}
            </Text>
          </Box>
        </SimpleGrid>

        {/* 統計信息 */}
        {status.statistics && (
          <SimpleGrid columns={4} spacing={2}>
            <Stat size="sm">
              <StatLabel fontSize="xs">交易次數</StatLabel>
              <StatNumber fontSize="md">{status.statistics.totalTrades}</StatNumber>
            </Stat>
            <Stat size="sm">
              <StatLabel fontSize="xs">勝率</StatLabel>
              <StatNumber fontSize="md">
                {(status.statistics.winRate * 100).toFixed(1)}%
              </StatNumber>
            </Stat>
            <Stat size="sm">
              <StatLabel fontSize="xs">總盈虧</StatLabel>
              <StatNumber fontSize="md">
                {formatPnL(status.statistics.totalPnL)}
              </StatNumber>
            </Stat>
            <Stat size="sm">
              <StatLabel fontSize="xs">交易量</StatLabel>
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
              {isExpanded ? '收起詳情' : '查看詳情'}
            </Button>

            <Collapse in={isExpanded} animateOpacity>
              <VStack align="stretch" spacing={3} pt={2}>
                {/* 持倉列表 */}
                {status.positions && status.positions.length > 0 && (
                  <Box>
                    <Text fontWeight="bold" fontSize="sm" mb={2}>
                      持倉列表
                    </Text>
                    <TableContainer>
                      <Table size="sm" variant="simple">
                        <Thead>
                          <Tr>
                            <Th>交易對</Th>
                            <Th isNumeric>數量</Th>
                            <Th isNumeric>入場價</Th>
                            <Th isNumeric>當前價</Th>
                            <Th isNumeric>盈虧</Th>
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
                      訂單列表
                    </Text>
                    <TableContainer>
                      <Table size="sm" variant="simple">
                        <Thead>
                          <Tr>
                            <Th>訂單ID</Th>
                            <Th>交易對</Th>
                            <Th>方向</Th>
                            <Th isNumeric>價格</Th>
                            <Th isNumeric>數量</Th>
                            <Th>狀態</Th>
                          </Tr>
                        </Thead>
                        <Tbody>
                          {status.orders.slice(0, 10).map((order, idx) => (
                            <Tr key={idx}>
                              <Td fontSize="xs">{order.orderId}</Td>
                              <Td>{order.symbol}</Td>
                              <Td>
                                <Badge colorScheme={order.side === 'BUY' ? 'green' : 'red'}>
                                  {order.side}
                                </Badge>
                              </Td>
                              <Td isNumeric>{order.price.toFixed(2)}</Td>
                              <Td isNumeric>{order.quantity.toFixed(6)}</Td>
                              <Td>
                                <Badge size="sm">{order.status}</Badge>
                              </Td>
                            </Tr>
                          ))}
                        </Tbody>
                      </Table>
                    </TableContainer>
                    {status.orders.length > 10 && (
                      <Text fontSize="xs" color="gray.500" mt={1}>
                        還有 {status.orders.length - 10} 個訂單未顯示
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

  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  const fetchStatus = useCallback(async () => {
    try {
      const response = await getStrategyRuntimeStatus(exchange, symbol)
      if (response.success) {
        setStatuses(response.strategies || [])
        setError(null)
      } else {
        setError(response.message || '獲取策略狀態失敗')
      }
      setLastRefresh(new Date())
    } catch (err) {
      setError('獲取策略狀態失敗')
      console.error('Failed to fetch strategy runtime status:', err)
    } finally {
      setLoading(false)
    }
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
          <Text color="gray.500">載入策略運行狀態...</Text>
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
        <HStack justify="space-between">
          <VStack align="start" spacing={0}>
            <Heading size="md">策略運行狀態</Heading>
            <Text fontSize="sm" color="gray.500">
              實時查看各策略的執行情況
            </Text>
          </VStack>
          <HStack spacing={2}>
            {lastRefresh && (
              <Text fontSize="xs" color="gray.400">
                上次更新: {lastRefresh.toLocaleTimeString()}
              </Text>
            )}
            <Button
              size="sm"
              leftIcon={<RepeatIcon />}
              onClick={handleRefresh}
              isLoading={loading}
              variant="outline"
            >
              刷新
            </Button>
          </HStack>
        </HStack>

        {/* 概覽統計 */}
        <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
          <Box textAlign="center" p={3} bg={useColorModeValue('gray.50', 'gray.700')} borderRadius="md">
            <Text fontSize="2xl" fontWeight="bold" color="blue.500">
              {statuses.length}
            </Text>
            <Text fontSize="sm" color="gray.500">總策略數</Text>
          </Box>
          <Box textAlign="center" p={3} bg={useColorModeValue('gray.50', 'gray.700')} borderRadius="md">
            <Text fontSize="2xl" fontWeight="bold" color="green.500">
              {runningStrategies.length}
            </Text>
            <Text fontSize="sm" color="gray.500">運行中</Text>
          </Box>
          <Box textAlign="center" p={3} bg={useColorModeValue('gray.50', 'gray.700')} borderRadius="md">
            <Text fontSize="2xl" fontWeight="bold" color="purple.500">
              {statuses.reduce((sum, s) => sum + s.positionCount, 0)}
            </Text>
            <Text fontSize="sm" color="gray.500">總持倉數</Text>
          </Box>
          <Box textAlign="center" p={3} bg={useColorModeValue('gray.50', 'gray.700')} borderRadius="md">
            <Text fontSize="2xl" fontWeight="bold" color="orange.500">
              {statuses.reduce((sum, s) => sum + s.orderCount, 0)}
            </Text>
            <Text fontSize="sm" color="gray.500">總訂單數</Text>
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
              <Text color="gray.500">沒有註冊的策略</Text>
              <Text fontSize="sm" color="gray.400">
                請在配置中啟用策略
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
