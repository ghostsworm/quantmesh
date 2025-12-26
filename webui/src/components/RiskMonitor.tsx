import React, { useEffect, useState } from 'react'
import { getRiskStatus, getRiskMonitorData, RiskStatusResponse, SymbolMonitorData } from '../services/api'
import './RiskMonitor.css'

const RiskMonitor: React.FC = () => {
  const [riskStatus, setRiskStatus] = useState<RiskStatusResponse | null>(null)
  const [monitorData, setMonitorData] = useState<SymbolMonitorData[]>([])
  const [loadingStatus, setLoadingStatus] = useState(true)
  const [loadingData, setLoadingData] = useState(true)
  const [errorStatus, setErrorStatus] = useState<string | null>(null)
  const [errorData, setErrorData] = useState<string | null>(null)

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
      <h2>风控监控</h2>

      {/* Risk Status */}
      {loadingStatus && !riskStatus ? (
        <p>加载风控状态...</p>
      ) : errorStatus ? (
        <p style={{ color: 'red' }}>错误: {errorStatus}</p>
      ) : riskStatus ? (
        <div className="risk-status-card">
          <div className={`status-indicator ${riskStatus.triggered ? 'triggered' : 'normal'}`}>
            <h3>{riskStatus.triggered ? '🚨 风控已触发' : '✅ 监控正常'}</h3>
            {riskStatus.triggered && riskStatus.triggered_time && (
              <p>触发时间: {formatTime(riskStatus.triggered_time)}</p>
            )}
            {!riskStatus.triggered && riskStatus.recovered_time && (
              <p>恢复时间: {formatTime(riskStatus.recovered_time)}</p>
            )}
            <p>监控币种: {riskStatus.monitor_symbols?.join(', ') || 'N/A'}</p>
          </div>
        </div>
      ) : (
        <p>暂无风控状态数据</p>
      )}

      {/* Monitor Data */}
      <h3 style={{ marginTop: '32px' }}>监控币种数据</h3>
      {loadingData && monitorData.length === 0 ? (
        <p>加载监控数据...</p>
      ) : errorData ? (
        <p style={{ color: 'red' }}>错误: {errorData}</p>
      ) : monitorData.length === 0 ? (
        <p>暂无监控数据</p>
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <table className="risk-monitor-table">
            <thead>
              <tr>
                <th>币种</th>
                <th style={{ textAlign: 'right' }}>当前价格</th>
                <th style={{ textAlign: 'right' }}>平均价格</th>
                <th style={{ textAlign: 'right' }}>价格偏离</th>
                <th style={{ textAlign: 'right' }}>当前成交量</th>
                <th style={{ textAlign: 'right' }}>平均成交量</th>
                <th style={{ textAlign: 'right' }}>成交量倍数</th>
                <th>状态</th>
                <th>更新时间</th>
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
                      <span style={{ color: '#ff4d4f', fontWeight: 'bold' }}>⚠️ 异常</span>
                    ) : (
                      <span style={{ color: '#52c41a' }}>✓ 正常</span>
                    )}
                  </td>
                  <td>{formatTime(data.last_update)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

export default RiskMonitor

