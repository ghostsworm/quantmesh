import React, { useEffect, useState } from 'react'
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

const Reconciliation: React.FC = () => {
  const [status, setStatus] = useState<ReconciliationStatus | null>(null)
  const [history, setHistory] = useState<ReconciliationHistoryItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [historyLimit, setHistoryLimit] = useState(50)
  const [historyOffset, setHistoryOffset] = useState(0)
  const [tooltip, setTooltip] = useState<TooltipData | null>(null)
  const [positionTooltip, setPositionTooltip] = useState<PositionTooltipData | null>(null)
  // 图例显示状态
  const [showEstimated, setShowEstimated] = useState(true)
  const [showActual, setShowActual] = useState(true)
  const [showLocalPosition, setShowLocalPosition] = useState(true)
  const [showExchangePosition, setShowExchangePosition] = useState(true)

  const fetchStatus = async () => {
    try {
      const response = await fetch('/api/reconciliation/status', {
        credentials: 'include',
      })
      if (!response.ok) throw new Error('获取对账状态失败')
      const data = await response.json()
      setStatus(data)
    } catch (err) {
      console.error('Failed to fetch reconciliation status:', err)
      setError(err instanceof Error ? err.message : '获取对账状态失败')
    }
  }

  const fetchHistory = async () => {
    try {
      const params = new URLSearchParams({
        limit: historyLimit.toString(),
        offset: historyOffset.toString(),
      })
      const response = await fetch(`/api/reconciliation/history?${params}`, {
        credentials: 'include',
      })
      if (!response.ok) throw new Error('获取对账历史失败')
      const data = await response.json()
      setHistory(data.history || [])
    } catch (err) {
      console.error('Failed to fetch reconciliation history:', err)
      setError(err instanceof Error ? err.message : '获取对账历史失败')
    }
  }

  useEffect(() => {
    const fetchData = async () => {
      setLoading(true)
      await Promise.all([fetchStatus(), fetchHistory()])
      setLoading(false)
    }

    fetchData()
    const interval = setInterval(fetchData, 10000) // 每10秒刷新一次
    return () => clearInterval(interval)
  }, [historyLimit, historyOffset])

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
        <h2>对账状态</h2>
        <p>加载中...</p>
      </div>
    )
  }

  if (error) {
    return (
      <div className="reconciliation">
        <h2>对账状态</h2>
        <p style={{ color: 'red' }}>错误: {error}</p>
      </div>
    )
  }

  return (
    <div className="reconciliation">
      <h2>对账状态</h2>

      {status && (
        <div className="status-cards">
          <div className="status-card">
            <h3>对账次数</h3>
            <p className="value">{status.reconcile_count}</p>
          </div>
          <div className="status-card">
            <h3>最后对账时间</h3>
            <p className="value">{formatTime(status.last_reconcile_time)}</p>
          </div>
          <div className="status-card">
            <h3>本地持仓</h3>
            <p className="value">{status.local_position.toFixed(4)}</p>
          </div>
          <div className="status-card">
            <h3>累计买入</h3>
            <p className="value">{status.total_buy_qty.toFixed(2)}</p>
          </div>
          <div className="status-card">
            <h3>累计卖出</h3>
            <p className="value">{status.total_sell_qty.toFixed(2)}</p>
          </div>
          <div className="status-card">
            <h3>预计盈利</h3>
            <p className="value" style={{ color: status.estimated_profit >= 0 ? '#52c41a' : '#ff4d4f' }}>
              {status.estimated_profit.toFixed(2)} USDT
            </p>
          </div>
        </div>
      )}

      {/* 盈利曲线图表 */}
      {history.length > 0 && (
        <div style={{ marginTop: '32px' }}>
          <h3>盈利趋势</h3>
          <div style={{ width: '100%', height: '400px', background: '#fff', padding: '20px', borderRadius: '8px', boxShadow: '0 2px 8px rgba(0,0,0,0.1)' }}>
            <div style={{ width: '100%', height: '100%', position: 'relative' }}>
              <svg width="100%" height="100%" viewBox="0 0 800 350" preserveAspectRatio="xMidYMid meet" onMouseLeave={() => setTooltip(null)}>
                {/* 绘制网格线 */}
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
                
                {/* 绘制坐标轴 */}
                <line x1="60" y1="290" x2="780" y2="290" stroke="#333" strokeWidth="2" />
                <line x1="60" y1="50" x2="60" y2="290" stroke="#333" strokeWidth="2" />
                
                {/* 绘制曲线 */}
                {(() => {
                  const sortedHistory = [...history].reverse()
                  const maxProfit = Math.max(...sortedHistory.map(h => Math.max(h.estimated_profit, h.actual_profit)))
                  const minProfit = Math.min(...sortedHistory.map(h => Math.min(h.estimated_profit, h.actual_profit)))
                  
                  // 改进 Y 轴范围计算，确保负数能正确显示
                  // 如果最小值为负数，确保范围包含 0 或至少正确显示负数范围
                  let yMin = minProfit
                  let yMax = maxProfit
                  
                  // 如果所有值都是负数，确保 Y 轴范围能正确显示
                  if (minProfit < 0 && maxProfit < 0) {
                    // 全部为负数时，保持原范围，但添加适当的 padding
                    yMin = minProfit
                    yMax = maxProfit
                  } else if (minProfit < 0 && maxProfit >= 0) {
                    // 有正有负时，确保包含 0
                    yMin = minProfit
                    yMax = maxProfit
                  } else if (minProfit >= 0 && maxProfit >= 0) {
                    // 全部为正数时，保持原逻辑
                    yMin = minProfit
                    yMax = maxProfit
                  }
                  
                  const range = yMax - yMin || 1
                  const padding = Math.max(range * 0.1, Math.abs(yMin) * 0.1, Math.abs(yMax) * 0.1) || 1
                  
                  // 确保 padding 不会让范围变得不合理
                  const finalMin = yMin - padding
                  const finalMax = yMax + padding
                  const finalRange = finalMax - finalMin
                  
                  const getY = (value: number) => {
                    return 290 - ((value - finalMin) / finalRange) * 240
                  }
                  
                  const getX = (index: number) => {
                    return 60 + (index / Math.max(sortedHistory.length - 1, 1)) * 720
                  }
                  
                  // 预计盈利曲线
                  const estimatedPath = sortedHistory.map((item, i) => 
                    `${i === 0 ? 'M' : 'L'} ${getX(i)} ${getY(item.estimated_profit)}`
                  ).join(' ')
                  
                  // 实际盈利曲线
                  const actualPath = sortedHistory.map((item, i) => 
                    `${i === 0 ? 'M' : 'L'} ${getX(i)} ${getY(item.actual_profit)}`
                  ).join(' ')
                  
                  // 绘制 0 线（如果范围包含 0）
                  const zeroY = finalMin <= 0 && finalMax >= 0 ? getY(0) : null
                  
                  return (
                    <>
                      {/* 0 线 */}
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
                      
                      {/* 预计盈利曲线 */}
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
                      
                      {/* 实际盈利曲线 */}
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
                        <text x="35" y="5" fontSize="12" fill="#666" opacity={showEstimated ? 1 : 0.5}>预计盈利</text>
                      </g>
                      <g 
                        transform="translate(650, 35)" 
                        style={{ cursor: 'pointer' }}
                        onClick={() => setShowActual(!showActual)}
                      >
                        <line x1="0" y1="0" x2="30" y2="0" stroke="#52c41a" strokeWidth="2" opacity={showActual ? 1 : 0.3} />
                        <text x="35" y="5" fontSize="12" fill="#666" opacity={showActual ? 1 : 0.5}>实际盈利</text>
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
                        <span className="tooltip-label">预计盈利:</span>
                        <span className="tooltip-value" style={{ color: tooltip.item.estimated_profit >= 0 ? '#52c41a' : '#ff4d4f' }}>
                          {tooltip.item.estimated_profit.toFixed(2)} USDT
                        </span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">实际盈利:</span>
                        <span className="tooltip-value" style={{ color: tooltip.item.actual_profit >= 0 ? '#52c41a' : '#ff4d4f' }}>
                          {tooltip.item.actual_profit.toFixed(2)} USDT
                        </span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">本地持仓:</span>
                        <span className="tooltip-value">{tooltip.item.local_position.toFixed(4)}</span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">累计买入:</span>
                        <span className="tooltip-value">{tooltip.item.total_buy_qty.toFixed(2)}</span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">累计卖出:</span>
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

      {/* 仓位走势图 */}
      {history.length > 0 && (
        <div style={{ marginTop: '32px' }}>
          <h3>仓位走势</h3>
          <div style={{ width: '100%', height: '400px', background: '#fff', padding: '20px', borderRadius: '8px', boxShadow: '0 2px 8px rgba(0,0,0,0.1)' }}>
            <div style={{ width: '100%', height: '100%', position: 'relative' }}>
              <svg width="100%" height="100%" viewBox="0 0 800 350" preserveAspectRatio="xMidYMid meet" onMouseLeave={() => setPositionTooltip(null)}>
                {/* 绘制网格线 */}
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
                
                {/* 绘制坐标轴 */}
                <line x1="60" y1="290" x2="780" y2="290" stroke="#333" strokeWidth="2" />
                <line x1="60" y1="50" x2="60" y2="290" stroke="#333" strokeWidth="2" />
                
                {/* 绘制曲线 */}
                {(() => {
                  const sortedHistory = [...history].reverse()
                  const maxPosition = Math.max(...sortedHistory.map(h => Math.max(h.local_position, h.exchange_position)))
                  const minPosition = Math.min(...sortedHistory.map(h => Math.min(h.local_position, h.exchange_position)))
                  
                  // 计算 Y 轴范围
                  let yMin = minPosition
                  let yMax = maxPosition
                  
                  // 如果所有值都是0或很小，设置一个最小范围
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
                  
                  // 本地持仓曲线
                  const localPositionPath = sortedHistory.map((item, i) => 
                    `${i === 0 ? 'M' : 'L'} ${getX(i)} ${getY(item.local_position)}`
                  ).join(' ')
                  
                  // 交易所持仓曲线
                  const exchangePositionPath = sortedHistory.map((item, i) => 
                    `${i === 0 ? 'M' : 'L'} ${getX(i)} ${getY(item.exchange_position)}`
                  ).join(' ')
                  
                  return (
                    <>
                      {/* 本地持仓曲线 */}
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
                      
                      {/* 交易所持仓曲线 */}
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
                        <text x="35" y="5" fontSize="12" fill="#666" opacity={showLocalPosition ? 1 : 0.5}>本地持仓</text>
                      </g>
                      <g 
                        transform="translate(650, 35)" 
                        style={{ cursor: 'pointer' }}
                        onClick={() => setShowExchangePosition(!showExchangePosition)}
                      >
                        <line x1="0" y1="0" x2="30" y2="0" stroke="#52c41a" strokeWidth="2" opacity={showExchangePosition ? 1 : 0.3} />
                        <text x="35" y="5" fontSize="12" fill="#666" opacity={showExchangePosition ? 1 : 0.5}>交易所持仓</text>
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
                        <span className="tooltip-label">本地持仓:</span>
                        <span className="tooltip-value">{positionTooltip.item.local_position.toFixed(4)}</span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">交易所持仓:</span>
                        <span className="tooltip-value">{positionTooltip.item.exchange_position.toFixed(4)}</span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">持仓差异:</span>
                        <span className="tooltip-value" style={{ color: Math.abs(positionTooltip.item.position_diff) > 0.0001 ? '#ff4d4f' : '#52c41a' }}>
                          {positionTooltip.item.position_diff.toFixed(4)}
                        </span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">挂单买单:</span>
                        <span className="tooltip-value">{positionTooltip.item.active_buy_orders}</span>
                      </div>
                      <div className="tooltip-row">
                        <span className="tooltip-label">挂单卖单:</span>
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

      <div style={{ marginTop: '32px' }}>
        <h3>对账历史</h3>
        <div className="history-filters">
          <label>
            每页:
            <select value={historyLimit} onChange={(e) => setHistoryLimit(Number(e.target.value))}>
              <option value={20}>20</option>
              <option value={50}>50</option>
              <option value={100}>100</option>
            </select>
          </label>
          <button onClick={() => setHistoryOffset(prev => Math.max(0, prev - historyLimit))}>上一页</button>
          <span>页 {Math.floor(historyOffset / historyLimit) + 1}</span>
          <button onClick={() => setHistoryOffset(prev => prev + historyLimit)}>下一页</button>
        </div>

        {history.length === 0 ? (
          <p>暂无对账历史</p>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="history-table">
              <thead>
                <tr>
                  <th>对账时间</th>
                  <th>本地持仓</th>
                  <th>交易所持仓</th>
                  <th>差异</th>
                  <th>挂单买单</th>
                  <th>挂单卖单</th>
                  <th>待卖数量</th>
                  <th>累计买入</th>
                  <th>累计卖出</th>
                  <th>预计盈利</th>
                  <th>实际盈利</th>
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
    </div>
  )
}

export default Reconciliation

