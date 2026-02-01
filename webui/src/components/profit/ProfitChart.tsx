import React, { useMemo } from 'react'
import {
  ComposedChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from 'recharts'
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
  SimpleGrid,
  Select,
  useColorModeValue,
  Center,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import type { ProfitTrendItem, StrategyProfit, PriceChangeItem } from '../../types/profit'

interface ProfitChartProps {
  trend: ProfitTrendItem[]
  strategyProfits: StrategyProfit[]
  period: '7d' | '30d' | '90d' | '1y'
  onPeriodChange: (period: '7d' | '30d' | '90d' | '1y') => void
  selectedSymbol?: string | null
  onSymbolChange?: (symbol: string | null) => void
  priceData?: PriceChangeItem[]
  symbols?: Array<{ symbol: string; exchange: string }>
  isLoadingPriceData?: boolean
}

const COLORS = [
  '#3182CE', // blue.500
  '#805AD5', // purple.500
  '#38A169', // green.500
  '#DD6B20', // orange.500
  '#E53E3E', // red.500
  '#00B5D8', // cyan.500
]

const CustomTooltip = ({ active, payload, label }: any) => {
  if (active && payload && payload.length) {
    return (
      <Box
        bg="gray.800"
        color="white"
        p={3}
        borderRadius="md"
        fontSize="sm"
        boxShadow="lg"
      >
        <Text fontWeight="bold" mb={2}>{label}</Text>
        {payload.map((entry: any) => (
          <Text key={entry.dataKey} color={entry.color}>
            {entry.name}: {entry.value != null && entry.value !== null ? (Number(entry.value) >= 0 ? '+' : '') + Number(entry.value).toFixed(2) : '-'} USDT
          </Text>
        ))}
      </Box>
    )
  }
  return null
}

const ProfitChart: React.FC<ProfitChartProps> = ({
  trend,
  strategyProfits,
  period,
  onPeriodChange,
  selectedSymbol,
  onSymbolChange,
  priceData = [],
  symbols = [],
  isLoadingPriceData = false,
}) => {
  const { t } = useTranslation()
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')
  const gridColor = useColorModeValue('rgba(0,0,0,0.06)', 'rgba(255,255,255,0.06)')
  const axisColor = useColorModeValue('gray.600', 'gray.400')

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

  const chartData = useMemo(() => {
    const priceMap = new Map<string, number>()
    priceData.forEach((p) => priceMap.set(p.date, p.priceChange))

    return trend.map((item) => ({
      date: item.date,
      profit: item.profit,
      cumulativeProfit: item.cumulativeProfit,
      priceChange: priceMap.get(item.date) ?? null,
    }))
  }, [trend, priceData])

  const hasPriceData = priceData.length > 0 && chartData.some((d) => d.priceChange != null)

  return (
    <Box
      p={6}
      borderWidth="1px"
      borderRadius="xl"
      borderColor={borderColor}
      bg={bgColor}
    >
      <VStack align="stretch" spacing={6}>
        <Flex justify="space-between" align="center" wrap="wrap" gap={4}>
          <Text fontWeight="bold" fontSize="lg">
            {t('profitManagement.profitTrend')}
          </Text>
          <HStack spacing={3} flexWrap="wrap">
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
            <Select
              size="sm"
              w="140px"
              value={selectedSymbol ?? ''}
              onChange={(e) => onSymbolChange?.(e.target.value || null)}
              placeholder={t('profitManagement.selectSymbol')}
              isDisabled={symbols.length === 0}
            >
              {symbols.map((s) => (
                <option key={`${s.exchange}-${s.symbol}`} value={s.symbol}>
                  {s.symbol}
                </option>
              ))}
            </Select>
          </HStack>
        </Flex>

        {/* Curve Chart */}
        <Box h="300px" w="100%">
          {chartData.length === 0 ? (
            <Center h="100%">
              <Text color="gray.500" fontSize="sm">
                {t('pnlChart.noData')}
              </Text>
            </Center>
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <ComposedChart
                data={chartData}
                margin={{ top: 10, right: 30, left: -20, bottom: 0 }}
              >
                <CartesianGrid strokeDasharray="3 3" vertical={false} stroke={gridColor} />
                <XAxis
                  dataKey="date"
                  axisLine={false}
                  tickLine={false}
                  tick={{ fontSize: 10, fill: axisColor }}
                  minTickGap={40}
                />
                <YAxis
                  yAxisId="left"
                  orientation="left"
                  axisLine={false}
                  tickLine={false}
                  tick={{ fontSize: 10, fill: axisColor }}
                  tickFormatter={(v) => `${v >= 0 ? '+' : ''}${v.toFixed(0)}`}
                />
                {hasPriceData && (
                  <YAxis
                    yAxisId="right"
                    orientation="right"
                    axisLine={false}
                    tickLine={false}
                    tick={{ fontSize: 10, fill: axisColor }}
                    tickFormatter={(v) => `${v >= 0 ? '+' : ''}${v.toFixed(0)}`}
                  />
                )}
                <Tooltip content={<CustomTooltip />} />
                <Legend wrapperStyle={{ fontSize: 12 }} />
                <Line
                  yAxisId="left"
                  type="monotone"
                  dataKey="profit"
                  name={t('profitManagement.dailyProfit')}
                  stroke="#38A169"
                  strokeWidth={2}
                  dot={false}
                  connectNulls
                  animationDuration={800}
                />
                {hasPriceData && (
                  <Line
                    yAxisId="right"
                    type="monotone"
                    dataKey="priceChange"
                    name={t('profitManagement.priceChange')}
                    stroke="#3182CE"
                    strokeWidth={2}
                    strokeDasharray="5 5"
                    dot={false}
                    connectNulls
                    animationDuration={800}
                  />
                )}
              </ComposedChart>
            </ResponsiveContainer>
          )}
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
                  _dark={{ bg: 'gray.700' }}
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
