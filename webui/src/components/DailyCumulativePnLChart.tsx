import React, { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Bar,
  CartesianGrid,
  ComposedChart,
  Legend,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { DailyEquityChartPoint } from '../utils/dailyEquityChartData'

interface DailyCumulativePnLChartProps {
  data: DailyEquityChartPoint[]
  height?: number
}

const DailyCumulativePnLChart: React.FC<DailyCumulativePnLChartProps> = ({ data, height = 280 }) => {
  const { t } = useTranslation()

  const hasData = data.length > 0

  const hasExchangeAccountEquity = useMemo(
    () => data.some((p) => p.accountEquity !== undefined && !Number.isNaN(p.accountEquity)),
    [data]
  )

  const chartData = useMemo(() => data, [data])

  if (!hasData) {
    return (
      <div
        style={{
          height,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: '#8c8c8c',
          border: '1px dashed #d9d9d9',
          borderRadius: 8,
        }}
      >
        {t('statistics.dailyEquityCurveEmpty')}
      </div>
    )
  }

  return (
    <div style={{ width: '100%', height, minHeight: height }}>
      <ResponsiveContainer width="100%" height="100%">
        <ComposedChart data={chartData} margin={{ top: 8, right: 12, left: 0, bottom: 8 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
          <XAxis dataKey="label" tick={{ fontSize: 11 }} interval="preserveStartEnd" />
          <YAxis
            yAxisId="left"
            tick={{ fontSize: 11 }}
            tickFormatter={(v: number) => v.toFixed(0)}
            width={56}
          />
          <YAxis
            yAxisId="right"
            orientation="right"
            tick={{ fontSize: 11 }}
            tickFormatter={(v: number) => v.toFixed(1)}
            width={48}
          />
          <Tooltip
            formatter={(value: number, name: string) => {
              const label =
                name === 'accountEquity'
                  ? t('statistics.dailyEquityCurveSeriesAccountEquity')
                  : name === 'cumulativePnl'
                    ? t('statistics.dailyEquityCurveSeriesCumulative')
                    : name === 'dailyNetWithFunding'
                      ? t('statistics.dailyEquityCurveSeriesDailyNet')
                      : name
              return [`${value >= 0 ? '+' : ''}${value.toFixed(2)}`, label]
            }}
            labelFormatter={(_, payload) => {
              const p = payload?.[0]?.payload as DailyEquityChartPoint | undefined
              return p?.dateKey ?? ''
            }}
          />
          <Legend
            formatter={(value) =>
              value === 'accountEquity'
                ? t('statistics.dailyEquityCurveSeriesAccountEquity')
                : value === 'cumulativePnl'
                  ? t('statistics.dailyEquityCurveSeriesCumulative')
                  : value === 'dailyNetWithFunding'
                    ? t('statistics.dailyEquityCurveSeriesDailyNet')
                    : value
            }
          />
          <Bar
            yAxisId="right"
            dataKey="dailyNetWithFunding"
            name="dailyNetWithFunding"
            fill="#fa8c16"
            opacity={0.85}
            maxBarSize={28}
          />
          {hasExchangeAccountEquity ? (
            <Line
              yAxisId="left"
              type="monotone"
              dataKey="accountEquity"
              name="accountEquity"
              stroke="#1890ff"
              strokeWidth={2}
              dot={chartData.length <= 45}
              activeDot={{ r: 4 }}
              connectNulls={false}
            />
          ) : (
            <Line
              yAxisId="left"
              type="monotone"
              dataKey="cumulativePnl"
              name="cumulativePnl"
              stroke="#1890ff"
              strokeWidth={2}
              dot={chartData.length <= 45}
              activeDot={{ r: 4 }}
            />
          )}
          {hasExchangeAccountEquity ? (
            <Line
              yAxisId="left"
              type="monotone"
              dataKey="cumulativePnl"
              name="cumulativePnl"
              stroke="#bfbfbf"
              strokeWidth={1.5}
              strokeDasharray="6 4"
              dot={false}
            />
          ) : null}
        </ComposedChart>
      </ResponsiveContainer>
    </div>
  )
}

export default DailyCumulativePnLChart
