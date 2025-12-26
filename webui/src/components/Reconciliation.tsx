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

const Reconciliation: React.FC = () => {
  const [status, setStatus] = useState<ReconciliationStatus | null>(null)
  const [history, setHistory] = useState<ReconciliationHistoryItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [historyLimit, setHistoryLimit] = useState(50)
  const [historyOffset, setHistoryOffset] = useState(0)

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
              <svg width="100%" height="100%" viewBox="0 0 800 350" preserveAspectRatio="xMidYMid meet">
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
                  const range = maxProfit - minProfit || 1
                  const padding = range * 0.1
                  
                  const getY = (value: number) => {
                    return 290 - ((value - minProfit + padding) / (range + 2 * padding)) * 240
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
                  
                  return (
                    <>
                      {/* 预计盈利曲线 */}
                      <path d={estimatedPath} fill="none" stroke="#1890ff" strokeWidth="2" />
                      {sortedHistory.map((item, i) => (
                        <circle key={`est-${i}`} cx={getX(i)} cy={getY(item.estimated_profit)} r="4" fill="#1890ff" />
                      ))}
                      
                      {/* 实际盈利曲线 */}
                      <path d={actualPath} fill="none" stroke="#52c41a" strokeWidth="2" />
                      {sortedHistory.map((item, i) => (
                        <circle key={`act-${i}`} cx={getX(i)} cy={getY(item.actual_profit)} r="4" fill="#52c41a" />
                      ))}
                      
                      {/* Y轴刻度 */}
                      {[0, 1, 2, 3, 4].map(i => {
                        const value = minProfit - padding + (range + 2 * padding) * (4 - i) / 4
                        return (
                          <text key={`y-${i}`} x="50" y={50 + i * 60 + 5} textAnchor="end" fontSize="12" fill="#666">
                            {value.toFixed(2)}
                          </text>
                        )
                      })}
                      
                      {/* 图例 */}
                      <g transform="translate(650, 20)">
                        <line x1="0" y1="0" x2="30" y2="0" stroke="#1890ff" strokeWidth="2" />
                        <text x="35" y="5" fontSize="12" fill="#666">预计盈利</text>
                      </g>
                      <g transform="translate(650, 35)">
                        <line x1="0" y1="0" x2="30" y2="0" stroke="#52c41a" strokeWidth="2" />
                        <text x="35" y="5" fontSize="12" fill="#666">实际盈利</text>
                      </g>
                    </>
                  )
                })()}
              </svg>
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

