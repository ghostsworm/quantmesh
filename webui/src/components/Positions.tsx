import React, { useEffect, useState } from 'react'
import { getPositions, getPositionsSummary } from '../services/api'

interface PositionInfo {
  price: number
  quantity: number
  value: number
  unrealized_pnl: number
}

interface PositionSummary {
  total_quantity: number
  total_value: number
  position_count: number
  average_price: number
  current_price: number
  unrealized_pnl: number
  pnl_percentage: number
}

interface PositionsResponse {
  summary: {
    total_quantity: number
    total_value: number
    position_count: number
    average_price: number
    current_price: number
    unrealized_pnl: number
    pnl_percentage: number
    positions: PositionInfo[]
  }
}

const Positions: React.FC = () => {
  const [summary, setSummary] = useState<PositionSummary | null>(null)
  const [positions, setPositions] = useState<PositionInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true)
        const data = await getPositions()
        setSummary(data.summary)
        setPositions(data.summary.positions || [])
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch positions')
        console.error('Failed to fetch positions:', err)
      } finally {
        setLoading(false)
      }
    }

    fetchData()
    // 每5秒刷新一次
    const interval = setInterval(fetchData, 5000)

    return () => clearInterval(interval)
  }, [])

  if (loading && !summary) {
    return (
      <div className="positions">
        <h2>持仓汇总</h2>
        <p>加载中...</p>
      </div>
    )
  }

  if (error) {
    return (
      <div className="positions">
        <h2>持仓汇总</h2>
        <p style={{ color: 'red' }}>错误: {error}</p>
      </div>
    )
  }

  return (
    <div className="positions">
      <h2>持仓汇总</h2>

      {/* 持仓汇总卡片 */}
      {summary && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '16px', marginTop: '16px' }}>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>总持仓数量</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{summary.total_quantity.toFixed(4)}</div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>总持仓价值</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{summary.total_value.toFixed(2)}</div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>持仓槽位数</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{summary.position_count}</div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>平均持仓价格</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{summary.average_price.toFixed(2)}</div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>当前市场价格</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{summary.current_price.toFixed(2)}</div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>未实现盈亏</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold', color: summary.unrealized_pnl >= 0 ? '#52c41a' : '#ff4d4f' }}>
              {summary.unrealized_pnl >= 0 ? '+' : ''}{summary.unrealized_pnl.toFixed(2)}
            </div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>亏损率</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold', color: (summary.pnl_percentage || 0) >= 0 ? '#52c41a' : '#ff4d4f' }}>
              {(summary.pnl_percentage || 0) >= 0 ? '+' : ''}{(summary.pnl_percentage || 0).toFixed(2)}%
            </div>
          </div>
        </div>
      )}

      {/* 持仓列表表格 */}
      {positions.length > 0 && (
        <div style={{ marginTop: '32px' }}>
          <h3>持仓列表</h3>
          <div style={{ overflowX: 'auto', marginTop: '16px' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ borderBottom: '2px solid #e8e8e8' }}>
                  <th style={{ padding: '12px', textAlign: 'left' }}>持仓价格</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>持仓数量</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>持仓价值</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>未实现盈亏</th>
                </tr>
              </thead>
              <tbody>
                {positions.map((pos, index) => (
                  <tr key={index} style={{ borderBottom: '1px solid #f0f0f0' }}>
                    <td style={{ padding: '12px' }}>{pos.price.toFixed(2)}</td>
                    <td style={{ padding: '12px', textAlign: 'right' }}>{pos.quantity.toFixed(4)}</td>
                    <td style={{ padding: '12px', textAlign: 'right' }}>{pos.value.toFixed(2)}</td>
                    <td style={{ padding: '12px', textAlign: 'right', color: pos.unrealized_pnl >= 0 ? '#52c41a' : '#ff4d4f' }}>
                      {pos.unrealized_pnl >= 0 ? '+' : ''}{pos.unrealized_pnl.toFixed(2)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {positions.length === 0 && summary && summary.position_count === 0 && (
        <div style={{ marginTop: '32px', padding: '32px', textAlign: 'center', color: '#8c8c8c' }}>
          暂无持仓
        </div>
      )}
    </div>
  )
}

export default Positions

