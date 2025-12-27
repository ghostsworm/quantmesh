import React, { useEffect, useState } from 'react'
import { Link as RouterLink } from 'react-router-dom'
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
  Card,
  CardHeader,
  CardBody,
  Badge,
  Text,
  Link,
  Spinner,
  Center,
  useToast,
} from '@chakra-ui/react'
import { useSymbol } from '../contexts/SymbolContext'
import { getStatus, startTrading, stopTrading } from '../services/api'
import { getSlots, SlotsResponse } from '../services/api'
import { getStrategyAllocation, StrategyAllocationResponse } from '../services/api'
import { getPendingOrders, PendingOrdersResponse } from '../services/api'
import { getPositionsSummary } from '../services/api'

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

const Dashboard: React.FC = () => {
  const { selectedExchange, selectedSymbol } = useSymbol()
  const [status, setStatus] = useState<SystemStatus | null>(null)
  const [slotsInfo, setSlotsInfo] = useState<SlotsResponse | null>(null)
  const [strategyAllocation, setStrategyAllocation] = useState<StrategyAllocationResponse | null>(null)
  const [pendingOrders, setPendingOrders] = useState<PendingOrdersResponse | null>(null)
  const [positionsSummary, setPositionsSummary] = useState<any>(null)
  const [isTrading, setIsTrading] = useState(false)
  const toast = useToast()

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
      } catch (error) {
        console.error('Failed to fetch data:', error)
      }
    }

    fetchData()
    const interval = setInterval(fetchData, 5000) // 每5秒更新一次

    return () => clearInterval(interval)
  }, [selectedExchange, selectedSymbol])

  const handleStartTrading = async () => {
    try {
      await startTrading()
      setIsTrading(true)
      toast({
        title: '交易已启动',
        status: 'success',
        duration: 3000,
        isClosable: true,
      })
    } catch (error) {
      toast({
        title: '启动交易失败',
        description: error instanceof Error ? error.message : 'Unknown error',
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    }
  }

  const handleStopTrading = async () => {
    try {
      await stopTrading()
      setIsTrading(false)
      toast({
        title: '交易已停止',
        status: 'info',
        duration: 3000,
        isClosable: true,
      })
    } catch (error) {
      toast({
        title: '停止交易失败',
        description: error instanceof Error ? error.message : 'Unknown error',
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    }
  }

  if (!status) {
    return (
      <Center h="200px">
        <Spinner size="xl" />
      </Center>
    )
  }

  const formatUptime = (seconds: number) => {
    const days = Math.floor(seconds / 86400)
    const hours = Math.floor((seconds % 86400) / 3600)
    const minutes = Math.floor((seconds % 3600) / 60)
    if (days > 0) return `${days}天 ${hours}小时 ${minutes}分钟`
    if (hours > 0) return `${hours}小时 ${minutes}分钟`
    return `${minutes}分钟`
  }

  return (
    <Box>
      <Box display="flex" justifyContent="space-between" alignItems="center" mb={6}>
        <Heading size="lg">系统状态</Heading>
        <ButtonGroup>
          {isTrading ? (
            <Button
              colorScheme="red"
              onClick={handleStopTrading}
            >
              停止交易
            </Button>
          ) : (
            <Button
              colorScheme="green"
              onClick={handleStartTrading}
            >
              启动交易
            </Button>
          )}
        </ButtonGroup>
      </Box>

      <SimpleGrid columns={{ base: 1, md: 2, lg: 4 }} spacing={4} mb={8}>
        <Card>
          <CardBody>
            <Stat>
              <StatLabel>运行状态</StatLabel>
              <StatNumber>
                <Badge colorScheme={status.running ? 'green' : 'red'} fontSize="md">
                  {status.running ? '运行中' : '已停止'}
                </Badge>
              </StatNumber>
            </Stat>
          </CardBody>
        </Card>

        <Card>
          <CardBody>
            <Stat>
              <StatLabel>交易所</StatLabel>
              <StatNumber fontSize="xl">{status.exchange}</StatNumber>
              <StatHelpText>{status.symbol}</StatHelpText>
            </Stat>
          </CardBody>
        </Card>

        <Card>
          <CardBody>
            <Stat>
              <StatLabel>当前价格</StatLabel>
              <StatNumber>{status.current_price.toFixed(2)}</StatNumber>
            </Stat>
          </CardBody>
        </Card>

        <Card>
          <CardBody>
            <Stat>
              <StatLabel>总盈亏</StatLabel>
              <StatNumber color={status.total_pnl >= 0 ? 'green.500' : 'red.500'}>
                {status.total_pnl >= 0 ? '+' : ''}{status.total_pnl.toFixed(2)}
              </StatNumber>
            </Stat>
          </CardBody>
        </Card>

        <Card>
          <CardBody>
            <Stat>
              <StatLabel>总交易数</StatLabel>
              <StatNumber>{status.total_trades}</StatNumber>
            </Stat>
          </CardBody>
        </Card>

        <Card>
          <CardBody>
            <Stat>
              <StatLabel>风控状态</StatLabel>
              <StatNumber>
                <Badge colorScheme={status.risk_triggered ? 'red' : 'green'} fontSize="md">
                  {status.risk_triggered ? '已触发' : '正常'}
                </Badge>
              </StatNumber>
            </Stat>
          </CardBody>
        </Card>

        <Card>
          <CardBody>
            <Stat>
              <StatLabel>运行时间</StatLabel>
              <StatNumber fontSize="lg">{formatUptime(status.uptime)}</StatNumber>
            </Stat>
          </CardBody>
        </Card>
      </SimpleGrid>

      {/* 槽位统计卡片 */}
      {slotsInfo && (
        <Box mb={8}>
          <Heading size="md" mb={4}>槽位统计</Heading>
          <SimpleGrid columns={{ base: 1, md: 3 }} spacing={4}>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>总槽位数</StatLabel>
                  <StatNumber>{slotsInfo.count}</StatNumber>
                  <StatHelpText>
                    <Link as={RouterLink} to="/slots" color="blue.500">
                      查看详情 →
                    </Link>
                  </StatHelpText>
                </Stat>
              </CardBody>
            </Card>

            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>有仓槽位</StatLabel>
                  <StatNumber color="green.500">
                    {slotsInfo.slots.filter(s => s.position_status === 'FILLED').length}
                  </StatNumber>
                </Stat>
              </CardBody>
            </Card>

            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>空仓槽位</StatLabel>
                  <StatNumber color="gray.500">
                    {slotsInfo.slots.filter(s => s.position_status === 'EMPTY').length}
                  </StatNumber>
                </Stat>
              </CardBody>
            </Card>
          </SimpleGrid>
        </Box>
      )}

      {/* 策略配比概览 */}
      {strategyAllocation && Object.keys(strategyAllocation.allocation).length > 0 && (
        <Box mb={8}>
          <Heading size="md" mb={4}>策略资金配比</Heading>
          <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={4}>
            {Object.entries(strategyAllocation.allocation).map(([name, cap]) => (
              <Card key={name}>
                <CardBody>
                  <Stat>
                    <StatLabel>{name}</StatLabel>
                    <StatNumber>{cap.allocated.toFixed(2)} USDT</StatNumber>
                    <StatHelpText>
                      权重: {(cap.weight * 100).toFixed(1)}% | 可用: {cap.available.toFixed(2)} USDT
                    </StatHelpText>
                  </Stat>
                </CardBody>
              </Card>
            ))}
          </SimpleGrid>
          <Link as={RouterLink} to="/strategies" color="blue.500" mt={4} display="inline-block">
            查看详细配比 →
          </Link>
        </Box>
      )}

      {/* 持仓汇总卡片 */}
      {positionsSummary && positionsSummary.position_count > 0 && (
        <Box mb={8}>
          <Heading size="md" mb={4}>持仓概览</Heading>
          <SimpleGrid columns={{ base: 1, md: 3 }} spacing={4}>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>总持仓数量</StatLabel>
                  <StatNumber>{positionsSummary.total_quantity?.toFixed(4) || '0'}</StatNumber>
                </Stat>
              </CardBody>
            </Card>

            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>总持仓价值</StatLabel>
                  <StatNumber>{positionsSummary.total_value?.toFixed(2) || '0'}</StatNumber>
                </Stat>
              </CardBody>
            </Card>

            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>未实现盈亏</StatLabel>
                  <StatNumber color={(positionsSummary.unrealized_pnl || 0) >= 0 ? 'green.500' : 'red.500'}>
                    {(positionsSummary.unrealized_pnl || 0) >= 0 ? '+' : ''}{positionsSummary.unrealized_pnl?.toFixed(2) || '0'}
                  </StatNumber>
                </Stat>
              </CardBody>
            </Card>
          </SimpleGrid>
          <Link as={RouterLink} to="/positions" color="blue.500" mt={4} display="inline-block">
            查看详细持仓 →
          </Link>
        </Box>
      )}

      {/* 待成交订单提示 */}
      {pendingOrders && pendingOrders.count > 0 && (
        <Box>
          <Heading size="md" mb={4}>待成交订单</Heading>
          <Card>
            <CardBody>
              <Text fontSize="lg" fontWeight="bold" mb={2}>
                当前有 {pendingOrders.count} 个待成交订单
              </Text>
              <Link as={RouterLink} to="/orders" color="blue.500">
                查看详情 →
              </Link>
            </CardBody>
          </Card>
        </Box>
      )}
    </Box>
  )
}

export default Dashboard
