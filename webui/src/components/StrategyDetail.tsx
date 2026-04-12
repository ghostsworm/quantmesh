import React, { useEffect, useState } from 'react'
import {
  Box,
  Container,
  Heading,
  HStack,
  VStack,
  Text,
  Badge,
  Spinner,
  Center,
  useToast,
  Button,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  TableContainer,
  useColorModeValue,
} from '@chakra-ui/react'
import { ChevronLeftIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { getStrategyRuntimeStatusById, type StrategyRuntimeStatus } from '../services/strategy'
import { useSymbol } from '../contexts/SymbolContext'
import StrategyVisualization from './strategy-visualization/StrategyVisualization'

const StrategyDetail: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const { setSymbolPair } = useSymbol()
  const exchange = searchParams.get('exchange') || ''
  const symbol = searchParams.get('symbol') || ''
  const marketType = searchParams.get('market_type') || 'futures'
  const strategyName = searchParams.get('strategy') || ''

  const [strategy, setStrategy] = useState<StrategyRuntimeStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const toast = useToast()

  const cardBg = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  useEffect(() => {
    if (!exchange || !symbol || !strategyName) {
      setLoading(false)
      return
    }
    const fetchData = async () => {
      setLoading(true)
      try {
        const res = await getStrategyRuntimeStatusById(strategyName, exchange, symbol, marketType)
        if (res.success && res.strategy) {
          setStrategy(res.strategy)
        } else {
          setStrategy(null)
        }
      } catch (err) {
        console.error('Failed to fetch strategy detail:', err)
        toast({
          title: t('common.error'),
          description: t('common.loadFailed'),
          status: 'error',
          duration: 3000,
        })
        setStrategy(null)
      } finally {
        setLoading(false)
      }
    }
    fetchData()
  }, [exchange, symbol, marketType, strategyName, t, toast])

  const handleBack = () => {
    navigate('/strategy-overview')
  }

  const handleGoToDashboard = () => {
    setSymbolPair(exchange, symbol, marketType)
  }

  if (!exchange || !symbol || !strategyName) {
    return (
      <Container maxW="container.xl" py={6}>
        <Center py={16}>
          <VStack spacing={4}>
            <Text color="gray.500">{t('strategyDetail.missingParams', '缺少必要参数')}</Text>
            <Button leftIcon={<ChevronLeftIcon />} onClick={() => navigate('/strategy-overview')}>
              {t('strategyDetail.backToOverview', '返回策略总览')}
            </Button>
          </VStack>
        </Center>
      </Container>
    )
  }

  if (loading) {
    return (
      <Center minH="300px">
        <Spinner size="xl" thickness="3px" color="blue.500" />
      </Center>
    )
  }

  if (!strategy) {
    return (
      <Container maxW="container.xl" py={6}>
        <Center py={16}>
          <VStack spacing={4}>
            <Text color="gray.500">{t('strategyDetail.notFound', '未找到该策略')}</Text>
            <Button leftIcon={<ChevronLeftIcon />} onClick={handleBack}>
              {t('strategyDetail.backToOverview', '返回策略总览')}
            </Button>
          </VStack>
        </Center>
      </Container>
    )
  }

  return (
    <Container maxW="container.xl" py={6}>
      <HStack mb={6} spacing={4}>
        <Button leftIcon={<ChevronLeftIcon />} variant="ghost" size="sm" onClick={handleBack}>
          {t('strategyDetail.backToOverview', '返回策略总览')}
        </Button>
        <Button variant="outline" size="sm" onClick={handleGoToDashboard}>
          {t('strategyDetail.goToTradingPanel', '进入交易面板')}
        </Button>
      </HStack>

      <Heading size="lg" mb={6}>
        {t(`strategyNames.${strategy.name}`, { defaultValue: strategy.name })} · {symbol}
      </Heading>

      <VStack align="stretch" spacing={6}>
        <HStack spacing={4} flexWrap="wrap">
          <Badge colorScheme={strategy.isRunning ? 'green' : 'gray'} fontSize="sm">
            {strategy.isRunning ? t('strategyOverview.running', '运行中') : t('strategyOverview.stopped', '已停止')}
          </Badge>
          <Text fontSize="sm" color="gray.500">
            {exchange} · {marketType === 'spot' ? t('strategyOverview.spot', '现货') : t('strategyOverview.futures', '合约')}
          </Text>
        </HStack>

        {strategy.statistics && (
          <HStack spacing={6} flexWrap="wrap">
            <Box>
              <Text fontSize="xs" color="gray.500">{t('strategyOverview.pnl', '累计盈亏')}</Text>
              <Text fontWeight="bold" color={strategy.statistics.totalPnL >= 0 ? 'green.500' : 'red.500'}>
                {strategy.statistics.totalPnL >= 0 ? '+' : ''}{strategy.statistics.totalPnL.toFixed(2)} USDT
              </Text>
            </Box>
            <Box>
              <Text fontSize="xs" color="gray.500">{t('strategyDetail.totalTrades', '总交易数')}</Text>
              <Text fontWeight="bold">{strategy.statistics.totalTrades}</Text>
            </Box>
            <Box>
              <Text fontSize="xs" color="gray.500">{t('strategyDetail.winRate', '胜率')}</Text>
              <Text fontWeight="bold">{(strategy.statistics.winRate * 100).toFixed(1)}%</Text>
            </Box>
            <Box>
              <Text fontSize="xs" color="gray.500">{t('strategyOverview.positions', '持仓数')}</Text>
              <Text fontWeight="bold">{strategy.positionCount}</Text>
            </Box>
            <Box>
              <Text fontSize="xs" color="gray.500">{t('strategyOverview.orders', '挂单数')}</Text>
              <Text fontWeight="bold">{strategy.orderCount}</Text>
            </Box>
          </HStack>
        )}

        {strategy.visualizationData && (
          <Box bg={cardBg} p={4} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
            <Text fontWeight="semibold" mb={3}>{t('dashboard.strategyVisualization', '策略可视化')}</Text>
            <StrategyVisualization strategy={strategy} exchange={exchange} symbol={symbol} />
          </Box>
        )}

        {strategy.positions && strategy.positions.length > 0 && (
          <Box bg={cardBg} p={4} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
            <Text fontWeight="semibold" mb={3}>{t('strategyDetail.positions', '持仓列表')}</Text>
            <TableContainer>
              <Table size="sm">
                <Thead>
                  <Tr>
                    <Th>{t('strategyDetail.symbol', '交易对')}</Th>
                    <Th>{t('strategyDetail.size', '数量')}</Th>
                    <Th>{t('strategyDetail.entryPrice', '开仓价')}</Th>
                    <Th>{t('strategyDetail.currentPrice', '现价')}</Th>
                    <Th>{t('strategyDetail.pnl', '盈亏')}</Th>
                  </Tr>
                </Thead>
                <Tbody>
                  {strategy.positions.map((p, i) => (
                    <Tr key={i}>
                      <Td>{p.symbol}</Td>
                      <Td>{p.size.toFixed(6)}</Td>
                      <Td>{p.entryPrice.toFixed(2)}</Td>
                      <Td>{p.currentPrice.toFixed(2)}</Td>
                      <Td color={p.pnl >= 0 ? 'green.500' : 'red.500'}>
                        {p.pnl >= 0 ? '+' : ''}{p.pnl.toFixed(2)}
                      </Td>
                    </Tr>
                  ))}
                </Tbody>
              </Table>
            </TableContainer>
          </Box>
        )}

        {strategy.orders && strategy.orders.length > 0 && (
          <Box bg={cardBg} p={4} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
            <Text fontWeight="semibold" mb={3}>{t('strategyDetail.orders', '订单列表')}</Text>
            <TableContainer>
              <Table size="sm">
                <Thead>
                  <Tr>
                    <Th>{t('strategyDetail.orderId', '订单ID')}</Th>
                    <Th>{t('strategyDetail.side', '方向')}</Th>
                    <Th>{t('strategyDetail.price', '价格')}</Th>
                    <Th>{t('strategyDetail.quantity', '数量')}</Th>
                    <Th>{t('strategyDetail.status', '状态')}</Th>
                  </Tr>
                </Thead>
                <Tbody>
                  {strategy.orders.map((o, i) => (
                    <Tr key={i}>
                      <Td fontFamily="mono" fontSize="xs">{o.orderId}</Td>
                      <Td>{o.side}</Td>
                      <Td>{o.price.toFixed(2)}</Td>
                      <Td>{o.quantity.toFixed(6)}</Td>
                      <Td>{o.status}</Td>
                    </Tr>
                  ))}
                </Tbody>
              </Table>
            </TableContainer>
          </Box>
        )}
      </VStack>
    </Container>
  )
}

export default StrategyDetail
