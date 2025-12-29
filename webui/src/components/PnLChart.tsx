import React from 'react'
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from 'recharts'
import { Box, Center, Text } from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'

interface PnLDataPoint {
  time: string
  pnl: number
}

interface PnLChartProps {
  data: PnLDataPoint[]
  height?: number | string
  color?: string
}

const CustomTooltip = ({ active, payload, label }: any) => {
  if (active && payload && payload.length) {
    return (
      <Box
        bg="white"
        p={3}
        border="1px solid"
        borderColor="gray.100"
        borderRadius="xl"
        boxShadow="xl"
        backdropFilter="blur(10px)"
      >
        <Box fontSize="xs" color="gray.500" mb={1}>{label}</Box>
        <Box fontWeight="bold" color={payload[0].value >= 0 ? 'green.500' : 'red.500'}>
          {payload[0].value >= 0 ? '+' : ''}{payload[0].value.toFixed(2)} USDT
        </Box>
      </Box>
    )
  }
  return null
}

const PnLChart: React.FC<PnLChartProps> = ({ data, height = 300, color = '#3182ce' }) => {
  const { t } = useTranslation()
  const gridColor = 'rgba(0,0,0,0.05)'
  const axisColor = 'gray.400'

  // 如果没有数据，显示空状态提示
  if (!data || data.length === 0) {
    return (
      <Box w="100%" h={height}>
        <Center h="100%">
          <Text color="gray.500" fontSize="sm">{t('pnlChart.noData')}</Text>
        </Center>
      </Box>
    )
  }

  return (
    <Box w="100%" h={height}>
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
          <defs>
            <linearGradient id="colorPnL" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor={color} stopOpacity={0.3} />
              <stop offset="95%" stopColor={color} stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" vertical={false} stroke={gridColor} />
          <XAxis
            dataKey="time"
            axisLine={false}
            tickLine={false}
            tick={{ fontSize: 10, fill: axisColor }}
            minTickGap={30}
          />
          <YAxis
            axisLine={false}
            tickLine={false}
            tick={{ fontSize: 10, fill: axisColor }}
          />
          <Tooltip content={<CustomTooltip />} />
          <Area
            type="monotone"
            dataKey="pnl"
            stroke={color}
            strokeWidth={3}
            fillOpacity={1}
            fill="url(#colorPnL)"
            animationDuration={1500}
          />
        </AreaChart>
      </ResponsiveContainer>
    </Box>
  )
}

export default PnLChart

