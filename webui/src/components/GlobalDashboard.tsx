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
  StatArrow,
  Card,
  CardHeader,
  CardBody,
  Badge,
  Text,
  Spinner,
  Center,
  useToast,
  Flex,
  Icon,
} from '@chakra-ui/react'
import { CheckCircleIcon, WarningIcon } from '@chakra-ui/icons'
import { getSymbols, getSystemStatus, SymbolInfo } from '../services/api'
import { useSymbol } from '../contexts/SymbolContext'

const GlobalDashboard: React.FC = () => {
  const [symbols, setSymbols] = useState<SymbolInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [symbolStatuses, setSymbolStatuses] = useState<Map<string, any>>(new Map())
  const toast = useToast()
  const { setSymbolPair } = useSymbol()

  useEffect(() => {
    const fetchData = async () => {
      try {
        const symbolsData = await getSymbols()
        setSymbols(symbolsData.symbols)
        
        // 获取每个交易对的详细状态
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
        console.error('获取交易对列表失败:', error)
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

    fetchData()
    const interval = setInterval(fetchData, 10000) // 每10秒更新一次

    return () => clearInterval(interval)
  }, [toast])

  // 计算汇总数据
  const summary = useMemo(() => {
    let totalPnL = 0
    let totalTrades = 0
    let activeCount = 0
    let maxUptime = 0

    symbolStatuses.forEach((status) => {
      if (status.running) {
        activeCount++
      }
      totalPnL += status.total_pnl || 0
      totalTrades += status.total_trades || 0
      maxUptime = Math.max(maxUptime, status.uptime || 0)
    })

    return {
      totalPnL,
      totalTrades,
      activeCount,
      totalCount: symbols.length,
      maxUptime,
    }
  }, [symbols, symbolStatuses])

  const handleSymbolClick = (exchange: string, symbol: string) => {
    setSymbolPair(exchange, symbol)
  }

  const formatUptime = (seconds: number): string => {
    const hours = Math.floor(seconds / 3600)
    const minutes = Math.floor((seconds % 3600) / 60)
    return `${hours}h ${minutes}m`
  }

  if (loading) {
    return (
      <Center h="400px">
        <Spinner size="xl" color="blue.500" />
      </Center>
    )
  }

  return (
    <Box minH="100vh" bg="gray.900" color="gray.100" py={8}>
      <Container maxW="container.xl">
        <Heading size="xl" mb={8} color="white">
          全局概览
        </Heading>

        {/* 汇总统计 */}
        <SimpleGrid columns={{ base: 1, md: 2, lg: 4 }} spacing={6} mb={8}>
          <Card bg="gray.800" borderColor="gray.700" borderWidth={1}>
            <CardBody>
              <Stat>
                <StatLabel color="gray.400">总盈亏</StatLabel>
                <StatNumber color={summary.totalPnL >= 0 ? 'green.400' : 'red.400'}>
                  {summary.totalPnL >= 0 ? '+' : ''}
                  {summary.totalPnL.toFixed(2)} USDT
                </StatNumber>
                <StatHelpText color="gray.500">
                  {summary.totalPnL >= 0 && <StatArrow type="increase" />}
                  {summary.totalPnL < 0 && <StatArrow type="decrease" />}
                  所有交易对汇总
                </StatHelpText>
              </Stat>
            </CardBody>
          </Card>

          <Card bg="gray.800" borderColor="gray.700" borderWidth={1}>
            <CardBody>
              <Stat>
                <StatLabel color="gray.400">总交易量</StatLabel>
                <StatNumber color="blue.400">{summary.totalTrades}</StatNumber>
                <StatHelpText color="gray.500">累计成交笔数</StatHelpText>
              </Stat>
            </CardBody>
          </Card>

          <Card bg="gray.800" borderColor="gray.700" borderWidth={1}>
            <CardBody>
              <Stat>
                <StatLabel color="gray.400">活跃交易对</StatLabel>
                <StatNumber color="cyan.400">
                  {summary.activeCount} / {summary.totalCount}
                </StatNumber>
                <StatHelpText color="gray.500">正在运行</StatHelpText>
              </Stat>
            </CardBody>
          </Card>

          <Card bg="gray.800" borderColor="gray.700" borderWidth={1}>
            <CardBody>
              <Stat>
                <StatLabel color="gray.400">系统运行时间</StatLabel>
                <StatNumber color="purple.400">{formatUptime(summary.maxUptime)}</StatNumber>
                <StatHelpText color="gray.500">最长运行时间</StatHelpText>
              </Stat>
            </CardBody>
          </Card>
        </SimpleGrid>

        {/* 交易对卡片网格 */}
        <Heading size="lg" mb={6} color="white">
          交易对详情
        </Heading>
        
        {symbols.length === 0 ? (
          <Card bg="gray.800" borderColor="gray.700" borderWidth={1}>
            <CardBody>
              <Text color="gray.400" textAlign="center" py={8}>
                暂无配置的交易对
              </Text>
            </CardBody>
          </Card>
        ) : (
          <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={6}>
            {symbols.map((sym) => {
              const key = `${sym.exchange}:${sym.symbol}`
              const status = symbolStatuses.get(key)
              const isActive = sym.is_active && status?.running

              return (
                <Card
                  key={key}
                  bg="gray.800"
                  borderColor={isActive ? 'green.500' : 'gray.600'}
                  borderWidth={2}
                  cursor="pointer"
                  transition="all 0.2s"
                  _hover={{
                    transform: 'scale(1.02)',
                    borderColor: isActive ? 'green.400' : 'gray.500',
                    boxShadow: 'lg',
                  }}
                  onClick={() => handleSymbolClick(sym.exchange, sym.symbol)}
                >
                  <CardHeader pb={2}>
                    <Flex justify="space-between" align="center">
                      <Heading size="md" color="white">
                        {sym.symbol}
                      </Heading>
                      <Badge
                        colorScheme={isActive ? 'green' : 'gray'}
                        fontSize="sm"
                        px={2}
                        py={1}
                      >
                        {isActive ? (
                          <Flex align="center" gap={1}>
                            <Icon as={CheckCircleIcon} />
                            运行中
                          </Flex>
                        ) : (
                          <Flex align="center" gap={1}>
                            <Icon as={WarningIcon} />
                            未运行
                          </Flex>
                        )}
                      </Badge>
                    </Flex>
                    <Text color="gray.400" fontSize="sm" mt={1}>
                      {sym.exchange.toUpperCase()}
                    </Text>
                  </CardHeader>
                  <CardBody pt={2}>
                    <SimpleGrid columns={2} spacing={4}>
                      <Box>
                        <Text color="gray.500" fontSize="xs" mb={1}>
                          当前价格
                        </Text>
                        <Text color="white" fontSize="lg" fontWeight="semibold">
                          ${sym.current_price.toFixed(2)}
                        </Text>
                      </Box>
                      {status && (
                        <>
                          <Box>
                            <Text color="gray.500" fontSize="xs" mb={1}>
                              盈亏
                            </Text>
                            <Text
                              color={status.total_pnl >= 0 ? 'green.400' : 'red.400'}
                              fontSize="lg"
                              fontWeight="semibold"
                            >
                              {status.total_pnl >= 0 ? '+' : ''}
                              {status.total_pnl.toFixed(2)}
                            </Text>
                          </Box>
                          <Box>
                            <Text color="gray.500" fontSize="xs" mb={1}>
                              交易笔数
                            </Text>
                            <Text color="blue.400" fontSize="lg" fontWeight="semibold">
                              {status.total_trades}
                            </Text>
                          </Box>
                          <Box>
                            <Text color="gray.500" fontSize="xs" mb={1}>
                              运行时间
                            </Text>
                            <Text color="purple.400" fontSize="sm">
                              {formatUptime(status.uptime)}
                            </Text>
                          </Box>
                        </>
                      )}
                    </SimpleGrid>
                  </CardBody>
                </Card>
              )
            })}
          </SimpleGrid>
        )}
      </Container>
    </Box>
  )
}

export default GlobalDashboard

