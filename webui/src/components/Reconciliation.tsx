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

