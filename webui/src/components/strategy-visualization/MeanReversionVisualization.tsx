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
  Progress,
  useColorModeValue,
} from '@chakra-ui/react'
import { TriangleUpIcon, TriangleDownIcon } from '@chakra-ui/icons'
import PriceChart from './PriceChart'
import type { MeanReversionVisualizationData } from '../../services/strategy'

interface MeanReversionVisualizationProps {
  data: MeanReversionVisualizationData
  exchange?: string
  symbol?: string
}

const MeanReversionVisualization: React.FC<MeanReversionVisualizationProps> = ({ data }) => {
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  // 生成价格图表数据
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

  // 计算价格在布林带中的位置百分比
  const positionPercent = data.positionInBand || 50

  return (
    <VStack spacing={4} align="stretch">
      {/* 关键指标 */}
      <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">当前价格</StatLabel>
          <StatNumber fontSize="lg">${data.currentPrice?.toFixed(2) || '—'}</StatNumber>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">上轨</StatLabel>
          <StatNumber fontSize="lg">${data.upperBand?.toFixed(2) || '—'}</StatNumber>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">中轨（均值）</StatLabel>
          <StatNumber fontSize="lg">${data.middleBand?.toFixed(2) || '—'}</StatNumber>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">下轨</StatLabel>
          <StatNumber fontSize="lg">${data.lowerBand?.toFixed(2) || '—'}</StatNumber>
        </Stat>
      </SimpleGrid>

      {/* 价格在布林带中的位置 */}
      {data.upperBand && data.lowerBand && (
        <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <Text fontSize="sm" fontWeight="bold" mb={3}>价格在布林带中的位置</Text>
          <VStack spacing={2}>
            <HStack w="100%" justify="space-between" fontSize="xs" color="gray.600">
              <Text>下轨 (${data.lowerBand.toFixed(2)})</Text>
              <Text>上轨 (${data.upperBand.toFixed(2)})</Text>
            </HStack>
            <Box w="100%" position="relative">
              <Progress
                value={positionPercent}
                colorScheme={positionPercent < 20 ? 'green' : positionPercent > 80 ? 'red' : 'blue'}
                height="30px"
                borderRadius="md"
              />
              <Box
                position="absolute"
                left={`${positionPercent}%`}
                top="50%"
                transform="translate(-50%, -50%)"
                bg="white"
                px={2}
                py={1}
                borderRadius="md"
                boxShadow="md"
                fontSize="xs"
                fontWeight="bold"
              >
                ${data.currentPrice?.toFixed(2)}
              </Box>
            </Box>
            <Text fontSize="xs" color="gray.500">
              位置: {positionPercent.toFixed(1)}% ({positionPercent < 20 ? '接近下轨' : positionPercent > 80 ? '接近上轨' : '中间区域'})
            </Text>
          </VStack>
        </Box>
      )}

      {/* 价格图表 */}
      {chartData.length > 0 && (
        <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <Text fontSize="sm" fontWeight="bold" mb={3}>价格走势与布林带</Text>
          <PriceChart data={chartData} height={250} />
        </Box>
      )}

      {/* 交易信号和持仓状态 */}
      <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
        <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <Text fontSize="sm" fontWeight="bold" mb={3}>交易信号</Text>
          <VStack spacing={2} align="stretch">
            <HStack justify="space-between">
              <Text fontSize="sm" color="gray.600">买入信号</Text>
              <Badge colorScheme={data.buySignal ? 'green' : 'gray'}>
                {data.buySignal ? '是' : '否'}
              </Badge>
            </HStack>
            <HStack justify="space-between">
              <Text fontSize="sm" color="gray.600">卖出信号</Text>
              <Badge colorScheme={data.sellSignal ? 'red' : 'gray'}>
                {data.sellSignal ? '是' : '否'}
              </Badge>
            </HStack>
            {data.touchesLowerBand && (
              <HStack>
                <TriangleDownIcon color="green.500" />
                <Text fontSize="sm" color="green.600">价格触及下轨</Text>
              </HStack>
            )}
            {data.touchesUpperBand && (
              <HStack>
                <TriangleUpIcon color="red.500" />
                <Text fontSize="sm" color="red.600">价格触及上轨</Text>
              </HStack>
            )}
            {data.distanceToBuy !== undefined && !data.hasPosition && (
              <HStack justify="space-between">
                <Text fontSize="sm" color="gray.600">距离买入点</Text>
                <Text fontSize="sm" fontWeight="bold">
                  {data.distanceToBuy >= 0 ? '+' : ''}{data.distanceToBuy.toFixed(2)}%
                </Text>
              </HStack>
            )}
            {data.distanceToSell !== undefined && data.hasPosition && (
              <HStack justify="space-between">
                <Text fontSize="sm" color="gray.600">距离卖出点</Text>
                <Text fontSize="sm" fontWeight="bold">
                  {data.distanceToSell >= 0 ? '+' : ''}{data.distanceToSell.toFixed(2)}%
                </Text>
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
            <HStack justify="space-between">
              <Text fontSize="sm" color="gray.600">周期</Text>
              <Text fontSize="sm">{data.period}</Text>
            </HStack>
            <HStack justify="space-between">
              <Text fontSize="sm" color="gray.600">标准差倍数</Text>
              <Text fontSize="sm">{data.stdMultiplier}</Text>
            </HStack>
          </VStack>
        </Box>
      </SimpleGrid>
    </VStack>
  )
}

export default MeanReversionVisualization
