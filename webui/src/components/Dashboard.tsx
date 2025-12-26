import React, { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getStatus, startTrading, stopTrading } from '../services/api'
import { getSlots, SlotsResponse } from '../services/api'
import { getStrategyAllocation, StrategyAllocationResponse } from '../services/api'
import { getPendingOrders, PendingOrdersResponse } from '../services/api'
import { getPositionsSummary } from '../services/api'

interface SystemStatus {
  running: boolean
  exchange: string
  symbol: string
  current_price: number
  total_pnl: number
  total_trades: number
  risk_triggered: boolean
  uptime: number
}

const Dashboard: React.FC = () => {
  const [status, setStatus] = useState<SystemStatus | null>(null)
  const [slotsInfo, setSlotsInfo] = useState<SlotsResponse | null>(null)
  const [strategyAllocation, setStrategyAllocation] = useState<StrategyAllocationResponse | null>(null)
  const [pendingOrders, setPendingOrders] = useState<PendingOrdersResponse | null>(null)
  const [positionsSummary, setPositionsSummary] = useState<any>(null)
  const [isTrading, setIsTrading] = useState(false)

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [statusData, slotsData, allocationData, ordersData, positionsData] = await Promise.all([
          getStatus(),
          getSlots().catch(() => null),
          getStrategyAllocation().catch(() => null),
          getPendingOrders().catch(() => null),
          getPositionsSummary().catch(() => null),
        ])
        setStatus(statusData)
        setSlotsInfo(slotsData)
        setStrategyAllocation(allocationData)
        setPendingOrders(ordersData)
        setPositionsSummary(positionsData)
        setIsTrading(statusData?.running || false)
      } catch (error) {
        console.error('Failed to fetch data:', error)
      }
    }

    fetchData()
    const interval = setInterval(fetchData, 5000) // 每5秒更新一次

    return () => clearInterval(interval)
  }, [])

  const handleStartTrading = async () => {
    try {
      await startTrading()
      setIsTrading(true)
    } catch (error) {
      console.error('Failed to start trading:', error)
      alert('启动交易失败: ' + (error instanceof Error ? error.message : 'Unknown error'))
    }
  }

  const handleStopTrading = async () => {
    try {
      await stopTrading()
      setIsTrading(false)
    } catch (error) {
      console.error('Failed to stop trading:', error)
      alert('停止交易失败: ' + (error instanceof Error ? error.message : 'Unknown error'))
    }
  }

  if (!status) {
    return <div>加载中...</div>
  }

  const formatUptime = (seconds: number) => {
    const days = Math.floor(seconds / 86400)
    const hours = Math.floor((seconds % 86400) / 3600)
    const minutes = Math.floor((seconds % 3600) / 60)
    if (days > 0) return `${days}天 ${hours}小时 ${minutes}分钟`
    if (hours > 0) return `${hours}小时 ${minutes}分钟`
    return `${minutes}分钟`
  }

  return (
    <div className="dashboard">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
        <h2>系统状态</h2>
        <div style={{ display: 'flex', gap: '8px' }}>
          {isTrading ? (
            <button
              onClick={handleStopTrading}
              style={{
                padding: '8px 16px',
                background: '#ff4d4f',
                color: 'white',
                border: 'none',
                borderRadius: '4px',
                cursor: 'pointer',
              }}
            >
              停止交易
            </button>
          ) : (
            <button
              onClick={handleStartTrading}
              style={{
                padding: '8px 16px',
                background: '#52c41a',
                color: 'white',
                border: 'none',
                borderRadius: '4px',
                cursor: 'pointer',
              }}
            >
              启动交易
            </button>
          )}
        </div>
      </div>
      <div className="status-grid">
        <div className="status-item">
          <label>运行状态:</label>
          <span className={status.running ? 'running' : 'stopped'}>
            {status.running ? '运行中' : '已停止'}
          </span>
        </div>
        <div className="status-item">
          <label>交易所:</label>
          <span>{status.exchange}</span>
        </div>
        <div className="status-item">
          <label>交易对:</label>
          <span>{status.symbol}</span>
        </div>
        <div className="status-item">
          <label>当前价格:</label>
          <span>{status.current_price.toFixed(2)}</span>
        </div>
        <div className="status-item">
          <label>总盈亏:</label>
          <span className={status.total_pnl >= 0 ? 'profit' : 'loss'}>
            {status.total_pnl.toFixed(2)}
          </span>
        </div>
        <div className="status-item">
          <label>总交易数:</label>
          <span>{status.total_trades}</span>
        </div>
        <div className="status-item">
          <label>风控状态:</label>
          <span className={status.risk_triggered ? 'risk-triggered' : 'normal'}>
            {status.risk_triggered ? '已触发' : '正常'}
          </span>
        </div>
        <div className="status-item">
          <label>运行时间:</label>
          <span>{formatUptime(status.uptime)}</span>
        </div>
      </div>

      {/* 槽位统计卡片 */}
      {slotsInfo && (
        <div style={{ marginTop: '32px' }}>
          <h3>槽位统计</h3>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '16px', marginTop: '16px' }}>
            <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
              <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>总槽位数</div>
              <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{slotsInfo.count}</div>
              <Link to="/slots" style={{ fontSize: '12px', color: '#1890ff', marginTop: '8px', display: 'block' }}>
                查看详情 →
              </Link>
            </div>
            <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
              <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>有仓槽位</div>
              <div style={{ fontSize: '24px', fontWeight: 'bold', color: '#52c41a' }}>
                {slotsInfo.slots.filter(s => s.position_status === 'FILLED').length}
              </div>
            </div>
            <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
              <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>空仓槽位</div>
              <div style={{ fontSize: '24px', fontWeight: 'bold', color: '#8c8c8c' }}>
                {slotsInfo.slots.filter(s => s.position_status === 'EMPTY').length}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* 策略配比概览 */}
      {strategyAllocation && Object.keys(strategyAllocation.allocation).length > 0 && (
        <div style={{ marginTop: '32px' }}>
          <h3>策略资金配比</h3>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '16px', marginTop: '16px' }}>
            {Object.entries(strategyAllocation.allocation).map(([name, cap]) => (
              <div key={name} style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
                <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>{name}</div>
                <div style={{ fontSize: '20px', fontWeight: 'bold' }}>{cap.allocated.toFixed(2)} USDT</div>
                <div style={{ fontSize: '12px', color: '#8c8c8c', marginTop: '4px' }}>
                  权重: {(cap.weight * 100).toFixed(1)}%
                </div>
                <div style={{ fontSize: '12px', color: '#52c41a', marginTop: '4px' }}>
                  可用: {cap.available.toFixed(2)} USDT
                </div>
              </div>
            ))}
          </div>
          <Link to="/strategies" style={{ fontSize: '14px', color: '#1890ff', marginTop: '16px', display: 'inline-block' }}>
            查看详细配比 →
          </Link>
        </div>
      )}

      {/* 持仓汇总卡片 */}
      {positionsSummary && positionsSummary.position_count > 0 && (
        <div style={{ marginTop: '32px' }}>
          <h3>持仓概览</h3>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '16px', marginTop: '16px' }}>
            <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
              <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>总持仓数量</div>
              <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{positionsSummary.total_quantity?.toFixed(4) || '0'}</div>
            </div>
            <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
              <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>总持仓价值</div>
              <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{positionsSummary.total_value?.toFixed(2) || '0'}</div>
            </div>
            <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
              <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>未实现盈亏</div>
              <div style={{ fontSize: '24px', fontWeight: 'bold', color: (positionsSummary.unrealized_pnl || 0) >= 0 ? '#52c41a' : '#ff4d4f' }}>
                {(positionsSummary.unrealized_pnl || 0) >= 0 ? '+' : ''}{positionsSummary.unrealized_pnl?.toFixed(2) || '0'}
              </div>
            </div>
          </div>
          <Link to="/positions" style={{ fontSize: '14px', color: '#1890ff', marginTop: '16px', display: 'inline-block' }}>
            查看详细持仓 →
          </Link>
        </div>
      )}

      {/* 待成交订单提示 */}
      {pendingOrders && pendingOrders.count > 0 && (
        <div style={{ marginTop: '32px' }}>
          <h3>待成交订单</h3>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px', marginTop: '16px' }}>
            <div style={{ fontSize: '18px', fontWeight: 'bold', marginBottom: '8px' }}>
              当前有 {pendingOrders.count} 个待成交订单
            </div>
            <Link to="/orders" style={{ fontSize: '14px', color: '#1890ff' }}>
              查看详情 →
            </Link>
          </div>
        </div>
      )}
    </div>
  )
}

export default Dashboard

