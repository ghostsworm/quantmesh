import React from 'react'
import {
  Box,
  Text,
  useColorModeValue,
} from '@chakra-ui/react'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  ReferenceLine,
} from 'recharts'

interface PriceChartProps {
  data: Array<{ time: string | number; price: number }>
  height?: number
  showGrid?: boolean
  referenceLines?: Array<{ value: number; label: string; color: string }>
}

const PriceChart: React.FC<PriceChartProps> = ({
  data,
  height = 300,
  showGrid = true,
  referenceLines = [],
}) => {
  const gridColor = useColorModeValue('rgba(0,0,0,0.05)', 'rgba(255,255,255,0.05)')
  const axisColor = useColorModeValue('gray.400', 'gray.500')

  if (!data || data.length === 0) {
    return (
      <Box h={height} display="flex" alignItems="center" justifyContent="center">
        <Text color="gray.500" fontSize="sm">暂无价格数据</Text>
      </Box>
    )
  }

  return (
    <Box w="100%" h={height}>
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
          {showGrid && <CartesianGrid strokeDasharray="3 3" vertical={false} stroke={gridColor} />}
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
            domain={['auto', 'auto']}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: useColorModeValue('white', 'gray.800'),
              border: `1px solid ${useColorModeValue('#e2e8f0', '#4a5568')}`,
              borderRadius: '8px',
            }}
            formatter={(value: number) => [`$${value.toFixed(2)}`, '价格']}
          />
          {referenceLines.map((line, index) => (
            <ReferenceLine
              key={index}
              y={line.value}
              stroke={line.color}
              strokeDasharray="5 5"
              label={{ value: line.label, position: 'right', fill: line.color }}
            />
          ))}
          <Line
            type="monotone"
            dataKey="price"
            stroke="#3182ce"
            strokeWidth={2}
            dot={false}
            animationDuration={800}
          />
        </LineChart>
      </ResponsiveContainer>
    </Box>
  )
}

export default PriceChart
