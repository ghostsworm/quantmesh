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
  Divider,
  useColorModeValue,
} from '@chakra-ui/react'
import { TriangleUpIcon, TriangleDownIcon, WarningIcon } from '@chakra-ui/icons'
import PriceChart from './PriceChart'
import type { DCAVisualizationData } from '../../services/strategy'

interface DCAVisualizationProps {
  data: DCAVisualizationData
  exchange?: string
  symbol?: string
}

const DCAVisualization: React.FC<DCAVisualizationProps> = ({ data }) => {
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  // 生成价格图表数据（模拟最近的价格走势）
  const chartData = useMemo(() => {
    if (!data.currentPrice) return []
    const prices: Array<{ time: string; price: number }> = []
    const basePrice = data.currentPrice
    for (let i = 20; i >= 0; i--) {
      prices.push({
        time: `${i}`,
        price: basePrice * (1 + (Math.random() - 0.5) * 0.02),
      })
    }
    return prices
  }, [data.currentPrice])

  // 生成参考线（止盈止损）
  const referenceLines = useMemo(() => {
    const lines: Array<{ value: number; label: string; color: string }> = []
    if (data.avgEntryPrice) {
      if (data.firstOrderTakeProfit) {
        lines.push({
          value: data.firstOrderTakeProfit,
          label: '首单止盈',
          color: '#38A169',
        })
      }
      if (data.totalTakeProfit) {
        lines.push({
          value: data.totalTakeProfit,
          label: '全仓止盈',
          color: '#38A169',
        })
      }
      if (data.stopLoss) {
        lines.push({
          value: data.stopLoss,
          label: '止损',
          color: '#E53E3E',
        })
      }
    }
    return lines
  }, [data.avgEntryPrice, data.firstOrderTakeProfit, data.totalTakeProfit, data.stopLoss])

  return (
    <VStack spacing={4} align="stretch">
      {/* 关键指标 */}
      <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">当前价格</StatLabel>
          <StatNumber fontSize="lg">${data.currentPrice?.toFixed(2) || '—'}</StatNumber>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">平均成本</StatLabel>
          <StatNumber fontSize="lg">${data.avgEntryPrice?.toFixed(2) || '—'}</StatNumber>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">ATR值</StatLabel>
          <StatNumber fontSize="lg">{data.atr?.toFixed(4) || '—'}</StatNumber>
          <StatHelpText fontSize="xs">动态间距: {data.dynamicInterval?.toFixed(2)}%</StatHelpText>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">当前层级</StatLabel>
          <StatNumber fontSize="lg">
            {data.currentLayer || 0}/{data.maxLayers || 0}
          </StatNumber>
        </Stat>
      </SimpleGrid>

      {/* 价格图表 */}
      {chartData.length > 0 && (
        <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <Text fontSize="sm" fontWeight="bold" mb={3}>价格走势</Text>
          <PriceChart data={chartData} height={250} referenceLines={referenceLines} />
        </Box>
      )}

      {/* 分层持仓 */}
      {data.layers && data.layers.length > 0 && (
        <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <Text fontSize="sm" fontWeight="bold" mb={3}>分层持仓</Text>
          <VStack spacing={2} align="stretch" maxH="300px" overflowY="auto">
            {data.layers.map((layer, index) => {
              const isProfit = layer.pnl >= 0
              return (
                <Box
                  key={index}
                  p={3}
                  bg={useColorModeValue('gray.50', 'gray.700')}
                  borderRadius="md"
                  borderLeft="4px solid"
                  borderLeftColor={isProfit ? 'green.500' : 'red.500'}
                >
                  <HStack justify="space-between" mb={2}>
                    <HStack>
                      <Badge colorScheme={layer.status === 'filled' ? 'green' : 'gray'}>
                        第{layer.index}层
                      </Badge>
                      <Text fontSize="sm" fontWeight="bold">
                        ${layer.price.toFixed(2)}
                      </Text>
                    </HStack>
                    <HStack>
                      {isProfit ? (
                        <TriangleUpIcon color="green.500" />
                      ) : (
                        <TriangleDownIcon color="red.500" />
                      )}
                      <Text fontSize="sm" color={isProfit ? 'green.500' : 'red.500'}>
                        {isProfit ? '+' : ''}{layer.pnlPercent.toFixed(2)}%
                      </Text>
                    </HStack>
                  </HStack>
                  <SimpleGrid columns={3} spacing={2} fontSize="xs" color="gray.600">
                    <Text>数量: {layer.quantity.toFixed(4)}</Text>
                    <Text>成本: ${layer.cost.toFixed(2)}</Text>
                    <Text>盈亏: ${layer.pnl.toFixed(2)}</Text>
                  </SimpleGrid>
                </Box>
              )
            })}
          </VStack>
        </Box>
      )}

      {/* 决策依据 */}
      <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
        <Text fontSize="sm" fontWeight="bold" mb={3}>决策依据</Text>
        <VStack spacing={2} align="stretch" fontSize="sm">
          {data.nextBuyPrice && data.distanceToNextBuy !== undefined && (
            <HStack justify="space-between">
              <Text color="gray.600">下一买入点</Text>
              <HStack>
                <Text fontWeight="bold">${data.nextBuyPrice.toFixed(2)}</Text>
                <Text color="gray.500">
                  ({data.distanceToNextBuy >= 0 ? '+' : ''}{data.distanceToNextBuy.toFixed(2)}%)
                </Text>
              </HStack>
            </HStack>
          )}
          {data.isPaused && (
            <HStack>
              <WarningIcon color="orange.500" />
              <Text color="orange.600">瀑布保护已激活，暂停加仓</Text>
            </HStack>
          )}
          {data.trendFilterEnabled !== undefined && (
            <HStack justify="space-between">
              <Text color="gray.600">趋势过滤</Text>
              <Badge colorScheme={data.isTrendUp ? 'green' : 'red'}>
                {data.isTrendUp ? '向上' : '向下'}
              </Badge>
            </HStack>
          )}
          {data.takeProfitTriggered && (
            <HStack>
              <Text color="green.600">追踪止盈已激活</Text>
              <Text color="gray.500">最高盈利: {data.highestProfit?.toFixed(2)}%</Text>
            </HStack>
          )}
        </VStack>
      </Box>
    </VStack>
  )
}

export default DCAVisualization
