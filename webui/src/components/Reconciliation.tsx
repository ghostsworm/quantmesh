import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSymbol } from '../contexts/SymbolContext'
import { getSymbols } from '../services/api'
import './Reconciliation.css'

interface ReconciliationStatus {
  reconcile_count: number
  last_reconcile_time: string
  local_position: number
  total_buy_qty: number
  total_sell_qty: number
  estimated_profit: number
}

interface ReconciliationHistoryItem {
  id: number
  exchange?: string
  symbol: string
  reconcile_time: string
  local_position: number
  exchange_position: number
  position_diff: number
  active_buy_orders: number
  active_sell_orders: number
  pending_sell_qty: number
  total_buy_qty: number
  total_sell_qty: number
  estimated_profit: number
  actual_profit: number
  created_at: string
}

interface TooltipData {
  x: number
  y: number
  item: ReconciliationHistoryItem
  type: 'estimated' | 'actual'
}

interface PositionTooltipData {
  x: number
  y: number
  item: ReconciliationHistoryItem
  type: 'local' | 'exchange'
}

interface AggregatedData {
  date: string
  avg_local_position: number
  avg_exchange_position: number
  avg_position_diff: number
  total_buy_qty: number
  total_sell_qty: number
  estimated_profit: number
  actual_profit: number
  record_count: number
}

interface AggregatedTooltipData {
  x: number
  y: number
  item: AggregatedData
  type: string
}

type TimePeriod = 'day' | 'week' | 'month'
type ViewMode = 'raw' | 'aggregated'

