import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { 
  getRiskStatus, 
  getRiskMonitorData, 
  getRiskCheckHistory,
  RiskStatusResponse, 
  SymbolMonitorData,
  RiskCheckHistoryItem 
} from '../services/api'
import { BarChart, Bar, XAxis, YAxis, Tooltip, Legend, ResponsiveContainer, Cell } from 'recharts'
import './RiskMonitor.css'

const RiskMonitor: React.FC = () => {
  const { t } = useTranslation()
  const [riskStatus, setRiskStatus] = useState<RiskStatusResponse | null>(null)
  const [monitorData, setMonitorData] = useState<SymbolMonitorData[]>([])
  const [historyData, setHistoryData] = useState<RiskCheckHistoryItem[]>([])
  const [loadingStatus, setLoadingStatus] = useState(true)
  const [loadingData, setLoadingData] = useState(true)
  const [loadingHistory, setLoadingHistory] = useState(true)
  const [errorStatus, setErrorStatus] = useState<string | null>(null)
  const [errorData, setErrorData] = useState<string | null>(null)
  const [errorHistory, setErrorHistory] = useState<string | null>(null)

  // Fetch Risk Status
  useEffect(() => {
    const fetchStatus = async () => {
      try {
        setLoadingStatus(true)
        const data = await getRiskStatus()
        setRiskStatus(data)
        setErrorStatus(null)
      } catch (err) {
        setErrorStatus(err instanceof Error ? err.message : 'Failed to fetch risk status')
        console.error('Failed to fetch risk status:', err)
      } finally {
        setLoadingStatus(false)
      }
    }

    fetchStatus()
    const interval = setInterval(fetchStatus, 5000) // Refresh every 5 seconds
    return () => clearInterval(interval)
  }, [])

  // Fetch Monitor Data
  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoadingData(true)
        const data = await getRiskMonitorData()
        setMonitorData(data.symbols)
        setErrorData(null)
      } catch (err) {
        setErrorData(err instanceof Error ? err.message : 'Failed to fetch monitor data')
        console.error('Failed to fetch monitor data:', err)
      } finally {
        setLoadingData(false)
      }
    }

    fetchData()
    const interval = setInterval(fetchData, 5000) // Refresh every 5 seconds
    return () => clearInterval(interval)
  }, [])

  const [timeRange, setTimeRange] = useState<number>(7) // 默认7天

  // Fetch History Data
  useEffect(() => {
    const fetchHistory = async () => {
      try {
        setLoadingHistory(true)
        const endTime = new Date()
        const startTime = new Date()
        startTime.setDate(startTime.getDate() - timeRange)
        
        // 限制返回數量，避免前端渲染過多數據導致卡顿
        const data = await getRiskCheckHistory({
          start_time: startTime.toISOString(),
          end_time: endTime.toISOString(),
          limit: 200,
        })
        setHistoryData(data.history)
        setErrorHistory(null)
      } catch (err) {
        setErrorHistory(err instanceof Error ? err.message : 'Failed to fetch history data')
        console.error('Failed to fetch history data:', err)
      } finally {
        setLoadingHistory(false)
      }
    }

    fetchHistory()
    const interval = setInterval(fetchHistory, 30000) // Refresh every 30 seconds
    return () => clearInterval(interval)
  }, [timeRange])

  const formatTime = (timeStr: string | Date) => {
    if (!timeStr) return 'N/A'
    try {
      return new Date(timeStr).toLocaleString('zh-CN')
    } catch {
      return String(timeStr)
    }
  }

  return (
    <div className="risk-monitor">
      <h2>{t('riskMonitor.title')}</h2>

      {loadingStatus && !riskStatus ? (
        <p>{t('riskMonitor.loadingStatus')}</p>
      ) : errorStatus ? (
        <p style={{ color: 'red' }}>{t('riskMonitor.error')}: {errorStatus}</p>
      ) : riskStatus ? (
        <div className="risk-status-card">
          <div className={`status-indicator ${riskStatus.triggered ? 'triggered' : 'normal'}`}>
            <h3>{riskStatus.triggered ? `🚨 ${t('riskMonitor.riskTriggered')}` : `✅ ${t('riskMonitor.monitorNormal')}`}</h3>
            {riskStatus.triggered && riskStatus.triggered_time && (
              <p>{t('riskMonitor.triggerTime')} {formatTime(riskStatus.triggered_time)}</p>
            )}
            {!riskStatus.triggered && riskStatus.recovered_time && (
              <p>{t('riskMonitor.recoveredTime')} {formatTime(riskStatus.recovered_time)}</p>
            )}
            <p>{t('riskMonitor.monitorSymbols')} {riskStatus.monitor_symbols?.join(', ') || 'N/A'}</p>
          </div>
        </div>
      ) : (
        <p>{t('riskMonitor.noRiskData')}</p>
      )}

      <h3 style={{ marginTop: '32px' }}>{t('riskMonitor.monitorSymbolData')}</h3>
      {loadingData && monitorData.length === 0 ? (
        <p>{t('riskMonitor.loadingData')}</p>
      ) : errorData ? (
        <p style={{ color: 'red' }}>{t('riskMonitor.error')}: {errorData}</p>
      ) : monitorData.length === 0 ? (
        <p>{t('riskMonitor.noMonitorData')}</p>
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <table className="risk-monitor-table">
            <thead>
              <tr>
                <th>{t('riskMonitor.symbol')}</th>
                <th style={{ textAlign: 'right' }}>{t('riskMonitor.currentPrice')}</th>
                <th style={{ textAlign: 'right' }}>{t('riskMonitor.avgPrice')}</th>
                <th style={{ textAlign: 'right' }}>{t('riskMonitor.priceDeviation')}</th>
                <th style={{ textAlign: 'right' }}>{t('riskMonitor.currentVolume')}</th>
                <th style={{ textAlign: 'right' }}>{t('riskMonitor.avgVolume')}</th>
                <th style={{ textAlign: 'right' }}>{t('riskMonitor.volumeMultiplier')}</th>
                <th>{t('riskMonitor.status')}</th>
                <th>{t('riskMonitor.updatedAt')}</th>
              </tr>
            </thead>
            <tbody>
              {monitorData.map((data) => (
                <tr key={data.symbol} className={data.is_abnormal ? 'abnormal-row' : ''}>
                  <td><strong>{data.symbol}</strong></td>
                  <td style={{ textAlign: 'right' }}>{data.current_price.toFixed(2)}</td>
                  <td style={{ textAlign: 'right' }}>{data.average_price.toFixed(2)}</td>
                  <td style={{ 
                    textAlign: 'right', 
                    color: Math.abs(data.price_deviation) > 5 ? '#ff4d4f' : '#52c41a' 
                  }}>
                    {data.price_deviation.toFixed(2)}%
                  </td>
                  <td style={{ textAlign: 'right' }}>{data.current_volume.toFixed(0)}</td>
                  <td style={{ textAlign: 'right' }}>{data.average_volume.toFixed(0)}</td>
                  <td style={{ 
                    textAlign: 'right', 
                    color: data.volume_ratio > 2 ? '#ff4d4f' : '#52c41a' 
                  }}>
                    {data.volume_ratio.toFixed(2)}x
                  </td>
                  <td>
                    {data.is_abnormal ? (
                      <span style={{ color: '#ff4d4f', fontWeight: 'bold' }}>⚠️ {t('riskMonitor.abnormal')}</span>
                    ) : (
                      <span style={{ color: '#52c41a' }}>✓ {t('riskMonitor.normal')}</span>
                    )}
                  </td>
                  <td>{formatTime(data.last_update)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div style={{ marginTop: '32px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h3 style={{ margin: 0 }}>{t('riskMonitor.historyHealth')}</h3>
        <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
          <label style={{ fontSize: '14px' }}>{t('riskMonitor.timeRange')}</label>
          <select 
            value={timeRange} 
            onChange={(e) => setTimeRange(Number(e.target.value))}
            style={{ 
              padding: '4px 8px', 
              fontSize: '14px',
              border: '1px solid #d9d9d9',
              borderRadius: '4px',
              cursor: 'pointer'
            }}
          >
            <option value={1}>{t('riskMonitor.last1d')}</option>
            <option value={7}>{t('riskMonitor.last7d')}</option>
            <option value={30}>{t('riskMonitor.last30d')}</option>
            <option value={90}>{t('riskMonitor.last90d')}</option>
          </select>
        </div>
      </div>
      {loadingHistory && historyData.length === 0 ? (
        <p>{t('riskMonitor.loadingHistory')}</p>
      ) : errorHistory ? (
        <p style={{ color: 'red' }}>{t('riskMonitor.error')}: {errorHistory}</p>
      ) : historyData.length === 0 ? (
        <p>{t('riskMonitor.noHistoryData')}</p>
      ) : (
        <div style={{ marginTop: '16px', width: '100%', height: '400px' }}>
          <ResponsiveContainer width="100%" height="100%">
            <BarChart
              data={historyData.map((item, itemIndex) => {
                // 為每個检查時间段創建數據点
                const dataPoint: any = {
                  time: new Date(item.check_time).toLocaleString('zh-CN', {
                    month: 'short',
                    day: 'numeric',
                    hour: '2-digit',
                    minute: '2-digit'
                  }),
                  timestamp: item.check_time,
                  symbols: item.symbols,
                  total: item.total_count,
                  healthy: item.healthy_count,
                }
                
                // 為每個币种添加堆叠數據（每個币种占1個單位高度）
                item.symbols.forEach((symbol, symbolIndex) => {
                  dataPoint[`symbol_${symbolIndex}`] = 1
                })
                
                return dataPoint
              })}
              margin={{ top: 20, right: 30, left: 20, bottom: 60 }}
            >
              <XAxis 
                dataKey="time" 
                angle={-45}
                textAnchor="end"
                height={80}
                interval="preserveStartEnd"
                tick={{ fontSize: 12 }}
              />
              <YAxis 
                label={{ value: t('riskMonitor.symbolCount'), angle: -90, position: 'insideLeft' }}
                domain={[0, 'dataMax']}
                ticks={historyData.length > 0 ? Array.from({ length: historyData[0].total_count + 1 }, (_, i) => i) : []}
              />
              <Tooltip
                content={({ active, payload }) => {
                  if (!active || !payload || !payload.length) return null
                  
                  const data = payload[0].payload
                  const checkTime = new Date(data.timestamp).toLocaleString('zh-CN')
                  
                  return (
                    <div style={{
                      backgroundColor: 'rgba(255, 255, 255, 0.95)',
                      padding: '12px',
                      border: '1px solid #ccc',
                      borderRadius: '4px',
                      boxShadow: '0 2px 8px rgba(0,0,0,0.15)',
                      maxWidth: '300px'
                    }}>
                      <p style={{ fontWeight: 'bold', marginBottom: '8px', fontSize: '14px' }}>
                        {t('riskMonitor.checkTime')} {checkTime}
                      </p>
                      <p style={{ marginBottom: '8px', fontSize: '13px' }}>
                        {t('riskMonitor.healthyCount', { healthy: data.healthy, total: data.total })}
                      </p>
                      <div style={{ maxHeight: '200px', overflowY: 'auto' }}>
                        {data.symbols && data.symbols.map((symbol: any, index: number) => (
                          <div 
                            key={index}
                            style={{ 
                              margin: '4px 0',
                              padding: '4px',
                              backgroundColor: symbol.is_healthy ? '#f6ffed' : '#fff1f0',
                              borderRadius: '4px',
                              fontSize: '12px'
                            }}
                          >
                            <span style={{ 
                              color: symbol.is_healthy ? '#52c41a' : '#ff4d4f',
                              fontWeight: 'bold'
                            }}>
                              {symbol.symbol}: {symbol.is_healthy ? `✓ ${t('riskMonitor.healthy')}` : `⚠ ${t('riskMonitor.abnormal')}`}
                            </span>
                            {symbol.reason && !symbol.is_healthy && (
                              <div style={{ fontSize: '11px', color: '#666', marginTop: '2px', marginLeft: '8px' }}>
                                {symbol.reason}
                              </div>
                            )}
                          </div>
                        ))}
                      </div>
                    </div>
                  )
                }}
              />
              {historyData.length > 0 && historyData[0].symbols.map((symbol, index) => (
                <Bar
                  key={index}
                  dataKey={`symbol_${index}`}
                  stackId="health"
                  name={symbol.symbol}
                  isAnimationActive={false}
                >
                  {historyData.map((entry, entryIndex) => {
                    const symbolData = entry.symbols[index]
                    return (
                      <Cell
                        key={`cell-${entryIndex}-${index}`}
                        fill={symbolData?.is_healthy ? '#52c41a' : '#ff4d4f'}
                      />
                    )
                  })}
                </Bar>
              ))}
            </BarChart>
          </ResponsiveContainer>
        </div>
      )}
    </div>
  )
}

export default RiskMonitor

