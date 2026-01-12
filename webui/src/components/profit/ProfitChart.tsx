import React, { useMemo } from 'react'
import {
  Box,
  VStack,
  HStack,
  Text,
  Flex,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  StatArrow,
  SimpleGrid,
  Select,
  useColorModeValue,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import type { ProfitTrendItem, StrategyProfit } from '../../types/profit'

interface ProfitChartProps {
  trend: ProfitTrendItem[]
  strategyProfits: StrategyProfit[]
  period: '7d' | '30d' | '90d' | '1y'
  onPeriodChange: (period: '7d' | '30d' | '90d' | '1y') => void
}

const COLORS = [
  '#3182CE', // blue.500
  '#805AD5', // purple.500
  '#38A169', // green.500
  '#DD6B20', // orange.500
  '#E53E3E', // red.500
  '#00B5D8', // cyan.500
]

const ProfitChart: React.FC<ProfitChartProps> = ({
  trend,
  strategyProfits,
  period,
  onPeriodChange,
}) => {
  const { t } = useTranslation()
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  const chartStats = useMemo(() => {
    if (trend.length === 0) {
      return { maxProfit: 0, minProfit: 0, totalProfit: 0, avgProfit: 0 }
    }
    const profits = trend.map((t) => t.profit)
    return {
      maxProfit: Math.max(...profits),
      minProfit: Math.min(...profits),
      totalProfit: trend[trend.length - 1]?.cumulativeProfit || 0,
      avgProfit: profits.reduce((a, b) => a + b, 0) / profits.length,
    }
  }, [trend])

  const maxCumulative = useMemo(() => {
    if (trend.length === 0) return 0
    return Math.max(...trend.map((t) => Math.abs(t.cumulativeProfit)))
  }, [trend])

  return (
    <Box
      p={6}
      borderWidth="1px"
      borderRadius="xl"
      borderColor={borderColor}
      bg={bgColor}
    >
      <VStack align="stretch" spacing={6}>
        <Flex justify="space-between" align="center">
          <Text fontWeight="bold" fontSize="lg">
            {t('profitManagement.profitTrend')}
          </Text>
          <Select
            size="sm"
            w="120px"
            value={period}
            onChange={(e) => onPeriodChange(e.target.value as typeof period)}
          >
            <option value="7d">{t('profitManagement.period7d')}</option>
            <option value="30d">{t('profitManagement.period30d')}</option>
            <option value="90d">{t('profitManagement.period90d')}</option>
            <option value="1y">{t('profitManagement.period1y')}</option>
          </Select>
        </Flex>

        {/* Simple Bar Chart */}
        <Box h="200px" position="relative">
          <Flex h="100%" align="flex-end" justify="space-between" gap={1}>
            {(trend || []).slice(-30).map((item, index) => {
              const height = maxCumulative > 0 ? (Math.abs(item.cumulativeProfit) / maxCumulative) * 100 : 0
              const isPositive = item.cumulativeProfit >= 0
              return (
                <Box
                  key={item.date}
                  flex={1}
                  maxW="20px"
                  h={`${Math.max(height, 2)}%`}
                  bg={isPositive ? 'green.400' : 'red.400'}
                  borderRadius="sm"
                  position="relative"
                  _hover={{
                    bg: isPositive ? 'green.500' : 'red.500',
                    '& > .tooltip': { display: 'block' },
                  }}
                  transition="background 0.2s"
                >
                  <Box
                    className="tooltip"
                    display="none"
                    position="absolute"
                    bottom="100%"
                    left="50%"
                    transform="translateX(-50%)"
                    bg="gray.800"
                    color="white"
                    px={2}
                    py={1}
                    borderRadius="md"
                    fontSize="xs"
                    whiteSpace="nowrap"
                    zIndex={10}
                    mb={1}
                  >
                    <Text>{item.date}</Text>
                    <Text fontWeight="bold">
                      {(item.profit || 0) >= 0 ? '+' : ''}{(item.profit || 0).toFixed(2)} USDT
                    </Text>
                  </Box>
                </Box>
              )
            })}
          </Flex>
          {/* Zero line */}
          <Box
            position="absolute"
            bottom="50%"
            left={0}
            right={0}
            h="1px"
            bg="gray.300"
          />
        </Box>

        {/* Stats */}
        <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
          <Stat>
            <StatLabel>{t('profitManagement.totalProfit')}</StatLabel>
            <StatNumber
              fontSize="lg"
              color={(chartStats.totalProfit || 0) >= 0 ? 'green.500' : 'red.500'}
            >
              {(chartStats.totalProfit || 0) >= 0 ? '+' : ''}
              {(chartStats.totalProfit || 0).toFixed(2)}
            </StatNumber>
            <StatHelpText>USDT</StatHelpText>
          </Stat>
          <Stat>
            <StatLabel>{t('profitManagement.avgDaily')}</StatLabel>
            <StatNumber
              fontSize="lg"
              color={(chartStats.avgProfit || 0) >= 0 ? 'green.500' : 'red.500'}
            >
              {(chartStats.avgProfit || 0) >= 0 ? '+' : ''}
              {(chartStats.avgProfit || 0).toFixed(2)}
            </StatNumber>
            <StatHelpText>USDT / {t('profitManagement.day')}</StatHelpText>
          </Stat>
          <Stat>
            <StatLabel>{t('profitManagement.bestDay')}</StatLabel>
            <StatNumber fontSize="lg" color="green.500">
              +{(chartStats.maxProfit || 0).toFixed(2)}
            </StatNumber>
            <StatHelpText>USDT</StatHelpText>
          </Stat>
          <Stat>
            <StatLabel>{t('profitManagement.worstDay')}</StatLabel>
            <StatNumber fontSize="lg" color="red.500">
              {(chartStats.minProfit || 0).toFixed(2)}
            </StatNumber>
            <StatHelpText>USDT</StatHelpText>
          </Stat>
        </SimpleGrid>

        {/* Strategy Breakdown */}
        {strategyProfits.length > 0 && (
          <>
            <Text fontWeight="bold" fontSize="md" mt={2}>
              {t('profitManagement.byStrategy')}
            </Text>
            <VStack align="stretch" spacing={2}>
              {strategyProfits.map((sp, index) => (
                <HStack
                  key={sp.strategyId}
                  justify="space-between"
                  p={3}
                  bg="gray.50"
                  borderRadius="md"
                  borderLeft="4px solid"
                  borderLeftColor={COLORS[index % COLORS.length]}
                >
                  <VStack align="start" spacing={0}>
                    <Text fontWeight="medium">{sp.strategyName}</Text>
                    <Text fontSize="xs" color="gray.500">
                      {sp.tradeCount} {t('profitManagement.trades')} · {((sp.winRate || 0) * 100).toFixed(1)}% {t('profitManagement.winRate')}
                    </Text>
                  </VStack>
                  <VStack align="end" spacing={0}>
                    <Text
                      fontWeight="bold"
                      color={(sp.totalProfit || 0) >= 0 ? 'green.500' : 'red.500'}
                    >
                      {(sp.totalProfit || 0) >= 0 ? '+' : ''}{(sp.totalProfit || 0).toFixed(2)} USDT
                    </Text>
                    <Text fontSize="xs" color="gray.500">
                      {t('profitManagement.today')}: {(sp.todayProfit || 0) >= 0 ? '+' : ''}{(sp.todayProfit || 0).toFixed(2)}
                    </Text>
                  </VStack>
                </HStack>
              ))}
            </VStack>
          </>
        )}
      </VStack>
    </Box>
  )
}

export default ProfitChart
