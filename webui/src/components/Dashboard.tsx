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
  useColorModeValue,
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
} from '@chakra-ui/icons'
import { motion, AnimatePresence } from 'framer-motion'
import { useSymbol } from '../contexts/SymbolContext'
import { getStatus, startTrading, stopTrading, getSlots, SlotsResponse, getStrategyAllocation, StrategyAllocationResponse, getPendingOrders, PendingOrdersResponse, getPositionsSummary } from '../services/api'

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
  const bg = useColorModeValue('white', 'rgba(255, 255, 255, 0.05)')
  const borderColor = useColorModeValue('gray.100', 'whiteAlpha.100')
  
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
  const { selectedExchange, selectedSymbol } = useSymbol()
  const [status, setStatus] = useState<SystemStatus | null>(null)
  const [slotsInfo, setSlotsInfo] = useState<SlotsResponse | null>(null)
  const [strategyAllocation, setStrategyAllocation] = useState<StrategyAllocationResponse | null>(null)
  const [pendingOrders, setPendingOrders] = useState<PendingOrdersResponse | null>(null)
  const [positionsSummary, setPositionsSummary] = useState<any>(null)
  const [isTrading, setIsTrading] = useState(false)
  const [loading, setLoading] = useState(true)
  const toast = useToast()

  const cardBg = useColorModeValue('white', 'gray.800')

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [statusData, slotsData, allocationData, ordersData, positionsData] = await Promise.all([
          getStatus(selectedExchange || undefined, selectedSymbol || undefined),
          getSlots(selectedExchange || undefined, selectedSymbol || undefined).catch(() => null),
          getStrategyAllocation().catch(() => null),
          getPendingOrders().catch(() => null),
          getPositionsSummary(selectedExchange || undefined, selectedSymbol || undefined).catch(() => null),
        ])
        setStatus(statusData)
        setSlotsInfo(slotsData)
        setStrategyAllocation(allocationData)
        setPendingOrders(ordersData)
        setPositionsSummary(positionsData)
        setIsTrading(statusData?.running || false)
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
        await stopTrading()
        setIsTrading(false)
        toast({ title: '交易已停止', status: 'info', borderRadius: 'full' })
      } else {
        await startTrading()
        setIsTrading(true)
        toast({ title: '交易已启动', status: 'success', borderRadius: 'full' })
      }
    } catch (error) {
      toast({ title: '操作失败', description: error instanceof Error ? error.message : '未知错误', status: 'error' })
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

  return (
    <Container maxW="container.xl" py={4}>
      <VStack spacing={8} align="stretch">
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
              <Text color="gray.500" fontSize="sm">当前价格: <Text as="span" fontWeight="bold" color="blue.500">${status.current_price.toFixed(2)}</Text></Text>
            </VStack>
          </HStack>

          <GlassCard p={2}>
            <HStack spacing={6} px={4}>
              <VStack align="start" spacing={0}>
                <Text fontSize="10px" fontWeight="bold" color="gray.400" textTransform="uppercase">Status</Text>
                <HStack spacing={2}>
                  <Box w={2} h={2} borderRadius="full" bg={isTrading ? 'green.500' : 'red.500'} boxShadow={isTrading ? '0 0 8px #48BB78' : 'none'} />
                  <Text fontWeight="bold" fontSize="sm">{isTrading ? 'Running' : 'Stopped'}</Text>
                </HStack>
              </VStack>
              <Divider orientation="vertical" h="30px" />
              <Button
                size="md"
                colorScheme={isTrading ? 'red' : 'green'}
                onClick={handleToggleTrading}
                borderRadius="2xl"
                px={8}
                fontSize="sm"
                fontWeight="800"
                boxShadow={isTrading ? '0 4px 12px rgba(245, 101, 101, 0.3)' : '0 4px 12px rgba(72, 187, 120, 0.3)'}
                _hover={{ transform: 'translateY(-2px)' }}
                _active={{ transform: 'scale(0.95)' }}
              >
                {isTrading ? 'STOP' : 'START'}
              </Button>
            </HStack>
          </GlassCard>
        </Flex>

        {/* Top Metrics Row */}
        <SimpleGrid columns={{ base: 1, md: 3 }} spacing={6}>
          <GlassCard>
            <Stat>
              <StatLabel fontSize="xs" fontWeight="bold" color="gray.500" mb={2}>TOTAL P&L</StatLabel>
              <StatNumber fontSize="3xl" fontWeight="800" color={status.total_pnl >= 0 ? 'green.500' : 'red.500'}>
                {status.total_pnl >= 0 ? '+' : ''}{status.total_pnl.toFixed(2)}
                <Text as="span" fontSize="sm" ml={1} color="gray.400">USDT</Text>
              </StatNumber>
              <StatHelpText>
                <HStack spacing={1}>
                  <Icon as={status.total_pnl >= 0 ? TriangleUpIcon : TriangleDownIcon} />
                  <Text fontWeight="600">{((status.total_pnl / 1000) * 100).toFixed(2)}%</Text>
                  <Text color="gray.400">ROI</Text>
                </HStack>
              </StatHelpText>
            </Stat>
          </GlassCard>

          <GlassCard>
            <Stat>
              <StatLabel fontSize="xs" fontWeight="bold" color="gray.500" mb={2}>TRADING VOLUME</StatLabel>
              <StatNumber fontSize="3xl" fontWeight="800">{status.total_trades}</StatNumber>
              <StatHelpText>
                <HStack spacing={1}>
                  <Text fontWeight="600" color="blue.500">{(status.total_trades / (status.uptime / 3600)).toFixed(1)}</Text>
                  <Text color="gray.400">trades / hour</Text>
                </HStack>
              </StatHelpText>
            </Stat>
          </GlassCard>

          <GlassCard>
            <Stat>
              <StatLabel fontSize="xs" fontWeight="bold" color="gray.500" mb={2}>SYSTEM UPTIME</StatLabel>
              <StatNumber fontSize="3xl" fontWeight="800">{formatUptime(status.uptime)}</StatNumber>
              <StatHelpText>
                <HStack spacing={1}>
                  <Icon as={CheckCircleIcon} color="green.500" />
                  <Text fontWeight="600" color="green.500">Normal</Text>
                  <Text color="gray.400">Status</Text>
                </HStack>
              </StatHelpText>
            </Stat>
          </GlassCard>
        </SimpleGrid>

        {/* Details Grid */}
        <SimpleGrid columns={{ base: 1, lg: 2 }} spacing={8}>
          {/* Slots & Allocation */}
          <VStack align="stretch" spacing={6}>
            <GlassCard title="Slots Matrix">
              {slotsInfo ? (
                <SimpleGrid columns={4} spacing={4}>
                  {slotsInfo.slots.map((slot, i) => (
                    <VStack 
                      key={i} 
                      bg={slot.position_status === 'FILLED' ? 'green.50' : 'gray.50'} 
                      p={3} 
                      borderRadius="xl"
                      border="1px solid"
                      borderColor={slot.position_status === 'FILLED' ? 'green.100' : 'gray.100'}
                    >
                      <Text fontSize="10px" fontWeight="bold" color="gray.400">#{i+1}</Text>
                      <Box w={3} h={3} borderRadius="full" bg={slot.position_status === 'FILLED' ? 'green.500' : 'gray.300'} />
                    </VStack>
                  ))}
                </SimpleGrid>
              ) : (
                <Text color="gray.400" fontSize="sm">No slots information available</Text>
              )}
            </GlassCard>

            {strategyAllocation && (
              <GlassCard title="Capital Allocation">
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

          {/* Positions & Orders */}
          <VStack align="stretch" spacing={6}>
            <GlassCard title="Active Positions">
              {positionsSummary && positionsSummary.position_count > 0 ? (
                <VStack align="stretch" spacing={4}>
                  <Flex justify="space-between" align="center">
                    <VStack align="start" spacing={0}>
                      <Text fontSize="xs" color="gray.500">Size</Text>
                      <Text fontWeight="800" fontSize="xl">{positionsSummary.total_quantity?.toFixed(4)}</Text>
                    </VStack>
                    <VStack align="end" spacing={0}>
                      <Text fontSize="xs" color="gray.500">Unrealized P&L</Text>
                      <Text fontWeight="800" fontSize="xl" color={(positionsSummary.unrealized_pnl || 0) >= 0 ? 'green.500' : 'red.500'}>
                        {(positionsSummary.unrealized_pnl || 0) >= 0 ? '+' : ''}{positionsSummary.unrealized_pnl?.toFixed(2)}
                      </Text>
                    </VStack>
                  </Flex>
                  <Divider />
                  <Flex justify="space-between">
                    <Text fontSize="xs" color="gray.500">Entry Price: ${positionsSummary.average_entry_price?.toFixed(2)}</Text>
                    <Text fontSize="xs" color="gray.500">Value: ${positionsSummary.total_value?.toFixed(2)}</Text>
                  </Flex>
                </VStack>
              ) : (
                <Center h="100px">
                  <VStack spacing={2}>
                    <Icon as={InfoIcon} color="gray.300" w={6} h={6} />
                    <Text color="gray.400" fontSize="sm">No active positions</Text>
                  </VStack>
                </Center>
              )}
            </GlassCard>

            <GlassCard title="Recent Activity">
              {pendingOrders && pendingOrders.count > 0 ? (
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
                    <Text fontSize="xs" color="blue.500" textAlign="center" cursor="pointer">View all {pendingOrders.count} orders</Text>
                  )}
                </VStack>
              ) : (
                <Text color="gray.400" fontSize="sm" textAlign="center">No pending orders</Text>
              )}
            </GlassCard>
          </VStack>
        </SimpleGrid>
      </VStack>
    </Container>
  )
}

export default Dashboard
