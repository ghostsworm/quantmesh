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
import PriceChart from './PriceChart'
import type { GridVisualizationData } from '../../services/strategy'

interface GridVisualizationProps {
  data: GridVisualizationData
  exchange?: string
  symbol?: string
}

const GridVisualization: React.FC<GridVisualizationProps> = ({ data }) => {
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  // 生成价格区间图表数据
  const chartData = useMemo(() => {
    if (!data.minPrice || !data.maxPrice) return []
    const prices: Array<{ time: string; price: number }> = []
    const range = data.maxPrice - data.minPrice
    const midPrice = (data.minPrice + data.maxPrice) / 2
    
    // 生成模拟价格数据
    for (let i = 20; i >= 0; i--) {
      prices.push({
        time: `${i}`,
        price: midPrice + (Math.random() - 0.5) * range * 0.8,
      })
    }
    return prices
  }, [data.minPrice, data.maxPrice])

  // 计算填充率
  const fillRate = data.slotCount && data.filledCount
    ? (data.filledCount / data.slotCount) * 100
    : 0

  return (
    <VStack spacing={4} align="stretch">
      {/* 关键指标 */}
      <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">总槽位数</StatLabel>
          <StatNumber fontSize="lg">{data.slotCount || 0}</StatNumber>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">已填充</StatLabel>
          <StatNumber fontSize="lg" color="green.500">
            {data.filledCount || 0}
          </StatNumber>
          <StatHelpText fontSize="xs">填充率: {fillRate.toFixed(1)}%</StatHelpText>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">价格区间</StatLabel>
          <StatNumber fontSize="lg">
            ${data.minPrice?.toFixed(2) || '—'} - ${data.maxPrice?.toFixed(2) || '—'}
          </StatNumber>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">价格间隔</StatLabel>
          <StatNumber fontSize="lg">${data.priceInterval?.toFixed(2) || '—'}</StatNumber>
        </Stat>
      </SimpleGrid>

      {/* 价格区间图表 */}
      {chartData.length > 0 && (
        <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <Text fontSize="sm" fontWeight="bold" mb={3}>网格价格区间</Text>
          <PriceChart data={chartData} height={250} />
          {data.minPrice && data.maxPrice && (
            <HStack mt={2} fontSize="xs" color="gray.500" justify="space-between">
              <Text>最低价: ${data.minPrice.toFixed(2)}</Text>
              <Text>价格范围: ${data.priceRange?.toFixed(2) || '—'}</Text>
              <Text>最高价: ${data.maxPrice.toFixed(2)}</Text>
            </HStack>
          )}
        </Box>
      )}

      {/* 槽位状态概览 */}
      {data.slots && data.slots.length > 0 && (
        <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <Text fontSize="sm" fontWeight="bold" mb={3}>槽位状态概览</Text>
          <VStack spacing={2} align="stretch" maxH="300px" overflowY="auto">
            {/* 只显示前20个槽位，避免列表过长 */}
            {data.slots.slice(0, 20).map((slot, index) => (
              <Box
                key={index}
                p={2}
                bg={useColorModeValue('gray.50', 'gray.700')}
                borderRadius="md"
                borderLeft="4px solid"
                borderLeftColor={
                  slot.positionStatus === 'FILLED'
                    ? 'green.500'
                    : slot.slotStatus === 'LOCKED'
                    ? 'red.500'
                    : 'gray.300'
                }
              >
                <HStack justify="space-between" fontSize="sm">
                  <HStack>
                    <Text fontWeight="bold">${slot.price.toFixed(2)}</Text>
                    <Badge
                      colorScheme={
                        slot.positionStatus === 'FILLED'
                          ? 'green'
                          : slot.slotStatus === 'LOCKED'
                          ? 'red'
                          : 'gray'
                      }
                      fontSize="xs"
                    >
                      {slot.positionStatus === 'FILLED'
                        ? '已填充'
                        : slot.slotStatus === 'LOCKED'
                        ? '锁定'
                        : '空闲'}
                    </Badge>
                  </HStack>
                  {slot.positionQty > 0 && (
                    <Text fontSize="xs" color="gray.600">
                      数量: {slot.positionQty.toFixed(4)}
                    </Text>
                  )}
                </HStack>
              </Box>
            ))}
            {data.slots.length > 20 && (
              <Text fontSize="xs" color="gray.500" textAlign="center" mt={2}>
                显示前20个槽位，共{data.slots.length}个
              </Text>
            )}
          </VStack>
        </Box>
      )}
    </VStack>
  )
}

export default GridVisualization
