import React from 'react'
import { useTranslation } from 'react-i18next'
import {
  Box,
  Card,
  CardHeader,
  CardBody,
  Text,
  HStack,
  Select,
  useColorModeValue,
  Center,
  Spinner,
} from '@chakra-ui/react'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from 'recharts'
import { NewsHistoryItem } from '../services/api'

interface NewsTrendChartProps {
  data: NewsHistoryItem[]
  loading?: boolean
  period: string
  onPeriodChange: (period: string) => void
}

const NewsTrendChart: React.FC<NewsTrendChartProps> = ({
  data,
  loading,
  period,
  onPeriodChange,
}) => {
  const { t } = useTranslation()
  const textColor = useColorModeValue('gray.600', 'gray.400')
  const gridColor = useColorModeValue('#edf2f7', '#2d3748')

  // 处理数据：按时间升序，并格式化
  const chartData = [...data]
    .sort((a, b) => new Date(a.analysis_time).getTime() - new Date(b.analysis_time).getTime())
    .map((item) => ({
      time: new Date(item.analysis_time).toLocaleString(undefined, {
        month: 'numeric',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      }),
      fullTime: new Date(item.analysis_time).toLocaleString(),
      riskScore: item.overall_risk_score || 0,
      crashProb: (item.crash_probability || 0) * 100,
    }))

  return (
    <Card variant="outline" w="full">
      <CardHeader py={3}>
        <HStack justify="space-between">
          <Text fontSize="sm" fontWeight="600">
            {t('newsAnalysis.riskTrend')}
          </Text>
          <Select
            size="xs"
            w="120px"
            value={period}
            onChange={(e) => onPeriodChange(e.target.value)}
          >
            <option value="7">{t('newsAnalysis.last7d')}</option>
            <option value="30">{t('newsAnalysis.last30d')}</option>
            <option value="90">{t('newsAnalysis.last90d')}</option>
          </Select>
        </HStack>
      </CardHeader>
      <CardBody pt={0} h="300px">
        {loading ? (
          <Center h="full">
            <Spinner size="md" />
          </Center>
        ) : chartData.length === 0 ? (
          <Center h="full">
            <Text color="gray.500" fontSize="sm">
              {t('newsAnalysis.noTrendData')}
            </Text>
          </Center>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={chartData} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke={gridColor} vertical={false} />
              <XAxis
                dataKey="time"
                fontSize={10}
                tick={{ fill: textColor }}
                axisLine={false}
                tickLine={false}
              />
              <YAxis
                fontSize={10}
                tick={{ fill: textColor }}
                axisLine={false}
                tickLine={false}
                domain={[0, 100]}
              />
              <Tooltip
                contentStyle={{
                  backgroundColor: useColorModeValue('white', '#1a202c'),
                  borderColor: gridColor,
                  fontSize: '12px',
                  borderRadius: '8px',
                }}
                labelStyle={{ fontWeight: 'bold', marginBottom: '4px' }}
                labelFormatter={(label, payload) => payload[0]?.payload?.fullTime || label}
              />
              <Legend iconType="circle" wrapperStyle={{ fontSize: '12px', paddingTop: '10px' }} />
              <Line
                type="monotone"
                dataKey="riskScore"
                name={t('newsAnalysis.riskScore')}
                stroke="#E53E3E"
                strokeWidth={2}
                dot={{ r: 2 }}
                activeDot={{ r: 4 }}
              />
              <Line
                type="monotone"
                dataKey="crashProb"
                name={t('newsAnalysis.crashProbability') + ' (%)'}
                stroke="#3182CE"
                strokeWidth={2}
                dot={{ r: 2 }}
                activeDot={{ r: 4 }}
              />
            </LineChart>
          </ResponsiveContainer>
        )}
      </CardBody>
    </Card>
  )
}

export default NewsTrendChart
