import React, { useEffect, useState, useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
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
import { Box, Tabs, TabList, Tab } from '@chakra-ui/react'
import { getFundingRateCurrent, getFundingRateHistory, FundingRateInfo, FundingRateHistoryItem } from '../services/api'
import AIMarketInterpret from './AIMarketInterpret'

// 多线图颜色池（区分度高的颜色）
const LINE_COLORS = [
  '#6366f1', '#ef4444', '#10b981', '#f59e0b', '#8b5cf6',
  '#ec4899', '#06b6d4', '#84cc16', '#f97316', '#14b8a6',
]

const FundingRate: React.FC = () => {
  const { t, i18n } = useTranslation()
  const [currentRates, setCurrentRates] = useState<Record<string, FundingRateInfo>>({})
  const [history, setHistory] = useState<FundingRateHistoryItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedSymbol, setSelectedSymbol] = useState<string>('')
  const [limit, setLimit] = useState(100)
  const [selectedExchangeTab, setSelectedExchangeTab] = useState<string>('all')

  // 獲取當前资金费率
  const fetchCurrentRates = async () => {
    try {
      const data = await getFundingRateCurrent()
      setCurrentRates(data.rates || {})
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('fundingRate.fetchCurrentFailed'))
      console.error('Failed to fetch current funding rates:', err)
    }
  }

  // 獲取歷史资金费率（始終用 all 獲取所有交易所，前端按 tab 篩選）
  const fetchHistory = async () => {
    try {
      const data = await getFundingRateHistory(selectedSymbol || undefined, limit, 'all')
      setHistory(data.history || [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('fundingRate.fetchHistoryFailed'))
      console.error('Failed to fetch funding rate history:', err)
    }
  }

  useEffect(() => {
    const loadData = async () => {
      setLoading(true)
      await Promise.all([fetchCurrentRates(), fetchHistory()])
      setLoading(false)
    }
    loadData()
    const interval = setInterval(fetchCurrentRates, 30000)
    return () => clearInterval(interval)
  }, [])

  useEffect(() => {
    fetchHistory()
  }, [selectedSymbol, limit])

  const formatRate = (rate: number) => {
    return (rate * 100).toFixed(6) + '%'
  }

  // 從歷史數據提取唯一交易所列表
  const exchanges = useMemo(() => {
    const set = new Set<string>()
    for (const item of history) {
      if (item.exchange) set.add(item.exchange)
    }
    return Array.from(set).sort()
  }, [history])

  // 按交易所篩選後的歷史
  const filteredHistory = useMemo(() => {
    if (selectedExchangeTab === 'all') return history
    return history.filter((h) => h.exchange === selectedExchangeTab)
  }, [history, selectedExchangeTab])

  // 構建多線圖數據：每條線 = 一個 exchange+symbol 或 symbol（單交易所時）
  const chartData = useMemo(() => {
    const seriesKeys = new Set<string>()
    const byTime: Record<string, Record<string, number>> = {}

    for (const item of filteredHistory) {
      const key = exchanges.length > 1 ? `${item.exchange}:${item.symbol}` : item.symbol
      seriesKeys.add(key)
      const t = item.timestamp
      if (!byTime[t]) byTime[t] = {}
      byTime[t][key] = item.rate * 100 // 百分比顯示
    }

    const keys = Array.from(seriesKeys).sort()
    return {
      keys,
      data: Object.entries(byTime)
        .map(([time, values]) => ({
          time,
          timeLabel: new Date(time).toLocaleString(i18n.language, {
            month: 'numeric',
            day: 'numeric',
            hour: '2-digit',
            minute: '2-digit',
          }),
          ...Object.fromEntries(keys.map((k) => [k, values[k] ?? undefined])),
        }))
        .sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime()),
    }
  }, [filteredHistory, exchanges.length, i18n.language])

  const getRateColor = (rate: number) => {
    if (rate > 0.0001) return '#ef4444'
    if (rate < -0.0001) return '#10b981'
    return '#6b7280'
  }

  const getPageData = useCallback(() => {
    const data: Record<string, unknown> = {}
    if (Object.keys(currentRates).length > 0) {
      data.current_rates = Object.entries(currentRates).map(([sym, info]) => ({
        symbol: sym,
        rate: info.rate,
        timestamp: info.timestamp,
      }))
    }
    return data
  }, [currentRates])

  const aiSymbol = selectedSymbol || 'BTCUSDT'
  const symbols = Object.keys(currentRates).sort()

  const showExchangeTabs = exchanges.length > 1

  if (loading && Object.keys(currentRates).length === 0) {
    return (
      <div style={{ padding: '40px', textAlign: 'center' }}>
        <h2>{t('fundingRate.title')}</h2>
        <p>{t('common.loading')}</p>
      </div>
    )
  }

  return (
    <div style={{ padding: '20px' }}>
      <h2>{t('fundingRate.title')}</h2>

      {error && (
        <div style={{ padding: '10px', marginBottom: '20px', backgroundColor: '#fee', color: '#c33', borderRadius: '4px' }}>
          {t('fundingRate.error')}: {error}
        </div>
      )}

      {/* 多線圖：每條線 = 一個交易所+交易對 */}
      <Box marginBottom="32px">
        <h3 style={{ marginBottom: '12px', fontSize: '1rem', fontWeight: '600' }}>
          {t('fundingRate.chartTitle')}
        </h3>

        {showExchangeTabs && (
          <Tabs
            index={selectedExchangeTab === 'all' ? 0 : Math.max(0, exchanges.indexOf(selectedExchangeTab)) + 1}
            onChange={(i) => setSelectedExchangeTab(i === 0 ? 'all' : exchanges[i - 1] ?? 'all')}
            variant="soft-rounded"
            colorScheme="blue"
            mb={4}
          >
            <TabList overflowX="auto" pb={2}>
              <Tab px={4}>{t('common.allExchanges')}</Tab>
              {exchanges.map((ex) => (
                <Tab key={ex} px={4}>
                  {ex}
                </Tab>
              ))}
            </TabList>
          </Tabs>
        )}

        <Box
          height="320px"
          backgroundColor="#fff"
          borderRadius="8px"
          padding="12px"
          border="1px solid #e5e7eb"
        >
          {chartData.data.length === 0 || chartData.keys.length === 0 ? (
            <div
              style={{
                height: '100%',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                color: '#6b7280',
                fontSize: '0.875rem',
              }}
            >
              {t('fundingRate.noChartData')}
            </div>
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={chartData.data} margin={{ top: 8, right: 8, left: 8, bottom: 8 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" vertical={false} />
                <XAxis
                  dataKey="timeLabel"
                  fontSize={10}
                  tick={{ fill: '#6b7280' }}
                  axisLine={false}
                  tickLine={false}
                />
                <YAxis
                  fontSize={10}
                  tick={{ fill: '#6b7280' }}
                  axisLine={false}
                  tickLine={false}
                  tickFormatter={(v) => String(Number(v.toFixed(4)))}
                />
                <Tooltip
                  labelFormatter={(_, payload) =>
                    payload?.[0]?.payload?.time
                      ? new Date(payload[0].payload.time).toLocaleString(i18n.language)
                      : ''
                  }
                  formatter={(value: number, name: string) => [
                    value != null ? Number(value.toFixed(6)) : '—',
                    name,
                  ]}
                  contentStyle={{ fontSize: '12px', borderRadius: '6px' }}
                />
                <Legend wrapperStyle={{ fontSize: '11px' }} />
                {chartData.keys.map((key, i) => (
                  <Line
                    key={key}
                    type="monotone"
                    dataKey={key}
                    stroke={LINE_COLORS[i % LINE_COLORS.length]}
                    strokeWidth={2}
                    dot={false}
                    name={key}
                    connectNulls
                  />
                ))}
              </LineChart>
            </ResponsiveContainer>
          )}
        </Box>
      </Box>

      {/* 當前资金费率表格 */}
      <div style={{ marginBottom: '40px' }}>
        <h3>{t('fundingRate.currentRates')}</h3>
        <div style={{ overflowX: 'auto' }}>
          <table
            style={{
              width: '100%',
              borderCollapse: 'collapse',
              backgroundColor: '#fff',
              borderRadius: '8px',
              overflow: 'hidden',
            }}
          >
            <thead>
              <tr style={{ backgroundColor: '#f3f4f6' }}>
                <th style={{ padding: '12px', textAlign: 'left', borderBottom: '2px solid #e5e7eb' }}>
                  {t('fundingRate.tradingPair')}
                </th>
                <th style={{ padding: '12px', textAlign: 'right', borderBottom: '2px solid #e5e7eb' }}>
                  {t('fundingRate.rate')}
                </th>
                <th style={{ padding: '12px', textAlign: 'right', borderBottom: '2px solid #e5e7eb' }}>
                  {t('fundingRate.updateTime')}
                </th>
              </tr>
            </thead>
            <tbody>
              {symbols.length === 0 ? (
                <tr>
                  <td colSpan={3} style={{ padding: '20px', textAlign: 'center', color: '#6b7280' }}>
                    {t('fundingRate.noData')}
                  </td>
                </tr>
              ) : (
                symbols.map((symbol) => {
                  const rateInfo = currentRates[symbol]
                  return (
                    <tr key={symbol} style={{ borderBottom: '1px solid #e5e7eb' }}>
                      <td style={{ padding: '12px', fontWeight: '500' }}>{symbol}</td>
                      <td style={{ padding: '12px', textAlign: 'right', color: getRateColor(rateInfo.rate) }}>
                        {formatRate(rateInfo.rate)}
                      </td>
                      <td style={{ padding: '12px', textAlign: 'right', color: '#6b7280', fontSize: '0.875rem' }}>
                        {new Date(rateInfo.timestamp).toLocaleString(i18n.language)}
                      </td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* 历史资金费率 */}
      <div>
        <h3>{t('fundingRate.historyRates')}</h3>
        <div style={{ marginBottom: '20px', display: 'flex', gap: '10px', alignItems: 'center' }}>
          <label>
            {t('fundingRate.tradingPair')}:
            <select
              value={selectedSymbol}
              onChange={(e) => setSelectedSymbol(e.target.value)}
              style={{ marginLeft: '8px', padding: '6px 12px', borderRadius: '4px', border: '1px solid #d1d5db' }}
            >
              <option value="">{t('common.all')}</option>
              {symbols.map((sym) => (
                <option key={sym} value={sym}>
                  {sym}
                </option>
              ))}
            </select>
          </label>
          <label>
            {t('fundingRate.count')}:
            <input
              type="number"
              value={limit}
              onChange={(e) => setLimit(parseInt(e.target.value) || 100)}
              min={1}
              max={1000}
              style={{
                marginLeft: '8px',
                padding: '6px 12px',
                borderRadius: '4px',
                border: '1px solid #d1d5db',
                width: '100px',
              }}
            />
          </label>
        </div>

        <div style={{ overflowX: 'auto' }}>
          <table
            style={{
              width: '100%',
              borderCollapse: 'collapse',
              backgroundColor: '#fff',
              borderRadius: '8px',
              overflow: 'hidden',
            }}
          >
            <thead>
              <tr style={{ backgroundColor: '#f3f4f6' }}>
                <th style={{ padding: '12px', textAlign: 'left', borderBottom: '2px solid #e5e7eb' }}>
                  {t('fundingRate.time')}
                </th>
                <th style={{ padding: '12px', textAlign: 'left', borderBottom: '2px solid #e5e7eb' }}>
                  {t('fundingRate.tradingPair')}
                </th>
                <th style={{ padding: '12px', textAlign: 'left', borderBottom: '2px solid #e5e7eb' }}>
                  {t('fundingRate.exchange')}
                </th>
                <th style={{ padding: '12px', textAlign: 'right', borderBottom: '2px solid #e5e7eb' }}>
                  {t('fundingRate.rate')}
                </th>
              </tr>
            </thead>
            <tbody>
              {history.length === 0 ? (
                <tr>
                  <td colSpan={4} style={{ padding: '20px', textAlign: 'center', color: '#6b7280' }}>
                    {t('fundingRate.noHistoryData')}
                  </td>
                </tr>
              ) : (
                history.map((item) => (
                  <tr key={item.id} style={{ borderBottom: '1px solid #e5e7eb' }}>
                    <td style={{ padding: '12px', color: '#6b7280', fontSize: '0.875rem' }}>
                      {new Date(item.timestamp).toLocaleString(i18n.language)}
                    </td>
                    <td style={{ padding: '12px', fontWeight: '500' }}>{item.symbol}</td>
                    <td style={{ padding: '12px', color: '#6b7280' }}>{item.exchange}</td>
                    <td style={{ padding: '12px', textAlign: 'right', color: getRateColor(item.rate) }}>
                      {formatRate(item.rate)}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      <div style={{ marginTop: '40px' }}>
        <AIMarketInterpret pageType="funding" symbol={aiSymbol} getPageData={getPageData} />
      </div>
    </div>
  )
}

export default FundingRate
