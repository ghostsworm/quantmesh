import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { getFundingRateCurrent, getFundingRateHistory, FundingRateInfo, FundingRateHistoryItem } from '../services/api'

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

  const getRateColor = (rate: number) => {
    if (rate > 0.0001) return '#ef4444' // 红色：费率较高
    if (rate < -0.0001) return '#10b981' // 绿色：负费率（做多可收到费用）
    return '#6b7280' // 灰色：接近0
  }

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
    </div>
  )
}

export default FundingRate

