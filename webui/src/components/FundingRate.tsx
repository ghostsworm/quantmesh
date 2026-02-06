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
} from 'recharts'
import { getFundingRateCurrent, getFundingRateHistory, FundingRateInfo, FundingRateHistoryItem } from '../services/api'
import AIMarketInterpret from './AIMarketInterpret'

const FundingRate: React.FC = () => {
  const { t, i18n } = useTranslation()
  const [currentRates, setCurrentRates] = useState<Record<string, FundingRateInfo>>({})
  const [history, setHistory] = useState<FundingRateHistoryItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedSymbol, setSelectedSymbol] = useState<string>('')
  const [limit, setLimit] = useState(100)

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

  // 獲取歷史资金费率
  const fetchHistory = async () => {
    try {
      const data = await getFundingRateHistory(selectedSymbol || undefined, limit)
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
    // 每30秒刷新當前资金费率
    const interval = setInterval(fetchCurrentRates, 30000)
    return () => clearInterval(interval)
  }, [])

  useEffect(() => {
    fetchHistory()
  }, [selectedSymbol, limit])

  const formatRate = (rate: number) => {
    return (rate * 100).toFixed(6) + '%'
  }

  // 按时间聚合：同一时刻所有交易对的 rate 相加（去掉百分号后的数值），用于曲线图
  const sumCurveData = useMemo(() => {
    const byTime: Record<string, number> = {}
    for (const item of history) {
      const t = item.timestamp
      byTime[t] = (byTime[t] ?? 0) + item.rate
    }
    return Object.entries(byTime)
      .map(([time, sum]) => ({
        time,
        timeLabel: new Date(time).toLocaleString(i18n.language, {
          month: 'numeric',
          day: 'numeric',
          hour: '2-digit',
          minute: '2-digit',
        }),
        sum: sum * 100, // 显示为小数形式，不加百分号
      }))
      .sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime())
  }, [history, i18n.language])

  const getRateColor = (rate: number) => {
    if (rate > 0.0001) return '#ef4444' // 红色：费率较高
    if (rate < -0.0001) return '#10b981' // 绿色：负费率（做多可收到费用）
    return '#6b7280' // 灰色：接近0
  }

  // 收集当前页面数据快照供 AI 解读使用
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

  // 当前用于 AI 解读的 symbol：如果用户选择了特定交易对则用它，否则用 BTCUSDT 或第一个
  const aiSymbol = selectedSymbol || 'BTCUSDT'

  const symbols = Object.keys(currentRates).sort()

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

      {/* 资金费率按时间合计曲线（去掉百分号，同一时刻所有数字相加） */}
      <div style={{ marginBottom: '32px' }}>
        <h3 style={{ marginBottom: '12px', fontSize: '1rem', fontWeight: '600' }}>{t('fundingRate.sumCurveTitle')}</h3>
        <div style={{ height: '280px', backgroundColor: '#fff', borderRadius: '8px', padding: '12px', border: '1px solid #e5e7eb' }}>
          {sumCurveData.length === 0 ? (
            <div style={{ height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#6b7280', fontSize: '0.875rem' }}>
              {t('fundingRate.noChartData')}
            </div>
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={sumCurveData} margin={{ top: 8, right: 8, left: 8, bottom: 8 }}>
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
                  labelFormatter={(_, payload) => payload?.[0]?.payload?.time ? new Date(payload[0].payload.time).toLocaleString(i18n.language) : ''}
                  formatter={(value: number) => [Number(value.toFixed(6)), t('fundingRate.sumCurveYLabel')]}
                  contentStyle={{ fontSize: '12px', borderRadius: '6px' }}
                />
                <Line type="monotone" dataKey="sum" stroke="#6366f1" strokeWidth={2} dot={false} name={t('fundingRate.sumCurveYLabel')} />
              </LineChart>
            </ResponsiveContainer>
          )}
        </div>
      </div>

      {/* 當前资金费率表格 */}
      <div style={{ marginBottom: '40px' }}>
        <h3>{t('fundingRate.currentRates')}</h3>
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', backgroundColor: '#fff', borderRadius: '8px', overflow: 'hidden' }}>
            <thead>
              <tr style={{ backgroundColor: '#f3f4f6' }}>
                <th style={{ padding: '12px', textAlign: 'left', borderBottom: '2px solid #e5e7eb' }}>{t('fundingRate.tradingPair')}</th>
                <th style={{ padding: '12px', textAlign: 'right', borderBottom: '2px solid #e5e7eb' }}>{t('fundingRate.rate')}</th>
                <th style={{ padding: '12px', textAlign: 'right', borderBottom: '2px solid #e5e7eb' }}>{t('fundingRate.updateTime')}</th>
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
              style={{ marginLeft: '8px', padding: '6px 12px', borderRadius: '4px', border: '1px solid #d1d5db', width: '100px' }}
            />
          </label>
        </div>

        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', backgroundColor: '#fff', borderRadius: '8px', overflow: 'hidden' }}>
            <thead>
              <tr style={{ backgroundColor: '#f3f4f6' }}>
                <th style={{ padding: '12px', textAlign: 'left', borderBottom: '2px solid #e5e7eb' }}>{t('fundingRate.time')}</th>
                <th style={{ padding: '12px', textAlign: 'left', borderBottom: '2px solid #e5e7eb' }}>{t('fundingRate.tradingPair')}</th>
                <th style={{ padding: '12px', textAlign: 'left', borderBottom: '2px solid #e5e7eb' }}>{t('fundingRate.exchange')}</th>
                <th style={{ padding: '12px', textAlign: 'right', borderBottom: '2px solid #e5e7eb' }}>{t('fundingRate.rate')}</th>
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

      {/* AI 解读 */}
      <div style={{ marginTop: '40px' }}>
        <AIMarketInterpret
          pageType="funding"
          symbol={aiSymbol}
          getPageData={getPageData}
        />
      </div>
    </div>
  )
}

export default FundingRate