const Reconciliation: React.FC = () => {
  const { t } = useTranslation()
  const { selectedExchange, selectedSymbol } = useSymbol()
  const [status, setStatus] = useState<ReconciliationStatus | null>(null)
  const [history, setHistory] = useState<ReconciliationHistoryItem[]>([])
  const [aggregatedData, setAggregatedData] = useState<AggregatedData[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [historyLimit, setHistoryLimit] = useState(50)
  const [historyOffset, setHistoryOffset] = useState(0)
  const [tooltip, setTooltip] = useState<TooltipData | null>(null)
  const [positionTooltip, setPositionTooltip] = useState<PositionTooltipData | null>(null)
  const [symbolDirection, setSymbolDirection] = useState<'LONG' | 'SHORT' | null>(null)
  const [aggregatedTooltip, setAggregatedTooltip] = useState<AggregatedTooltipData | null>(null)
  // 图例显示状態
  const [showEstimated, setShowEstimated] = useState(true)
  const [showActual, setShowActual] = useState(true)
  const [showLocalPosition, setShowLocalPosition] = useState(true)
  const [showExchangePosition, setShowExchangePosition] = useState(true)
  // 新增：時间维度和視图模式
  const [viewMode, setViewMode] = useState<ViewMode>('raw')
  const [timePeriod, setTimePeriod] = useState<TimePeriod>('day')

  const fetchStatus = async () => {
    try {
      const params = new URLSearchParams()
      if (selectedExchange) params.append('exchange', selectedExchange)
      if (selectedSymbol) params.append('symbol', selectedSymbol)
      const response = await fetch(`/api/reconciliation/status?${params}`, {
        credentials: 'include',
      })
      if (!response.ok) {
        const errorText = await response.text()
        console.error('Failed to fetch reconciliation status:', response.status, errorText)
        throw new Error(`Failed to fetch reconciliation status: ${response.status} ${errorText}`)
      }
      const data = await response.json()
      console.log('Reconciliation status data:', data)
      setStatus(data)
      // 如果累计買入和累计賣出都是0，記錄警告
      if (data && data.total_buy_qty === 0 && data.total_sell_qty === 0) {
        console.warn('Total buy/sell qty are both 0:', { selectedExchange, selectedSymbol, data })
      }
    } catch (err) {
      console.error('Failed to fetch reconciliation status:', err)
      setError(err instanceof Error ? err.message : 'Failed to fetch reconciliation status')
    }
  }

  const fetchHistory = async () => {
    try {
      const params = new URLSearchParams({
        limit: historyLimit.toString(),
        offset: historyOffset.toString(),
      })
      if (selectedExchange) params.append('exchange', selectedExchange)
      if (selectedSymbol) params.append('symbol', selectedSymbol)
      // 扩大時间範圍到最近30天，确保能查詢到所有历史記錄
      const endTime = new Date()
      const startTime = new Date()
      startTime.setDate(startTime.getDate() - 30)
      params.append('start_time', startTime.toISOString())
      params.append('end_time', endTime.toISOString())
      const response = await fetch(`/api/reconciliation/history?${params}`, {
        credentials: 'include',
      })
      if (!response.ok) {
        const errorText = await response.text()
        console.error('Failed to fetch reconciliation history:', response.status, errorText)
        throw new Error(`Failed to fetch reconciliation history: ${response.status} ${errorText}`)
      }
      const data = await response.json()
      console.log('Reconciliation history data:', data)
      setHistory(data.history || [])
      if (data.history && data.history.length === 0) {
        console.warn('No reconciliation history found for:', { selectedExchange, selectedSymbol, params: params.toString() })
      }
    } catch (err) {
      console.error('Failed to fetch reconciliation history:', err)
      setError(err instanceof Error ? err.message : 'Failed to fetch reconciliation history')
    }
  }

  const fetchAggregatedData = async () => {
    try {
      const params = new URLSearchParams({
        period: timePeriod,
      })
      if (selectedExchange) params.append('exchange', selectedExchange)
      if (selectedSymbol) params.append('symbol', selectedSymbol)
      
      // 根據時间周期設置查詢範圍
      const endTime = new Date()
      const startTime = new Date()
      switch (timePeriod) {
        case 'month':
          startTime.setMonth(startTime.getMonth() - 12)
          break
        case 'week':
          startTime.setDate(startTime.getDate() - 90)
          break
        default: // day
          startTime.setDate(startTime.getDate() - 30)
      }
      params.append('start_time', startTime.toISOString())
      params.append('end_time', endTime.toISOString())
      
      const response = await fetch(`/api/reconciliation/aggregated?${params}`, {
        credentials: 'include',
      })
      if (!response.ok) {
        const errorText = await response.text()
        console.error('Failed to fetch aggregated data:', response.status, errorText)
        throw new Error(`Failed to fetch aggregated data: ${response.status} ${errorText}`)
      }
      const data = await response.json()
      console.log('Aggregated data:', data)
      setAggregatedData(data.data || [])
    } catch (err) {
      console.error('Failed to fetch aggregated data:', err)
      setError(err instanceof Error ? err.message : 'Failed to fetch aggregated data')
    }
  }

  useEffect(() => {
    const fetchData = async () => {
      setLoading(true)
      if (viewMode === 'raw') {
        await Promise.all([fetchStatus(), fetchHistory()])
      } else {
        await Promise.all([fetchStatus(), fetchAggregatedData()])
      }
      setLoading(false)
    }

    fetchData()
    const interval = setInterval(fetchData, 10000) // 每10秒刷新一次
    return () => clearInterval(interval)
  }, [historyLimit, historyOffset, selectedExchange, selectedSymbol, viewMode, timePeriod])

  // 獲取當前交易對的方向（做多/做空）
  useEffect(() => {
    if (!selectedExchange || !selectedSymbol) {
      setSymbolDirection(null)
      return
    }
    const loadDirection = async () => {
      try {
        const res = await getSymbols()
        const sym = res.symbols?.find(
          s => s.exchange?.toLowerCase() === selectedExchange?.toLowerCase() && s.symbol === selectedSymbol
        )
        setSymbolDirection(sym?.direction === 'SHORT' ? 'SHORT' : 'LONG')
      } catch {
        setSymbolDirection('LONG')
      }
    }
    loadDirection()
  }, [selectedExchange, selectedSymbol])

  const formatTime = (timeStr: string) => {
    try {
      return new Date(timeStr).toLocaleString('zh-CN')
    } catch {
      return timeStr
    }
  }

  if (loading && !status) {
    return (
      <div className="reconciliation">
        <h2>{t('reconciliation.history')}</h2>
        <p>加載中...</p>
      </div>
    )
  }

  if (error) {
    return (
      <div className="reconciliation">
        <h2>{t('reconciliation.history')}</h2>
        <p style={{ color: 'red' }}>錯误: {error}</p>
      </div>
    )
  }

  return (
    <div className="reconciliation">
      <h2 style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
        {t('reconciliation.history')}
        {symbolDirection != null && (
          <span
            style={{
              fontSize: '12px',
              padding: '2px 8px',
              borderRadius: '4px',
              backgroundColor: symbolDirection === 'SHORT' ? '#ed8936' : '#38a169',
              color: '#fff',
              fontWeight: 500,
            }}
          >
            {symbolDirection === 'SHORT' ? t('configuration.directionShort') : t('configuration.directionLong')}
          </span>
        )}
      </h2>

      {status && (
        <div className="status-cards">
          <div className="status-card">
            <h3>{t('reconciliation.history')}</h3>
            <p className="value">{status.reconcile_count}</p>
          </div>
          <div className="status-card">
            <h3>{t('reconciliation.reconcileTime')}</h3>
            <p className="value">{formatTime(status.last_reconcile_time)}</p>
          </div>
          <div className="status-card">
            <h3>{t('reconciliation.localPosition')}</h3>
            <p className="value">{status.local_position.toFixed(4)}</p>
          </div>
          <div className="status-card">
            <h3>{t('reconciliation.totalBuyQty')}</h3>
            <p className="value">{status.total_buy_qty.toFixed(2)}</p>
          </div>
          <div className="status-card">
            <h3>{t('reconciliation.totalSellQty')}</h3>
            <p className="value">{status.total_sell_qty.toFixed(2)}</p>
          </div>
          <div className="status-card">
            <h3>{t('reconciliation.estimatedProfit')}</h3>
            <p className="value" style={{ color: status.estimated_profit >= 0 ? '#52c41a' : '#ff4d4f' }}>
              {status.estimated_profit.toFixed(2)} USDT
            </p>
          </div>
        </div>
      )}

      {/* 數據視图控制面板 */}
      <div className="view-controls" style={{ marginTop: '24px', padding: '16px', background: '#f9fafb', borderRadius: '8px', display: 'flex', gap: '16px', alignItems: 'center' }}>
        <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
          <label style={{ fontSize: '14px', fontWeight: '500', color: '#374151' }}>數據視图:</label>
          <button
            onClick={() => setViewMode('raw')}
            style={{
              padding: '6px 16px',
              background: viewMode === 'raw' ? '#3b82f6' : '#fff',
              color: viewMode === 'raw' ? '#fff' : '#374151',
              border: '1px solid #d1d5db',
              borderRadius: '4px',
              cursor: 'pointer',
              fontSize: '14px',
            }}
          >
            原始數據
          </button>
          <button
            onClick={() => setViewMode('aggregated')}
            style={{
              padding: '6px 16px',
              background: viewMode === 'aggregated' ? '#3b82f6' : '#fff',
              color: viewMode === 'aggregated' ? '#fff' : '#374151',
              border: '1px solid #d1d5db',
              borderRadius: '4px',
              cursor: 'pointer',
              fontSize: '14px',
            }}
          >
            聚合數據
          </button>
        </div>
        
        {viewMode === 'aggregated' && (
          <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
            <label style={{ fontSize: '14px', fontWeight: '500', color: '#374151' }}>時间维度:</label>
            <button
              onClick={() => setTimePeriod('day')}
              style={{
                padding: '6px 16px',
                background: timePeriod === 'day' ? '#3b82f6' : '#fff',
                color: timePeriod === 'day' ? '#fff' : '#374151',
                border: '1px solid #d1d5db',
                borderRadius: '4px',
                cursor: 'pointer',
                fontSize: '14px',
              }}
            >
              按日
            </button>
            <button
              onClick={() => setTimePeriod('week')}
              style={{
                padding: '6px 16px',
                background: timePeriod === 'week' ? '#3b82f6' : '#fff',
                color: timePeriod === 'week' ? '#fff' : '#374151',
                border: '1px solid #d1d5db',
                borderRadius: '4px',
                cursor: 'pointer',
                fontSize: '14px',
              }}
            >
              按周
            </button>
            <button
              onClick={() => setTimePeriod('month')}
              style={{
                padding: '6px 16px',
                background: timePeriod === 'month' ? '#3b82f6' : '#fff',
                color: timePeriod === 'month' ? '#fff' : '#374151',
                border: '1px solid #d1d5db',
                borderRadius: '4px',
                cursor: 'pointer',
                fontSize: '14px',
              }}
            >
              按月
            </button>
          </div>
        )}
      </div>

      {/* 聚合數據多指標图表 */}
      {viewMode === 'aggregated' && aggregatedData.length > 0 && (
        <div style={{ marginTop: '32px' }}>
          <h3>盈利趋势（{timePeriod === 'day' ? '按日' : timePeriod === 'week' ? '按周' : '按月'}）</h3>
          <div style={{ width: '100%', height: '400px', background: '#fff', padding: '20px', borderRadius: '8px', boxShadow: '0 2px 8px rgba(0,0,0,0.1)' }}>
            <div style={{ width: '100%', height: '100%', position: 'relative' }}>
              <svg width="100%" height="100%" viewBox="0 0 800 350" preserveAspectRatio="xMidYMid meet" onMouseLeave={() => setAggregatedTooltip(null)}>
                {(() => {
                  const data = aggregatedData
                  const maxProfit = Math.max(...data.map(d => Math.max(d.estimated_profit, d.actual_profit)))
                  const minProfit = Math.min(...data.map(d => Math.min(d.estimated_profit, d.actual_profit)))
                  
                  let yMin = minProfit
                  let yMax = maxProfit
                  const range = yMax - yMin || 1
                  const padding = Math.max(range * 0.1, Math.abs(yMin) * 0.1, Math.abs(yMax) * 0.1) || 1
                  
                  const finalMin = yMin - padding
                  const finalMax = yMax + padding
                  const finalRange = finalMax - finalMin
                  
                  const getY = (value: number) => 290 - ((value - finalMin) / finalRange) * 240
                  const getX = (index: number) => 60 + (index / Math.max(data.length - 1, 1)) * 720
                  
                  const estimatedPath = data.map((item, i) => 
                    `${i === 0 ? 'M' : 'L'} ${getX(i)} ${getY(item.estimated_profit)}`
                  ).join(' ')
                  
                  const actualPath = data.map((item, i) => 
                    `${i === 0 ? 'M' : 'L'} ${getX(i)} ${getY(item.actual_profit)}`
                  ).join(' ')
                  
                  const zeroY = finalMin <= 0 && finalMax >= 0 ? getY(0) : null
                  
                  return (
                    <>
                      {/* 网格線 */}
                      {[0, 1, 2, 3, 4].map(i => (
                        <line key={`grid-${i}`} x1="60" y1={50 + i * 60} x2="780" y2={50 + i * 60} stroke="#e8e8e8" strokeWidth="1" />
                      ))}
                      
                      {/* 坐標轴 */}
                      <line x1="60" y1="290" x2="780" y2="290" stroke="#333" strokeWidth="2" />
                      <line x1="60" y1="50" x2="60" y2="290" stroke="#333" strokeWidth="2" />
                      
                      {/* 0線 */}
                      {zeroY !== null && (
                        <line x1="60" y1={zeroY} x2="780" y2={zeroY} stroke="#999" strokeWidth="1" strokeDasharray="4,4" opacity="0.5" />
                      )}
                      
                      {/* 預计盈利曲線 */}
                      {showEstimated && <path d={estimatedPath} fill="none" stroke="#1890ff" strokeWidth="2" />}
                      {showEstimated && data.map((item, i) => (
                        <circle
                          key={`est-${i}`}
                          cx={getX(i)}
                          cy={getY(item.estimated_profit)}
                          r="4"
                          fill="#1890ff"
                          className="profit-point"
                          onMouseEnter={(e) => {
                            const circle = e.currentTarget
                            const svg = circle.ownerSVGElement as SVGSVGElement
                            if (svg) {
                              const svgRect = svg.getBoundingClientRect()
                              const rect = circle.getBoundingClientRect()
                              setAggregatedTooltip({
                                x: rect.left - svgRect.left + rect.width / 2,
                                y: rect.top - svgRect.top - 10,
                                item,
                                type: 'estimated'
                              })
                            }
                          }}
                        />
                      ))}
                      
                      {/* 實際盈利曲線 */}
                      {showActual && <path d={actualPath} fill="none" stroke="#52c41a" strokeWidth="2" />}
                      {showActual && data.map((item, i) => (
                        <circle
                          key={`act-${i}`}
                          cx={getX(i)}
                          cy={getY(item.actual_profit)}
                          r="4"
                          fill="#52c41a"
                          className="profit-point"
                          onMouseEnter={(e) => {
                            const circle = e.currentTarget
                            const svg = circle.ownerSVGElement as SVGSVGElement
                            if (svg) {
                              const svgRect = svg.getBoundingClientRect()
                              const rect = circle.getBoundingClientRect()
                              setAggregatedTooltip({
                                x: rect.left - svgRect.left + rect.width / 2,
                                y: rect.top - svgRect.top - 10,
                                item,
                                type: 'actual'
                              })
                            }
                          }}
                        />
                      ))}
                      
                      {/* Y轴刻度 */}
                      {[0, 1, 2, 3, 4].map(i => {
                        const value = finalMin + finalRange * (4 - i) / 4
                        return (
                          <text key={`y-${i}`} x="50" y={50 + i * 60 + 5} textAnchor="end" fontSize="12" fill="#666">
                            {value.toFixed(2)}
                          </text>
                        )
                      })}
                      
                      {/* X轴刻度 */}
                      {data.map((item, i) => {
                        if (i % Math.ceil(data.length / 8) === 0 || i === data.length - 1) {
                          return (
                            <text key={`x-${i}`} x={getX(i)} y="310" textAnchor="middle" fontSize="10" fill="#666">
                              {item.date}
                            </text>
                          )
                        }
                        return null
                      })}
                      
                      {/* 图例 */}
                      <g transform="translate(650, 20)" style={{ cursor: 'pointer' }} onClick={() => setShowEstimated(!showEstimated)}>
                        <line x1="0" y1="0" x2="30" y2="0" stroke="#1890ff" strokeWidth="2" opacity={showEstimated ? 1 : 0.3} />
                        <text x="35" y="5" fontSize="12" fill="#666" opacity={showEstimated ? 1 : 0.5}>預计盈利</text>
                      </g>
                      <g transform="translate(650, 35)" style={{ cursor: 'pointer' }} onClick={() => setShowActual(!showActual)}>
                        <line x1="0" y1="0" x2="30" y2="0" stroke="#52c41a" strokeWidth="2" opacity={showActual ? 1 : 0.3} />
                        <text x="35" y="5" fontSize="12" fill="#666" opacity={showActual ? 1 : 0.5}>實際盈利</text>
                      </g>
                    </>
                  )
                })()}
              </svg>
              
              {/* Tooltip */}
              {aggregatedTooltip && (
                <div className="profit-tooltip" style={{ position: 'absolute', left: `${aggregatedTooltip.x}px`, top: `${aggregatedTooltip.y}px`, transform: 'translate(-50%, -100%)', pointerEvents: 'none' }}>
                  <div className="tooltip-content">
                    <div className="tooltip-header">
                      <strong>{aggregatedTooltip.item.date}</strong>
                    </div>
                    <div className="tooltip-body">
                      <div className="tooltip-row">
                        <span className="tooltip-label">預计盈利:</span>
                        <span className="tooltip-value" style={{ color: aggregatedTooltip.item.estimated_profit >= 0 ? '#52c41a' : '#ff4d4f' }}>
                          {aggregatedTooltip.item.estimated_profit.toFixed(2)} USDT
                        </span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">實際盈利:</span>
                        <span className="tooltip-value" style={{ color: aggregatedTooltip.item.actual_profit >= 0 ? '#52c41a' : '#ff4d4f' }}>
                          {aggregatedTooltip.item.actual_profit.toFixed(2)} USDT
                        </span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">累计買入:</span>
                        <span className="tooltip-value">{aggregatedTooltip.item.total_buy_qty.toFixed(2)}</span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">累计賣出:</span>
                        <span className="tooltip-value">{aggregatedTooltip.item.total_sell_qty.toFixed(2)}</span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">記錄數:</span>
                        <span className="tooltip-value">{aggregatedTooltip.item.record_count}</span>
                      </div>
                    </div>
                  </div>
                </div>
              )}
            </div>
          </div>
          
          {/* 持倉走势图 */}
          <div style={{ marginTop: '32px' }}>
            <h3>持倉走势（{timePeriod === 'day' ? '按日' : timePeriod === 'week' ? '按周' : '按月'}）</h3>
            <div style={{ width: '100%', height: '400px', background: '#fff', padding: '20px', borderRadius: '8px', boxShadow: '0 2px 8px rgba(0,0,0,0.1)' }}>
              <div style={{ width: '100%', height: '100%', position: 'relative' }}>
                <svg width="100%" height="100%" viewBox="0 0 800 350" preserveAspectRatio="xMidYMid meet" onMouseLeave={() => setAggregatedTooltip(null)}>
                  {(() => {
                    const data = aggregatedData
                    const maxPosition = Math.max(...data.map(d => Math.max(d.avg_local_position, d.avg_exchange_position)))
                    const minPosition = Math.min(...data.map(d => Math.min(d.avg_local_position, d.avg_exchange_position)))
                    
                    let yMin = minPosition
                    let yMax = maxPosition
                    
                    if (yMax - yMin < 0.0001) {
                      yMin = Math.max(0, yMin - Math.max(Math.abs(yMin) * 0.1, 0.01))
                      yMax = yMax + Math.max(Math.abs(yMax) * 0.1, 0.01)
                    }
                    
                    const range = yMax - yMin || 0.01
                    const padding = Math.max(range * 0.1, 0.01)
                    const finalMin = Math.max(0, yMin - padding)
                    const finalMax = yMax + padding
                    const finalRange = finalMax - finalMin
                    
                    const getY = (value: number) => 290 - ((value - finalMin) / finalRange) * 240
                    const getX = (index: number) => 60 + (index / Math.max(data.length - 1, 1)) * 720
                    
                    const localPath = data.map((item, i) => 
                      `${i === 0 ? 'M' : 'L'} ${getX(i)} ${getY(item.avg_local_position)}`
                    ).join(' ')
                    
                    const exchangePath = data.map((item, i) => 
                      `${i === 0 ? 'M' : 'L'} ${getX(i)} ${getY(item.avg_exchange_position)}`
                    ).join(' ')
                    
                    return (
                      <>
                        {/* 网格線 */}
                        {[0, 1, 2, 3, 4].map(i => (
                          <line key={`pos-grid-${i}`} x1="60" y1={50 + i * 60} x2="780" y2={50 + i * 60} stroke="#e8e8e8" strokeWidth="1" />
                        ))}
                        
                        {/* 坐標轴 */}
                        <line x1="60" y1="290" x2="780" y2="290" stroke="#333" strokeWidth="2" />
                        <line x1="60" y1="50" x2="60" y2="290" stroke="#333" strokeWidth="2" />
                        
                        {/* 本地持倉曲線 */}
                        {showLocalPosition && <path d={localPath} fill="none" stroke="#1890ff" strokeWidth="2" />}
                        {showLocalPosition && data.map((item, i) => (
                          <circle
                            key={`local-${i}`}
                            cx={getX(i)}
                            cy={getY(item.avg_local_position)}
                            r="4"
                            fill="#1890ff"
                            className="profit-point"
                            onMouseEnter={(e) => {
                              const circle = e.currentTarget
                              const svg = circle.ownerSVGElement as SVGSVGElement
                              if (svg) {
                                const svgRect = svg.getBoundingClientRect()
                                const rect = circle.getBoundingClientRect()
                                setAggregatedTooltip({
                                  x: rect.left - svgRect.left + rect.width / 2,
                                  y: rect.top - svgRect.top - 10,
                                  item,
                                  type: 'local'
                                })
                              }
                            }}
                          />
                        ))}
                        
                        {/* 交易所持倉曲線 */}
                        {showExchangePosition && <path d={exchangePath} fill="none" stroke="#52c41a" strokeWidth="2" />}
                        {showExchangePosition && data.map((item, i) => (
                          <circle
                            key={`exchange-${i}`}
                            cx={getX(i)}
                            cy={getY(item.avg_exchange_position)}
                            r="4"
                            fill="#52c41a"
                            className="profit-point"
                            onMouseEnter={(e) => {
                              const circle = e.currentTarget
                              const svg = circle.ownerSVGElement as SVGSVGElement
                              if (svg) {
                                const svgRect = svg.getBoundingClientRect()
                                const rect = circle.getBoundingClientRect()
                                setAggregatedTooltip({
                                  x: rect.left - svgRect.left + rect.width / 2,
                                  y: rect.top - svgRect.top - 10,
                                  item,
                                  type: 'exchange'
                                })
                              }
                            }}
                          />
                        ))}
                        
                        {/* Y轴刻度 */}
                        {[0, 1, 2, 3, 4].map(i => {
                          const value = finalMin + finalRange * (4 - i) / 4
                          return (
                            <text key={`pos-y-${i}`} x="50" y={50 + i * 60 + 5} textAnchor="end" fontSize="12" fill="#666">
                              {value.toFixed(4)}
                            </text>
                          )
                        })}
                        
                        {/* X轴刻度 */}
                        {data.map((item, i) => {
                          if (i % Math.ceil(data.length / 8) === 0 || i === data.length - 1) {
                            return (
                              <text key={`pos-x-${i}`} x={getX(i)} y="310" textAnchor="middle" fontSize="10" fill="#666">
                                {item.date}
                              </text>
                            )
                          }
                          return null
                        })}
                        
                        {/* 图例 */}
                        <g transform="translate(650, 20)" style={{ cursor: 'pointer' }} onClick={() => setShowLocalPosition(!showLocalPosition)}>
                          <line x1="0" y1="0" x2="30" y2="0" stroke="#1890ff" strokeWidth="2" opacity={showLocalPosition ? 1 : 0.3} />
                          <text x="35" y="5" fontSize="12" fill="#666" opacity={showLocalPosition ? 1 : 0.5}>本地持倉</text>
                        </g>
                        <g transform="translate(650, 35)" style={{ cursor: 'pointer' }} onClick={() => setShowExchangePosition(!showExchangePosition)}>
                          <line x1="0" y1="0" x2="30" y2="0" stroke="#52c41a" strokeWidth="2" opacity={showExchangePosition ? 1 : 0.3} />
                          <text x="35" y="5" fontSize="12" fill="#666" opacity={showExchangePosition ? 1 : 0.5}>交易所持倉</text>
                        </g>
                      </>
                    )
                  })()}
                </svg>
                
                {/* Tooltip */}
                {aggregatedTooltip && (
                  <div className="profit-tooltip" style={{ position: 'absolute', left: `${aggregatedTooltip.x}px`, top: `${aggregatedTooltip.y}px`, transform: 'translate(-50%, -100%)', pointerEvents: 'none' }}>
                    <div className="tooltip-content">
                      <div className="tooltip-header">
                        <strong>{aggregatedTooltip.item.date}</strong>
                      </div>
                      <div className="tooltip-body">
                        <div className="tooltip-row">
                          <span className="tooltip-label">平均本地持倉:</span>
                          <span className="tooltip-value">{aggregatedTooltip.item.avg_local_position.toFixed(4)}</span>
                        </div>
                        <div className="tooltip-row">
                          <span className="tooltip-label">平均交易所持倉:</span>
                          <span className="tooltip-value">{aggregatedTooltip.item.avg_exchange_position.toFixed(4)}</span>
                        </div>
                        <div className="tooltip-row">
                          <span className="tooltip-label">平均持倉差异:</span>
                          <span className="tooltip-value" style={{ color: Math.abs(aggregatedTooltip.item.avg_position_diff) > 0.0001 ? '#ff4d4f' : '#52c41a' }}>
                            {aggregatedTooltip.item.avg_position_diff.toFixed(4)}
                          </span>
                        </div>
                      </div>
                    </div>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* 盈利曲線图表 */}
      {viewMode === 'raw' && history.length > 0 && (
        <div style={{ marginTop: '32px' }}>
          <h3>盈利趋势</h3>
          <div style={{ width: '100%', height: '400px', background: '#fff', padding: '20px', borderRadius: '8px', boxShadow: '0 2px 8px rgba(0,0,0,0.1)' }}>
            <div style={{ width: '100%', height: '100%', position: 'relative' }}>
              <svg width="100%" height="100%" viewBox="0 0 800 350" preserveAspectRatio="xMidYMid meet" onMouseLeave={() => setTooltip(null)}>
                {/* 绘制网格線 */}
                <g>
                  {[0, 1, 2, 3, 4].map(i => (
                    <line
                      key={`grid-${i}`}
                      x1="60"
                      y1={50 + i * 60}
                      x2="780"
                      y2={50 + i * 60}
                      stroke="#e8e8e8"
                      strokeWidth="1"
                    />
                  ))}
                </g>
                
                {/* 绘制坐標轴 */}
                <line x1="60" y1="290" x2="780" y2="290" stroke="#333" strokeWidth="2" />
                <line x1="60" y1="50" x2="60" y2="290" stroke="#333" strokeWidth="2" />
                
                {/* 绘制曲線 */}
                {(() => {
                  const sortedHistory = [...history].reverse()
                  const maxProfit = Math.max(...sortedHistory.map(h => Math.max(h.estimated_profit, h.actual_profit)))
                  const minProfit = Math.min(...sortedHistory.map(h => Math.min(h.estimated_profit, h.actual_profit)))
                  
                  // 改進 Y 轴範圍计算，确保负數能正确显示
                  // 如果最小值為负數，确保範圍包含 0 或至少正确显示负數範圍
                  let yMin = minProfit
                  let yMax = maxProfit
                  
                  // 如果所有值都是负數，确保 Y 轴範圍能正确显示
                  if (minProfit < 0 && maxProfit < 0) {
                    // 全部為负數時，保持原範圍，但添加适當的 padding
                    yMin = minProfit
                    yMax = maxProfit
                  } else if (minProfit < 0 && maxProfit >= 0) {
                    // 有正有负時，确保包含 0
                    yMin = minProfit
                    yMax = maxProfit
                  } else if (minProfit >= 0 && maxProfit >= 0) {
                    // 全部為正數時，保持原逻辑
                    yMin = minProfit
                    yMax = maxProfit
                  }
                  
                  const range = yMax - yMin || 1
                  const padding = Math.max(range * 0.1, Math.abs(yMin) * 0.1, Math.abs(yMax) * 0.1) || 1
                  
                  // 确保 padding 不會让範圍变得不合理
                  const finalMin = yMin - padding
                  const finalMax = yMax + padding
                  const finalRange = finalMax - finalMin
                  
                  const getY = (value: number) => {
                    return 290 - ((value - finalMin) / finalRange) * 240
                  }
                  
                  const getX = (index: number) => {
                    return 60 + (index / Math.max(sortedHistory.length - 1, 1)) * 720
                  }
                  
                  // 預计盈利曲線
                  const estimatedPath = sortedHistory.map((item, i) => 
                    `${i === 0 ? 'M' : 'L'} ${getX(i)} ${getY(item.estimated_profit)}`
                  ).join(' ')
                  
                  // 實際盈利曲線
                  const actualPath = sortedHistory.map((item, i) => 
                    `${i === 0 ? 'M' : 'L'} ${getX(i)} ${getY(item.actual_profit)}`
                  ).join(' ')
                  
                  // 绘制 0 線（如果範圍包含 0）
                  const zeroY = finalMin <= 0 && finalMax >= 0 ? getY(0) : null
                  
                  return (
                    <>
                      {/* 0 線 */}
                      {zeroY !== null && (
                        <line
                          x1="60"
                          y1={zeroY}
                          x2="780"
                          y2={zeroY}
                          stroke="#999"
                          strokeWidth="1"
                          strokeDasharray="4,4"
                          opacity="0.5"
                        />
                      )}
                      
                      {/* 預计盈利曲線 */}
                      {showEstimated && <path d={estimatedPath} fill="none" stroke="#1890ff" strokeWidth="2" />}
                      {showEstimated && sortedHistory.map((item, i) => {
                        const x = getX(i)
                        const y = getY(item.estimated_profit)
                        return (
                          <circle
                            key={`est-${i}`}
                            cx={x}
                            cy={y}
                            r="4"
                            fill="#1890ff"
                            className="profit-point"
                            onMouseEnter={(e) => {
                              const circle = e.currentTarget
                              const svg = circle.ownerSVGElement as SVGSVGElement
                              if (svg) {
                                const svgRect = svg.getBoundingClientRect()
                                const point = svg.createSVGPoint()
                                point.x = parseFloat(circle.getAttribute('cx') || '0')
                                point.y = parseFloat(circle.getAttribute('cy') || '0')
                                const screenCTM = circle.getScreenCTM()
                                if (screenCTM) {
                                  const transformedPoint = point.matrixTransform(screenCTM)
                                  setTooltip({
                                    x: transformedPoint.x - svgRect.left,
                                    y: transformedPoint.y - svgRect.top - 10,
                                    item,
                                    type: 'estimated'
                                  })
                                } else {
                                  // 降级方案：使用 getBoundingClientRect
                                  const rect = circle.getBoundingClientRect()
                                  setTooltip({
                                    x: rect.left - svgRect.left + rect.width / 2,
                                    y: rect.top - svgRect.top - 10,
                                    item,
                                    type: 'estimated'
                                  })
                                }
                              }
                            }}
                            onMouseLeave={() => setTooltip(null)}
                          />
                        )
                      })}
                      
                      {/* 實際盈利曲線 */}
                      {showActual && <path d={actualPath} fill="none" stroke="#52c41a" strokeWidth="2" />}
                      {showActual && sortedHistory.map((item, i) => {
                        const x = getX(i)
                        const y = getY(item.actual_profit)
                        return (
                          <circle
                            key={`act-${i}`}
                            cx={x}
                            cy={y}
                            r="4"
                            fill="#52c41a"
                            className="profit-point"
                            onMouseEnter={(e) => {
                              const circle = e.currentTarget
                              const svg = circle.ownerSVGElement as SVGSVGElement
                              if (svg) {
                                const svgRect = svg.getBoundingClientRect()
                                const point = svg.createSVGPoint()
                                point.x = parseFloat(circle.getAttribute('cx') || '0')
                                point.y = parseFloat(circle.getAttribute('cy') || '0')
                                const screenCTM = circle.getScreenCTM()
                                if (screenCTM) {
                                  const transformedPoint = point.matrixTransform(screenCTM)
                                  setTooltip({
                                    x: transformedPoint.x - svgRect.left,
                                    y: transformedPoint.y - svgRect.top - 10,
                                    item,
                                    type: 'actual'
                                  })
                                } else {
                                  // 降级方案：使用 getBoundingClientRect
                                  const rect = circle.getBoundingClientRect()
                                  setTooltip({
                                    x: rect.left - svgRect.left + rect.width / 2,
                                    y: rect.top - svgRect.top - 10,
                                    item,
                                    type: 'actual'
                                  })
                                }
                              }
                            }}
                            onMouseLeave={() => setTooltip(null)}
                          />
                        )
                      })}
                      
                      {/* Y轴刻度 */}
                      {[0, 1, 2, 3, 4].map(i => {
                        const value = finalMin + finalRange * (4 - i) / 4
                        return (
                          <text key={`y-${i}`} x="50" y={50 + i * 60 + 5} textAnchor="end" fontSize="12" fill="#666">
                            {value.toFixed(2)}
                          </text>
                        )
                      })}
                      
                      {/* 图例 */}
                      <g 
                        transform="translate(650, 20)" 
                        style={{ cursor: 'pointer' }}
                        onClick={() => setShowEstimated(!showEstimated)}
                      >
                        <line x1="0" y1="0" x2="30" y2="0" stroke="#1890ff" strokeWidth="2" opacity={showEstimated ? 1 : 0.3} />
                        <text x="35" y="5" fontSize="12" fill="#666" opacity={showEstimated ? 1 : 0.5}>預计盈利</text>
                      </g>
                      <g 
                        transform="translate(650, 35)" 
                        style={{ cursor: 'pointer' }}
                        onClick={() => setShowActual(!showActual)}
                      >
                        <line x1="0" y1="0" x2="30" y2="0" stroke="#52c41a" strokeWidth="2" opacity={showActual ? 1 : 0.3} />
                        <text x="35" y="5" fontSize="12" fill="#666" opacity={showActual ? 1 : 0.5}>實際盈利</text>
                      </g>
                    </>
                  )
                })()}
              </svg>
              
              {/* Tooltip */}
              {tooltip && (
                <div
                  className="profit-tooltip"
                  style={{
                    position: 'absolute',
                    left: `${tooltip.x}px`,
                    top: `${tooltip.y}px`,
                    transform: 'translate(-50%, -100%)',
                    pointerEvents: 'none',
                  }}
                >
                  <div className="tooltip-content">
                    <div className="tooltip-header">
                      <strong>{formatTime(tooltip.item.reconcile_time)}</strong>
                    </div>
                    <div className="tooltip-body">
                      <div className="tooltip-row">
                        <span className="tooltip-label">預计盈利:</span>
                        <span className="tooltip-value" style={{ color: tooltip.item.estimated_profit >= 0 ? '#52c41a' : '#ff4d4f' }}>
                          {tooltip.item.estimated_profit.toFixed(2)} USDT
                        </span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">實際盈利:</span>
                        <span className="tooltip-value" style={{ color: tooltip.item.actual_profit >= 0 ? '#52c41a' : '#ff4d4f' }}>
                          {tooltip.item.actual_profit.toFixed(2)} USDT
                        </span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">本地持倉:</span>
                        <span className="tooltip-value">{tooltip.item.local_position.toFixed(4)}</span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">累计買入:</span>
                        <span className="tooltip-value">{tooltip.item.total_buy_qty.toFixed(2)}</span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">累计賣出:</span>
                        <span className="tooltip-value">{tooltip.item.total_sell_qty.toFixed(2)}</span>
                      </div>
                    </div>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* 倉位走势图 */}
      {viewMode === 'raw' && history.length > 0 && (
        <div style={{ marginTop: '32px' }}>
          <h3>倉位走势</h3>
          <div style={{ width: '100%', height: '400px', background: '#fff', padding: '20px', borderRadius: '8px', boxShadow: '0 2px 8px rgba(0,0,0,0.1)' }}>
            <div style={{ width: '100%', height: '100%', position: 'relative' }}>
              <svg width="100%" height="100%" viewBox="0 0 800 350" preserveAspectRatio="xMidYMid meet" onMouseLeave={() => setPositionTooltip(null)}>
                {/* 绘制网格線 */}
                <g>
                  {[0, 1, 2, 3, 4].map(i => (
                    <line
                      key={`pos-grid-${i}`}
                      x1="60"
                      y1={50 + i * 60}
                      x2="780"
                      y2={50 + i * 60}
                      stroke="#e8e8e8"
                      strokeWidth="1"
                    />
                  ))}
                </g>
                
                {/* 绘制坐標轴 */}
                <line x1="60" y1="290" x2="780" y2="290" stroke="#333" strokeWidth="2" />
                <line x1="60" y1="50" x2="60" y2="290" stroke="#333" strokeWidth="2" />
                
                {/* 绘制曲線 */}
                {(() => {
                  const sortedHistory = [...history].reverse()
                  const maxPosition = Math.max(...sortedHistory.map(h => Math.max(h.local_position, h.exchange_position)))
                  const minPosition = Math.min(...sortedHistory.map(h => Math.min(h.local_position, h.exchange_position)))
                  
                  // 计算 Y 轴範圍
                  let yMin = minPosition
                  let yMax = maxPosition
                  
                  // 如果所有值都是0或很小，設置一個最小範圍
                  if (yMax - yMin < 0.0001) {
                    yMin = Math.max(0, yMin - Math.max(Math.abs(yMin) * 0.1, 0.01))
                    yMax = yMax + Math.max(Math.abs(yMax) * 0.1, 0.01)
                  }
                  
                  const range = yMax - yMin || 0.01
                  const padding = Math.max(range * 0.1, 0.01)
                  
                  const finalMin = Math.max(0, yMin - padding)
                  const finalMax = yMax + padding
                  const finalRange = finalMax - finalMin
                  
                  const getY = (value: number) => {
                    return 290 - ((value - finalMin) / finalRange) * 240
                  }
                  
                  const getX = (index: number) => {
                    return 60 + (index / Math.max(sortedHistory.length - 1, 1)) * 720
                  }
                  
                  // 本地持倉曲線
                  const localPositionPath = sortedHistory.map((item, i) => 
                    `${i === 0 ? 'M' : 'L'} ${getX(i)} ${getY(item.local_position)}`
                  ).join(' ')
                  
                  // 交易所持倉曲線
                  const exchangePositionPath = sortedHistory.map((item, i) => 
                    `${i === 0 ? 'M' : 'L'} ${getX(i)} ${getY(item.exchange_position)}`
                  ).join(' ')
                  
                  return (
                    <>
                      {/* 本地持倉曲線 */}
                      {showLocalPosition && <path d={localPositionPath} fill="none" stroke="#1890ff" strokeWidth="2" />}
                      {showLocalPosition && sortedHistory.map((item, i) => {
                        const x = getX(i)
                        const y = getY(item.local_position)
                        return (
                          <circle
                            key={`local-pos-${i}`}
                            cx={x}
                            cy={y}
                            r="4"
                            fill="#1890ff"
                            className="profit-point"
                            onMouseEnter={(e) => {
                              const circle = e.currentTarget
                              const svg = circle.ownerSVGElement as SVGSVGElement
                              if (svg) {
                                const svgRect = svg.getBoundingClientRect()
                                const point = svg.createSVGPoint()
                                point.x = parseFloat(circle.getAttribute('cx') || '0')
                                point.y = parseFloat(circle.getAttribute('cy') || '0')
                                const screenCTM = circle.getScreenCTM()
                                if (screenCTM) {
                                  const transformedPoint = point.matrixTransform(screenCTM)
                                  setPositionTooltip({
                                    x: transformedPoint.x - svgRect.left,
                                    y: transformedPoint.y - svgRect.top - 10,
                                    item,
                                    type: 'local'
                                  })
                                } else {
                                  const rect = circle.getBoundingClientRect()
                                  setPositionTooltip({
                                    x: rect.left - svgRect.left + rect.width / 2,
                                    y: rect.top - svgRect.top - 10,
                                    item,
                                    type: 'local'
                                  })
                                }
                              }
                            }}
                            onMouseLeave={() => setPositionTooltip(null)}
                          />
                        )
                      })}
                      
                      {/* 交易所持倉曲線 */}
                      {showExchangePosition && <path d={exchangePositionPath} fill="none" stroke="#52c41a" strokeWidth="2" />}
                      {showExchangePosition && sortedHistory.map((item, i) => {
                        const x = getX(i)
                        const y = getY(item.exchange_position)
                        return (
                          <circle
                            key={`exchange-pos-${i}`}
                            cx={x}
                            cy={y}
                            r="4"
                            fill="#52c41a"
                            className="profit-point"
                            onMouseEnter={(e) => {
                              const circle = e.currentTarget
                              const svg = circle.ownerSVGElement as SVGSVGElement
                              if (svg) {
                                const svgRect = svg.getBoundingClientRect()
                                const point = svg.createSVGPoint()
                                point.x = parseFloat(circle.getAttribute('cx') || '0')
                                point.y = parseFloat(circle.getAttribute('cy') || '0')
                                const screenCTM = circle.getScreenCTM()
                                if (screenCTM) {
                                  const transformedPoint = point.matrixTransform(screenCTM)
                                  setPositionTooltip({
                                    x: transformedPoint.x - svgRect.left,
                                    y: transformedPoint.y - svgRect.top - 10,
                                    item,
                                    type: 'exchange'
                                  })
                                } else {
                                  const rect = circle.getBoundingClientRect()
                                  setPositionTooltip({
                                    x: rect.left - svgRect.left + rect.width / 2,
                                    y: rect.top - svgRect.top - 10,
                                    item,
                                    type: 'exchange'
                                  })
                                }
                              }
                            }}
                            onMouseLeave={() => setPositionTooltip(null)}
                          />
                        )
                      })}
                      
                      {/* Y轴刻度 */}
                      {[0, 1, 2, 3, 4].map(i => {
                        const value = finalMin + finalRange * (4 - i) / 4
                        return (
                          <text key={`pos-y-${i}`} x="50" y={50 + i * 60 + 5} textAnchor="end" fontSize="12" fill="#666">
                            {value.toFixed(4)}
                          </text>
                        )
                      })}
                      
                      {/* 图例 */}
                      <g 
                        transform="translate(650, 20)" 
                        style={{ cursor: 'pointer' }}
                        onClick={() => setShowLocalPosition(!showLocalPosition)}
                      >
                        <line x1="0" y1="0" x2="30" y2="0" stroke="#1890ff" strokeWidth="2" opacity={showLocalPosition ? 1 : 0.3} />
                        <text x="35" y="5" fontSize="12" fill="#666" opacity={showLocalPosition ? 1 : 0.5}>本地持倉</text>
                      </g>
                      <g 
                        transform="translate(650, 35)" 
                        style={{ cursor: 'pointer' }}
                        onClick={() => setShowExchangePosition(!showExchangePosition)}
                      >
                        <line x1="0" y1="0" x2="30" y2="0" stroke="#52c41a" strokeWidth="2" opacity={showExchangePosition ? 1 : 0.3} />
                        <text x="35" y="5" fontSize="12" fill="#666" opacity={showExchangePosition ? 1 : 0.5}>交易所持倉</text>
                      </g>
                    </>
                  )
                })()}
              </svg>
              
              {/* Position Tooltip */}
              {positionTooltip && (
                <div
                  className="profit-tooltip"
                  style={{
                    position: 'absolute',
                    left: `${positionTooltip.x}px`,
                    top: `${positionTooltip.y}px`,
                    transform: 'translate(-50%, -100%)',
                    pointerEvents: 'none',
                  }}
                >
                  <div className="tooltip-content">
                    <div className="tooltip-header">
                      <strong>{formatTime(positionTooltip.item.reconcile_time)}</strong>
                    </div>
                    <div className="tooltip-body">
                      <div className="tooltip-row">
                        <span className="tooltip-label">本地持倉:</span>
                        <span className="tooltip-value">{positionTooltip.item.local_position.toFixed(4)}</span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">交易所持倉:</span>
                        <span className="tooltip-value">{positionTooltip.item.exchange_position.toFixed(4)}</span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">持倉差异:</span>
                        <span className="tooltip-value" style={{ color: Math.abs(positionTooltip.item.position_diff) > 0.0001 ? '#ff4d4f' : '#52c41a' }}>
                          {positionTooltip.item.position_diff.toFixed(4)}
                        </span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">挂單買單:</span>
                        <span className="tooltip-value">{positionTooltip.item.active_buy_orders}</span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">挂單賣單:</span>
                        <span className="tooltip-value">{positionTooltip.item.active_sell_orders}</span>
                      </div>
                    </div>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {viewMode === 'raw' && (
        <div style={{ marginTop: '32px' }}>
          <h3>{t('reconciliation.history')}</h3>
          <div className="history-filters">
            <label>
              {t('reconciliation.perPage')}
              <select value={historyLimit} onChange={(e) => setHistoryLimit(Number(e.target.value))}>
                <option value={20}>20</option>
                <option value={50}>50</option>
                <option value={100}>100</option>
              </select>
            </label>
            <button onClick={() => setHistoryOffset(prev => Math.max(0, prev - historyLimit))}>{t('reconciliation.previousPage')}</button>
            <span>{t('reconciliation.page')} {Math.floor(historyOffset / historyLimit) + 1}</span>
            <button onClick={() => setHistoryOffset(prev => prev + historyLimit)}>{t('reconciliation.nextPage')}</button>
          </div>

          {history.length === 0 ? (
            <p>{t('reconciliation.noHistory')}</p>
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table className="history-table">
                <thead>
                  <tr>
                    <th>{t('reconciliation.reconcileTime')}</th>
                    <th>{t('reconciliation.localPosition')}</th>
                    <th>{t('reconciliation.exchangePosition')}</th>
                    <th>{t('reconciliation.difference')}</th>
                    <th>{t('reconciliation.activeBuyOrders')}</th>
                    <th>{t('reconciliation.activeSellOrders')}</th>
                    <th>{t('reconciliation.pendingSellQty')}</th>
                    <th>{t('reconciliation.totalBuyQty')}</th>
                    <th>{t('reconciliation.totalSellQty')}</th>
                    <th>{t('reconciliation.estimatedProfit')}</th>
                    <th>{t('reconciliation.actualProfit')}</th>
                  </tr>
                </thead>
                <tbody>
                  {history.map((item) => (
                    <tr key={item.id}>
                      <td>{formatTime(item.reconcile_time)}</td>
                      <td>{item.local_position.toFixed(4)}</td>
                      <td>{item.exchange_position.toFixed(4)}</td>
                      <td style={{ color: Math.abs(item.position_diff) > 0.0001 ? '#ff4d4f' : '#52c41a' }}>
                        {item.position_diff.toFixed(4)}
                      </td>
                      <td>{item.active_buy_orders}</td>
                      <td>{item.active_sell_orders}</td>
                      <td>{item.pending_sell_qty.toFixed(4)}</td>
                      <td>{item.total_buy_qty.toFixed(2)}</td>
                      <td>{item.total_sell_qty.toFixed(2)}</td>
                      <td style={{ color: item.estimated_profit >= 0 ? '#52c41a' : '#ff4d4f' }}>
                        {item.estimated_profit.toFixed(2)}
                      </td>
                      <td style={{ color: item.actual_profit >= 0 ? '#52c41a' : '#ff4d4f' }}>
                        {item.actual_profit.toFixed(2)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* 聚合數據表格 */}
      {viewMode === 'aggregated' && aggregatedData.length > 0 && (
        <div style={{ marginTop: '32px' }}>
          <h3>聚合數據详情（{timePeriod === 'day' ? '按日' : timePeriod === 'week' ? '按周' : '按月'}）</h3>
          <div style={{ overflowX: 'auto' }}>
            <table className="history-table">
              <thead>
                <tr>
                  <th>日期</th>
                  <th>平均本地持倉</th>
                  <th>平均交易所持倉</th>
                  <th>平均持倉差异</th>
                  <th>累计買入</th>
                  <th>累计賣出</th>
                  <th>預计盈利</th>
                  <th>實際盈利</th>
                  <th>記錄數</th>
                </tr>
              </thead>
              <tbody>
                {aggregatedData.map((item, index) => (
                  <tr key={index}>
                    <td>{item.date}</td>
                    <td>{item.avg_local_position.toFixed(4)}</td>
                    <td>{item.avg_exchange_position.toFixed(4)}</td>
                    <td style={{ color: Math.abs(item.avg_position_diff) > 0.0001 ? '#ff4d4f' : '#52c41a' }}>
                      {item.avg_position_diff.toFixed(4)}
                    </td>
                    <td>{item.total_buy_qty.toFixed(2)}</td>
                    <td>{item.total_sell_qty.toFixed(2)}</td>
                    <td style={{ color: item.estimated_profit >= 0 ? '#52c41a' : '#ff4d4f' }}>
                      {item.estimated_profit.toFixed(2)}
                    </td>
                    <td style={{ color: item.actual_profit >= 0 ? '#52c41a' : '#ff4d4f' }}>
                      {item.actual_profit.toFixed(2)}
                    </td>
                    <td>{item.record_count}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}

export default Reconciliation

