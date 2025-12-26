import React, { useEffect, useState } from 'react'
import { getStrategyAllocation, StrategyCapitalInfo } from '../services/api'

const StrategyAllocation: React.FC = () => {
  const [allocation, setAllocation] = useState<Record<string, StrategyCapitalInfo>>({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const fetchAllocation = async () => {
      try {
        setLoading(true)
        const data = await getStrategyAllocation()
        setAllocation(data.allocation)
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch strategy allocation')
        console.error('Failed to fetch strategy allocation:', err)
      } finally {
        setLoading(false)
      }
    }

    fetchAllocation()
    // 每5秒刷新一次
    const interval = setInterval(fetchAllocation, 5000)

    return () => clearInterval(interval)
  }, [])

  const totalAllocated = Object.values(allocation).reduce((sum, cap) => sum + cap.allocated, 0)
  const totalUsed = Object.values(allocation).reduce((sum, cap) => sum + cap.used, 0)
  const totalAvailable = Object.values(allocation).reduce((sum, cap) => sum + cap.available, 0)

  if (loading && Object.keys(allocation).length === 0) {
    return (
      <div className="strategy-allocation">
        <h2>策略资金分配</h2>
        <p>加载中...</p>
      </div>
    )
  }

  if (error) {
    return (
      <div className="strategy-allocation">
        <h2>策略资金分配</h2>
        <p style={{ color: 'red' }}>错误: {error}</p>
      </div>
    )
  }

  const strategies = Object.entries(allocation)

  return (
    <div className="strategy-allocation">
      <h2>策略资金分配</h2>
      
      {strategies.length === 0 ? (
        <p>暂无策略配置</p>
      ) : (
        <>
          {/* 总览卡片 */}
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '16px', marginBottom: '24px' }}>
            <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
              <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>总分配资金</div>
              <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{totalAllocated.toFixed(2)} USDT</div>
            </div>
            <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
              <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>已使用</div>
              <div style={{ fontSize: '24px', fontWeight: 'bold', color: '#ff4d4f' }}>{totalUsed.toFixed(2)} USDT</div>
            </div>
            <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
              <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>可用资金</div>
              <div style={{ fontSize: '24px', fontWeight: 'bold', color: '#52c41a' }}>{totalAvailable.toFixed(2)} USDT</div>
            </div>
          </div>

          {/* 策略列表 */}
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ borderBottom: '2px solid #e8e8e8' }}>
                  <th style={{ padding: '12px', textAlign: 'left' }}>策略名称</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>分配资金</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>已使用</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>可用资金</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>权重</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>固定资金池</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>使用率</th>
                </tr>
              </thead>
              <tbody>
                {strategies.map(([name, cap]) => {
                  const usageRate = cap.allocated > 0 ? (cap.used / cap.allocated) * 100 : 0
                  return (
                    <tr key={name} style={{ borderBottom: '1px solid #f0f0f0' }}>
                      <td style={{ padding: '12px', fontWeight: 'bold' }}>{name}</td>
                      <td style={{ padding: '12px', textAlign: 'right' }}>{cap.allocated.toFixed(2)} USDT</td>
                      <td style={{ padding: '12px', textAlign: 'right', color: '#ff4d4f' }}>
                        {cap.used.toFixed(2)} USDT
                      </td>
                      <td style={{ padding: '12px', textAlign: 'right', color: '#52c41a' }}>
                        {cap.available.toFixed(2)} USDT
                      </td>
                      <td style={{ padding: '12px', textAlign: 'right' }}>{(cap.weight * 100).toFixed(2)}%</td>
                      <td style={{ padding: '12px', textAlign: 'right' }}>
                        {cap.fixed_pool > 0 ? `${cap.fixed_pool.toFixed(2)} USDT` : '-'}
                      </td>
                      <td style={{ padding: '12px', textAlign: 'right' }}>
                        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '8px' }}>
                          <div style={{ width: '100px', height: '8px', backgroundColor: '#f0f0f0', borderRadius: '4px', overflow: 'hidden' }}>
                            <div
                              style={{
                                width: `${Math.min(usageRate, 100)}%`,
                                height: '100%',
                                backgroundColor: usageRate > 80 ? '#ff4d4f' : usageRate > 50 ? '#faad14' : '#52c41a',
                                transition: 'width 0.3s',
                              }}
                            />
                          </div>
                          <span>{usageRate.toFixed(1)}%</span>
                        </div>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>

          {/* 饼图展示（简单版本） */}
          {totalAllocated > 0 && (
            <div style={{ marginTop: '32px' }}>
              <h3>资金分配比例</h3>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: '16px', marginTop: '16px' }}>
                {strategies.map(([name, cap]) => {
                  const percentage = (cap.allocated / totalAllocated) * 100
                  const colors = ['#1890ff', '#52c41a', '#faad14', '#ff4d4f', '#722ed1', '#13c2c2']
                  const color = colors[strategies.indexOf([name, cap]) % colors.length]
                  return (
                    <div key={name} style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                      <div
                        style={{
                          width: '16px',
                          height: '16px',
                          backgroundColor: color,
                          borderRadius: '2px',
                        }}
                      />
                      <span>{name}: {percentage.toFixed(1)}%</span>
                    </div>
                  )
                })}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}

export default StrategyAllocation

