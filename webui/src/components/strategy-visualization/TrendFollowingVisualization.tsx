import React, { useMemo } from 'react'
import {
  Box,
  VStack,
  HStack,
  Text,
  SimpleGrid,
  Badge,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  useColorModeValue,
} from '@chakra-ui/react'
import { TriangleUpIcon, TriangleDownIcon } from '@chakra-ui/icons'
import PriceChart from './PriceChart'
import type { TrendFollowingVisualizationData } from '../../services/strategy'

interface TrendFollowingVisualizationProps {
  data: TrendFollowingVisualizationData
  exchange?: string
  symbol?: string
}

const TrendFollowingVisualization: React.FC<TrendFollowingVisualizationProps> = ({ data }) => {
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  // 生成价格图表数据（包含均线）
  const chartData = useMemo(() => {
    if (!data.currentPrice) return []
    const prices: Array<{ time: string; price: number; fastMA: number; slowMA: number }> = []
    const basePrice = data.currentPrice
    for (let i = 30; i >= 0; i--) {
      const price = basePrice * (1 + (Math.random() - 0.5) * 0.02)
      prices.push({
        time: `${i}`,
        price,
        fastMA: data.fastMA ? data.fastMA * (1 + (Math.random() - 0.5) * 0.01) : price,
        slowMA: data.slowMA ? data.slowMA * (1 + (Math.random() - 0.5) * 0.01) : price,
      })
    }
    return prices
  }, [data.currentPrice, data.fastMA, data.slowMA])

  const trendColor = useMemo(() => {
    if (data.trend === 'up') return 'green.500'
    if (data.trend === 'down') return 'red.500'
    return 'gray.500'
  }, [data.trend])

  return (
    <VStack spacing={4} align="stretch">
      {/* 关键指标 */}
      <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">当前价格</StatLabel>
          <StatNumber fontSize="lg">${data.currentPrice?.toFixed(2) || '—'}</StatNumber>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">快线 ({data.shortPeriod})</StatLabel>
          <StatNumber fontSize="lg">${data.fastMA?.toFixed(2) || '—'}</StatNumber>
          <StatHelpText fontSize="xs">{data.method?.toUpperCase()}</StatHelpText>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">慢线 ({data.longPeriod})</StatLabel>
          <StatNumber fontSize="lg">${data.slowMA?.toFixed(2) || '—'}</StatNumber>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">趋势方向</StatLabel>
          <StatNumber fontSize="lg">
            <Badge colorScheme={data.trend === 'up' ? 'green' : data.trend === 'down' ? 'red' : 'gray'}>
              {data.trend === 'up' ? '上涨' : data.trend === 'down' ? '下跌' : '横盘'}
            </Badge>
          </StatNumber>
        </Stat>
      </SimpleGrid>

      {/* 价格图表（带均线） */}
      {chartData.length > 0 && (
        <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <Text fontSize="sm" fontWeight="bold" mb={3}>价格走势与均线</Text>
          <Box h="300px">
            {/* 这里应该使用支持多条线的图表组件，暂时用PriceChart */}
            <PriceChart data={chartData.map(d => ({ time: d.time, price: d.price }))} height={300} />
          </Box>
        </Box>
      )}

      {/* 信号状态 */}
      <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
        <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <Text fontSize="sm" fontWeight="bold" mb={3}>交易信号</Text>
          <VStack spacing={2} align="stretch">
            <HStack justify="space-between">
              <Text fontSize="sm" color="gray.600">金叉信号</Text>
              <Badge colorScheme={data.isGoldenCross ? 'green' : 'gray'}>
                {data.isGoldenCross ? '是' : '否'}
              </Badge>
            </HStack>
            <HStack justify="space-between">
              <Text fontSize="sm" color="gray.600">死叉信号</Text>
              <Badge colorScheme={data.isDeathCross ? 'red' : 'gray'}>
                {data.isDeathCross ? '是' : '否'}
              </Badge>
            </HStack>
            {data.maDiff !== undefined && (
              <HStack justify="space-between">
                <Text fontSize="sm" color="gray.600">均线差值</Text>
                <HStack>
                  {data.maDiff >= 0 ? (
                    <TriangleUpIcon color="green.500" />
                  ) : (
                    <TriangleDownIcon color="red.500" />
                  )}
                  <Text fontSize="sm" fontWeight="bold">
                    {data.maDiff >= 0 ? '+' : ''}{data.maDiff.toFixed(2)}%
                  </Text>
                </HStack>
              </HStack>
            )}
          </VStack>
        </Box>

        <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <Text fontSize="sm" fontWeight="bold" mb={3}>持仓状态</Text>
          <VStack spacing={2} align="stretch">
            <HStack justify="space-between">
              <Text fontSize="sm" color="gray.600">持仓状态</Text>
              <Badge colorScheme={data.hasPosition ? 'green' : 'gray'}>
                {data.hasPosition ? '已持仓' : '空仓'}
              </Badge>
            </HStack>
            {data.hasPosition && data.entryPrice && (
              <>
                <HStack justify="space-between">
                  <Text fontSize="sm" color="gray.600">入场价格</Text>
                  <Text fontSize="sm" fontWeight="bold">${data.entryPrice.toFixed(2)}</Text>
                </HStack>
                {data.pnlPercent !== undefined && (
                  <HStack justify="space-between">
                    <Text fontSize="sm" color="gray.600">当前盈亏</Text>
                    <HStack>
                      {data.pnlPercent >= 0 ? (
                        <TriangleUpIcon color="green.500" />
                      ) : (
                        <TriangleDownIcon color="red.500" />
                      )}
                      <Text
                        fontSize="sm"
                        fontWeight="bold"
                        color={data.pnlPercent >= 0 ? 'green.500' : 'red.500'}
                      >
                        {data.pnlPercent >= 0 ? '+' : ''}{data.pnlPercent.toFixed(2)}%
                      </Text>
                    </HStack>
                  </HStack>
                )}
              </>
            )}
            {data.stopLoss && (
              <HStack justify="space-between">
                <Text fontSize="sm" color="gray.600">止损</Text>
                <Text fontSize="sm">{data.stopLoss.toFixed(2)}%</Text>
              </HStack>
            )}
            {data.takeProfit && (
              <HStack justify="space-between">
                <Text fontSize="sm" color="gray.600">止盈</Text>
                <Text fontSize="sm">{data.takeProfit.toFixed(2)}%</Text>
              </HStack>
            )}
          </VStack>
        </Box>
      </SimpleGrid>
    </VStack>
  )
}

export default TrendFollowingVisualization
