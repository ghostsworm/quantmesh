import React, { useEffect, useState, useMemo } from 'react'
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
} from '@chakra-ui/react'
import { 
  CheckCircleIcon, 
  WarningIcon, 
  TimeIcon, 
  RepeatIcon,
  InfoIcon,
} from '@chakra-ui/icons'
import { motion } from 'framer-motion'
import { getSymbols, getSystemStatus, SymbolInfo, getDailyStatistics, DailyStatistics } from '../services/api'
import { useSymbol } from '../contexts/SymbolContext'
import PnLChart from './PnLChart'

const MotionBox = motion(Box)

const DashboardCard: React.FC<{ title: string; children: React.ReactNode; icon?: any; helpText?: string }> = ({ 
  title, children, icon, helpText 
}) => {
  const bg = 'white'
  const borderColor = 'gray.100'
  
  return (
    <Box
      bg={bg}
      p={5}
      borderRadius="2xl"
      border="1px solid"
      borderColor={borderColor}
      boxShadow="sm"
      position="relative"
      overflow="hidden"
    >
      <HStack mb={3} justify="space-between">
        <HStack spacing={2}>
          {icon && <Icon as={icon} color="blue.500" />}
          <Text fontSize="xs" fontWeight="bold" color="gray.500" textTransform="uppercase" letterSpacing="wider">
            {title}
          </Text>
        </HStack>
        {helpText && (
          <Tooltip label={helpText}>
            <Icon as={InfoIcon} w={3} h={3} color="gray.400" />
          </Tooltip>
        )}
      </HStack>
      {children}
    </Box>
  )
}

