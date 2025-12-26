import React, { useEffect, useState } from 'react'
import { getStatistics, getDailyStatistics } from '../services/api'

interface StatisticsData {
  total_trades: number
  total_volume: number
  total_pnl: number
  win_rate: number
}

interface DailyStatistics {
  date: string
  total_trades: number
  total_volume: number
  total_pnl: number
  win_rate: number
}

interface PnLBySymbol {
  symbol: string
  total_pnl: number
  total_trades: number
  total_volume: number
  win_rate: number
}

const Statistics: React.FC = () => {
  const [stats, setStats] = useState<StatisticsData | null>(null)
  const [dailyStats, setDailyStats] = useState<DailyStatistics[]>([])
  const [pnlByTimeRange, setPnlByTimeRange] = useState<PnLBySymbol[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [days, setDays] = useState(30)
  const [startDate, setStartDate] = useState<string>(new Date(Date.now() - 30 * 24 * 60 * 60 * 1000).toISOString().split('T')[0])
  const [endDate, setEndDate] = useState<string>(new Date().toISOString().split('T')[0])

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true)
        const [statsData, dailyData] = await Promise.all([
          getStatistics(),
          getDailyStatistics(days).catch(() => ({ statistics: [] })),
        ])
        setStats(statsData)
        setDailyStats(dailyData.statistics || [])
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch statistics')
        console.error('Failed to fetch statistics:', err)
      } finally {
        setLoading(false)
      }
    }

    fetchData()
    // 每30秒刷新一次
    const interval = setInterval(fetchData, 30000)

    return () => clearInterval(interval)
  }, [days])

  // 获取按时间区间的盈亏数据
  useEffect(() => {
    const fetchPnLByTimeRange = async () => {
      try {
        const params = new URLSearchParams({
          start_time: new Date(startDate).toISOString(),
          end_time: new Date(endDate + 'T23:59:59').toISOString(),
        })
        const response = await fetch(`/api/statistics/pnl/time-range?${params}`, {
          credentials: 'include',
        })
        if (!response.ok) throw new Error('获取盈亏数据失败')
        const data = await response.json()
        setPnlByTimeRange(data.pnl_by_symbol || [])
      } catch (err) {
        console.error('Failed to fetch PnL by time range:', err)
      }
    }

    fetchPnLByTimeRange()
  }, [startDate, endDate])

  if (loading && !stats) {
    return (
      <div className="statistics">
        <h2>交易统计</h2>
        <p>加载中...</p>
      </div>
    )
  }

  if (error) {
    return (
      <div className="statistics">
        <h2>交易统计</h2>
        <p style={{ color: 'red' }}>错误: {error}</p>
      </div>
    )
  }

  return (
    <div className="statistics">
      <h2>交易统计</h2>

      {/* 关键指标卡片 */}
      {stats && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '16px', marginTop: '16px' }}>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>总交易数</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{stats.total_trades}</div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>总交易量</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{stats.total_volume.toFixed(4)}</div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>总盈亏</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold', color: stats.total_pnl >= 0 ? '#52c41a' : '#ff4d4f' }}>
              {stats.total_pnl >= 0 ? '+' : ''}{stats.total_pnl.toFixed(2)}
            </div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>胜率</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{(stats.win_rate * 100).toFixed(2)}%</div>
          </div>
        </div>
      )}

      {/* 每日统计 */}
      <div style={{ marginTop: '32px' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
          <h3>每日统计</h3>
          <select value={days} onChange={(e) => setDays(Number(e.target.value))} style={{ padding: '8px' }}>
            <option value={7}>最近7天</option>
            <option value={30}>最近30天</option>
            <option value={90}>最近90天</option>
          </select>
        </div>
        {dailyStats.length > 0 ? (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ borderBottom: '2px solid #e8e8e8' }}>
                  <th style={{ padding: '12px', textAlign: 'left' }}>日期</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>交易数</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>交易量</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>盈亏</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>胜率</th>
                </tr>
              </thead>
              <tbody>
                {dailyStats.map((stat, index) => (
                  <tr key={index} style={{ borderBottom: '1px solid #f0f0f0' }}>
                    <td style={{ padding: '12px' }}>{new Date(stat.date).toLocaleDateString('zh-CN')}</td>
                    <td style={{ padding: '12px', textAlign: 'right' }}>{stat.total_trades}</td>
                    <td style={{ padding: '12px', textAlign: 'right' }}>{stat.total_volume.toFixed(4)}</td>
                    <td style={{ padding: '12px', textAlign: 'right', color: stat.total_pnl >= 0 ? '#52c41a' : '#ff4d4f' }}>
                      {stat.total_pnl >= 0 ? '+' : ''}{stat.total_pnl.toFixed(2)}
                    </td>
                    <td style={{ padding: '12px', textAlign: 'right' }}>{(stat.win_rate * 100).toFixed(2)}%</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div style={{ padding: '32px', textAlign: 'center', color: '#8c8c8c' }}>暂无统计数据</div>
        )}
      </div>

      {/* 按时间区间查询盈亏 */}
      <div style={{ marginTop: '32px' }}>
        <h3>按时间区间查询盈亏</h3>
        <div style={{ display: 'flex', gap: '12px', alignItems: 'center', marginBottom: '16px' }}>
          <label>
            开始日期:
            <input
              type="date"
              value={startDate}
              onChange={(e) => setStartDate(e.target.value)}
              style={{ marginLeft: '8px', padding: '6px' }}
            />
          </label>
          <label>
            结束日期:
            <input
              type="date"
              value={endDate}
              onChange={(e) => setEndDate(e.target.value)}
              style={{ marginLeft: '8px', padding: '6px' }}
            />
          </label>
        </div>

        {pnlByTimeRange.length > 0 ? (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ borderBottom: '2px solid #e8e8e8' }}>
                  <th style={{ padding: '12px', textAlign: 'left' }}>币种对</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>交易数</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>交易量</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>盈亏</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>胜率</th>
                </tr>
              </thead>
              <tbody>
                {pnlByTimeRange.map((item, index) => (
                  <tr key={index} style={{ borderBottom: '1px solid #f0f0f0' }}>
                    <td style={{ padding: '12px' }}>{item.symbol}</td>
                    <td style={{ padding: '12px', textAlign: 'right' }}>{item.total_trades}</td>
                    <td style={{ padding: '12px', textAlign: 'right' }}>{item.total_volume.toFixed(4)}</td>
                    <td style={{ padding: '12px', textAlign: 'right', color: item.total_pnl >= 0 ? '#52c41a' : '#ff4d4f' }}>
                      {item.total_pnl >= 0 ? '+' : ''}{item.total_pnl.toFixed(2)}
                    </td>
                    <td style={{ padding: '12px', textAlign: 'right' }}>{(item.win_rate * 100).toFixed(2)}%</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div style={{ padding: '32px', textAlign: 'center', color: '#8c8c8c' }}>该时间段暂无交易数据</div>
        )}
      </div>
    </div>
  )
}

export default Statistics
