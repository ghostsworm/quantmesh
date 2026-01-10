import React, { useMemo } from 'react'
import { Box, VStack, HStack, Text, Flex, useColorModeValue } from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import type { StrategyCapitalInfo } from '../../types/capital'

interface AllocationChartProps {
  strategies: StrategyCapitalInfo[]
  totalCapital: number
}

const COLORS = [
  '#3182CE', // blue.500
  '#805AD5', // purple.500
  '#38A169', // green.500
  '#DD6B20', // orange.500
  '#E53E3E', // red.500
  '#00B5D8', // cyan.500
  '#D69E2E', // yellow.500
  '#ED64A6', // pink.500
]

const AllocationChart: React.FC<AllocationChartProps> = ({ strategies, totalCapital }) => {
  const { t } = useTranslation()
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  const chartData = useMemo(() => {
    const allocated = strategies.reduce((sum, s) => sum + s.allocated, 0)
    const unallocated = Math.max(0, totalCapital - allocated)

    const data = strategies.map((s, index) => ({
      id: s.strategyId,
      name: s.strategyName,
      value: s.allocated,
      percentage: totalCapital > 0 ? (s.allocated / totalCapital) * 100 : 0,
      color: COLORS[index % COLORS.length],
      used: s.used,
      available: s.available,
    }))

    if (unallocated > 0) {
      data.push({
        id: 'unallocated',
        name: t('capitalManagement.unallocated'),
        value: unallocated,
        percentage: (unallocated / totalCapital) * 100,
        color: '#A0AEC0', // gray.400
        used: 0,
        available: unallocated,
      })
    }

    return data
  }, [strategies, totalCapital, t])

  const totalAllocated = strategies.reduce((sum, s) => sum + s.allocated, 0)
  const totalUsed = strategies.reduce((sum, s) => sum + s.used, 0)

  // Simple bar chart
  return (
    <Box
      p={6}
      borderWidth="1px"
      borderRadius="xl"
      borderColor={borderColor}
      bg={bgColor}
    >
      <VStack align="stretch" spacing={4}>
        <Text fontWeight="bold" fontSize="lg">
          {t('capitalManagement.allocationChart')}
        </Text>

        {/* Stacked Bar */}
        <Box>
          <Flex h="40px" borderRadius="lg" overflow="hidden" bg="gray.100">
            {chartData.map((item, index) => (
              <Box
                key={item.id}
                w={`${item.percentage}%`}
                bg={item.color}
                transition="width 0.3s ease"
                position="relative"
                _hover={{
                  filter: 'brightness(1.1)',
                  '& > .tooltip': { display: 'block' },
                }}
              >
                <Box
                  className="tooltip"
                  display="none"
                  position="absolute"
                  top="-60px"
                  left="50%"
                  transform="translateX(-50%)"
                  bg="gray.800"
                  color="white"
                  px={3}
                  py={2}
                  borderRadius="md"
                  fontSize="xs"
                  whiteSpace="nowrap"
                  zIndex={10}
                >
                  <Text fontWeight="bold">{item.name}</Text>
                  <Text>{item.value.toFixed(2)} USDT ({item.percentage.toFixed(1)}%)</Text>
                </Box>
              </Box>
            ))}
          </Flex>
        </Box>

        {/* Legend */}
        <Flex wrap="wrap" gap={4}>
          {chartData.map((item) => (
            <HStack key={item.id} spacing={2}>
              <Box w="12px" h="12px" borderRadius="sm" bg={item.color} />
              <Text fontSize="sm" color="gray.600">
                {item.name}: {item.percentage.toFixed(1)}%
              </Text>
            </HStack>
          ))}
        </Flex>

        {/* Summary Stats */}
        <Flex justify="space-between" pt={4} borderTop="1px" borderColor={borderColor}>
          <VStack align="start" spacing={0}>
            <Text fontSize="xs" color="gray.500">
              {t('capitalManagement.totalBalance')}
            </Text>
            <Text fontWeight="bold">{totalCapital.toFixed(2)} USDT</Text>
          </VStack>
          <VStack align="center" spacing={0}>
            <Text fontSize="xs" color="gray.500">
              {t('capitalManagement.allocated')}
            </Text>
            <Text fontWeight="bold" color="blue.500">
              {totalAllocated.toFixed(2)} USDT
            </Text>
          </VStack>
          <VStack align="end" spacing={0}>
            <Text fontSize="xs" color="gray.500">
              {t('capitalManagement.inUse')}
            </Text>
            <Text fontWeight="bold" color="orange.500">
              {totalUsed.toFixed(2)} USDT
            </Text>
          </VStack>
        </Flex>
      </VStack>
    </Box>
  )
}

export default AllocationChart