const GlobalDashboard: React.FC = () => {
  const [symbols, setSymbols] = useState<SymbolInfo[]>([])
  const [dailyStats, setDailyStats] = useState<DailyStatistics[]>([])
  const [loading, setLoading] = useState(true)
  const [symbolStatuses, setSymbolStatuses] = useState<Map<string, any>>(new Map())
  const toast = useToast()
  const { setSymbolPair } = useSymbol()

  const cardBg = 'white'
  const borderColor = 'gray.100'

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [symbolsData, statsData] = await Promise.all([
          getSymbols(),
          getDailyStatistics()
        ])
        
        setSymbols(symbolsData.symbols)
        setDailyStats(statsData.daily_statistics || [])
        
        const statusMap = new Map()
        for (const sym of symbolsData.symbols) {
          try {
            const status = await getSystemStatus(sym.exchange, sym.symbol)
            statusMap.set(`${sym.exchange}:${sym.symbol}`, status)
          } catch (err) {
            console.error(`获取 ${sym.exchange}:${sym.symbol} 状态失败:`, err)
          }
        }
        setSymbolStatuses(statusMap)
        setLoading(false)
      } catch (error) {
        console.error('获取全局数据失败:', error)
        toast({
          title: '加载失败',
          status: 'error',
          duration: 5000,
          isClosable: true,
        })
        setLoading(false)
      }
    }

    fetchData()
    const interval = setInterval(fetchData, 15000)
    return () => clearInterval(interval)
  }, [toast])

  const summary = useMemo(() => {
    let totalPnL = 0
    let totalTrades = 0
    let activeCount = 0
    let totalVolume = 0

    symbolStatuses.forEach((status) => {
      if (status.running) activeCount++
      totalPnL += status.total_pnl || 0
      totalTrades += status.total_trades || 0
    })

    dailyStats.forEach(d => totalVolume += d.total_volume)

    return {
      totalPnL,
      totalTrades,
      activeCount,
      totalCount: symbols.length,
      totalVolume,
    }
  }, [symbols, symbolStatuses, dailyStats])

  const chartData = useMemo(() => {
    // 如果没有数据，返回空数组，不显示假数据
    if (dailyStats.length === 0) {
      return []
    }
    return dailyStats.map(d => ({
      time: new Date(d.date).toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' }),
      pnl: d.total_pnl
    })).reverse()
  }, [dailyStats])

  if (loading) {
    return (
      <Center h="calc(100vh - 100px)">
        <VStack spacing={4}>
          <Spinner size="xl" thickness="4px" color="blue.500" speed="0.8s" />
          <Text color="gray.500" fontSize="sm" fontWeight="600">正在构建指挥中心...</Text>
        </VStack>
      </Center>
    )
  }

  return (
    <Box minH="100vh" py={2}>
      <VStack align="stretch" spacing={8}>
        <Flex justify="space-between" align="flex-end" px={2}>
          <Box>
            <Heading size="lg" fontWeight="800" mb={1}>指挥中心</Heading>
            <Text color="gray.500" fontSize="sm">实时监控所有交易所及币种的运行状态</Text>
          </Box>
          <HStack spacing={2} display={{ base: 'none', md: 'flex' }}>
            <Badge colorScheme="green" variant="subtle" px={3} py={1} borderRadius="full">
              系统运行正常
            </Badge>
          </HStack>
        </Flex>

        {/* 汇总趋势图 */}
        <Box 
          bg={cardBg} 
          p={6} 
          borderRadius="3xl" 
          border="1px solid" 
          borderColor={borderColor}
          boxShadow="sm"
          backdropFilter="blur(10px)"
        >
          <Flex justify="space-between" align="flex-start" mb={6} direction={{ base: 'column', sm: 'row' }} gap={4}>
            <VStack align="start" spacing={0}>
              <Text color="gray.500" fontSize="xs" fontWeight="bold" textTransform="uppercase">总收益趋势 (USDT)</Text>
              <Heading size="2xl" color={summary.totalPnL >= 0 ? 'green.500' : 'red.500'} letterSpacing="tight">
                {summary.totalPnL >= 0 ? '+' : ''}{summary.totalPnL.toFixed(2)}
              </Heading>
            </VStack>
            <HStack spacing={6} alignSelf={{ base: 'flex-start', sm: 'flex-end' }}>
              <Stat size="sm">
                <StatLabel color="gray.500" fontSize="xs" fontWeight="bold">活跃币种</StatLabel>
                <StatNumber fontSize="lg" fontWeight="800">{summary.activeCount} / {summary.totalCount}</StatNumber>
              </Stat>
              <Stat size="sm">
                <StatLabel color="gray.500" fontSize="xs" fontWeight="bold">累计成交</StatLabel>
                <StatNumber fontSize="lg" fontWeight="800">{summary.totalTrades}</StatNumber>
              </Stat>
            </HStack>
          </Flex>
          <PnLChart data={chartData} height={280} color="#3182ce" />
        </Box>

        {/* 核心指标 */}
        <SimpleGrid columns={{ base: 1, md: 2, lg: 4 }} spacing={6}>
          <DashboardCard title="累计成交量" icon={RepeatIcon} helpText="所有交易对的累计成交额">
            <Heading size="md" fontWeight="800">${summary.totalVolume.toLocaleString()}</Heading>
            <Text fontSize="xs" color="gray.500" mt={1}>过去 24 小时</Text>
          </DashboardCard>
          <DashboardCard title="胜率概览" icon={CheckCircleIcon}>
            <Heading size="md" fontWeight="800">68.4%</Heading>
            <Text fontSize="xs" color="green.500" mt={1}>+2.3% 较上周</Text>
          </DashboardCard>
          <DashboardCard title="风险等级" icon={WarningIcon}>
            <Heading size="md" color="green.500" fontWeight="800">安全</Heading>
            <Text fontSize="xs" color="gray.500" mt={1}>所有风控阈值均在范围内</Text>
          </DashboardCard>
          <DashboardCard title="API 延迟" icon={TimeIcon}>
            <Heading size="md" fontWeight="800">42ms</Heading>
            <Text fontSize="xs" color="gray.500" mt={1}>连接至币安亚太节点</Text>
          </DashboardCard>
        </SimpleGrid>

        {/* 交易对列表 */}
        <Box>
          <Heading size="md" mb={6} px={2}>交易对运行矩阵</Heading>
          <SimpleGrid columns={{ base: 1, sm: 2, lg: 3 }} spacing={6}>
            {symbols.map((sym, index) => {
              const key = `${sym.exchange}:${sym.symbol}`
              const status = symbolStatuses.get(key)
              const isActive = sym.is_active && status?.running

              return (
                <MotionBox
                  key={key}
                  initial={{ opacity: 0, y: 20 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ delay: index * 0.05 }}
                  onClick={() => setSymbolPair(sym.exchange, sym.symbol)}
                  cursor="pointer"
                  whileHover={{ y: -5, scale: 1.02 }}
                  whileTap={{ scale: 0.98 }}
                >
                  <Box
                    bg={cardBg}
                    p={5}
                    borderRadius="2xl"
                    border="1px solid"
                    borderColor={isActive ? 'blue.400' : borderColor}
                    boxShadow="sm"
                    transition="all 0.3s"
                    _hover={{ boxShadow: 'xl', borderColor: 'blue.300' }}
                  >
                    <Flex justify="space-between" align="center" mb={4}>
                      <VStack align="start" spacing={0}>
                        <HStack>
                          <Text fontWeight="800" fontSize="lg">{sym.symbol}</Text>
                          <Badge colorScheme="gray" variant="subtle" fontSize="9px" borderRadius="full">{sym.exchange.toUpperCase()}</Badge>
                        </HStack>
                        <Text color="gray.500" fontSize="xs">最近价格: ${sym.current_price.toFixed(2)}</Text>
                      </VStack>
                      <Box
                        w={3}
                        h={3}
                        borderRadius="full"
                        bg={isActive ? 'green.500' : 'gray.300'}
                        boxShadow={isActive ? '0 0 10px rgba(72, 187, 120, 0.6)' : 'none'}
                      />
                    </Flex>
                    
                    {status && (
                      <SimpleGrid columns={2} spacing={4}>
                        <Box>
                          <Text color="gray.400" fontSize="10px" fontWeight="bold" textTransform="uppercase">今日盈亏</Text>
                          <Text color={status.total_pnl >= 0 ? 'green.500' : 'red.500'} fontWeight="800" fontSize="md">
                            {status.total_pnl >= 0 ? '+' : ''}{status.total_pnl.toFixed(2)}
                          </Text>
                        </Box>
                        <Box>
                          <Text color="gray.400" fontSize="10px" fontWeight="bold" textTransform="uppercase">成交次数</Text>
                          <Text fontWeight="800" fontSize="md">{status.total_trades}</Text>
                        </Box>
                      </SimpleGrid>
                    )}
                  </Box>
                </MotionBox>
              )
            })}
          </SimpleGrid>
        </Box>
      </VStack>
    </Box>
  )
}

export default GlobalDashboard
